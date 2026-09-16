package proxy

import "testing"

func FuzzParse(f *testing.F) {
	for _, seed := range []string{"http://user:pass@proxy.invalid:8080", "socks5h://proxy.invalid:1080", "proxy.invalid:8080", "[::1]:8080", "http://bad.invalid:0"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		result, err := Parse(raw)
		if err == nil && result.Endpoint.Validate() != nil {
			t.Fatal("parser returned invalid endpoint")
		}
		if len(raw) > maxEndpointBytes && err == nil {
			t.Fatal("oversized endpoint accepted")
		}
	})
}
