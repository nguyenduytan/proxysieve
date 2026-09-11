// Package app composes validated configuration into runtime components.
package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/transport/httpforward"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
)

var ErrUnsupportedListener = errors.New("configured listener type is not implemented")

type Runtime struct {
	Server *http.Server
	Bind   string
}
type resolver struct{}

func (resolver) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

type evaluator struct{ document policy.Policy }

func (e evaluator) Evaluate(_ context.Context, r policy.RequestContext, v policy.Visibility) (policy.Result, error) {
	return policy.Evaluate(e.document, r, v, false)
}

func Build(c config.Config) (Runtime, error) {
	if c.Validate() != nil {
		return Runtime{}, config.ErrInvalid
	}
	var listener *config.Listener
	for i := range c.Listeners {
		if c.Listeners[i].Type == "socks5" {
			return Runtime{}, ErrUnsupportedListener
		}
		if c.Listeners[i].Type == "http" {
			if listener != nil {
				return Runtime{}, config.ErrInvalid
			}
			listener = &c.Listeners[i]
		}
	}
	if listener == nil {
		return Runtime{}, ErrUnsupportedListener
	}
	doc := defaultPolicy(c.Security)
	handler, err := httpforward.New(httpforward.Options{Evaluator: evaluator{document: doc}, Resolver: resolver{}, DestinationPolicy: security.DestinationPolicy{DenyPrivate: c.Security.DenyPrivate}})
	if err != nil {
		return Runtime{}, err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Duration(listener.IdleTimeout), MaxHeaderBytes: 32 << 10}
	return Runtime{Server: server, Bind: listener.Bind}, nil
}
func (r Runtime) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", r.Bind)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	done := make(chan error, 1)
	go func() { done <- r.Server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = r.Server.Shutdown(shutdownCtx)
		err = <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
func defaultPolicy(s config.Security) policy.Policy {
	rule := policy.Rule{ID: "deny", Name: "Deny by default", Priority: 100, Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "reject"}}}
	if s.AllowDirect {
		rule = policy.Rule{ID: "direct", Name: "Explicit direct allowlist", Priority: 100, Enabled: true, StopProcessing: true, Conditions: policy.Condition{Field: "host", Operator: "wildcard", Values: s.DirectAllowlist}, Actions: []policy.Action{{Type: "direct"}}}
	}
	return policy.Policy{Version: 1, ID: "runtime", Name: "runtime", Rules: []policy.Rule{rule}}
}
