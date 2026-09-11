package proxy

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var ErrParse = errors.New("invalid proxy endpoint format")

type ParseWarning string

const WarningLegacyFormat ParseWarning = "legacy_colon_format"

type ParseResult struct {
	Endpoint Endpoint
	Warnings []ParseWarning
}

// Parse accepts URI forms and the legacy host:port:user:pass form. It never
// stores credentials; callers must put them in SecretStore and retain only a ref.
func Parse(raw string) (ParseResult, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return ParseResult{}, ErrParse
	}
	if !strings.Contains(raw, "://") && strings.Contains(raw, "@") {
		raw = "http://" + raw
	}
	if !strings.Contains(raw, "://") {
		parts := strings.Split(raw, ":")
		if len(parts) != 2 && len(parts) != 4 {
			return ParseResult{}, ErrParse
		}
		port, err := parsePort(parts[1])
		if err != nil || !ValidHost(parts[0]) {
			return ParseResult{}, ErrParse
		}
		e := Endpoint{ID: "imported", Name: "Imported proxy", Protocol: HTTP, Host: parts[0], Port: port, Enabled: true}
		if len(parts) == 4 {
			if parts[2] == "" || parts[3] == "" {
				return ParseResult{}, ErrParse
			}
			e.Metadata = map[string]string{"username_present": "true"}
		}
		return ParseResult{Endpoint: e, Warnings: []ParseWarning{WarningLegacyFormat}}, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil && u.Host == "" {
		return ParseResult{}, ErrParse
	}
	var p Protocol
	switch strings.ToLower(u.Scheme) {
	case "http":
		p = HTTP
	case "https":
		p = HTTPS
	case "socks5":
		p = SOCKS5
	case "socks5h":
		p = SOCKS5H
	default:
		return ParseResult{}, ErrParse
	}
	if u.User != nil && (u.User.Username() == "" || u.User.String() == "") {
		return ParseResult{}, ErrParse
	}
	if u.User != nil {
		if _, ok := u.User.Password(); ok && strings.TrimSpace(func() string { p, _ := u.User.Password(); return p }()) == "" {
			return ParseResult{}, ErrParse
		}
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !ValidHost(u.Hostname()) {
		return ParseResult{}, ErrParse
	}
	port, err := parsePort(u.Port())
	if err != nil {
		return ParseResult{}, ErrParse
	}
	e := Endpoint{ID: "imported", Name: "Imported proxy", Protocol: p, Host: u.Hostname(), Port: port, Enabled: true}
	if u.User != nil {
		e.Metadata = map[string]string{"username_present": "true", "password_present": strconv.FormatBool(func() bool { _, ok := u.User.Password(); return ok }())}
	}
	return ParseResult{Endpoint: e}, nil
}
func parsePort(raw string) (uint16, error) {
	if raw == "" {
		return 0, ErrParse
	}
	v, err := strconv.ParseUint(raw, 10, 16)
	if err != nil || v == 0 {
		return 0, ErrParse
	}
	return uint16(v), nil
}
