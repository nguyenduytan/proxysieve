package routing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

var benchmarkSelectedID model.ID

func BenchmarkSelector(b *testing.B) {
	for _, count := range []int{100, 1_000} {
		b.Run(fmt.Sprintf("endpoints-%d", count), func(b *testing.B) {
			candidates := make([]Candidate, count)
			for i := range count {
				id := model.ID(fmt.Sprintf("proxy-%d", i))
				candidates[i] = Candidate{Endpoint: proxy.Endpoint{ID: id, Name: string(id), Protocol: proxy.HTTP, Host: fmt.Sprintf("proxy-%d.invalid", i), Port: 8080, Enabled: true}, Latency: time.Duration(count-i) * time.Millisecond}
			}
			selector, err := NewBuiltIn(LowestLatency)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				selected, selectErr := selector.Select(context.Background(), SelectionContext{}, candidates)
				if selectErr != nil {
					b.Fatal(selectErr)
				}
				benchmarkSelectedID = selected
			}
		})
	}
}
