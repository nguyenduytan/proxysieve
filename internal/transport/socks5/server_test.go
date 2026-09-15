package socks5

import (
	"bufio"
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"io"
	"net"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	trafficpkg "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

type eventRecorder chan trafficpkg.Event

func (r eventRecorder) Record(_ context.Context, event trafficpkg.Event) error {
	r <- event
	return nil
}

type eval func(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error)

func (e eval) Evaluate(ctx context.Context, r policy.RequestContext, v policy.Visibility) (policy.Result, error) {
	return e(ctx, r, v)
}

type route func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error)

func (r route) Route(ctx context.Context, q policy.RequestContext, p policy.Result) (gateway.Route, error) {
	return r(ctx, q, p)
}
func TestConnect(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	targetClient, targetServer := net.Pipe()
	defer func() { _ = targetClient.Close() }()
	done := make(chan policy.RequestContext, 1)
	observed := make(chan bool, 1)
	recorded := make(eventRecorder, 1)
	s, err := New(Options{Evaluator: eval(func(_ context.Context, r policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
		done <- r
		return policy.Result{Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}, nil
	}), Router: route(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{Action: "proxy", PoolID: "pool", ProxyID: "proxy", Rate: &trafficpkg.Rate{Price: trafficpkg.Money{Currency: "USD", Micros: 1_000_000_000}, Unit: trafficpkg.GB, EffectiveAt: time.Unix(0, 0)}, Dial: func(context.Context, string) (net.Conn, error) { return targetServer, nil }, Observe: func(success bool, _ int, _ time.Duration) { observed <- success }}, nil
	}), Recorder: recorded})
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(context.Background(), server)
	if _, err = client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err = io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatal(reply, err)
	}
	if _, err = client.Write([]byte{5, 1, 0, 3, 4, 't', 'e', 's', 't', 1, 187}); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 10)
	if _, err = io.ReadFull(client, response); err != nil || response[1] != 0 {
		t.Fatal(response, err)
	}
	request := <-done
	if request.Host != "test" || request.Port != 443 {
		t.Fatal(request)
	}
	if success := <-observed; !success {
		t.Fatal("successful SOCKS5 dial was reported as failed")
	}
	go func() { _, _ = client.Write([]byte("ping")) }()
	upload := make([]byte, 4)
	if _, err = io.ReadFull(targetClient, upload); err != nil || string(upload) != "ping" {
		t.Fatal(string(upload), err)
	}
	go func() { _, _ = targetClient.Write([]byte("ok")) }()
	b := make([]byte, 2)
	if _, err = io.ReadFull(client, b); err != nil || string(b) != "ok" {
		t.Fatal(string(b), err)
	}
	_ = targetClient.Close()
	_ = client.Close()
	select {
	case event := <-recorded:
		if event.Protocol != "socks5" || event.Action != "proxy" || event.PoolID != model.ID("pool") || event.ProxyID != model.ID("proxy") || event.ClientUpload != 4 || event.ClientDownload != 2 || event.UpstreamUpload != 4 || event.UpstreamDownload != 2 || event.Direct != 0 || event.ConfiguredCost == nil || event.ConfiguredCost.Amount != (trafficpkg.Money{Currency: "USD", Micros: 6}) {
			t.Fatal(event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SOCKS5 traffic event was not recorded")
	}
}

func TestConnectRetriesBeforeReply(t *testing.T) {
	targetClient, targetServer := net.Pipe()
	defer func() { _ = targetClient.Close() }()
	observed := make(chan bool, 2)
	recorded := make(eventRecorder, 2)
	s, err := New(Options{Evaluator: eval(func(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error) {
		return policy.Result{Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}, nil
	}), Router: route(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{
			Action: "proxy", PoolID: "pool", ProxyID: "first", Dial: func(context.Context, string) (net.Conn, error) { return nil, errors.New("first proxy failed") },
			Observe: func(success bool, _ int, _ time.Duration) { observed <- success },
			Retry: func(context.Context) (gateway.Route, error) {
				return gateway.Route{Action: "proxy", PoolID: "pool", ProxyID: "second", Dial: func(context.Context, string) (net.Conn, error) { return targetServer, nil }, Observe: func(success bool, _ int, _ time.Duration) { observed <- success }}, nil
			},
		}, nil
	}), Recorder: recorded})
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() { s.Serve(context.Background(), server); close(done) }()
	if _, err = client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err = io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatal(reply, err)
	}
	if _, err = client.Write([]byte{5, 1, 0, 3, 4, 't', 'e', 's', 't', 1, 187}); err != nil {
		t.Fatal(err)
	}
	reply = make([]byte, 10)
	if _, err = io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatal(reply, err)
	}
	if first, second := <-observed, <-observed; first || !second {
		t.Fatal(first, second)
	}
	_ = client.Close()
	_ = targetClient.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry tunnel did not close")
	}
	first, second := <-recorded, <-recorded
	if first.ProxyID != "first" || first.StatusCode != 5 || second.ProxyID != "second" || second.StatusCode != 0 || first.RequestID != second.RequestID || first.ConnectionID != second.ConnectionID {
		t.Fatal(first, second)
	}
}
func TestRejectsUnsupportedMethodsAndRoutes(t *testing.T) {
	for _, input := range [][]byte{{4, 1, 0}, {5, 1, 2}} {
		client, server := net.Pipe()
		go func() {
			defer func() { _ = server.Close() }()
			s, _ := New(Options{Evaluator: eval(func(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error) {
				return policy.Result{}, nil
			}), Router: route(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
				return gateway.Route{}, nil
			})})
			s.Serve(context.Background(), server)
		}()
		_, _ = client.Write(input)
		_ = client.SetReadDeadline(time.Now().Add(time.Second))
		reply := make([]byte, 2)
		_, _ = io.ReadFull(client, reply)
		_ = client.Close()
		if input[0] == 5 && reply[1] != 255 {
			t.Fatal(reply)
		}
	}
}

