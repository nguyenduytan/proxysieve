package secret

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestNoAccidentalSerialization(t *testing.T) {
	raw := []byte("fake-private-value")
	v, err := New(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'x'
	if string(v.Reveal()) != "fake-private-value" {
		t.Fatal("input aliased")
	}
	copy := v.Reveal()
	copy[0] = 'z'
	if string(v.Reveal()) != "fake-private-value" {
		t.Fatal("output aliased")
	}
	for _, format := range []string{"%s", "%v", "%+v", "%#v", "%q", "%x"} {
		got := fmt.Sprintf(format, v)
		if got != Redacted {
			t.Fatalf("format %s leaked: %s", format, got)
		}
	}
	b, err := json.Marshal(struct{ Secret Value }{v})
	if err != nil || strings.Contains(string(b), "private-value") {
		t.Fatalf("JSON %s %v", b, err)
	}
	var out bytes.Buffer
	slog.New(slog.NewJSONHandler(&out, nil)).Info("test", "secret", v)
	if strings.Contains(out.String(), "private-value") {
		t.Fatal("slog leaked")
	}
}
func TestReferencesAndBounds(t *testing.T) {
	for _, r := range []Ref{"secret://proxy/demo/password", "secret://one"} {
		if !r.Valid() {
			t.Fatal(r)
		}
	}
	for _, r := range []Ref{"", "secret://../password", "secret://user:pass@host", "file:///key", "secret://one?secret=x"} {
		if r.Valid() {
			t.Fatal(r)
		}
	}
	for _, raw := range [][]byte{nil, make([]byte, MaxBytes+1)} {
		if _, err := New(raw); err == nil {
			t.Fatal("invalid size")
		}
	}
}
