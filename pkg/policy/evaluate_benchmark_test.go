package policy

import (
	"fmt"
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/model"
)

var benchmarkPolicyResult Result

func BenchmarkPolicyEvaluation(b *testing.B) {
	for _, count := range []int{100, 1_000, 10_000} {
		b.Run(fmt.Sprintf("rules-%d", count), func(b *testing.B) {
			rules := make([]Rule, count)
			for i := range count - 1 {
				rules[i] = rule(fmt.Sprintf("rule-%d", i), count-i, Condition{Field: "host", Operator: "equals", Values: []string{"blocked.invalid"}}, Action{Type: "block"}, true)
			}
			rules[count-1] = rule("fallback", 0, Condition{}, Action{Type: "direct"}, true)
			document := Policy{Version: 1, ID: "benchmark", Name: "Benchmark", Rules: rules}
			request := RequestContext{Host: "allowed.invalid", Method: model.Optional[string]{Known: true, Value: "GET"}}
			visibility := Visibility{Host: true, Method: true}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				result, err := Evaluate(document, request, visibility, false)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkPolicyResult = result
			}
		})
	}
}
