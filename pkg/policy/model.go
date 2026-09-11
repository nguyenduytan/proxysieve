// Package policy defines versioned policy documents; evaluation is implemented in M5.
package policy

import (
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"net/netip"
	"time"
)

type Condition struct {
	Field    string      `json:"field,omitempty"`
	Operator string      `json:"operator,omitempty"`
	Values   []string    `json:"values,omitempty"`
	All      []Condition `json:"all,omitempty"`
	Any      []Condition `json:"any,omitempty"`
	Not      *Condition  `json:"not,omitempty"`
}
type Action struct {
	Type   string   `json:"type"`
	PoolID model.ID `json:"pool_id,omitempty"`
	Value  string   `json:"value,omitempty"`
}
type Rule struct {
	ID             model.ID  `json:"id"`
	Name           string    `json:"name"`
	Priority       int       `json:"priority"`
	Enabled        bool      `json:"enabled"`
	StopProcessing bool      `json:"stop_processing"`
	Conditions     Condition `json:"conditions"`
	Actions        []Action  `json:"actions"`
}
type Policy struct {
	Version int      `json:"version"`
	ID      model.ID `json:"id"`
	Name    string   `json:"name"`
	Rules   []Rule   `json:"rules"`
}

// RequestContext records only observed values. Known false differs from empty data.
// It is not a logging DTO: visible headers can contain secrets and need redaction.
type RequestContext struct {
	RequestID, ConnectionID, ClientID    model.ID
	Listener, Protocol, Scheme, Host     string
	Port                                 uint16
	SourceIP, DestinationIP              netip.Addr
	Method, Path, ResourceType, MIMEHint model.Optional[string]
	ContentLength                        model.Optional[int64]
	Headers                              model.Optional[map[string][]string]
	Timestamp                            time.Time
	InspectMode                          bool
}
