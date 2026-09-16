package security

import (
	"net/url"
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/secret"
)

func FuzzRedactURL(f *testing.F) {
	for _, seed := range []string{"https://user:pass@example.invalid/path?token=value#secret", "http://example.invalid/", ":bad"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		redacted := RedactURL(raw)
		if redacted == secret.Redacted {
			return
		}
		parsed, err := url.Parse(redacted)
		if err != nil || parsed.User != nil || parsed.RawQuery != "" && parsed.RawQuery != "redacted" || parsed.Fragment != "" && parsed.Fragment != "redacted" {
			t.Fatal("redaction returned unsafe URL")
		}
	})
}
