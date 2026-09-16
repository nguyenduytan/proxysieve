package cli

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/api"
	internalcache "github.com/nguyenduytan/proxysieve/internal/cache"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	publiccache "github.com/nguyenduytan/proxysieve/pkg/cache"
)

func TestCacheCLIAgainstAdminAPI(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	service, _ := admin.New(store, security.DefaultPasswordParams())
	setupToken, _ := service.SetupToken(t.Context())
	const password = "integration-password"
	if _, err = service.Setup(t.Context(), setupToken, "admin", password); err != nil {
		t.Fatal(err)
	}
	responseCache, _ := internalcache.NewMemory(2, 32)
	responseCache.Put(publiccache.Key{ClientID: "client", SessionHash: "hash", RouteID: "route", Method: "GET", URL: "https://example.invalid"}, internalcache.Entry{Body: []byte("cached"), ExpiresAt: time.Now().Add(time.Minute)})
	responseCache.Put(publiccache.Key{ClientID: "client", SessionHash: "hash", RouteID: "route", Method: "GET", URL: "https://other.invalid"}, internalcache.Entry{Body: []byte("other"), ExpiresAt: time.Now().Add(time.Minute)})
	apiServer, _ := api.New(service, nil, store, store, store)
	apiServer.SetCache(responseCache)
	httpServer := httptest.NewServer(apiServer.Handler())
	defer httpServer.Close()
	env := map[string]string{adminCLIPasswordEnv: password}

	var stdout, stderr bytes.Buffer
	if code := runCache([]string{"stats", "--admin", httpServer.URL, "--username", "admin"}, &stdout, &stderr, env); code != 0 || !strings.Contains(stdout.String(), "2/2 entries") || stderr.Len() != 0 {
		t.Fatalf("stats code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := runCache([]string{"purge-domain", "--domain", "example.invalid", "--admin", httpServer.URL, "--username", "admin"}, &stdout, &stderr, env); code != 0 || !strings.Contains(stdout.String(), "1 cache entries for example.invalid") || responseCache.Stats(time.Now()).Entries != 1 || stderr.Len() != 0 {
		t.Fatalf("domain purge code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := runCache([]string{"purge", "--admin", httpServer.URL, "--username", "admin", "--json"}, &stdout, &stderr, env); code != 0 || !strings.Contains(stdout.String(), `"entries": 1`) || responseCache.Stats(time.Now()).Entries != 0 || stderr.Len() != 0 {
		t.Fatalf("purge code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), password) {
		t.Fatal("password leaked")
	}
}

func TestCacheCLIRejectsUnsafeInput(t *testing.T) {
	for _, tc := range []struct {
		args []string
		env  map[string]string
		code int
	}{
		{[]string{"stats", "--admin", "http://example.com:9090", "--username", "admin"}, map[string]string{adminCLIPasswordEnv: "password"}, 1},
		{[]string{"stats", "--username", "admin"}, map[string]string{}, 1},
		{[]string{"purge", "--username", "admin", "extra"}, map[string]string{adminCLIPasswordEnv: "password"}, 2},
		{[]string{"purge-domain", "--username", "admin"}, map[string]string{adminCLIPasswordEnv: "password"}, 2},
		{[]string{"stats", "--domain", "example.invalid", "--username", "admin"}, map[string]string{adminCLIPasswordEnv: "password"}, 2},
	} {
		var stdout, stderr bytes.Buffer
		if code := runCache(tc.args, &stdout, &stderr, tc.env); code != tc.code || stdout.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", tc.args, code, stdout.String(), stderr.String())
		}
	}
	for _, body := range []string{
		`{"enabled":true,"stats":{"max_entries":2,"max_bytes":32}}`,
		`{"purged":{"entries":1},"stats":{"entries":0,"bytes_stored":0,"max_entries":2,"max_bytes":32,"hits":0,"misses":0,"bypasses":0,"expired":0,"evictions":0,"bytes_served":0,"hit_ratio":0}}`,
	} {
		command := "stats"
		if strings.Contains(body, "purged") {
			command = "purge"
		}
		if validateCachePayload(command, []byte(body)) == nil {
			t.Fatal("incomplete cache payload accepted", body)
		}
	}
}
