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
