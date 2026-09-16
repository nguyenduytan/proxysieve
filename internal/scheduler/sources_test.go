package scheduler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internalsource "github.com/nguyenduytan/proxysieve/internal/source"
	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	publicstore "github.com/nguyenduytan/proxysieve/pkg/store"
)

type schedulerResolverFunc func(context.Context, string) ([]netip.Addr, error)

func (f schedulerResolverFunc) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return f(ctx, host)
}

func TestSourceJobRefreshesDueSourcesAcrossBoundedRuns(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		_, _ = w.Write([]byte("http://" + id + ".example.invalid:8080"))
	}))
	defer feed.Close()
	repository := schedulerStore(t)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b", "c"} {
		source := contract.Source(id)
		source.Config["url"] = feed.URL + "?id=" + id
		if _, err := repository.PutSource(t.Context(), source, 0); err != nil {
			t.Fatal(err)
		}
	}
	recent := contract.Source("z-recent")
	recent.Config["url"] = feed.URL + "?id=recent"
	recent.LastRefreshAt = now.Add(-30 * time.Minute)
	if _, err := repository.PutSource(t.Context(), recent, 0); err != nil {
		t.Fatal(err)
	}
	disabled := contract.Source("z-disabled")
	disabled.Enabled = false
	if _, err := repository.PutSource(t.Context(), disabled, 0); err != nil {
		t.Fatal(err)
	}
	audits, err := audit.NewMemory(20)
	if err != nil {
		t.Fatal(err)
	}
	job := &SourceJob{
		Store: repository, Refresher: internalsource.NewRefresher(""), Resolver: schedulerLoopbackResolver(),
		Policy: security.DestinationPolicy{AllowTrusted: true}, Audit: audits, Now: func() time.Time { return now },
		PageSize: 2, MaxPages: 2, MaxRefreshes: 1, PerSourceTimeout: 5 * time.Second,
	}
	for range 3 {
		if err = job.Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		record, getErr := repository.GetSource(t.Context(), model.ID(id))
		if getErr != nil || record.Revision != 2 || !strings.HasPrefix(record.Source.LastRefreshStatus, "ok:") {
			t.Fatal(id, record, getErr)
		}
	}
	for _, id := range []string{"z-disabled", "z-recent"} {
		record, getErr := repository.GetSource(t.Context(), model.ID(id))
		if getErr != nil || record.Revision != 1 {
			t.Fatal(id, record, getErr)
		}
	}
	endpoints, err := repository.List(t.Context(), publicstore.Page{Limit: 10})
	if err != nil || len(endpoints) != 3 {
		t.Fatal(endpoints, err)
	}
	events, err := audits.ListAudit(t.Context(), audit.Page{Limit: 20})
	if err != nil || len(events) != 3 {
		t.Fatal(events, err)
	}
}

func TestSourceJobContinuesAfterRecordedFailure(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") == "fail" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("http://good.example.invalid:8080"))
	}))
	defer feed.Close()
	repository := schedulerStore(t)
	for id, query := range map[string]string{"a-fail": "fail", "b-good": "good"} {
		source := contract.Source(id)
		source.Config["url"] = feed.URL + "?id=" + url.QueryEscape(query)
		if _, err := repository.PutSource(t.Context(), source, 0); err != nil {
			t.Fatal(err)
		}
	}
	audits, err := audit.NewMemory(20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	job := &SourceJob{
		Store: repository, Refresher: internalsource.NewRefresher(""), Resolver: schedulerLoopbackResolver(),
		Policy: security.DestinationPolicy{AllowTrusted: true}, Audit: audits, Now: func() time.Time { return now },
		PageSize: 10, MaxPages: 1, MaxRefreshes: 2, PerSourceTimeout: 5 * time.Second,
	}
	if err = job.Run(t.Context()); !errors.Is(err, ErrSourceRefreshFailed) {
		t.Fatal(err)
	}
	failed, failErr := repository.GetSource(t.Context(), "a-fail")
	good, goodErr := repository.GetSource(t.Context(), "b-good")
	if failErr != nil || goodErr != nil || failed.Revision != 2 || failed.Source.LastRefreshStatus != "failed: fetch" || good.Revision != 2 || !strings.HasPrefix(good.Source.LastRefreshStatus, "ok:") {
		t.Fatal(failed, good, failErr, goodErr)
	}
	endpoints, err := repository.List(t.Context(), publicstore.Page{Limit: 10})
	if err != nil || len(endpoints) != 1 || endpoints[0].Endpoint.SourceID != "b-good" {
		t.Fatal(endpoints, err)
	}
	events, err := audits.ListAudit(t.Context(), audit.Page{Limit: 20})
	if err != nil || len(events) != 2 || !hasAuditActions(events, "source.refresh_failed", "source.refreshed") {
		t.Fatal(events, err)
	}
}

func TestSourceDueIncludesFileSources(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	source := contract.Source("file")
	source.Type = proxy.FileSource
	if !sourceDue(source, now) {
		t.Fatal("file source was not scheduled")
	}
}

func hasAuditActions(events []audit.Event, want ...string) bool {
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		seen[event.Action] = true
	}
	for _, action := range want {
		if !seen[action] {
			return false
		}
	}
	return true
}

func schedulerStore(t *testing.T) *sqlite.Store {
	t.Helper()
	repository, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository
}

func schedulerLoopbackResolver() schedulerResolverFunc {
	return func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
}
