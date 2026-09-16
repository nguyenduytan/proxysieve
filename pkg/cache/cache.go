// Package cache defines safe HTTP cache eligibility and partitioned cache keys.
package cache

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
)

var ErrInvalid = errors.New("invalid cache key")

type Eligibility struct {
	Eligible  bool
	Reason    string
	ExpiresAt time.Time
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
	directives := parseDirectives(strings.Join(r.Header.Values("Cache-Control"), ","))
	if hasDirective(directives, "no-store") || hasDirective(directives, "private") || hasDirective(directives, "no-cache") || directives["max-age"] == "0" || strings.EqualFold(strings.TrimSpace(r.Header.Get("Pragma")), "no-cache") {
		return Eligibility{Reason: "request cache-control"}
	}
	return Eligibility{Eligible: true}
}
func CheckResponse(status int, headers http.Header, contentLength int64, maxBytes int64, now time.Time) Eligibility {
	if status < 200 || status > 299 || status == http.StatusPartialContent || headers.Get("Content-Range") != "" {
		return Eligibility{Reason: "status"}
	}
	if headers.Get("Set-Cookie") != "" {
		return Eligibility{Reason: "set-cookie"}
	}
	directives := parseDirectives(strings.Join(headers.Values("Cache-Control"), ","))
	if hasDirective(directives, "no-store") || hasDirective(directives, "private") || hasDirective(directives, "no-cache") {
		return Eligibility{Reason: "response cache-control"}
	}
	if strings.TrimSpace(headers.Get("Vary")) != "" {
		return Eligibility{Reason: "vary"}
	}
	if contentLength < 0 || contentLength > maxBytes {
		return Eligibility{Reason: "size"}
	}
	expiresAt, ok := expiration(directives, headers, now)
	if !ok {
		return Eligibility{Reason: "freshness"}
	}
	return Eligibility{Eligible: true, ExpiresAt: expiresAt}
}
func parseDirectives(value string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(value, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(strings.ToLower(part)), "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key != "" {
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				value = value[1 : len(value)-1]
			}
			out[key] = value
		}
	}
	return out
}

func hasDirective(directives map[string]string, name string) bool {
	_, ok := directives[name]
	return ok
}

func expiration(directives map[string]string, headers http.Header, now time.Time) (time.Time, bool) {
	value, explicit := directives["s-maxage"]
	if !explicit {
		value, explicit = directives["max-age"]
	}
	if explicit {
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil || seconds <= 0 || seconds > (1<<63-1)/int64(time.Second) {
			return time.Time{}, false
		}
		if age := strings.TrimSpace(headers.Get("Age")); age != "" {
			ageSeconds, err := strconv.ParseInt(age, 10, 64)
			if err != nil || ageSeconds < 0 || ageSeconds >= seconds {
				return time.Time{}, false
			}
			seconds -= ageSeconds
		}
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	expiresAt, err := http.ParseTime(headers.Get("Expires"))
	return expiresAt, err == nil && expiresAt.After(now)
}

type Key struct {
	ClientID        model.ID
	SessionHash     string
	RouteID         model.ID
	ProxyID         model.ID
	PolicyID        model.ID
	RuleID          model.ID
	RuntimeRevision int64
	Method          string
	URL             string
	Vary            map[string]string
}

func (k Key) Validate() error {
	if !k.ClientID.Valid() || !k.RouteID.Valid() || k.SessionHash == "" || k.Method == "" || k.URL == "" || len(k.URL) > 8192 || len(k.Vary) > 32 {
		return ErrInvalid
	}
	return nil
}
func (k Key) String() string {
	parts := []string{string(k.ClientID), k.SessionHash, string(k.RouteID), string(k.ProxyID), string(k.PolicyID), string(k.RuleID), strconv.FormatInt(k.RuntimeRevision, 10), k.Method, k.URL}
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
