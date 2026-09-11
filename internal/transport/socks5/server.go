// Package socks5 implements the downstream SOCKS5 TCP CONNECT listener.
package socks5

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
)

const (
	version5           = 5
	connectCommand     = 1
	noAuth             = 0
	noAcceptableMethod = 255
	addressIPv4        = 1
	addressDomain      = 3
	addressIPv6        = 4
)

type Evaluator gateway.Evaluator
type Router gateway.Router

type Options struct {
	Evaluator   gateway.Evaluator
	Router      gateway.Router
	IdleTimeout time.Duration
}
type Server struct {
	evaluator gateway.Evaluator
	router    gateway.Router
	idle      time.Duration
}

func New(options Options) (*Server, error) {
	if options.Evaluator == nil || options.Router == nil {
		return nil, errors.New("socks5 dependencies are required")
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 2 * time.Minute
	}
	if options.IdleTimeout > 24*time.Hour {
		return nil, errors.New("invalid socks5 idle timeout")
	}
	return &Server{evaluator: options.Evaluator, router: options.Router, idle: options.IdleTimeout}, nil
}

// Serve accepts one downstream connection. No-auth is only used by a listener
// already restricted to local/trusted scope by config validation. Password auth is
// deliberately rejected until client identity storage is implemented in M11.
func (s *Server) Serve(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(s.idle))
	reader := bufio.NewReader(conn)
	if !negotiate(reader, conn) {
		return
	}
	host, port, ok := readRequest(reader)
	if !ok {
		return
	}
	request := policy.RequestContext{RequestID: model.NewID(), ConnectionID: model.NewID(), Listener: "socks", Protocol: "socks5", Host: host, Port: port, Timestamp: time.Now().UTC()}
	result, err := s.evaluator.Evaluate(ctx, request, policy.Visibility{Host: true})
	if err != nil {
		writeReply(conn, 2)
		return
	}
	route, err := s.router.Route(ctx, request, result)
	if err != nil || route.Dial == nil {
		writeReply(conn, 2)
		return
	}
	upstream, err := route.Dial(ctx, net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		writeReply(conn, 5)
		return
	}
	defer func() { _ = upstream.Close() }()
	if !writeReply(conn, 0) {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	var copies sync.WaitGroup
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(upstream, reader)
		if writer, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = writer.CloseWrite()
		}
	}()
	go func() {
		defer copies.Done()
		_, _ = io.Copy(conn, upstream)
		if writer, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = writer.CloseWrite()
		}
	}()
	copies.Wait()
}
func negotiate(reader *bufio.Reader, conn net.Conn) bool {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != version5 || header[1] == 0 {
		return false
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(reader, methods); err != nil {
		return false
	}
	selected := byte(noAcceptableMethod)
	for _, method := range methods {
		if method == noAuth {
			selected = noAuth
			break
		}
	}
	if _, err := conn.Write([]byte{version5, selected}); err != nil {
		return false
	}
	return selected == noAuth
}
func readRequest(reader *bufio.Reader) (string, uint16, bool) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != version5 || header[1] != connectCommand || header[2] != 0 {
		return "", 0, false
	}
	var host string
	switch header[3] {
	case addressIPv4:
		b := make([]byte, 4)
		if _, err := io.ReadFull(reader, b); err != nil {
			return "", 0, false
		}
		host = netip.AddrFrom4([4]byte(b)).String()
	case addressIPv6:
		b := make([]byte, 16)
		if _, err := io.ReadFull(reader, b); err != nil {
			return "", 0, false
		}
		var bytes [16]byte
		copy(bytes[:], b)
		host = netip.AddrFrom16(bytes).String()
	case addressDomain:
		length := []byte{0}
		if _, err := io.ReadFull(reader, length); err != nil || length[0] == 0 {
			return "", 0, false
		}
		b := make([]byte, length[0])
		if _, err := io.ReadFull(reader, b); err != nil {
			return "", 0, false
		}
		host = string(b)
	default:
		return "", 0, false
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return "", 0, false
	}
	port := uint16(portBytes[0])<<8 | uint16(portBytes[1])
	return host, port, port > 0
}
func writeReply(conn net.Conn, code byte) bool {
	_, err := conn.Write([]byte{version5, code, 0, addressIPv4, 0, 0, 0, 0, 0, 0})
	return err == nil
}
