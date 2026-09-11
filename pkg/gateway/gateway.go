// Package gateway defines protocol-neutral request decisions for listener adapters.
package gateway

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
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
}

type Router interface {
	Route(context.Context, policy.RequestContext, policy.Result) (Route, error)
}
