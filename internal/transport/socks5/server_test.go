package socks5

import (
	"bufio"
	"context"
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
	recorded := make(eventRecorder, 1)
	s, err := New(Options{Evaluator: eval(func(_ context.Context, r policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
		done <- r
		return policy.Result{Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}, nil
	}), Router: route(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{Action: "proxy", PoolID: "pool", ProxyID: "proxy", Rate: &trafficpkg.Rate{Price: trafficpkg.Money{Currency: "USD", Micros: 1_000_000_000}, Unit: trafficpkg.GB, EffectiveAt: time.Unix(0, 0)}, Dial: func(context.Context, string) (net.Conn, error) { return targetServer, nil }}, nil
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

var _ = bufio.Reader{}
