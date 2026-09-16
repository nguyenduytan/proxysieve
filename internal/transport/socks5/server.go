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

	internalbudget "github.com/nguyenduytan/proxysieve/internal/budget"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	retrypkg "github.com/nguyenduytan/proxysieve/pkg/retry"
	trafficpkg "github.com/nguyenduytan/proxysieve/pkg/traffic"
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
	Evaluator gateway.Evaluator
	Router    gateway.Router
	Recorder  trafficpkg.Recorder
	// AuthenticatePassword validates one RFC1929 username/password exchange
	// and returns the opaque downstream client identity.
	AuthenticatePassword func(context.Context, string, string) (model.ID, error)
	IdleTimeout          time.Duration
}
type Server struct {
	evaluator gateway.Evaluator
	router    gateway.Router
	recorder  trafficpkg.Recorder
	auth      func(context.Context, string, string) (model.ID, error)
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
	return &Server{evaluator: options.Evaluator, router: options.Router, recorder: options.Recorder, auth: options.AuthenticatePassword, idle: options.IdleTimeout}, nil
}

// Serve accepts one downstream connection. No-auth is only used by a listener
// already restricted to local/trusted scope by config validation.
func (s *Server) Serve(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(s.idle))
	reader := bufio.NewReader(conn)
	clientID, ok := negotiate(ctx, reader, conn, s.auth)
	if !ok {
		return
	}
	host, port, ok := readRequest(reader)
	if !ok {
		return
	}
	request := policy.RequestContext{RequestID: model.NewID(), ConnectionID: model.NewID(), ClientID: clientID, Listener: "socks", Protocol: "socks5", Host: host, Port: port, Timestamp: time.Now().UTC()}
	result, err := s.evaluator.Evaluate(ctx, request, policy.Visibility{Host: true})
	if err != nil {
		recordTunnel(s.recorder, ctx, request, gateway.Route{Action: "reject", PolicyID: result.PolicyID, RuleID: result.TerminalRuleID}, 2, 0, 0, 0, 0)
		writeReply(conn, 2)
		return
	}
	route, err := s.router.Route(ctx, request, result)
	route.PolicyID, route.RuleID = result.PolicyID, result.TerminalRuleID
	if err != nil || route.Dial == nil {
		if route.Action != "block" && route.Action != "reject" {
			route.Action = "reject"
		}
		recordTunnel(s.recorder, ctx, request, route, 2, 0, 0, 0, 0)
		writeReply(conn, 2)
		return
	}
	if err = internalbudget.Available(ctx, route.Reserve); err != nil {
		route.Action = "budget_reject"
		recordTunnel(s.recorder, ctx, request, route, 2, 0, 0, 0, 0)
		writeReply(conn, 2)
		return
	}
	if route.Acquire != nil && !route.Acquire() {
		recordTunnel(s.recorder, ctx, request, route, 5, 0, 0, 0, 0)
		writeReply(conn, 5)
		return
	}
	var upstream net.Conn
	attempt := uint8(1)
	retryPolicy := retrypkg.DefaultPolicy()
	if route.RetryPolicy != nil {
		retryPolicy = *route.RetryPolicy
	}
	for {
		started := time.Now()
		upstream, err = route.Dial(ctx, net.JoinHostPort(host, strconv.Itoa(int(port))))
		latency := time.Since(started)
		if err == nil {
			if route.Observe != nil {
				route.Observe(publichealth.Observation{Success: true, Latency: latency, ConnectLatency: latency})
			}
			break
		}
		if route.Observe != nil {
			route.Observe(publichealth.Observation{Success: false, Latency: latency, ConnectLatency: latency, Cause: errors.Join(err, ctx.Err())})
		}
		recordTunnel(s.recorder, ctx, request, route, 5, 0, 0, 0, 0)
		if route.Retry == nil || attempt >= retryPolicy.MaxAttempts || retrypkg.Wait(ctx, attempt) != nil {
			break
		}
		next, retryErr := route.Retry(ctx)
		if retryErr != nil || next.Dial == nil || internalbudget.Available(ctx, next.Reserve) != nil || next.Acquire != nil && !next.Acquire() {
			break
		}
		next.PolicyID, next.RuleID = result.PolicyID, result.TerminalRuleID
		route = next
		attempt++
	}
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
	clientUpload := &internaltraffic.Reader{Source: reader}
	upstreamUpload := &internaltraffic.Writer{Destination: &internalbudget.Writer{Context: ctx, Destination: upstream, Reserve: route.Reserve}}
	upstreamDownload := &internaltraffic.Reader{Source: &internalbudget.Reader{Context: ctx, Source: upstream, Reserve: route.Reserve}}
	clientDownload := &internaltraffic.Writer{Destination: conn}
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(upstreamUpload, clientUpload)
		if writer, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = writer.CloseWrite()
		}
	}()
	go func() {
		defer copies.Done()
		_, _ = io.Copy(clientDownload, upstreamDownload)
		if writer, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = writer.CloseWrite()
		}
	}()
	copies.Wait()
	recordTunnel(s.recorder, ctx, request, route, 0, clientUpload.Bytes(), clientDownload.Bytes(), upstreamUpload.Bytes(), upstreamDownload.Bytes())
}

