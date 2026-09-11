// Package security implements secret protection adapters and safe diagnostic helpers.
package security

import (
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"net/http"
	"net/url"
	"strings"
)

func sensitiveKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	return strings.Contains(k, "token") || strings.Contains(k, "secret") || strings.Contains(k, "password") || strings.Contains(k, "credential") || k == "authorization" || k == "proxyauthorization" || k == "cookie" || k == "setcookie" || strings.Contains(k, "apikey") || k == "key" || k == "signature" || k == "sig"
}

// RedactHeaders returns an independent copy; input data is never mutated.
func RedactHeaders(h http.Header) http.Header {
	out := h.Clone()
	for k := range out {
		if sensitiveKey(k) {
			out[k] = []string{secret.Redacted}
		}
	}
	return out
}

// RedactURL strips all userinfo and query/fragment values (including unknown token
// parameter names). Paths may also contain secrets: avoid logging raw URLs by default.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return secret.Redacted
	}
	u.User = nil
	if u.RawQuery != "" {
		u.RawQuery = "redacted"
	}
	if u.Fragment != "" {
		u.Fragment = "redacted"
		u.RawFragment = ""
	}
	return u.String()
}
