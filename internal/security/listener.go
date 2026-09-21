package security

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
)

var ErrConnectionLimit = errors.New("connection limit reached")

// LimitedListener admits at most max connections at a time. Rejected sockets are
// closed immediately; it never queues unbounded connections in user space.
type LimitedListener struct {
	net.Listener
	slots   chan struct{}
	metrics *ListenerMetrics
}

type ListenerMetrics struct {
	accepted atomic.Uint64
	active   atomic.Int64
	rejected atomic.Uint64
}

type ListenerStats struct {
	Accepted uint64 `json:"accepted"`
	Active   uint64 `json:"active"`
	Rejected uint64 `json:"rejected"`
}

func (m *ListenerMetrics) Snapshot() ListenerStats {
	if m == nil {
		return ListenerStats{}
	}
	return ListenerStats{Accepted: m.accepted.Load(), Active: uint64(m.active.Load()), Rejected: m.rejected.Load()}
}

func NewLimitedListener(listener net.Listener, max int, metrics *ListenerMetrics) (*LimitedListener, error) {
	if listener == nil || max < 1 || max > 100_000 {
		return nil, ErrConnectionLimit
	}
	if metrics == nil {
		metrics = &ListenerMetrics{}
	}
	return &LimitedListener{Listener: listener, slots: make(chan struct{}, max), metrics: metrics}, nil
}
func (l *LimitedListener) Accept() (net.Conn, error) {
	for {
		connection, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.slots <- struct{}{}:
			l.metrics.accepted.Add(1)
			l.metrics.active.Add(1)
			return &limitedConn{Conn: connection, release: func() { <-l.slots; l.metrics.active.Add(-1) }}, nil
		default:
			l.metrics.rejected.Add(1)
			_ = connection.Close()
		}
	}
}

type limitedConn struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *limitedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }
