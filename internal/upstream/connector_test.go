package upstream

import (
	"bufio"
	"context"
	"crypto/tls"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type credentials struct{ values map[secret.Ref]Credentials }

func (c credentials) ResolveCredentials(_ context.Context, ref secret.Ref) (Credentials, error) {
	v, ok := c.values[ref]
	if !ok {
		return Credentials{}, ErrCredentials
	}
	return v, nil
}
func fakeCredentials(t *testing.T) Credentials {
	t.Helper()
	u, _ := secret.New([]byte("demo-user"))
	p, _ := secret.New([]byte("fake-password"))
	return Credentials{Username: u, Password: p}
}
func endpoint(protocol proxy.Protocol, address string, ref secret.Ref) proxy.Endpoint {
	host, port, _ := net.SplitHostPort(address)
	p, _ := net.LookupPort("tcp", port)
	return proxy.Endpoint{ID: "upstream", Name: "upstream", Protocol: protocol, Host: host, Port: uint16(p), CredentialRef: ref, Enabled: true}
}
func TestHTTPConnect(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	creds := fakeCredentials(t)
	var got string
	done := make(chan struct{})
	go func() {
		defer close(done)
		r := bufio.NewReader(server)
		line, _ := r.ReadString('\n')
		if !strings.HasPrefix(line, "CONNECT target.example.invalid:443 ") {
			return
		}
		for {
			line, _ = r.ReadString('\n')
			if strings.HasPrefix(line, "Proxy-Authorization:") {
				got = strings.TrimSpace(strings.TrimPrefix(line, "Proxy-Authorization:"))
			}
			if line == "\r\n" {
				break
			}
		}
		_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\n\r\nreply")
	}()
	c := Connector{Credentials: credentials{map[secret.Ref]Credentials{"secret://upstream/auth": creds}}, DialContext: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
	conn, err := c.Connect(context.Background(), endpoint(proxy.HTTP, "proxy.example.invalid:8080", "secret://upstream/auth"), "target.example.invalid:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	b := make([]byte, 5)
	if _, err = io.ReadFull(conn, b); err != nil || string(b) != "reply" {
		t.Fatal(string(b), err)
	}
	<-done
	if !strings.HasPrefix(got, "Basic ") || strings.Contains(got, "fake-password") {
		t.Fatal("bad auth", got)
	}
}
func TestHTTPConnectAuthenticationAndProtocolFailures(t *testing.T) {
	for _, response := range []string{"HTTP/1.1 407 Proxy Authentication Required\r\n\r\n", "bad\r\n\r\n", "HTTP/1.1 200 OK\r\n Broken\r\n\r\n"} {
		client, server := net.Pipe()
		go func() {
			defer func() { _ = server.Close() }()
			_, _ = bufio.NewReader(server).ReadString('\n')
			_, _ = io.WriteString(server, response)
		}()
		c := Connector{DialContext: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
		if conn, err := c.Connect(context.Background(), endpoint(proxy.HTTP, "proxy.example.invalid:8080", ""), "target.example.invalid:443"); err == nil {
			_ = conn.Close()
			t.Fatal("accepted", response)
		}
	}
}
func TestSOCKS5AndRemoteDNS(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	done := make(chan []byte, 1)
	go func() {
		defer func() { _ = server.Close() }()
		b := make([]byte, 3)
		_, _ = io.ReadFull(server, b)
		if string(b) != "\x05\x01\x00" {
			done <- b
			return
		}
		_, _ = server.Write([]byte{5, 0})
		request := make([]byte, 5)
		_, _ = io.ReadFull(server, request)
		length := int(request[4])
		rest := make([]byte, length+2)
		_, _ = io.ReadFull(server, rest)
		done <- append(request, rest...)
		_, _ = server.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
	}()
	c := Connector{DialContext: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
	conn, err := c.Connect(context.Background(), endpoint(proxy.SOCKS5H, "proxy.example.invalid:1080", ""), "target.example.invalid:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	request := <-done
	if request[3] != 3 || string(request[5:5+request[4]]) != "target.example.invalid" {
		t.Fatal(request)
	}
}
func TestSOCKSAuthentication(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	creds := fakeCredentials(t)
	done := make(chan []byte, 1)
	go func() {
		defer func() { _ = server.Close() }()
		greeting := make([]byte, 3)
		_, _ = io.ReadFull(server, greeting)
		_, _ = server.Write([]byte{5, 2})
		head := make([]byte, 2)
		_, _ = io.ReadFull(server, head)
		user := make([]byte, head[1])
		_, _ = io.ReadFull(server, user)
		passwordLength := []byte{0}
		_, _ = io.ReadFull(server, passwordLength)
		password := make([]byte, passwordLength[0])
		_, _ = io.ReadFull(server, password)
		done <- append(append(append(head, user...), passwordLength...), password...)
		_, _ = server.Write([]byte{1, 0})
		request := make([]byte, 10)
		_, _ = io.ReadFull(server, request)
		_, _ = server.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
	}()
	c := Connector{Credentials: credentials{map[secret.Ref]Credentials{"secret://upstream/auth": creds}}, DialContext: func(context.Context, string, string) (net.Conn, error) { return client, nil }, Resolver: resolverFunc(func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.1")}, nil
	})}
	conn, err := c.Connect(context.Background(), endpoint(proxy.SOCKS5, "proxy.example.invalid:1080", "secret://upstream/auth"), "target.example.invalid:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	auth := <-done
	if !strings.Contains(string(auth), "demo-user") || !strings.Contains(string(auth), "fake-password") {
		t.Fatal("missing auth")
	}
}

type resolverFunc func(context.Context, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return f(ctx, host)
}
func TestHTTPTransportUsesProxyAndNeverOriginProxyCredential(t *testing.T) {
	var auth string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = r.Header.Get("Proxy-Authorization")
		mu.Unlock()
		if r.URL.String() != "http://origin.example.invalid/path" {
			t.Errorf("target %s", r.URL)
		}
		_, _ = io.WriteString(w, "through upstream")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	creds := fakeCredentials(t)
	transport, err := NewHTTPTransport(endpoint(proxy.HTTP, u.Host, ""), creds, Connector{})
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, "http://origin.example.invalid/path", nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if string(body) != "through upstream" || !strings.HasPrefix(auth, "Basic ") || strings.Contains(auth, "fake-password") {
		t.Fatalf("%s %q", body, auth)
	}
}
func TestHTTPSUpstreamTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Error("wrong method")
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	config := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	c := Connector{TLSConfig: &tls.Config{RootCAs: config.RootCAs, MinVersion: tls.VersionTLS12}}
	conn, err := c.Connect(context.Background(), endpoint(proxy.HTTPS, u.Host, ""), "target.example.invalid:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}
