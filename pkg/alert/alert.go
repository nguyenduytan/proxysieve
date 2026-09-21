// Package alert defines persisted alert rules, webhook destinations and delivery metadata.
package alert

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
)

var ErrInvalid = errors.New("invalid alert configuration")

var eventTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,127}$`)

type Webhook struct {
	ID        model.ID   `json:"id"`
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	SecretRef secret.Ref `json:"secret_ref,omitempty"`
	Enabled   bool       `json:"enabled"`
}

func (w Webhook) Validate() error {
	target, err := url.Parse(w.URL)
	if !w.ID.Valid() || !validName(w.Name) || err != nil || len(w.URL) > 2048 || target.Scheme != "https" || target.Hostname() == "" || target.User != nil || target.Fragment != "" || target.RawQuery != "" || w.SecretRef != "" && !w.SecretRef.Valid() {
		return ErrInvalid
	}
	return nil
}

type Rule struct {
	ID         model.ID   `json:"id"`
	Name       string     `json:"name"`
	EventTypes []string   `json:"event_types"`
	WebhookIDs []model.ID `json:"webhook_ids"`
	Enabled    bool       `json:"enabled"`
}

func (r Rule) Validate() error {
	if !r.ID.Valid() || !validName(r.Name) || len(r.EventTypes) < 1 || len(r.EventTypes) > 64 || len(r.WebhookIDs) < 1 || len(r.WebhookIDs) > 32 {
		return ErrInvalid
	}
	events, webhooks := map[string]bool{}, map[model.ID]bool{}
	for _, eventType := range r.EventTypes {
		if !eventTypePattern.MatchString(eventType) || events[eventType] {
			return ErrInvalid
		}
		events[eventType] = true
	}
	for _, id := range r.WebhookIDs {
		if !id.Valid() || webhooks[id] {
			return ErrInvalid
		}
		webhooks[id] = true
	}
	return nil
}

func (r Rule) Matches(eventType string) bool {
	if !r.Enabled {
		return false
	}
	for _, configured := range r.EventTypes {
		if configured == eventType {
			return true
		}
	}
	return false
}

type Delivery struct {
	ID          model.ID  `json:"id"`
	WebhookID   model.ID  `json:"webhook_id"`
	EventID     model.ID  `json:"event_id"`
	AttemptedAt time.Time `json:"attempted_at"`
	Attempt     int       `json:"attempt"`
	Success     bool      `json:"success"`
	StatusCode  int       `json:"status_code,omitempty"`
	ErrorCode   string    `json:"error_code,omitempty"`
	DurationNS  int64     `json:"duration_ns"`
}

func (d Delivery) Validate() error {
	if !d.ID.Valid() || !d.WebhookID.Valid() || !d.EventID.Valid() || d.AttemptedAt.IsZero() || d.Attempt < 1 || d.Attempt > 3 || d.StatusCode < 0 || d.StatusCode > 999 || len(d.ErrorCode) > 64 || d.DurationNS < 0 || d.Success && (d.StatusCode < 200 || d.StatusCode > 299 || d.ErrorCode != "") || !d.Success && d.ErrorCode == "" {
		return ErrInvalid
	}
	return nil
}

func validName(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}
