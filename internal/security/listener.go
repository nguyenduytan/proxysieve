package security

import (
	"errors"
	"net"
	"sync"
)

var ErrConnectionLimit = errors.New("connection limit reached")

// LimitedListener admits at most max connections at a time. Rejected sockets are
// closed immediately; it never queues unbounded connections in user space.
type LimitedListener struct {
	net.Listener
	slots chan struct{}
}

func NewLimitedListener(listener net.Listener, max int) (*LimitedListener, error) {
	if listener == nil || max < 1 || max > 100_000 {
		return nil, ErrConnectionLimit
	}
	return &LimitedListener{Listener: listener, slots: make(chan struct{}, max)}, nil
}
func (l *LimitedListener) Accept() (net.Conn, error) {
	for {
		connection, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.slots <- struct{}{}:
			return &limitedConn{Conn: connection, release: func() { <-l.slots }}, nil
		default:
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