func recordTunnel(recorder trafficpkg.Recorder, ctx context.Context, request policy.RequestContext, route gateway.Route, status int, clientUpload, clientDownload, routeUpload, routeDownload trafficpkg.Bytes) {
	if route.Complete != nil {
		route.Complete(context.WithoutCancel(ctx), routeUpload, routeDownload)
	}
	if recorder == nil {
		return
	}
	direct := trafficpkg.Bytes(0)
	proxyUpload, proxyDownload := trafficpkg.Bytes(0), trafficpkg.Bytes(0)
	switch route.Action {
	case "direct":
		direct, _ = routeUpload.Add(routeDownload)
	case "proxy", "chain":
		proxyUpload, proxyDownload = routeUpload, routeDownload
	}
	cost, _ := trafficpkg.NewCostSnapshot(route.Rate, proxyUpload, proxyDownload)
	_ = recorder.Record(context.WithoutCancel(ctx), trafficpkg.Event{
		At: time.Now().UTC(), RequestID: request.RequestID, ConnectionID: request.ConnectionID,
		ClientID: request.ClientID, PolicyID: route.PolicyID, RuleID: route.RuleID, PoolID: route.PoolID, ProxyID: route.ProxyID, ChainID: route.ChainID,
		Host: request.Host, Protocol: "socks5", Action: route.Action, StatusCode: status,
		ClientUpload: clientUpload, ClientDownload: clientDownload,
		UpstreamUpload: proxyUpload, UpstreamDownload: proxyDownload, Direct: direct, ConfiguredCost: cost,
	})
}
func negotiate(ctx context.Context, reader *bufio.Reader, conn net.Conn, authenticate func(context.Context, string, string) (model.ID, error)) (model.ID, bool) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != version5 || header[1] == 0 {
		return "", false
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(reader, methods); err != nil {
		return "", false
	}
	selected := byte(noAcceptableMethod)
	if authenticate != nil {
		for _, method := range methods {
			if method == 2 {
				selected = 2
				break
			}
		}
	} else {
		for _, method := range methods {
			if method == noAuth {
				selected = noAuth
				break
			}
		}
	}
	if _, err := conn.Write([]byte{version5, selected}); err != nil {
		return "", false
	}
	if selected == noAuth {
		return "local-socks", true
	}
	if selected != 2 {
		return "", false
	}
	username, password, ok := readPassword(reader)
	if !ok {
		return "", false
	}
	clientID, err := authenticate(ctx, username, password)
	status := byte(1)
	if err == nil && clientID.Valid() {
		status = 0
	}
	if _, writeErr := conn.Write([]byte{1, status}); writeErr != nil || status != 0 {
		return "", false
	}
	return clientID, true
}

func readPassword(reader *bufio.Reader) (string, string, bool) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != 1 || header[1] == 0 {
		return "", "", false
	}
	username := make([]byte, header[1])
	if _, err := io.ReadFull(reader, username); err != nil {
		return "", "", false
	}
	length := []byte{0}
	if _, err := io.ReadFull(reader, length); err != nil || length[0] == 0 {
		return "", "", false
	}
	password := make([]byte, length[0])
	if _, err := io.ReadFull(reader, password); err != nil {
		return "", "", false
	}
	resultUser, resultPassword := string(username), string(password)
	for i := range username {
		username[i] = 0
	}
	for i := range password {
		password[i] = 0
	}
	return resultUser, resultPassword, true
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
