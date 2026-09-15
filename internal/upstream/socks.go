package upstream

import (
	"context"
	"encoding/binary"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"io"
	"net"
	"net/netip"
	"strconv"
)

func (c Connector) connectSOCKS(ctx context.Context, endpoint proxy.Endpoint, target string) (net.Conn, error) {
	conn, err := c.dial(ctx, endpoint.Address())
	if err != nil {
		return nil, ErrConnect
	}
	clearDeadline := applyContextDeadline(ctx, conn)
	defer clearDeadline()
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = conn.Close()
		}
	}()
	creds, err := c.credentials(ctx, endpoint.CredentialRef)
	if err != nil {
		return nil, err
	}
	if err = socksGreeting(conn, creds); err != nil {
		return nil, err
	}
	host, portRaw, err := net.SplitHostPort(target)
	if err != nil {
		return nil, ErrConnect
	}
	port64, err := strconv.ParseUint(portRaw, 10, 16)
	if err != nil || port64 == 0 {
		return nil, ErrConnect
	}
	address, err := c.socksAddress(ctx, endpoint.Protocol, host)
	if err != nil {
		return nil, err
	}
	request := append([]byte{5, 1, 0}, address...)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, uint16(port64))
	request = append(request, port...)
	if _, err = conn.Write(request); err != nil {
		return nil, ErrConnect
	}
	if err = socksResponse(conn); err != nil {
		return nil, err
	}
	closeOnError = false
	return conn, nil
}
func socksGreeting(conn net.Conn, creds Credentials) error {
	methods := []byte{0}
	if creds.Valid() {
		methods = []byte{2}
	}
	if _, err := conn.Write(append([]byte{5, byte(len(methods))}, methods...)); err != nil {
		return ErrConnect
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil || reply[0] != 5 {
		return ErrProtocol
	}
	if reply[1] == 255 {
		return ErrCredentials
	}
	if reply[1] == 2 {
		if !creds.Valid() {
			return ErrCredentials
		}
		user := creds.Username.Reveal()
		pass := creds.Password.Reveal()
		defer wipe(user)
		defer wipe(pass)
		if len(user) > 255 || len(pass) > 255 {
			return ErrCredentials
		}
		auth := append([]byte{1, byte(len(user))}, user...)
		auth = append(auth, byte(len(pass)))
		auth = append(auth, pass...)
		if _, err := conn.Write(auth); err != nil {
			return ErrConnect
		}
		if _, err := io.ReadFull(conn, reply); err != nil || reply[0] != 1 || reply[1] != 0 {
			return ErrCredentials
		}
	} else if reply[1] != 0 {
		return ErrProtocol
	}
	return nil
}
func (c Connector) socksAddress(ctx context.Context, protocol proxy.Protocol, host string) ([]byte, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return socksIP(ip), nil
	}
	if protocol == proxy.SOCKS5H {
		if len(host) == 0 || len(host) > 255 {
			return nil, ErrProtocol
		}
		return append([]byte{3, byte(len(host))}, []byte(host)...), nil
	}
	if c.Resolver == nil {
		return nil, ErrConnect
	}
	ips, err := c.Resolver.LookupNetIP(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, ErrConnect
	}
	return socksIP(ips[0]), nil
}
func socksIP(ip netip.Addr) []byte {
	ip = ip.Unmap()
	if ip.Is4() {
		v := ip.As4()
		return append([]byte{1}, v[:]...)
	}
	v := ip.As16()
	return append([]byte{4}, v[:]...)
}
func socksResponse(conn net.Conn) error {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil || header[0] != 5 || header[1] != 0 {
		return ErrProtocol
	}
	var count int
	switch header[3] {
	case 1:
		count = 4
	case 4:
		count = 16
	case 3:
		n := []byte{0}
		if _, err := io.ReadFull(conn, n); err != nil {
			return ErrProtocol
		}
		count = int(n[0])
	default:
		return ErrProtocol
	}
	discard := make([]byte, count+2)
	if _, err := io.ReadFull(conn, discard); err != nil {
		return ErrProtocol
	}
	return nil
}
