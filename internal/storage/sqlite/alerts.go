package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/alert"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

const maxWebhookDeliveries = 10_000

func (s *Store) GetWebhook(ctx context.Context, id model.ID) (store.WebhookRecord, error) {
	var document []byte
	var record store.WebhookRecord
	err := s.db.QueryRowContext(ctx, "SELECT document,revision FROM webhook_configs WHERE id=?", string(id)).Scan(&document, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return record, store.ErrNotFound
	}
	if err != nil {
		return record, safeError(ctx, err)
	}
	if json.Unmarshal(document, &record.Webhook) != nil || record.Webhook.Validate() != nil || record.Webhook.ID != id {
		return record, store.ErrSchema
	}
	return record, nil
}

func (s *Store) PutWebhook(ctx context.Context, webhook alert.Webhook, expected int64) (store.WebhookRecord, error) {
	document, err := marshalAlertDocument(webhook, webhook.Validate())
	if err != nil || expected < 0 || expected == math.MaxInt64 {
		return store.WebhookRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.db.ExecContext(ctx, "INSERT INTO webhook_configs(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(webhook.ID), string(document))
	} else {
		result, err = s.db.ExecContext(ctx, "UPDATE webhook_configs SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(document), string(webhook.ID), expected)
	}
	if err != nil {
		return store.WebhookRecord{}, safeError(ctx, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return store.WebhookRecord{}, safeError(ctx, err)
	}
	if changed == 0 {
		if expected > 0 {
			if _, err = s.GetWebhook(ctx, webhook.ID); err != nil {
				return store.WebhookRecord{}, err
			}
		}
		return store.WebhookRecord{}, store.ErrConflict
	}
	return store.WebhookRecord{Webhook: webhook, Revision: expected + 1}, nil
}

func (s *Store) DeleteWebhook(ctx context.Context, id model.ID, expected int64) error {
	return deleteRevisioned(ctx, s.db, "webhook_configs", id, expected, func() error { _, err := s.GetWebhook(ctx, id); return err })
}

func (s *Store) ListWebhooks(ctx context.Context) ([]store.WebhookRecord, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,document,revision FROM webhook_configs ORDER BY id")
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := []store.WebhookRecord{}
	for rows.Next() {
		var id model.ID
		var document []byte
		var record store.WebhookRecord
		if err = rows.Scan(&id, &document, &record.Revision); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(document, &record.Webhook) != nil || record.Webhook.Validate() != nil || record.Webhook.ID != id {
			return nil, store.ErrSchema
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}

func (s *Store) GetAlertRule(ctx context.Context, id model.ID) (store.AlertRuleRecord, error) {
	var document []byte
	var record store.AlertRuleRecord
	err := s.db.QueryRowContext(ctx, "SELECT document,revision FROM alert_rules WHERE id=?", string(id)).Scan(&document, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return record, store.ErrNotFound
	}
	if err != nil {
		return record, safeError(ctx, err)
	}
	if json.Unmarshal(document, &record.Rule) != nil || record.Rule.Validate() != nil || record.Rule.ID != id {
		return record, store.ErrSchema
	}
	return record, nil
}

func (s *Store) PutAlertRule(ctx context.Context, rule alert.Rule, expected int64) (store.AlertRuleRecord, error) {
	document, err := marshalAlertDocument(rule, rule.Validate())
	if err != nil || expected < 0 || expected == math.MaxInt64 {
		return store.AlertRuleRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.db.ExecContext(ctx, "INSERT INTO alert_rules(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(rule.ID), string(document))
	} else {
		result, err = s.db.ExecContext(ctx, "UPDATE alert_rules SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(document), string(rule.ID), expected)
	}
	if err != nil {
		return store.AlertRuleRecord{}, safeError(ctx, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return store.AlertRuleRecord{}, safeError(ctx, err)
	}
	if changed == 0 {
		if expected > 0 {
			if _, err = s.GetAlertRule(ctx, rule.ID); err != nil {
				return store.AlertRuleRecord{}, err
			}
		}
		return store.AlertRuleRecord{}, store.ErrConflict
	}
	return store.AlertRuleRecord{Rule: rule, Revision: expected + 1}, nil
}

func (s *Store) DeleteAlertRule(ctx context.Context, id model.ID, expected int64) error {
	return deleteRevisioned(ctx, s.db, "alert_rules", id, expected, func() error { _, err := s.GetAlertRule(ctx, id); return err })
}

func (s *Store) ListAlertRules(ctx context.Context) ([]store.AlertRuleRecord, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,document,revision FROM alert_rules ORDER BY id")
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := []store.AlertRuleRecord{}
	for rows.Next() {
		var id model.ID
		var document []byte
		var record store.AlertRuleRecord
		if err = rows.Scan(&id, &document, &record.Revision); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(document, &record.Rule) != nil || record.Rule.Validate() != nil || record.Rule.ID != id {
			return nil, store.ErrSchema
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}

func (s *Store) RecordWebhookDelivery(ctx context.Context, delivery alert.Delivery) error {
	if delivery.Validate() != nil {
		return store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, "INSERT INTO webhook_deliveries(id,webhook_id,event_id,attempted_at,attempt,success,status_code,error_code,duration_ns) VALUES (?,?,?,?,?,?,?,?,?)", string(delivery.ID), string(delivery.WebhookID), string(delivery.EventID), delivery.AttemptedAt.UnixNano(), delivery.Attempt, delivery.Success, delivery.StatusCode, delivery.ErrorCode, delivery.DurationNS)
	if err != nil {
		return safeError(ctx, err)
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM webhook_deliveries WHERE id IN (SELECT id FROM webhook_deliveries ORDER BY attempted_at DESC,id DESC LIMIT -1 OFFSET ?)", maxWebhookDeliveries)
	if err != nil {
		return safeError(ctx, err)
	}
	return safeError(ctx, tx.Commit())
}

func (s *Store) ListWebhookDeliveries(ctx context.Context, webhookID model.ID, limit int) ([]alert.Delivery, error) {
	if !webhookID.Valid() || limit < 1 || limit > 1000 {
		return nil, store.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,event_id,attempted_at,attempt,success,status_code,error_code,duration_ns FROM webhook_deliveries WHERE webhook_id=? ORDER BY attempted_at DESC,id DESC LIMIT ?", string(webhookID), limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	items := []alert.Delivery{}
	for rows.Next() {
		var delivery alert.Delivery
		var attemptedAt int64
		delivery.WebhookID = webhookID
		if err = rows.Scan(&delivery.ID, &delivery.EventID, &attemptedAt, &delivery.Attempt, &delivery.Success, &delivery.StatusCode, &delivery.ErrorCode, &delivery.DurationNS); err != nil {
			return nil, safeError(ctx, err)
		}
		delivery.AttemptedAt = time.Unix(0, attemptedAt).UTC()
		if delivery.Validate() != nil {
			return nil, store.ErrSchema
		}
		items = append(items, delivery)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return items, nil
}

func marshalAlertDocument(value any, validation error) ([]byte, error) {
	if validation != nil {
		return nil, store.ErrInvalid
	}
	document, err := json.Marshal(value)
	if err != nil || len(document) > 64<<10 {
		return nil, store.ErrInvalid
	}
	return document, nil
}

func deleteRevisioned(ctx context.Context, db *sql.DB, table string, id model.ID, expected int64, exists func() error) error {
	if !id.Valid() || expected < 1 || table != "webhook_configs" && table != "alert_rules" {
		return store.ErrInvalid
	}
	result, err := db.ExecContext(ctx, "DELETE FROM "+table+" WHERE id=? AND revision=?", string(id), expected)
	if err != nil {
		return safeError(ctx, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if changed == 1 {
		return nil
	}
	if err = exists(); err != nil {
		return err
	}
	return store.ErrConflict
}

var _ store.Alerts = (*Store)(nil)
