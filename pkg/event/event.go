// Package event defines sanitized operational events exposed by the control plane.
package event

import (
	"errors"
	"regexp"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
)

var ErrInvalid = errors.New("invalid operational event")

type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

func (s Severity) Valid() bool { return s == Info || s == Warning || s == Error }

type Event struct {
	ID         model.ID  `json:"id"`
	At         time.Time `json:"at"`
	Type       string    `json:"type"`
	Severity   Severity  `json:"severity"`
	Source     string    `json:"source"`
	ActorID    model.ID  `json:"actor_id,omitempty"`
	TargetType string    `json:"target_type,omitempty"`
	TargetID   string    `json:"target_id,omitempty"`
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,127}$`)

func (e Event) Validate() bool {
	return e.ID.Valid() && !e.At.IsZero() && namePattern.MatchString(e.Type) &&
		e.Severity.Valid() && namePattern.MatchString(e.Source) &&
		(e.ActorID == "" || e.ActorID.Valid()) &&
		(e.TargetType == "" || namePattern.MatchString(e.TargetType)) && len(e.TargetID) <= 128
}
