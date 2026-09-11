package app

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"testing"
)

func TestBuildSafety(t *testing.T) {
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	r, err := Build(c)
	if err != nil || r.Bind != "127.0.0.1:8080" {
		t.Fatal(r, err)
	}
	c.Listeners = append(c.Listeners, config.Listener{Name: "socks", Type: "socks5", Bind: "127.0.0.1:1080", Auth: "local", Policy: "default", MaxConnections: 1, IdleTimeout: config.Duration(1)})
	if _, err = Build(c); !errors.Is(err, ErrUnsupportedListener) {
		t.Fatal(err)
	}
	c = config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Security.AllowDirect = true
	c.Security.DirectAllowlist = []string{"*.example.invalid"}
	if _, err = Build(c); err != nil {
		t.Fatal(err)
	}
}
func TestRunCancelled(t *testing.T) {
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Run(ctx); err != nil {
		t.Fatal(err)
	}
}
