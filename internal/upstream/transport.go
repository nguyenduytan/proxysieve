package upstream

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

type HTTPTransport struct {
	endpoint    proxy.Endpoint
	credentials Credentials
	connector   Connector
	transport   *http.Transport
}

func NewHTTPTransport(endpoint proxy.Endpoint, credentials Credentials, connector Connector) (*HTTPTransport, error) {
	if endpoint.Validate() != nil {
		return nil, ErrConnect
	}
	t := &http.Transport{ForceAttemptHTTP2: false, MaxIdleConns: 64, MaxIdleConnsPerHost: 8, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	switch endpoint.Protocol {
	case proxy.HTTP, proxy.HTTPS:
		u := &url.URL{Scheme: string(endpoint.Protocol), Host: endpoint.Address()}
		if credentials.Valid() {
			user := credentials.Username.Reveal()
			pass := credentials.Password.Reveal()
			u.User = url.UserPassword(string(user), string(pass))
			wipe(user)
			wipe(pass)
		}
		t.Proxy = http.ProxyURL(u)
	case proxy.SOCKS5, proxy.SOCKS5H:
		t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return connector.Connect(ctx, endpoint, address)
		}
	default:
		return nil, ErrConnect
	}
	return &HTTPTransport{endpoint: endpoint, credentials: credentials, connector: connector, transport: t}, nil
}
func (t *HTTPTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.transport.RoundTrip(r)
}
func (t *HTTPTransport) CloseIdleConnections() { t.transport.CloseIdleConnections() }

var _ http.RoundTripper = (*HTTPTransport)(nil)
