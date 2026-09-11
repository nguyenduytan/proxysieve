package proxy

import "testing"

func TestParseFormats(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		p       Protocol
		host    string
		port    uint16
		warning bool
	}{
		{"example.invalid:8080", HTTP, "example.invalid", 8080, true},
		{"example.invalid:8080:user:fake-password", HTTP, "example.invalid", 8080, true},
		{"user:fake-password@example.invalid:8080", HTTP, "example.invalid", 8080, false},
		{"http://example.invalid:8080", HTTP, "example.invalid", 8080, false},
		{"https://user:fake-password@example.invalid:8443", HTTPS, "example.invalid", 8443, false},
		{"socks5://example.invalid:1080", SOCKS5, "example.invalid", 1080, false},
		{"socks5h://user:fake-password@example.invalid:1080", SOCKS5H, "example.invalid", 1080, false},
	} {
		got, err := Parse(tc.raw)
		if err != nil || got.Endpoint.Protocol != tc.p || got.Endpoint.Host != tc.host || got.Endpoint.Port != tc.port || (len(got.Warnings) > 0) != tc.warning {
			t.Fatalf("%s: %+v %v", tc.raw, got, err)
		}
		if got.Endpoint.String() == "" {
			t.Fatal("empty endpoint")
		}
	}
}
func TestParseRejectsSecretsInEndpoint(t *testing.T) {
	for _, raw := range []string{"", "example.invalid", "example.invalid:0", "example.invalid:65536", "http://example.invalid:8080/path", "http://user:fake-password@example.invalid:8080/path", "http://user:@example.invalid:8080", "http://user:fake-password@example.invalid:8080?token=fake-secret", "http://user:fake-password@bad host:8080", "host:8080:user", "host:8080::pass", "host:8080:user:pass:extra"} {
		if _, err := Parse(raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	got, err := Parse("http://user:fake-password@example.invalid:8080")
	if err != nil {
		t.Fatal(err)
	}
	if got.Endpoint.Metadata["username_present"] != "true" || got.Endpoint.Metadata["password_present"] != "true" {
		t.Fatal(got.Endpoint.Metadata)
	}
}
