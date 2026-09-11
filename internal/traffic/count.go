package traffic

import (
	public "github.com/nguyenduytan/proxysieve/pkg/traffic"
	"io"
	"sync/atomic"
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
