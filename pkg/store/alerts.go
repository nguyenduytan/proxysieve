package store

import (
	"context"

	"github.com/nguyenduytan/proxysieve/pkg/alert"
	"github.com/nguyenduytan/proxysieve/pkg/model"
)

type WebhookRecord struct {
	Webhook  alert.Webhook `json:"webhook"`
	Revision int64         `json:"revision"`
}

type AlertRuleRecord struct {
	Rule     alert.Rule `json:"rule"`
	Revision int64      `json:"revision"`
}

type Alerts interface {
	GetWebhook(context.Context, model.ID) (WebhookRecord, error)
	PutWebhook(context.Context, alert.Webhook, int64) (WebhookRecord, error)
	DeleteWebhook(context.Context, model.ID, int64) error
	ListWebhooks(context.Context) ([]WebhookRecord, error)
	GetAlertRule(context.Context, model.ID) (AlertRuleRecord, error)
	PutAlertRule(context.Context, alert.Rule, int64) (AlertRuleRecord, error)
	DeleteAlertRule(context.Context, model.ID, int64) error
	ListAlertRules(context.Context) ([]AlertRuleRecord, error)
	RecordWebhookDelivery(context.Context, alert.Delivery) error
	ListWebhookDeliveries(context.Context, model.ID, int) ([]alert.Delivery, error)
}
