// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"context"
	"io"
	"net"
	"time"
)

type idleReader struct {
	io.Reader
	conn    net.Conn
	timeout time.Duration
}

func (r idleReader) Read(buffer []byte) (int, error) {
	_ = r.conn.SetDeadline(time.Now().Add(r.timeout))
	return r.Reader.Read(buffer)
}

type idleWriter struct {
	io.Writer
	conn    net.Conn
	timeout time.Duration
}

func (w idleWriter) Write(buffer []byte) (int, error) {
	_ = w.conn.SetDeadline(time.Now().Add(w.timeout))
	return w.Writer.Write(buffer)
}

func IdleReader(reader io.Reader, conn net.Conn, timeout time.Duration) io.Reader {
	return idleReader{Reader: reader, conn: conn, timeout: timeout}
}

func IdleWriter(writer io.Writer, conn net.Conn, timeout time.Duration) io.Writer {
	return idleWriter{Writer: writer, conn: conn, timeout: timeout}
}

func CloseOnCancel(ctx context.Context, connections ...net.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			for _, connection := range connections {
				_ = connection.Close()
			}
		case <-done:
		}
	}()
	return func() { close(done) }
}
