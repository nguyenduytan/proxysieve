package traffic

import (
	"context"
	"testing"

	public "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

func BenchmarkTrafficRecordFullBuffer(b *testing.B) {
	recorder, err := NewMemory(1_024)
	if err != nil {
		b.Fatal(err)
	}
	event := public.Event{Action: "proxy"}
	for range 1_024 {
		_ = recorder.Record(context.Background(), event)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = recorder.Record(context.Background(), event)
	}
}
