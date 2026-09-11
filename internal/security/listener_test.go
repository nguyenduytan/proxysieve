package security

import (
	"net"
	"testing"
)

func TestLimitedListener(t *testing.T) {
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	listener, err := NewLimitedListener(base, 1)
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
	_ = first.Close()
	_ = accepted.Close()
	second, err := net.Dial("tcp", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	next, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	_ = next.Close()
	if _, err := NewLimitedListener(base, 0); err == nil {
		t.Fatal("invalid limit accepted")
	}
}
