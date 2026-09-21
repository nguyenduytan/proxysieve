// Package budget defines deterministic byte budgets and reservation contracts.
package budget

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

var ErrInvalid = errors.New("invalid budget")
var ErrExceeded = errors.New("hard budget exceeded")
var ErrClosed = errors.New("budget lease is closed")

type Action string
type Scope string
type Window string

const (
	ActionAlert    Action = "alert"
	ActionReject   Action = "reject"
	ActionThrottle Action = "throttle"
	ScopeSystem    Scope  = "system"
	ScopeClient    Scope  = "client"
	ScopePool      Scope  = "pool"
	ScopeProxy     Scope  = "proxy"
	WindowLifetime Window = "lifetime"
	WindowDaily    Window = "daily"
	WindowWeekly   Window = "weekly"
	WindowMonthly  Window = "monthly"
	WindowRolling  Window = "rolling"
	minRollingSecs        = int64(60)
	maxRollingSecs        = int64(365 * 24 * 60 * 60)
)

type Config struct {
	ID             model.ID      `json:"id" yaml:"id"`
	Name           string        `json:"name" yaml:"name"`
	Scope          Scope         `json:"scope" yaml:"scope"`
	ScopeID        model.ID      `json:"scope_id,omitempty" yaml:"scope_id,omitempty"`
	Limit          traffic.Bytes `json:"limit_bytes" yaml:"limit_bytes"`
	SoftLimit      traffic.Bytes `json:"soft_limit_bytes,omitempty" yaml:"soft_limit_bytes,omitempty"`
	Hard           bool          `json:"hard" yaml:"hard"`
	Action         Action        `json:"action" yaml:"action"`
	Window         Window        `json:"window,omitempty" yaml:"window,omitempty"`
	Timezone       string        `json:"timezone,omitempty" yaml:"timezone,omitempty"`
	RollingSeconds int64         `json:"rolling_seconds,omitempty" yaml:"rolling_seconds,omitempty"`
}

func (c Config) Validate() error {
	if !c.ID.Valid() || c.Name == "" || c.Limit == 0 || uint64(c.Limit) > math.MaxInt64 {
		return ErrInvalid
	}
	if !c.Hard || c.Action != ActionReject || c.SoftLimit >= c.Limit {
		return ErrInvalid
	}
	scope := c.Scope
	if scope == "" {
		scope = ScopeSystem
	}
	if scope != ScopeSystem && scope != ScopeClient && scope != ScopePool && scope != ScopeProxy {
		return ErrInvalid
	}
	if scope == ScopeSystem && c.ScopeID != "" || scope != ScopeSystem && !c.ScopeID.Valid() {
		return ErrInvalid
	}
	window := c.Window
	if window == "" {
		window = WindowLifetime
	}
	if window == WindowLifetime {
		if c.Timezone != "" || c.RollingSeconds != 0 {
			return ErrInvalid
		}
		return nil
	}
	if window == WindowRolling {
		if c.Timezone != "" || c.RollingSeconds < minRollingSecs || c.RollingSeconds > maxRollingSecs {
			return ErrInvalid
		}
		return nil
	}
	if c.RollingSeconds != 0 || window != WindowDaily && window != WindowWeekly && window != WindowMonthly || c.Timezone == "" || c.Timezone == "Local" || len(c.Timezone) > 128 || strings.TrimSpace(c.Timezone) != c.Timezone {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return ErrInvalid
	}
	return nil
}

// WindowBounds returns the half-open UTC interval containing at. A lifetime
// budget has zero bounds and therefore keeps the legacy durable usage row.
func (c Config) WindowBounds(at time.Time) (time.Time, time.Time, error) {
	if c.Validate() != nil || at.IsZero() {
		return time.Time{}, time.Time{}, ErrInvalid
	}
	window := c.Window
	if window == "" || window == WindowLifetime {
		return time.Time{}, time.Time{}, nil
	}
	if window == WindowRolling {
		end := at.UTC()
		return end.Add(-time.Duration(c.RollingSeconds) * time.Second), end, nil
	}
	location, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.Time{}, time.Time{}, ErrInvalid
	}
	local := at.In(location)
	year, month, day := local.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, location)
	switch window {
	case WindowDaily:
		return start.UTC(), start.AddDate(0, 0, 1).UTC(), nil
	case WindowWeekly:
		daysSinceMonday := (int(start.Weekday()) + 6) % 7
		start = start.AddDate(0, 0, -daysSinceMonday)
		return start.UTC(), start.AddDate(0, 0, 7).UTC(), nil
	case WindowMonthly:
		start = time.Date(year, month, 1, 0, 0, 0, 0, location)
		return start.UTC(), start.AddDate(0, 1, 0).UTC(), nil
	default:
		return time.Time{}, time.Time{}, ErrInvalid
	}
}

func (c Config) Applies(clientID, poolID, proxyID model.ID) bool {
	switch c.Scope {
	case "", ScopeSystem:
		return true
	case ScopeClient:
		return c.ScopeID == clientID
	case ScopePool:
		return c.ScopeID == poolID
	case ScopeProxy:
		return c.ScopeID == proxyID
	default:
		return false
	}
}

type Usage struct {
	Used     traffic.Bytes
	Reserved traffic.Bytes
}

type Lease interface {
	Consume(context.Context, traffic.Bytes) (traffic.Bytes, error)
	Close(context.Context) error
}

type ReserveFunc func(context.Context, traffic.Bytes) (Lease, error)