func TestPasswordAuthenticationSetsClientIdentity(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	gotClient := make(chan model.ID, 1)
	s, err := New(Options{
		Evaluator: eval(func(_ context.Context, request policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
			gotClient <- request.ClientID
			return policy.Result{Actions: []policy.Action{{Type: "reject"}}}, nil
		}),
		Router: route(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{Action: "reject"}, nil
		}),
		AuthenticatePassword: func(_ context.Context, username, password string) (model.ID, error) {
			if username != "alice" || password != "correct horse" {
				return "", errors.New("invalid credentials")
			}
			return "client-alice", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(context.Background(), server)
	if _, err = client.Write([]byte{5, 1, 2}); err != nil {
		t.Fatal(err)
	}
	methodReply := make([]byte, 2)
	if _, err = io.ReadFull(client, methodReply); err != nil || methodReply[0] != 5 || methodReply[1] != 2 {
		t.Fatal(methodReply, err)
	}
	auth := []byte{1, 5, 'a', 'l', 'i', 'c', 'e', 13, 'c', 'o', 'r', 'r', 'e', 'c', 't', ' ', 'h', 'o', 'r', 's', 'e'}
	if _, err = client.Write(auth); err != nil {
		t.Fatal(err)
	}
	authReply := make([]byte, 2)
	if _, err = io.ReadFull(client, authReply); err != nil || authReply[1] != 0 {
		t.Fatal(authReply, err)
	}
	if _, err = client.Write([]byte{5, 1, 0, 3, 4, 't', 'e', 's', 't', 1, 187}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err = io.ReadFull(client, reply); err != nil || reply[1] != 2 {
		t.Fatal(reply, err)
	}
	if got := <-gotClient; got != "client-alice" {
		t.Fatal(got)
	}
}

var _ = bufio.Reader{}
