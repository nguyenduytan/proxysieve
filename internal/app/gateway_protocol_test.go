package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/config"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
)

func TestProtocolMatrixHTTPThroughSOCKS(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "through-socks")
	}))
	defer target.Close()
	endpoint := startSOCKSUpstream(t)
	runtime := buildProtocolRuntime(t, "http", endpoint)
	downstream := httptest.NewServer(runtime.Server.Handler)
	defer downstream.Close()
	proxyURL, _ := url.Parse(downstream.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	response, err := client.Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "through-socks" {
		t.Fatal(string(body))
	}
}

func TestProtocolMatrixProxiedTunnels(t *testing.T) {
	for _, downstream := range []string{"connect", "socks5"} {
		for _, upstream := range []proxy.Protocol{proxy.HTTP, proxy.SOCKS5} {
			t.Run(downstream+"-via-"+string(upstream), func(t *testing.T) {
				target := startEchoServer(t)
				var endpoint proxy.Endpoint
				if upstream == proxy.HTTP {
					server := newConnectProxy(t, make(chan string, 1))
					t.Cleanup(server.Close)
					parsed, _ := url.Parse(server.URL)
					host, port, _ := net.SplitHostPort(parsed.Host)
					endpoint = proxy.Endpoint{ID: "upstream", Name: "upstream", Protocol: proxy.HTTP, Host: host, Port: parsePort(t, port), Enabled: true, TrustedRemoteDNS: true}
				} else {
					endpoint = startSOCKSUpstream(t)
				}
				runtime := buildProtocolRuntime(t, downstream, endpoint)
				if downstream == "connect" {
					testConnectTunnel(t, runtime, target)
				} else {
					testSOCKSTunnel(t, runtime, target)
				}
			})
		}
	}
}

func buildProtocolRuntime(t *testing.T, listener string, endpoint proxy.Endpoint) Runtime {
	t.Helper()
	c := config.Defaults(t.TempDir())
	c.Admin.Enabled = false
	c.Security.DenyPrivate = false
	if listener == "socks5" {
		c.Listeners = c.Listeners[1:]
	} else {
		c.Listeners = c.Listeners[:1]
	}
	c.Proxies = []proxy.Endpoint{endpoint}
	c.Pools = []routing.Pool{{ID: "pool", Name: "pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{endpoint.ID}, Enabled: true}}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "route", Name: "route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}}}}
	runtime, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func startSOCKSUpstream(t *testing.T) proxy.Endpoint {
	t.Helper()
	c := config.Defaults(t.TempDir())
	c.Admin.Enabled = false
	c.Listeners = c.Listeners[1:]
	c.Security.AllowDirect = true
	c.Security.DenyPrivate = false
	c.Security.DirectAllowlist = []string{"127.0.0.1"}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "direct", Name: "direct", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "direct"}}}}}}
	runtime, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go runtime.SOCKS.Serve(ctx, conn)
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		<-done
	})
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	return proxy.Endpoint{ID: "upstream", Name: "upstream", Protocol: proxy.SOCKS5, Host: host, Port: parsePort(t, port), Enabled: true}
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		payload := make([]byte, 4)
		if _, readErr := io.ReadFull(conn, payload); readErr == nil && string(payload) == "ping" {
			_, _ = conn.Write([]byte("pong"))
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	return listener.Addr().String()
}

func testConnectTunnel(t *testing.T, runtime Runtime, target string) {
	t.Helper()
	downstream := httptest.NewServer(runtime.Server.Handler)
	defer downstream.Close()
	parsed, _ := url.Parse(downstream.URL)
	conn, err := net.DialTimeout("tcp", parsed.Host, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal(response, err)
	}
	assertEcho(t, conn, reader)
}

func testSOCKSTunnel(t *testing.T, runtime Runtime, target string) {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		runtime.SOCKS.Serve(t.Context(), server)
		close(done)
	}()
	defer func() { _ = client.Close() }()
	_, _ = client.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatal(reply, err)
	}
	host, rawPort, _ := net.SplitHostPort(target)
	ip := net.ParseIP(host).To4()
	port := parsePort(t, rawPort)
	request := []byte{5, 1, 0, 1, ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)}
	_, _ = client.Write(request)
	reply = make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatal(reply, err)
	}
	assertEcho(t, client, client)
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SOCKS tunnel did not close")
	}
}

func assertEcho(t *testing.T, writer io.Writer, reader io.Reader) {
	t.Helper()
	if _, err := writer.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 4)
	if _, err := io.ReadFull(reader, payload); err != nil || string(payload) != "pong" {
		t.Fatal(string(payload), err)
	}
}
