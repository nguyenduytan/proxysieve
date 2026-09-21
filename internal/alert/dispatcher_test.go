package alert

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/events"
	"github.com/nguyenduytan/proxysieve/internal/security"
	publicalert "github.com/nguyenduytan/proxysieve/pkg/alert"
	publicevent "github.com/nguyenduytan/proxysieve/pkg/event"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type fixedResolver []netip.Addr

func (r fixedResolver) LookupNetIP(context.Context, string) ([]netip.Addr, error) { return r, nil }

type fixedSecrets struct{ value secret.Value }

func (s fixedSecrets) Resolve(context.Context, secret.Ref) (secret.Value, error) { return s.value, nil }

type alertStore struct {
	webhook    publicalert.Webhook
	rule       publicalert.Rule
	mu         sync.Mutex
	deliveries []publicalert.Delivery
}

func (s *alertStore) GetWebhook(context.Context, model.ID) (store.WebhookRecord, error) {
	return store.WebhookRecord{Webhook: s.webhook, Revision: 1}, nil
}
func (s *alertStore) ListWebhooks(context.Context) ([]store.WebhookRecord, error) {
	return []store.WebhookRecord{{Webhook: s.webhook, Revision: 1}}, nil
}
func (s *alertStore) ListAlertRules(context.Context) ([]store.AlertRuleRecord, error) {
	return []store.AlertRuleRecord{{Rule: s.rule, Revision: 1}}, nil
}
func (s *alertStore) RecordWebhookDelivery(_ context.Context, delivery publicalert.Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries = append(s.deliveries, delivery)
	return nil
}

func TestSenderSignsAndRejectsPrivateDestinations(t *testing.T) {
	key, _ := secret.New([]byte("test-secret"))
	webhook := publicalert.Webhook{ID: "primary", Name: "Primary", URL: "https://alerts.example/hook", SecretRef: "secret://webhooks/primary", Enabled: true}
	event := publicevent.Event{ID: "event", At: time.Now(), Type: "proxy.failed", Severity: publicevent.Error, Source: "gateway"}
	called := false
	sender := Sender{Resolver: fixedResolver{netip.MustParseAddr("203.0.113.10")}, Policy: security.DestinationPolicy{DenyPrivate: true}, Secrets: fixedSecrets{value: key}, do: func(request *http.Request) (*http.Response, error) {
		called = true
		body, _ := io.ReadAll(request.Body)
		mac := hmac.New(sha256.New, []byte("test-secret"))
		_, _ = mac.Write(body)
		if request.Header.Get("X-ProxySieve-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) || !strings.Contains(string(body), `"type":"proxy.failed"`) {
			t.Fatal(request.Header, string(body))
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
	}}
	if delivery := sender.send(t.Context(), webhook, event, 1); !delivery.Success || !called {
		t.Fatal(delivery, called)
	}
	called = false
	sender.Resolver = fixedResolver{netip.MustParseAddr("127.0.0.1")}
	if delivery := sender.send(t.Context(), webhook, event, 1); delivery.ErrorCode != "destination_denied" || called {
		t.Fatal(delivery, called)
	}
}

func TestDispatcherRoutesMatchingEvents(t *testing.T) {
	bus, _ := events.New(10)
	webhook := publicalert.Webhook{ID: "primary", Name: "Primary", URL: "https://alerts.example/hook", Enabled: true}
	storage := &alertStore{webhook: webhook, rule: publicalert.Rule{ID: "failures", Name: "Failures", EventTypes: []string{"proxy.failed"}, WebhookIDs: []model.ID{webhook.ID}, Enabled: true}}
	dispatcher, err := New(bus, storage, Sender{Resolver: fixedResolver{netip.MustParseAddr("203.0.113.10")}, Policy: security.DestinationPolicy{DenyPrivate: true}, do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.Start(t.Context())
	t.Cleanup(dispatcher.Stop)
	event := publicevent.Event{ID: "event", At: time.Now(), Type: "proxy.failed", Severity: publicevent.Error, Source: "gateway"}
	if err = bus.Publish(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		storage.mu.Lock()
		count := len(storage.deliveries)
		storage.mu.Unlock()
		if count == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("matching event was not delivered")
}
