package api

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	internalevents "github.com/nguyenduytan/proxysieve/internal/events"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

func TestAuthenticatedTrafficStream(t *testing.T) {
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	recorder, _ := internaltraffic.NewMemory(4)
	server, _ := New(service, recorder, nil, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	if response, err := http.Get(httpServer.URL + "/api/v1/traffic/stream"); err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatal(response, err)
	} else {
		_ = response.Body.Close()
	}
	token, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: time.Now()})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/api/v1/traffic/stream", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatal(response, err)
	}
	defer func() { _ = response.Body.Close() }()
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != "retry: 5000\n" {
		t.Fatal(line, err)
	}
	_, _ = reader.ReadString('\n')
	if err = recorder.Record(t.Context(), traffic.Event{RequestID: "request", ConnectionID: "connection", Host: "example.invalid", Action: "proxy"}); err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0, 3)
	for len(lines) < 3 {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			t.Fatal(readErr)
		}
		lines = append(lines, line)
	}
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, "event: traffic\n") || !strings.Contains(joined, `"request_id":"request"`) || !strings.Contains(joined, `"host":"example.invalid"`) {
		t.Fatal(joined)
	}
}

func TestOperationalEventsListAndStream(t *testing.T) {
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	audits, _ := audit.NewMemory(4)
	bus, _ := internalevents.New(4)
	server, _ := New(service, nil, nil, audits)
	server.SetEvents(bus)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	if response, err := http.Get(httpServer.URL + "/api/v1/events"); err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatal(response, err)
	} else {
		_ = response.Body.Close()
	}
	viewer := auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: time.Now()}
	token, _ := service.CreateSession(viewer)
	server.record(t.Context(), viewer, "proxy.created", "proxy", "proxy-one")

	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/events?limit=1", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal(response, err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if !strings.Contains(string(body), `"type":"proxy.created"`) || !strings.Contains(string(body), `"target_id":"proxy-one"`) {
		t.Fatal(string(body))
	}
	auditEvents, _ := audits.ListAudit(t.Context(), audit.Page{Limit: 1})
	operationalEvents, _, _ := bus.Snapshot(1)
	if len(auditEvents) != 1 || len(operationalEvents) != 1 || auditEvents[0].ID != operationalEvents[0].ID || !auditEvents[0].At.Equal(operationalEvents[0].At) {
		t.Fatal(auditEvents, operationalEvents)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	request, _ = http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/api/v1/events/stream", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	response, err = http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal(response, err)
	}
	defer func() { _ = response.Body.Close() }()
	reader := bufio.NewReader(response.Body)
	_, _ = reader.ReadString('\n')
	_, _ = reader.ReadString('\n')
	server.record(t.Context(), viewer, "proxy.updated", "proxy", "proxy-one")
	var streamed strings.Builder
	for range 3 {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			t.Fatal(readErr)
		}
		streamed.WriteString(line)
	}
	if !strings.Contains(streamed.String(), "event: event\n") || !strings.Contains(streamed.String(), `"type":"proxy.updated"`) {
		t.Fatal(streamed.String())
	}
}
