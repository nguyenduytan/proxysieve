package budget

import (
	"context"
	"errors"
	"io"

	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

const maxTransferReservation = 64 << 10

type Reader struct {
	Context context.Context
	Source  io.Reader
	Reserve publicbudget.ReserveFunc
	used    bool
}

func (r *Reader) Read(p []byte) (int, error) {
	if r.Reserve == nil || len(p) == 0 {
		return r.Source.Read(p)
	}
	if len(p) > maxTransferReservation {
		p = p[:maxTransferReservation]
	}
	lease, amount, err := reserveUpTo(r.Context, r.Reserve, len(p))
	if err != nil {
		if r.used && errors.Is(err, publicbudget.ErrExceeded) {
			return 0, io.EOF
		}
		return 0, err
	}
	n, readErr := r.Source.Read(p[:amount])
	if n > 0 {
		r.used = true
	}
	_, consumeErr := lease.Consume(r.Context, traffic.Bytes(n))
	closeErr := lease.Close(r.Context)
	if consumeErr == nil && closeErr == nil {
		return n, readErr
	}
	return n, errors.Join(readErr, consumeErr, closeErr)
}

type Writer struct {
	Context     context.Context
	Destination io.Writer
	Reserve     publicbudget.ReserveFunc
}

func (w *Writer) Write(p []byte) (int, error) {
	if w.Reserve == nil || len(p) == 0 {
		return w.Destination.Write(p)
	}
	written := 0
	for written < len(p) {
		want := min(len(p)-written, maxTransferReservation)
		lease, amount, err := reserveUpTo(w.Context, w.Reserve, want)
		if err != nil {
			return written, err
		}
		n, writeErr := w.Destination.Write(p[written : written+amount])
		_, consumeErr := lease.Consume(w.Context, traffic.Bytes(n))
		closeErr := lease.Close(w.Context)
		written += n
		if writeErr != nil || consumeErr != nil || closeErr != nil {
			combined := errors.Join(writeErr, consumeErr, closeErr)
			return written, combined
		}
		if n != amount {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func Available(ctx context.Context, reserve publicbudget.ReserveFunc) error {
	if reserve == nil {
		return nil
	}
	lease, err := reserve(ctx, 1)
	if err != nil {
		return err
	}
	return lease.Close(ctx)
}

func reserveUpTo(ctx context.Context, reserve publicbudget.ReserveFunc, want int) (publicbudget.Lease, int, error) {
	amount := want
	for amount > 0 {
		lease, err := reserve(ctx, traffic.Bytes(amount))
		if err == nil {
			return lease, amount, nil
		}
		if !errors.Is(err, publicbudget.ErrExceeded) || amount == 1 {
			return nil, 0, err
		}
		amount = (amount + 1) / 2
	}
	return nil, 0, publicbudget.ErrExceeded
}
