package cache

import (
	public "github.com/nguyenduytan/proxysieve/pkg/cache"
	"testing"
	"time"
)

func TestPartitionedMemoryCache(t *testing.T) {
	m, _ := NewMemory(2, 32)
	a := public.Key{ClientID: "clienta", SessionHash: "hasha", RouteID: "route", Method: "GET", URL: "https://example.invalid"}
	b := a
	b.ClientID = "clientb"
	entry := Entry{Status: 200, Body: []byte("safe"), ExpiresAt: time.Now().Add(time.Minute)}
	if !m.Put(a, entry) {
		t.Fatal("put")
	}
	if _, ok := m.Get(b, time.Now()); ok {
		t.Fatal("cross client leak")
	}
	got, ok := m.Get(a, time.Now())
	if !ok || string(got.Body) != "safe" {
		t.Fatal(got, ok)
	}
	got.Body[0] = 'x'
	again, _ := m.Get(a, time.Now())
	if string(again.Body) != "safe" {
		t.Fatal("alias")
	}
}
