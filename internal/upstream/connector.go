// Package upstream implements protocol-specific upstream proxy adapters.
package upstream

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
)

var (
	ErrCredentials = errors.New("upstream proxy credentials are unavailable")
	ErrConnect     = errors.New("upstream proxy connection failed")
	ErrProtocol    = errors.New("upstream proxy protocol failed")
)

const maxResponseHeaderBytes = 32 << 10

type Credentials struct {
	Username secret.Value
	Password secret.Value
}

func (c Credentials) Valid() bool { return !c.Username.Empty() && !c.Password.Empty() }

type CredentialResolver interface {
	ResolveCredentials(context.Context, secret.Ref) (Credentials, error)
}
type Lookup interface {
	LookupNetIP(context.Context, string) ([]netip.Addr, error)
}
type Connector struct {
	Credentials    CredentialResolver
	Resolver       Lookup
	DialContext    func(context.Context, string, string) (net.Conn, error)
	TLSConfig      *tls.Config
	ConnectTimeout time.Duration
}

func (c Connector) Connect(ctx context.Context, endpoint proxy.Endpoint, target string) (net.Conn, error) {
	if endpoint.Validate() != nil {
		return nil, ErrConnect
	}
	if _, _, err := net.SplitHostPort(target); err != nil {
		return nil, ErrConnect
	}
	switch endpoint.Protocol {
	case proxy.HTTP, proxy.HTTPS:
		return c.connectHTTP(ctx, endpoint, target)
	case proxy.SOCKS5, proxy.SOCKS5H:
		return c.connectSOCKS(ctx, endpoint, target)
	default:
		return nil, ErrConnect
	}
}
func (c Connector) dial(ctx context.Context, address string) (net.Conn, error) {
	if c.DialContext != nil {
		return c.DialContext(ctx, "tcp", address)
	}
	timeout := c.ConnectTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	return (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", address)
}
func (c Connector) credentials(ctx context.Context, ref secret.Ref) (Credentials, error) {
	if ref == "" {
		return Credentials{}, nil
	}
	if c.Credentials == nil {
		return Credentials{}, ErrCredentials
	}
	v, err := c.Credentials.ResolveCredentials(ctx, ref)
	if err != nil || !v.Valid() {
		return Credentials{}, ErrCredentials
	}
	return v, nil
}
func (c Connector) connectHTTP(ctx context.Context, endpoint proxy.Endpoint, target string) (net.Conn, error) {
	conn, err := c.dial(ctx, endpoint.Address())
	if err != nil {
		return nil, ErrConnect
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = conn.Close()
		}
	}()
	if endpoint.Protocol == proxy.HTTPS {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Host}
		if c.TLSConfig != nil {
			cfg = c.TLSConfig.Clone()
			if cfg.ServerName == "" {
				cfg.ServerName = endpoint.Host
			}
		}
		tlsConn := tls.Client(conn, cfg)
		if err = tlsConn.HandshakeContext(ctx); err != nil {
			return nil, ErrConnect
		}
		conn = tlsConn
	}
	creds, err := c.credentials(ctx, endpoint.CredentialRef)
	if err != nil {
		return nil, err
	}
	writer := bufio.NewWriter(conn)
	if _, err = fmt.Fprintf(writer, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target); err != nil {
		return nil, ErrConnect
	}
	if creds.Valid() {
		if _, err = fmt.Fprintf(writer, "Proxy-Authorization: %s\r\n", basic(creds)); err != nil {
			return nil, ErrConnect
		}
	}
	if _, err = writer.WriteString("Connection: keep-alive\r\n\r\n"); err != nil {
		return nil, ErrConnect
	}
	if err = writer.Flush(); err != nil {
		return nil, ErrConnect
	}
	reader := bufio.NewReader(conn)
	status, err := readConnectResponse(reader)
	if err != nil || status < 200 || status > 299 {
		return nil, ErrProtocol
	}
	closeOnError = false
	return &bufferedConn{Conn: conn, reader: reader}, nil
}
func basic(creds Credentials) string {
	user := creds.Username.Reveal()
	pass := creds.Password.Reveal()
	defer wipe(user)
	defer wipe(pass)
	raw := append(append(user, ':'), pass...)
	defer wipe(raw)
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "Basic " + encoded
}
func readConnectResponse(reader *bufio.Reader) (int, error) {
	used := 0
	line, err := readHeaderLine(reader, &used)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0, ErrProtocol
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, ErrProtocol
	}
	for {
		line, err = readHeaderLine(reader, &used)
		if err != nil {
			return 0, err
		}
		if line == "" {
			return status, nil
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || !strings.Contains(line, ":") {
			return 0, ErrProtocol
		}
	}
}
func readHeaderLine(reader *bufio.Reader, used *int) (string, error) {
	line, err := reader.ReadString('\n')
	*used += len(line)
	if err != nil || *used > maxResponseHeaderBytes {
		return "", ErrProtocol
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}
func wipe(bytes []byte) {
	for i := range bytes {
		bytes[i] = 0
	}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.reader.Read(b) }

var _ io.Reader = (*bufferedConn)(nil)
