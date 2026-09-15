package upstream

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
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
	for _, test := range []struct {
		response string
		auth     bool
	}{{"HTTP/1.1 407 Proxy Authentication Required\r\n\r\n", true}, {"bad\r\n\r\n", false}, {"HTTP/1.1 200 OK\r\n Broken\r\n\r\n", false}} {
		client, server := net.Pipe()
		go func() {
			defer func() { _ = server.Close() }()
			_, _ = bufio.NewReader(server).ReadString('\n')
			_, _ = io.WriteString(server, test.response)
		}()
		c := Connector{DialContext: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
		if conn, err := c.Connect(context.Background(), endpoint(proxy.HTTP, "proxy.example.invalid:8080", ""), "target.example.invalid:443"); err == nil {
			_ = conn.Close()
			t.Fatal("accepted", test.response)
		} else if errors.Is(err, ErrCredentials) != test.auth {
			t.Fatal(test.response, err)
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

func TestConnectorPreservesDNSAndTLSFailures(t *testing.T) {
	dnsFailure := &net.DNSError{Err: "lookup failed", Name: "proxy.example.invalid"}
	connector := Connector{DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, dnsFailure }}
	_, err := connector.Connect(t.Context(), endpoint(proxy.HTTP, "proxy.example.invalid:8080", ""), "target.example.invalid:443")
	var dnsError *net.DNSError
	if !errors.Is(err, ErrConnect) || !errors.As(err, &dnsError) {
		t.Fatalf("DNS cause was lost: %v", err)
	}

	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer plain.Close()
	plainURL, _ := url.Parse(plain.URL)
	_, err = (Connector{}).Connect(t.Context(), endpoint(proxy.HTTPS, plainURL.Host, ""), "target.example.invalid:443")
	if !errors.Is(err, ErrConnect) || !errors.Is(err, ErrTLS) {
		t.Fatalf("TLS cause was lost: %v", err)
	}
}

func TestConnectorConnectChainUsesEveryHopInOrder(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	done := make(chan error, 1)
	go func() {
		defer func() { _ = server.Close() }()
		reader := bufio.NewReader(server)
		for _, target := range []string{"second.example.invalid:8081", "target.example.invalid:443"} {
			line, err := reader.ReadString('\n')
			if err != nil || line != "CONNECT "+target+" HTTP/1.1\r\n" {
				done <- fmt.Errorf("unexpected request line %q: %w", line, err)
				return
			}
			for {
				line, err = reader.ReadString('\n')
				if err != nil {
					done <- err
					return
				}
				if line == "\r\n" {
					break
				}
			}
			if _, err = io.WriteString(server, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
				done <- err
				return
			}
		}
		payload := make([]byte, 4)
		if _, err := io.ReadFull(reader, payload); err != nil || string(payload) != "ping" {
			done <- fmt.Errorf("unexpected payload %q: %w", payload, err)
			return
		}
		_, err := io.WriteString(server, "pong")
		done <- err
	}()
	first := endpoint(proxy.HTTP, "first.example.invalid:8080", "")
	first.ID = "first"
	second := endpoint(proxy.HTTP, "second.example.invalid:8081", "")
	second.ID = "second"
	dials := 0
	connector := Connector{DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
		dials++
		if network != "tcp" || address != first.Address() {
			return nil, fmt.Errorf("unexpected dial %s %s", network, address)
		}
		return client, nil
	}}
	connection, err := connector.ConnectChain(t.Context(), []ChainHop{{PoolID: "first-pool", Endpoint: first, Timeout: time.Second}, {PoolID: "second-pool", Endpoint: second, Timeout: time.Second}}, "target.example.invalid:443")
	if err != nil || dials != 1 {
		t.Fatal(connection, err, dials)
	}
	defer func() { _ = connection.Close() }()
	if _, err = io.WriteString(connection, "ping"); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 4)
	if _, err = io.ReadFull(connection, reply); err != nil || string(reply) != "pong" {
		t.Fatal(string(reply), err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConnectorConnectChainReportsFailedHopAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name       string
		responders []string
		timeout    time.Duration
		wantHop    int
	}{
		{name: "second hop rejection", responders: []string{"HTTP/1.1 200 OK\r\n\r\n", "HTTP/1.1 502 Bad Gateway\r\n\r\n"}, timeout: time.Second, wantHop: 1},
		{name: "first hop timeout", timeout: 25 * time.Millisecond, wantHop: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer func() { _ = server.Close() }()
			go func() {
				reader := bufio.NewReader(server)
				for _, response := range tc.responders {
					for {
						line, err := reader.ReadString('\n')
						if err != nil {
							return
						}
						if line == "\r\n" {
							break
						}
					}
					_, _ = io.WriteString(server, response)
				}
			}()
			first := endpoint(proxy.HTTP, "first.example.invalid:8080", "")
			first.ID = "first"
			second := endpoint(proxy.HTTP, "second.example.invalid:8081", "")
			second.ID = "second"
			connector := Connector{DialContext: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
			_, err := connector.ConnectChain(t.Context(), []ChainHop{{PoolID: "first-pool", Endpoint: first, Timeout: tc.timeout}, {PoolID: "second-pool", Endpoint: second, Timeout: tc.timeout}}, "target.example.invalid:443")
			var chainErr *ChainError
			if !errors.As(err, &chainErr) || chainErr.Hop != tc.wantHop || chainErr.EndpointID == "" || chainErr.PoolID == "" {
				t.Fatalf("unexpected chain error: %#v %v", chainErr, err)
			}
		})
	}
}
