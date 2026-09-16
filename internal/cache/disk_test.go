package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/cache"
	"github.com/nguyenduytan/proxysieve/pkg/model"
)

func TestDiskCachePersistsBoundsAndPurges(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	key := func(client, target string) public.Key {
		return public.Key{ClientID: model.ID(client), SessionHash: "hash", RouteID: "route", Method: "GET", URL: target}
	}
	a := key("clienta", "https://example.invalid/a")
	b := key("clientb", "https://other.invalid/b")
	c := key("clientc", "https://sub.example.invalid/c")

	cache, err := NewDisk(dir, 2, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !cache.Put(a, Entry{Body: []byte("old"), ExpiresAt: now.Add(time.Minute)}) || !cache.Put(b, Entry{Body: []byte("bbbb"), ExpiresAt: now.Add(2 * time.Minute)}) {
		t.Fatal("seed disk cache")
	}
	unrelated := filepath.Join(dir, "notes.tmp")
	if err = os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewDisk(dir, 2, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(unrelated); err != nil {
		t.Fatal("disk cache removed unrelated file", err)
	}
	entry, ok := reopened.Get(a, now)
	if !ok || string(entry.Body) != "old" {
		t.Fatal(entry, ok)
	}
	entry.Body[0] = 'x'
	again, _ := reopened.Get(a, now)
	if string(again.Body) != "old" {
		t.Fatal("disk entry aliased")
	}
	if !reopened.Put(c, Entry{Body: []byte("cccc"), ExpiresAt: now.Add(3 * time.Minute)}) {
		t.Fatal("bounded disk put")
	}
	if _, ok := reopened.Get(a, now); ok {
		t.Fatal("earliest disk entry was not evicted")
	}
	removed := reopened.PurgeDomain("OTHER.INVALID.")
	if removed.Entries != 1 || removed.Bytes != 4 {
		t.Fatal(removed)
	}
	stats := reopened.Stats(now)
	if stats.Entries != 1 || stats.Bytes != 4 || stats.Hits != 2 || stats.Misses != 1 || stats.Evictions != 1 || stats.BytesServed != 6 {
		t.Fatalf("unexpected disk stats: %+v", stats)
	}

	path := filepath.Join(dir, diskID(c)+".cache")
	if err = os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Get(c, now); ok || reopened.Stats(now).Entries != 0 {
		t.Fatal("corrupt disk entry was served")
	}
}

func TestDiskCacheRecoversInterruptedReplacement(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewDisk(dir, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	key := public.Key{ClientID: "client", SessionHash: "hash", RouteID: "route", Method: "GET", URL: "https://example.invalid"}
	if !cache.Put(key, Entry{Body: []byte("safe"), ExpiresAt: time.Now().Add(time.Minute)}) {
		t.Fatal("put")
	}
	target := filepath.Join(dir, diskID(key)+".cache")
	if err = os.Rename(target, target+".old"); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewDisk(dir, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := recovered.Get(key, time.Now())
	if !ok || string(entry.Body) != "safe" {
		t.Fatal(entry, ok)
	}
}
