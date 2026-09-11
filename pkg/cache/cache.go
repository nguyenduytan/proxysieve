// Package cache defines safe HTTP cache eligibility and partitioned cache keys.
package cache

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"net/http"
	"sort"
	"strings"
)

var ErrInvalid = errors.New("invalid cache key")

type Eligibility struct {
	Eligible bool
	Reason   string
}

func CheckRequest(r *http.Request) Eligibility {
	if r == nil {
		return Eligibility{Reason: "missing request"}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return Eligibility{Reason: "method"}
	}
	if r.Header.Get("Authorization") != "" {
		return Eligibility{Reason: "authorization"}
	}
	if r.Header.Get("Cookie") != "" {
		return Eligibility{Reason: "cookie"}
	}
	directives := parseDirectives(r.Header.Get("Cache-Control"))
	if directives["no-store"] || directives["private"] {
		return Eligibility{Reason: "request cache-control"}
	}
	return Eligibility{Eligible: true}
}
func CheckResponse(status int, headers http.Header, contentLength int64, maxBytes int64) Eligibility {
	if status < 200 || status > 299 {
		return Eligibility{Reason: "status"}
	}
	if headers.Get("Set-Cookie") != "" {
		return Eligibility{Reason: "set-cookie"}
	}
	directives := parseDirectives(headers.Get("Cache-Control"))
	if directives["no-store"] || directives["private"] {
		return Eligibility{Reason: "response cache-control"}
	}
	vary := strings.TrimSpace(headers.Get("Vary"))
	if vary == "*" {
		return Eligibility{Reason: "vary wildcard"}
	}
	if contentLength < 0 || contentLength > maxBytes {
		return Eligibility{Reason: "size"}
	}
	return Eligibility{Eligible: true}
}
func parseDirectives(value string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		key, _, _ := strings.Cut(strings.TrimSpace(strings.ToLower(part)), "=")
		out[key] = true
	}
	return out
}

type Key struct {
	ClientID    model.ID
	SessionHash string
	RouteID     model.ID
	Method      string
	URL         string
	Vary        map[string]string
}

func (k Key) Validate() error {
	if !k.ClientID.Valid() || !k.RouteID.Valid() || k.SessionHash == "" || k.Method == "" || k.URL == "" || len(k.URL) > 8192 || len(k.Vary) > 32 {
		return ErrInvalid
	}
	return nil
}
func (k Key) String() string {
	parts := []string{string(k.ClientID), k.SessionHash, string(k.RouteID), k.Method, k.URL}
	names := make([]string, 0, len(k.Vary))
	for name := range k.Vary {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		parts = append(parts, strings.ToLower(name)+"="+k.Vary[name])
	}
	return strings.Join(parts, "\x00")
}
