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
)

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
	s, err := New(Options{Evaluator: eval(func(_ context.Context, r policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
		done <- r
		return policy.Result{Actions: []policy.Action{{Type: "direct"}}}, nil
	}), Router: route(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{Action: "direct", Dial: func(context.Context, string) (net.Conn, error) { return targetServer, nil }}, nil
	})})
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
	go func() { _, _ = targetClient.Write([]byte("ok")) }()
	b := make([]byte, 2)
	if _, err = io.ReadFull(client, b); err != nil || string(b) != "ok" {
		t.Fatal(string(b), err)
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
