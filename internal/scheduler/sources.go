package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internalsource "github.com/nguyenduytan/proxysieve/internal/source"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	publicstore "github.com/nguyenduytan/proxysieve/pkg/store"
)

var ErrSourceRefreshFailed = errors.New("one or more scheduled source refreshes failed")

type SourceJob struct {
	Store            publicstore.InventoryStore
	Refresher        *internalsource.Refresher
	Resolver         internalsource.Resolver
	Policy           security.DestinationPolicy
	Audit            audit.Writer
	Now              func() time.Time
	PageSize         int
	MaxPages         int
	MaxRefreshes     int
	PerSourceTimeout time.Duration

	mu    sync.Mutex
	after model.ID
}

// Run scans a bounded, rotating window of source IDs so large inventories do
// not starve later records. Each source has its own timeout and refresh lock.
func (j *SourceJob) Run(ctx context.Context) error {
	if j == nil || j.Store == nil || j.Refresher == nil || j.Resolver == nil {
		return publicstore.ErrInvalid
	}
	if !j.mu.TryLock() {
		return nil
	}
	defer j.mu.Unlock()

	pageSize, maxPages, maxRefreshes, perSourceTimeout := j.bounds()
	now := time.Now().UTC()
	if j.Now != nil {
		now = j.Now().UTC()
	}
	after := j.after
	refreshes := 0
	hadFailure := false
	for range maxPages {
		records, err := j.Store.ListSources(ctx, publicstore.Page{After: after, Limit: pageSize})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrSourceRefreshFailed
		}
		if len(records) == 0 {
			j.after = ""
			break
		}
		for _, record := range records {
			if err = ctx.Err(); err != nil {
				j.after = after
				return err
			}
			after = record.Source.ID
			j.after = after
			if !sourceDue(record.Source, now) {
				continue
			}
			refreshCtx, cancel := context.WithTimeout(ctx, perSourceTimeout)
			result, refreshErr := j.Refresher.Refresh(refreshCtx, internalsource.RefreshRequest{
				ID: record.Source.ID, Revision: record.Revision, Store: j.Store,
				Resolver: j.Resolver, Policy: j.Policy, Now: now,
			})
			cancel()
			refreshes++
			switch {
			case refreshErr == nil:
				j.record(ctx, now, "source.refreshed", record.Source.ID)
			case result.FailureRecorded:
				j.record(ctx, now, "source.refresh_failed", record.Source.ID)
				hadFailure = true
			case errors.Is(refreshErr, internalsource.ErrInProgress), errors.Is(refreshErr, publicstore.ErrConflict), errors.Is(refreshErr, publicstore.ErrNotFound), errors.Is(refreshErr, internalsource.ErrDisabled), errors.Is(refreshErr, internalsource.ErrUnsupported):
				// A concurrent operator action made this scan result stale; retry on
				// a later scheduler pass without treating it as an unhealthy run.
			default:
				hadFailure = true
			}
			if refreshes >= maxRefreshes {
				return sourceJobResult(hadFailure)
			}
		}
		if len(records) < pageSize {
			j.after = ""
			break
		}
	}
	return sourceJobResult(hadFailure)
}

func (j *SourceJob) bounds() (int, int, int, time.Duration) {
	pageSize, maxPages, maxRefreshes := j.PageSize, j.MaxPages, j.MaxRefreshes
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 100
	}
	if maxPages < 1 || maxPages > 1000 {
		maxPages = 10
	}
	if maxRefreshes < 1 || maxRefreshes > 1000 {
		maxRefreshes = 8
	}
	timeout := j.PerSourceTimeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 20 * time.Second
	}
	return pageSize, maxPages, maxRefreshes, timeout
}

func sourceDue(source proxy.Source, now time.Time) bool {
	return source.Enabled && (source.Type == proxy.APISource || source.Type == proxy.FileSource) && source.RefreshInterval > 0 &&
		(source.LastRefreshAt.IsZero() || !source.LastRefreshAt.Add(source.RefreshInterval).After(now))
}

func sourceJobResult(failed bool) error {
	if failed {
		return ErrSourceRefreshFailed
	}
	return nil
}

func (j *SourceJob) record(ctx context.Context, now time.Time, action string, id model.ID) {
	if j.Audit == nil {
		return
	}
	_ = j.Audit.Record(context.WithoutCancel(ctx), audit.Event{
		ID: model.NewID(), At: now, Action: action, TargetType: "source", TargetID: string(id),
	})
}
