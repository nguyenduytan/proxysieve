package alert

import (
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/model"
)

func TestAlertValidation(t *testing.T) {
	webhook := Webhook{ID: "primary", Name: "Primary", URL: "https://alerts.example/hook", SecretRef: "secret://webhooks/primary", Enabled: true}
	rule := Rule{ID: "failures", Name: "Failures", EventTypes: []string{"source.refresh_failed"}, WebhookIDs: []model.ID{"primary"}, Enabled: true}
	if webhook.Validate() != nil || rule.Validate() != nil || !rule.Matches("source.refresh_failed") {
		t.Fatal(webhook, rule)
	}
	webhook.URL = "http://127.0.0.1/hook"
	rule.EventTypes = append(rule.EventTypes, rule.EventTypes[0])
	if webhook.Validate() == nil || rule.Validate() == nil {
		t.Fatal("unsafe or duplicate configuration accepted")
	}
}
