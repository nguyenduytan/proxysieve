package cache

import (
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/cache"
)

// ResponseStore is the shared boundary for process-local and persistent response caches.
type ResponseStore interface {
	Kind() string
	Get(public.Key, time.Time) (Entry, bool)
	RecordBypass()
	Put(public.Key, Entry) bool
	Stats(time.Time) Stats
	Purge() PurgeResult
	PurgeDomain(string) PurgeResult
}
