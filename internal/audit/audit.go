// Package audit defines durable, metadata-only control-plane audit events.
package audit

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
)

var ErrInvalid = errors.New("invalid audit event")

type Event struct {
	ID         model.ID  `json:"id"`
	At         time.Time `json:"at"`
	ActorID    model.ID  `json:"actor_id,omitempty"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id,omitempty"`
	RequestID  model.ID  `json:"request_id,omitempty"`
}

var actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,127}$`)

func (e Event) Validate() bool {
	return e.ID.Valid() && !e.At.IsZero() && (e.ActorID == "" || e.ActorID.Valid()) && actionPattern.MatchString(e.Action) && actionPattern.MatchString(e.TargetType) && len(e.TargetID) <= 128 && (e.RequestID == "" || e.RequestID.Valid())
}

type Page struct {
	Before   time.Time
	BeforeID model.ID
	Limit    int
}

func (p Page) Valid() bool {
	return p.Limit >= 1 && p.Limit <= 1000 &&
		(p.BeforeID == "" || (!p.Before.IsZero() && p.BeforeID.Valid()))
}

type Writer interface {
	Record(context.Context, Event) error
}
type Reader interface {
	ListAudit(context.Context, Page) ([]Event, error)
}
