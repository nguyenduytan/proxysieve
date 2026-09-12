// Package gateway defines protocol-neutral request decisions for listener adapters.
package gateway

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"net"
	"net/http"
)

var ErrDenied = errors.New("request denied by gateway policy")
var ErrUnsupported = errors.New("request action unavailable for this protocol mode")

type Evaluator interface {
	Evaluate(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error)
}

// Route contains adapter-local objects selected after policy evaluation. It is
// scoped to one normalized destination and must not cross credential boundaries.
type Route struct {
	Action    string
	PoolID    model.ID
	ProxyID   model.ID
	Transport http.RoundTripper
	Dial      func(context.Context, string) (net.Conn, error)
	Observe   func(bool, int)
	Rate      *traffic.Rate
	Reserve   budget.ReserveFunc
}

type Router interface {
	Route(context.Context, policy.RequestContext, policy.Result) (Route, error)
}
