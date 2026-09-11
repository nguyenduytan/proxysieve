// Package gateway defines protocol-neutral request decisions for listener adapters.
package gateway

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
)

var ErrDenied = errors.New("request denied by gateway policy")
var ErrUnsupported = errors.New("request action unavailable for this protocol mode")

type Evaluator interface {
	Evaluate(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error)
}
