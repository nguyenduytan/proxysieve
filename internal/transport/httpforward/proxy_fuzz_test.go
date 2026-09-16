package httpforward

import (
	"net"
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

func FuzzConnectTarget(f *testing.F) {
	for _, seed := range []string{"example.invalid:443", "127.0.0.1:80", "[::1]:443", "example.invalid:0", "missing-port"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		host, port, ok := parseConnectTarget(raw)
		if !ok {
			return
		}
		parsedHost, parsedPort, err := net.SplitHostPort(raw)
		if err != nil || host != parsedHost || !proxy.ValidHost(host) || port == 0 || parsedPort == "" {
			t.Fatal("invalid CONNECT target accepted")
		}
	})
}
