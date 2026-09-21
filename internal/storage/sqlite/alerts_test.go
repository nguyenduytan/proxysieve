package sqlite

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/alert"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestAlertInventoryAndDeliveryLog(t *testing.T) {
	repository, err := Open(t.Context(), filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()

	webhook := alert.Webhook{ID: "primary", Name: "Primary", URL: "https://alerts.example/hook", Enabled: true}
	webhookRecord, err := repository.PutWebhook(t.Context(), webhook, 0)
	if err != nil || webhookRecord.Revision != 1 {
		t.Fatal(webhookRecord, err)
	}
	rule := alert.Rule{ID: "failures", Name: "Failures", EventTypes: []string{"source.refresh_failed"}, WebhookIDs: []model.ID{webhook.ID}, Enabled: true}
	ruleRecord, err := repository.PutAlertRule(t.Context(), rule, 0)
	if err != nil || ruleRecord.Revision != 1 {
		t.Fatal(ruleRecord, err)
	}
	delivery := alert.Delivery{ID: "delivery", WebhookID: webhook.ID, EventID: "event", AttemptedAt: time.Now().UTC(), Attempt: 1, Success: true, StatusCode: 204}
	if err = repository.RecordWebhookDelivery(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}
	deliveries, err := repository.ListWebhookDeliveries(t.Context(), webhook.ID, 10)
	if err != nil || len(deliveries) != 1 || deliveries[0].ID != delivery.ID {
		t.Fatal(deliveries, err)
	}
	backup := filepath.Join(t.TempDir(), "alerts-backup.db")
	if err = repository.BackupTo(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	copyStore, err := Open(t.Context(), backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = copyStore.GetAlertRule(t.Context(), rule.ID); err != nil {
		t.Fatal(err)
	}
	if copied, listErr := copyStore.ListWebhookDeliveries(t.Context(), webhook.ID, 10); listErr != nil || len(copied) != 1 {
		t.Fatal(copied, listErr)
	}
	_ = copyStore.Close()

	webhook.Name = "Updated"
	if _, err = repository.PutWebhook(t.Context(), webhook, webhookRecord.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.PutWebhook(t.Context(), webhook, webhookRecord.Revision); !errors.Is(err, store.ErrConflict) {
		t.Fatal(err)
	}
	if err = repository.DeleteAlertRule(t.Context(), rule.ID, ruleRecord.Revision); err != nil {
		t.Fatal(err)
	}
	if err = repository.DeleteWebhook(t.Context(), webhook.ID, 2); err != nil {
		t.Fatal(err)
	}
}
