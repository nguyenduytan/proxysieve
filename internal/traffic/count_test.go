package traffic

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type partialReader struct{}

func (partialReader) Read(b []byte) (int, error) { copy(b, "abc"); return 3, errors.New("partial") }

type partialWriter struct{}

func (partialWriter) Write(b []byte) (int, error) { return 2, errors.New("partial") }
func TestCountersIncludePartialOperations(t *testing.T) {
	r := Reader{Source: partialReader{}}
	b := make([]byte, 10)
	n, err := r.Read(b)
	if n != 3 || err == nil || r.Bytes() != 3 {
		t.Fatal(n, err, r.Bytes())
	}
	w := Writer{Destination: partialWriter{}}
	n, err = w.Write([]byte("abc"))
	if n != 2 || err == nil || w.Bytes() != 2 {
		t.Fatal(n, err, w.Bytes())
	}
	source := Reader{Source: bytes.NewBufferString("hello")}
	if _, err := source.Read(b); err != nil {
		t.Fatal(err)
	}
	if source.Bytes() != 5 {
		t.Fatal(source.Bytes())
	}
}

func TestThrottledReaderLimitsAverageRate(t *testing.T) {
	started := time.Now()
	reader := &ThrottledReader{Context: context.Background(), Source: strings.NewReader("1234"), BytesPerSecond: 80}
	if body, err := io.ReadAll(reader); err != nil || string(body) != "1234" {
		t.Fatal(string(body), err)
	}
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatal("throttle completed too quickly", elapsed)
	}
}
