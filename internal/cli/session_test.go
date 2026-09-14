package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/api"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internalsession "github.com/nguyenduytan/proxysieve/internal/session"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
)

func TestSessionCLIListRotateAndDelete(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := publicsession.Session{ID: "session", ClientID: "client", KeyHash: strings.Repeat("a", 64), PoolID: "pool", ProxyEndpointID: "proxy", CreatedAt: now, LastUsedAt: now, Status: "active", RotationReason: publicsession.Created, Policy: publicsession.Policy{Strategy: publicsession.Explicit}, RuntimeRevision: 1}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var credentials map[string]string
			if json.NewDecoder(r.Body).Decode(&credentials) != nil || credentials["username"] != "admin" || credentials["password"] != "private-password" {
				http.Error(w, "bad login", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "proxysieve_session", Value: "session-cookie", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "proxysieve_csrf", Value: "csrf-token", Path: "/"})
			_, _ = w.Write([]byte(`{"role":"admin"}`))
		case "/api/v1/auth/logout":
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/sessions":
			if r.Method != http.MethodGet {
				t.Fatal("unexpected list method", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []publicsession.Session{entry}})
		case "/api/v1/sessions/session/rotate":
			if r.Method != http.MethodPost || r.Header.Get("X-CSRF-Token") != "csrf-token" {
				t.Fatal("rotate missing CSRF", r.Method, r.Header)
			}
			rotated := entry
			rotated.Status = "rotated"
			rotated.RotationReason = publicsession.Manual
			_ = json.NewEncoder(w).Encode(rotated)
		case "/api/v1/sessions/session":
			if r.Method == http.MethodDelete {
				if r.Header.Get("X-CSRF-Token") != "csrf-token" {
					t.Fatal("delete missing CSRF")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(entry)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	env := map[string]string{sessionCLIPasswordEnv: "private-password"}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"list", "--admin", server.URL, "--username", "admin"}, "session\tactive"},
		{[]string{"show", "--admin", server.URL, "--username", "admin", "--json", "session"}, `"id": "session"`},
		{[]string{"rotate", "--admin", server.URL, "--username", "admin", "session"}, "session\trotated"},
		{[]string{"delete", "--admin", server.URL, "--username", "admin", "session"}, "session deleted"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runSession(tc.args, &stdout, &stderr, env); code != 0 || !strings.Contains(stdout.String(), tc.want) || stderr.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", tc.args, code, stdout.String(), stderr.String())
		}
		if strings.Contains(stdout.String()+stderr.String(), "private-password") {
			t.Fatal("password leaked")
		}
	}
}

func TestSessionCLIRejectsUnsafeURLAndMissingPassword(t *testing.T) {
	for _, tc := range []struct {
		args []string
		env  map[string]string
		code int
	}{
		{[]string{"list", "--admin", "http://example.com:9090", "--username", "admin"}, map[string]string{sessionCLIPasswordEnv: "password"}, 1},
		{[]string{"list", "--username", "admin"}, map[string]string{}, 1},
		{[]string{"show", "--username", "admin", "../invalid"}, map[string]string{sessionCLIPasswordEnv: "password"}, 2},
		{[]string{"delete", "--username", "admin", "--json", "session"}, map[string]string{sessionCLIPasswordEnv: "password"}, 2},
	} {
		var stdout, stderr bytes.Buffer
		if code := runSession(tc.args, &stdout, &stderr, tc.env); code != tc.code || stdout.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", tc.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestSessionCLIRejectsRedirectAndDoesNotLeakPassword(t *testing.T) {
	const password = "redirect-secret-password"
	received := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, nil, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	var stdout, stderr bytes.Buffer
	code := runSession([]string{"list", "--admin", redirect.URL, "--username", "admin"}, &stdout, &stderr, map[string]string{sessionCLIPasswordEnv: password})
	if code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), password) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	select {
	case body := <-received:
		t.Fatalf("redirect target received credentials: %q", body)
	default:
	}
}

func TestSessionCLIRejectsInvalidAdminPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "proxysieve_session", Value: "session-cookie", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "proxysieve_csrf", Value: "csrf-token", Path: "/"})
			_, _ = w.Write([]byte(`{"role":"admin"}`))
		case "/api/v1/auth/logout":
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/sessions":
			_, _ = w.Write([]byte(`{"items":[{"id":"syntactically-valid"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	code := runSession([]string{"list", "--admin", server.URL, "--username", "admin", "--json"}, &stdout, &stderr, map[string]string{sessionCLIPasswordEnv: "password"})
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "invalid session data") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestSessionCLIAgainstAdminAPI(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	service, err := admin.New(store, security.DefaultPasswordParams())
	if err != nil {
		t.Fatal(err)
	}
	setupToken, err := service.SetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	const password = "integration-password"
	if _, err = service.Setup(t.Context(), setupToken, "admin", password); err != nil {
		t.Fatal(err)
	}
	manager, err := internalsession.New([]byte("an integration session key long enough"), 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Resolve(t.Context(), internalsession.Request{ClientID: "client", PoolID: "pool", Key: "raw", Policy: publicsession.Policy{Strategy: publicsession.Explicit}, Select: func(context.Context) (model.ID, error) { return "proxy", nil }})
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := api.New(service, nil, store, store, store)
	if err != nil {
		t.Fatal(err)
	}
	apiServer.SetSessions(manager)
	httpServer := httptest.NewServer(apiServer.Handler())
	defer httpServer.Close()
	var stdout, stderr bytes.Buffer
	args := []string{"rotate", "--admin", httpServer.URL, "--username", "admin", string(created.Session.ID)}
	if code := runSession(args, &stdout, &stderr, map[string]string{sessionCLIPasswordEnv: password}); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	rotated, err := manager.Get(t.Context(), created.Session.ID)
	if err != nil || rotated.Status != "rotated" || rotated.RotationReason != publicsession.Manual {
		t.Fatalf("session was not rotated through API: %+v %v", rotated, err)
	}
}
