package security

import (
	"net"
	"testing"
	"time"
)

func TestLimitedListener(t *testing.T) {
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	metrics := &ListenerMetrics{}
	listener, err := NewLimitedListener(base, 1, metrics)
	if err != nil {
		t.Fatal(err)
	}
	first, err := net.Dial("tcp", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if stats := metrics.Snapshot(); stats.Accepted != 1 || stats.Active != 1 || stats.Rejected != 0 {
		t.Fatal(stats)
	}
	acceptedNext := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		acceptedNext <- connection
	}()
	second, err := net.Dial("tcp", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	deadline := time.Now().Add(time.Second)
	for metrics.Snapshot().Rejected == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if stats := metrics.Snapshot(); stats.Rejected != 1 || stats.Active != 1 {
		t.Fatal(stats)
	}
	_ = first.Close()
	_ = accepted.Close()
	third, err := net.Dial("tcp", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = third.Close() }()
	var next net.Conn
	select {
	case next = <-acceptedNext:
	case err = <-acceptErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("listener did not accept after a slot was released")
	}
	_ = next.Close()
	if stats := metrics.Snapshot(); stats.Accepted != 2 || stats.Active != 0 || stats.Rejected != 1 {
		t.Fatal(stats)
	}
	if _, err := NewLimitedListener(base, 0, nil); err == nil {
		t.Fatal("invalid limit accepted")
	}
}
