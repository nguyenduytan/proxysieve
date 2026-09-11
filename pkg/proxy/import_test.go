package proxy

import (
	"strings"
	"testing"
)

func TestPreview(t *testing.T) {
	p, err := Preview("http://a.example.invalid:8080\n\nhttp://a.example.invalid:8080\nsocks5://b.example.invalid:1080\nbad", 10)
	if err != nil || p.Valid != 3 || p.Invalid != 1 || p.Duplicates != 1 || p.Protocols[HTTP] != 2 || p.Protocols[SOCKS5] != 1 {
		t.Fatalf("%+v %v", p, err)
	}
}
func TestImportModes(t *testing.T) {
	input := "http://a.example.invalid:8080\nhttp://a.example.invalid:8080"
	existing := map[string]Endpoint{Identity(Endpoint{Protocol: HTTP, Host: "a.example.invalid", Port: 8080}): {ID: "existing", Protocol: HTTP, Host: "a.example.invalid", Port: 8080}}
	for _, mode := range []struct {
		m           DuplicateMode
		skip, count int
	}{{SkipDuplicates, 2, 0}, {UpdateDuplicates, 0, 2}, {CreateDuplicates, 0, 2}} {
		r, err := Import(ImportRequest{Input: input, Mode: mode.m, Existing: existing})
		if err != nil || r.Skipped != mode.skip || len(r.Endpoints) != mode.count {
			t.Fatalf("%s %+v %v", mode.m, r, err)
		}
		for _, e := range r.Endpoints {
			if e.ID == "imported" || strings.Contains(e.String(), "fake-password") {
				t.Fatal("unsafe result", e)
			}
		}
	}
}
func TestImportBounds(t *testing.T) {
	if _, err := Preview(strings.Repeat("http://a.example.invalid:80\n", 100_001), 100_000); err == nil {
		t.Fatal("line bound ignored")
	}
	if _, err := Import(ImportRequest{Input: "x", Mode: "invalid"}); err == nil {
		t.Fatal("mode accepted")
	}
	if CredentialFingerprint("fake-password") == "" {
		t.Fatal("empty fingerprint")
	}
}
