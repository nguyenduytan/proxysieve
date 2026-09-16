package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

var (
	ErrDisabled    = errors.New("proxy source is disabled")
	ErrUnsupported = errors.New("proxy source type is not supported")
	ErrConfig      = errors.New("proxy source configuration is invalid")
	ErrNoEndpoints = errors.New("proxy source has no valid endpoints")
	ErrInProgress  = errors.New("proxy source refresh is already in progress")
	ErrRead        = errors.New("proxy source file could not be read safely")
)

// Refresher prevents overlapping network refreshes for the same source while
// allowing independent sources to refresh concurrently.
type Refresher struct {
	mu         sync.Mutex
	active     map[model.ID]struct{}
	importRoot string
}

func NewRefresher(importRoot string) *Refresher {
	return &Refresher{active: make(map[model.ID]struct{}), importRoot: importRoot}
}

func (r *Refresher) Validate(source proxy.Source) error {
	if source.Type != proxy.APISource && source.Type != proxy.FileSource {
		return nil
	}
	if _, valid := sourceImportOptions(source); !valid {
		return ErrConfig
	}
	allowed := map[string]bool{
		"format": true, "items_field": true, "endpoint_field": true,
		"protocol_field": true, "host_field": true, "port_field": true,
	}
	switch source.Type {
	case proxy.APISource:
		allowed["url"] = true
		if _, valid := refreshURL(source); !valid {
			return ErrConfig
		}
	case proxy.FileSource:
		allowed["path"] = true
		name := source.Config["path"]
		clean := filepath.Clean(name)
		if r == nil || r.importRoot == "" || name == "" || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.ContainsRune(name, 0) {
			return ErrConfig
		}
	}
	for key := range source.Config {
		if !allowed[key] {
			return ErrConfig
		}
	}
	return nil
}

type RefreshRequest struct {
	ID       model.ID
	Revision int64
	Store    store.InventoryStore
	Resolver Resolver
	Policy   security.DestinationPolicy
	Now      time.Time
}

type RefreshResult struct {
	Source          store.SourceRecord `json:"source"`
	Created         int                `json:"created"`
	Updated         int                `json:"updated"`
	Skipped         int                `json:"skipped"`
	Invalid         int                `json:"invalid"`
	FailureRecorded bool               `json:"-"`
}

// Refresh reads and parses outside storage, then atomically reconciles endpoints
// and source status. Failed reads/parses never mutate endpoints.
func (r *Refresher) Refresh(ctx context.Context, request RefreshRequest) (RefreshResult, error) {
	if !request.ID.Valid() || request.Revision < 1 || request.Store == nil || request.Now.IsZero() {
		return RefreshResult{}, store.ErrInvalid
	}
	if r == nil {
		return RefreshResult{}, store.ErrInvalid
	}
	if !r.begin(request.ID) {
		return RefreshResult{}, ErrInProgress
	}
	defer r.end(request.ID)
	return r.refresh(ctx, request)
}

func (r *Refresher) refresh(ctx context.Context, request RefreshRequest) (RefreshResult, error) {
	current, err := request.Store.GetSource(ctx, request.ID)
	if err != nil {
		return RefreshResult{}, err
	}
	if current.Revision != request.Revision {
		return RefreshResult{}, store.ErrConflict
	}
	if !current.Source.Enabled {
		return RefreshResult{}, ErrDisabled
	}
	if err = r.Validate(current.Source); err != nil {
		return refreshFailure(ctx, request, current, "failed: invalid configuration", ErrConfig)
	}
	options, valid := sourceImportOptions(current.Source)
	if !valid {
		return refreshFailure(ctx, request, current, "failed: invalid configuration", ErrConfig)
	}
	var body []byte
	switch current.Source.Type {
	case proxy.APISource:
		rawURL, urlOK := refreshURL(current.Source)
		if !urlOK || request.Resolver == nil {
			return refreshFailure(ctx, request, current, "failed: invalid configuration", ErrConfig)
		}
		body, err = FetchHTTP(ctx, rawURL, request.Resolver, request.Policy)
		if err != nil {
			return refreshFailure(ctx, request, current, "failed: fetch", ErrFetch)
		}
	case proxy.FileSource:
		body, err = readImportFile(ctx, r.importRoot, current.Source.Config["path"])
		if err != nil {
			return refreshFailure(ctx, request, current, "failed: read", ErrRead)
		}
	default:
		return RefreshResult{}, ErrUnsupported
	}
	preview, err := proxy.PreviewWithOptions(string(body), 100_000, options)
	if err != nil || preview.Valid == 0 {
		return refreshFailure(ctx, request, current, "failed: no valid endpoints", ErrNoEndpoints)
	}
	imported, err := proxy.Import(proxy.ImportRequest{Input: string(body), Mode: proxy.SkipDuplicates, Options: options})
	if err != nil {
		return refreshFailure(ctx, request, current, "failed: parse", ErrNoEndpoints)
	}

	result := RefreshResult{Skipped: imported.Skipped, Invalid: preview.Invalid}
	err = request.Store.WithinInventoryTransaction(ctx, func(endpoints store.Endpoints, sources store.Sources) error {
		latest, txErr := sources.GetSource(ctx, request.ID)
		if txErr != nil {
			return txErr
		}
		if latest.Revision != request.Revision {
			return store.ErrConflict
		}
		existing, txErr := endpointRecordsByIdentity(ctx, endpoints)
		if txErr != nil {
			return txErr
		}
		for _, endpoint := range imported.Endpoints {
			identity := proxy.Identity(endpoint)
			if stored, exists := existing[identity]; exists {
				if stored.Endpoint.SourceID == request.ID {
					result.Skipped++
					continue
				}
				candidate := stored.Endpoint.Clone()
				candidate.SourceID = request.ID
				if _, txErr = endpoints.Put(ctx, candidate, stored.Revision); txErr != nil {
					return txErr
				}
				result.Updated++
				continue
			}
			endpoint.SourceID = request.ID
			if _, txErr = endpoints.Put(ctx, endpoint, 0); txErr != nil {
				return txErr
			}
			result.Created++
		}
		latest.Source.LastRefreshAt = request.Now.UTC()
		latest.Source.LastRefreshStatus = fmt.Sprintf("ok: created=%d updated=%d skipped=%d invalid=%d", result.Created, result.Updated, result.Skipped, result.Invalid)
		result.Source, txErr = sources.PutSource(ctx, latest.Source, latest.Revision)
		return txErr
	})
	if err != nil {
		return RefreshResult{}, err
	}
	return result, nil
}

