// Package budget defines deterministic byte budgets and reservation contracts.
package budget

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"math"
)

var ErrInvalid = errors.New("invalid budget")
var ErrExceeded = errors.New("hard budget exceeded")
var ErrClosed = errors.New("budget lease is closed")

type Action string
type Scope string

const (
	ActionAlert    Action = "alert"
	ActionReject   Action = "reject"
	ActionThrottle Action = "throttle"
	ScopeSystem    Scope  = "system"
	ScopeClient    Scope  = "client"
	ScopePool      Scope  = "pool"
	ScopeProxy     Scope  = "proxy"
)

type Config struct {
	ID      model.ID      `json:"id" yaml:"id"`
	Name    string        `json:"name" yaml:"name"`
	Scope   Scope         `json:"scope" yaml:"scope"`
	ScopeID model.ID      `json:"scope_id,omitempty" yaml:"scope_id,omitempty"`
	Limit   traffic.Bytes `json:"limit_bytes" yaml:"limit_bytes"`
	Hard    bool          `json:"hard" yaml:"hard"`
	Action  Action        `json:"action" yaml:"action"`
}

func (c Config) Validate() error {
	if !c.ID.Valid() || c.Name == "" || c.Limit == 0 || uint64(c.Limit) > math.MaxInt64 {
		return ErrInvalid
	}
	if c.Action != ActionAlert && c.Action != ActionReject && c.Action != ActionThrottle {
		return ErrInvalid
	}
	if c.Hard && c.Action != ActionReject {
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
	return nil
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
