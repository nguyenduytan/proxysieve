package traffic

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

type Reader struct {
	Source io.Reader
	count  atomic.Uint64
}

func (r *Reader) Read(b []byte) (int, error) {
	n, err := r.Source.Read(b)
	if n > 0 {
		r.count.Add(uint64(n))
	}
	return n, err
}
func (r *Reader) Bytes() public.Bytes { return public.Bytes(r.count.Load()) }

type Writer struct {
	Destination io.Writer
	count       atomic.Uint64
}

func (w *Writer) Write(b []byte) (int, error) {
	n, err := w.Destination.Write(b)
	if n > 0 {
		w.count.Add(uint64(n))
	}
	return n, err
}
func (w *Writer) Bytes() public.Bytes { return public.Bytes(w.count.Load()) }

type ThrottledReader struct {
	Context        context.Context
	Source         io.Reader
	BytesPerSecond int64
	started        time.Time
	read           int64
}

func (r *ThrottledReader) Read(buffer []byte) (int, error) {
	if r.started.IsZero() {
		r.started = time.Now()
	}
	n, err := r.Source.Read(buffer)
	r.read += int64(n)
	want := time.Duration(r.read/r.BytesPerSecond)*time.Second + time.Duration(r.read%r.BytesPerSecond)*time.Second/time.Duration(r.BytesPerSecond)
	if wait := time.Until(r.started.Add(want)); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-r.Context.Done():
			return n, errors.Join(err, r.Context.Err())
		case <-timer.C:
		}
	}
	return n, err
}