func sourceImportOptions(source proxy.Source) (proxy.ImportOptions, bool) {
	format := proxy.ImportFormat(source.Config["format"])
	if format == "" {
		format = proxy.ImportText
	}
	if format != proxy.ImportText && format != proxy.ImportCSV && format != proxy.ImportJSON {
		return proxy.ImportOptions{}, false
	}
	options := proxy.ImportOptions{Format: format, Mapping: proxy.ImportMapping{
		ItemsField: source.Config["items_field"], EndpointField: source.Config["endpoint_field"],
		ProtocolField: source.Config["protocol_field"], HostField: source.Config["host_field"], PortField: source.Config["port_field"],
	}}
	for _, value := range []string{options.Mapping.ItemsField, options.Mapping.EndpointField, options.Mapping.ProtocolField, options.Mapping.HostField, options.Mapping.PortField} {
		if len(value) > 256 || strings.ContainsRune(value, 0) {
			return proxy.ImportOptions{}, false
		}
	}
	return options, true
}

func readImportFile(ctx context.Context, root, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil || root == "" || name == "" || filepath.IsAbs(name) || strings.ContainsRune(name, 0) {
		return nil, ErrRead
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, ErrRead
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.Clean(name)))
	if err != nil || !withinRoot(root, target) {
		return nil, ErrRead
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, ErrRead
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBodyBytes {
		return nil, ErrRead
	}
	body, err := io.ReadAll(io.LimitReader(file, MaxBodyBytes+1))
	if err != nil || len(body) > MaxBodyBytes || ctx.Err() != nil {
		return nil, ErrRead
	}
	return body, nil
}

func withinRoot(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (r *Refresher) begin(id model.ID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.active[id]; exists {
		return false
	}
	r.active[id] = struct{}{}
	return true
}

func (r *Refresher) end(id model.ID) {
	r.mu.Lock()
	delete(r.active, id)
	r.mu.Unlock()
}

func refreshFailure(ctx context.Context, request RefreshRequest, current store.SourceRecord, status string, cause error) (RefreshResult, error) {
	current.Source.LastRefreshAt = request.Now.UTC()
	current.Source.LastRefreshStatus = status
	updated, err := request.Store.PutSource(ctx, current.Source, current.Revision)
	if errors.Is(err, store.ErrConflict) {
		return RefreshResult{}, err
	}
	return RefreshResult{Source: updated, FailureRecorded: err == nil}, cause
}

func refreshURL(source proxy.Source) (string, bool) {
	raw := source.Config["url"]
	target, err := url.Parse(raw)
	return raw, err == nil && (target.Scheme == "http" || target.Scheme == "https") && target.Hostname() != "" && target.User == nil
}

func endpointRecordsByIdentity(ctx context.Context, endpoints store.Endpoints) (map[string]store.EndpointRecord, error) {
	const pageSize = 1000
	records := make(map[string]store.EndpointRecord)
	after := model.ID("")
	for {
		page, err := endpoints.List(ctx, store.Page{After: after, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		for _, record := range page {
			records[proxy.Identity(record.Endpoint)] = record
		}
		if len(page) < pageSize {
			return records, nil
		}
		after = page[len(page)-1].Endpoint.ID
	}
}
