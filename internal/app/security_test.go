package app

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"net"
	"testing"
)

func TestDirectGlobalPermissionCannotBeBypassed(t *testing.T) {
	r := &router{}
	_, err := r.Route(t.Context(), policy.RequestContext{Host: "example.invalid", Port: 80}, policy.Result{Actions: []policy.Action{{Type: "direct"}}})
	if !errors.Is(err, gateway.ErrDenied) {
		t.Fatal(err)
	}
	r.directPolicy = config.Security{AllowDirect: true, DirectAllowlist: []string{"other.invalid"}}
	_, err = r.Route(t.Context(), policy.RequestContext{Host: "example.invalid", Port: 80}, policy.Result{Actions: []policy.Action{{Type: "direct"}}})
	if !errors.Is(err, gateway.ErrDenied) {
		t.Fatal(err)
	}
}
func TestUnavailableActionsAreNotReportedAsDestinationDenials(t *testing.T) {
	r := &router{}
	for _, action := range []string{"cache", "mock", "redirect", "rewrite"} {
		route, err := r.Route(t.Context(), policy.RequestContext{}, policy.Result{Actions: []policy.Action{{Type: action}}})
		if !errors.Is(err, gateway.ErrUnsupported) || route.Action != action {
			t.Fatalf("action=%q route=%+v err=%v", action, route, err)
		}
	}
}
func TestFailedBindDoesNotReportReady(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	c := config.Defaults(t.TempDir())
	c.Admin.Enabled = false
	c.Listeners = c.Listeners[:1]
	c.Listeners[0].Bind = busy.Addr().String()
	r, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = r.RunReady(t.Context(), func() error { called = true; return nil })
	if err == nil || called {
		t.Fatal("readiness reported before bind", err)
	}
}
