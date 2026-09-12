package source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
)

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

// RefreshHTTP fetches and parses outside storage, then atomically reconciles
// endpoints and source status. Failed fetches/parses never mutate endpoints.
func RefreshHTTP(ctx context.Context, request RefreshRequest) (RefreshResult, error) {
	if !request.ID.Valid() || request.Revision < 1 || request.Store == nil || request.Resolver == nil || request.Now.IsZero() {
		return RefreshResult{}, store.ErrInvalid
	}
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
	if current.Source.Type != proxy.APISource {
		return RefreshResult{}, ErrUnsupported
	}
	rawURL, valid := refreshURL(current.Source)
	if !valid {
		return refreshFailure(ctx, request, current, "failed: invalid configuration", ErrConfig)
	}
	body, err := FetchHTTP(ctx, rawURL, request.Resolver, request.Policy)
	if err != nil {
		return refreshFailure(ctx, request, current, "failed: fetch", ErrFetch)
	}
	preview, err := proxy.Preview(string(body), 100_000)
	if err != nil || preview.Valid == 0 {
		return refreshFailure(ctx, request, current, "failed: no valid endpoints", ErrNoEndpoints)
	}
	imported, err := proxy.Import(proxy.ImportRequest{Input: string(body), Mode: proxy.SkipDuplicates})
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
