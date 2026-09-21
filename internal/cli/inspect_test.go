package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectCACommands(t *testing.T) {
	home := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := runInspect([]string{"ca", "init"}, &stdout, &stderr, home, map[string]string{}); code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "SHA256 fingerprint") {
		t.Fatalf("init code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := runInspect([]string{"ca", "fingerprint"}, &stdout, &stderr, home, map[string]string{}); code != 0 || strings.Count(strings.TrimSpace(stdout.String()), ":") != 31 {
		t.Fatalf("fingerprint code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	exported := filepath.Join(t.TempDir(), "ca.crt")
	stdout.Reset()
	if code := runInspect([]string{"ca", "export", "--path", exported}, &stdout, &stderr, home, map[string]string{}); code != 0 || !strings.Contains(stdout.String(), exported) {
		t.Fatalf("export code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := runInspect([]string{"ca", "rotate"}, &stdout, &stderr, home, map[string]string{}); code != 0 || !strings.Contains(stdout.String(), "rotated") {
		t.Fatalf("rotate code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
