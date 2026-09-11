// Package budget defines deterministic byte budgets and reservation contracts.
package budget

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

var ErrInvalid = errors.New("invalid budget")
var ErrExceeded = errors.New("hard budget exceeded")
var ErrClosed = errors.New("budget lease is closed")

type Action string

const (
	ActionAlert    Action = "alert"
	ActionReject   Action = "reject"
	ActionThrottle Action = "throttle"
)

type Config struct {
	ID     model.ID
	Name   string
	Limit  traffic.Bytes
	Hard   bool
	Action Action
}

func (c Config) Validate() error {
	if !c.ID.Valid() || c.Name == "" || c.Limit == 0 {
		return ErrInvalid
	}
	if c.Action != ActionAlert && c.Action != ActionReject && c.Action != ActionThrottle {
		return ErrInvalid
	}
	if c.Hard && c.Action != ActionReject {
		return ErrInvalid
	}
	return nil
}

type Usage struct {
	Used     traffic.Bytes
	Reserved traffic.Bytes
}
