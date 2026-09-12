package traffic

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

var ErrClosed = errors.New("traffic recorder closed")

type BatchStore interface {
	RecordTrafficBatch(context.Context, []public.Event) error
}

type AsyncStats struct {
	Accepted      uint64 `json:"accepted"`
	Written       uint64 `json:"written"`
	QueueDropped  uint64 `json:"queue_dropped"`
	FailedEvents  uint64 `json:"failed_events"`
	WriteFailures uint64 `json:"write_failures"`
	Queued        int    `json:"queued"`
	Stopped       bool   `json:"stopped"`
}

// Async keeps SQLite latency off request handlers. Queue overflow and write
// failures are explicit statistics; this sink never masquerades as budget state.
type Async struct {
	store         BatchStore
	queue         chan public.Event
	batchSize     int
	flushInterval time.Duration
	stop          chan struct{}
	done          chan struct{}
	startOnce     sync.Once
	stopOnce      sync.Once
	mu            sync.RWMutex
	stopped       bool
	accepted      atomic.Uint64
	written       atomic.Uint64
	queueDropped  atomic.Uint64
	failedEvents  atomic.Uint64
	writeFailures atomic.Uint64
}

func NewAsync(store BatchStore, capacity, batchSize int, flushInterval time.Duration) (*Async, error) {
	if store == nil || capacity < 1 || capacity > 1_000_000 || batchSize < 1 || batchSize > capacity || flushInterval <= 0 || flushInterval > time.Minute {
		return nil, ErrFull
	}
	return &Async{store: store, queue: make(chan public.Event, capacity), batchSize: batchSize, flushInterval: flushInterval, stop: make(chan struct{}), done: make(chan struct{})}, nil
}

func (a *Async) Start() {
	a.startOnce.Do(func() { go a.run() })
}

func (a *Async) Record(ctx context.Context, event public.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if event.Validate() != nil {
		return public.ErrInvalidEvent
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.stopped {
		a.queueDropped.Add(1)
		return ErrClosed
	}
	select {
	case a.queue <- event:
		a.accepted.Add(1)
		return nil
	default:
		a.queueDropped.Add(1)
		return ErrFull
	}
}

// Stop rejects new events, drains accepted events, and joins the worker. Store
// calls have bounded contexts so shutdown cannot wait on SQLite indefinitely.
func (a *Async) Stop() {
	a.mu.Lock()
	a.stopped = true
	a.mu.Unlock()
	a.Start()
	a.stopOnce.Do(func() { close(a.stop) })
	<-a.done
}

func (a *Async) Stats() AsyncStats {
	a.mu.RLock()
	stopped := a.stopped
	a.mu.RUnlock()
	return AsyncStats{Accepted: a.accepted.Load(), Written: a.written.Load(), QueueDropped: a.queueDropped.Load(), FailedEvents: a.failedEvents.Load(), WriteFailures: a.writeFailures.Load(), Queued: len(a.queue), Stopped: stopped}
}

func (a *Async) run() {
	defer close(a.done)
	ticker := time.NewTicker(a.flushInterval)
	defer ticker.Stop()
	batch := make([]public.Event, 0, a.batchSize)
	for {
		select {
		case event := <-a.queue:
			batch = append(batch, event)
			a.collect(&batch, a.batchSize)
			a.write(batch)
			batch = batch[:0]
		case <-ticker.C:
			a.collect(&batch, a.batchSize)
			if len(batch) > 0 {
				a.write(batch)
				batch = batch[:0]
			}
		case <-a.stop:
			for len(a.queue) > 0 {
				a.collect(&batch, cap(a.queue)+len(batch))
				a.write(batch)
				batch = batch[:0]
			}
			return
		}
	}
}

func (a *Async) collect(batch *[]public.Event, limit int) {
	for len(*batch) < limit {
		select {
		case event := <-a.queue:
			*batch = append(*batch, event)
		default:
			return
		}
	}
}

func (a *Async) write(batch []public.Event) {
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := a.store.RecordTrafficBatch(ctx, batch)
	cancel()
	if err != nil {
		a.writeFailures.Add(1)
		a.failedEvents.Add(uint64(len(batch)))
		return
	}
	a.written.Add(uint64(len(batch)))
}

var _ public.Recorder = (*Async)(nil)
