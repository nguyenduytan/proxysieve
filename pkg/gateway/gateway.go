// Package gateway defines protocol-neutral request decisions for listener adapters.
package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/retry"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

var ErrDenied = errors.New("request denied by gateway policy")
var ErrUnsupported = errors.New("request action unavailable for this protocol mode")

type Evaluator interface {
	Evaluate(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error)
}

// Route contains adapter-local objects selected after policy evaluation. It is
// scoped to one normalized destination and must not cross credential boundaries.
type Route struct {
	Action       string
	PoolID       model.ID
	ProxyID      model.ID
	ChainID      model.ID
	ChainPools   []model.ID
	ChainProxies []model.ID
	SessionID    model.ID
	SessionHash  string
	Transport    http.RoundTripper
	Dial         func(context.Context, string) (net.Conn, error)
	Acquire      func() bool
	Retry        func(context.Context) (Route, error)
	RetryPolicy  *retry.Policy
	Observe      func(health.Observation)
	Complete     func(context.Context, traffic.Bytes, traffic.Bytes)
	Rate         *traffic.Rate
	Reserve      budget.ReserveFunc
}

type Router interface {
	Route(context.Context, policy.RequestContext, policy.Result) (Route, error)
}
