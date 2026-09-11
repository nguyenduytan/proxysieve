package cli

import (
	"bytes"
	"encoding/json"
	"github.com/nguyenduytan/proxysieve/internal/configload"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCommands(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "test.yaml")
	if err := os.WriteFile(file, []byte("version: 1\nlogging:\n  level: debug\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"validate"}, 0},
		{[]string{"validate", "--file", file}, 0},
		{[]string{"print-effective", "--file", file, "--set", "logging.level=warn"}, 0},
		{[]string{"validate", "--file", filepath.Join(home, "missing")}, 1},
		{[]string{"validate", "--set", "admin.bind=0.0.0.0:9090"}, 1},
		{[]string{"validate", "--set", "bad=fake-secret"}, 1},
		{[]string{"validate", "--set", "fake-secret"}, 2},
		{[]string{"validate", "--bad=fake-secret"}, 2},
		{[]string{"validate", "--set", "logging.level=warn", "--set", "logging.level=info"}, 2},
		{[]string{"invalid"}, 2},
	} {
		var out, errOut bytes.Buffer
		code := runConfig(tc.args, &out, &errOut, home, nil)
		if code != tc.code {
			t.Fatalf("%v: %d %s", tc.args, code, errOut.String())
		}
		if strings.Contains(out.String()+errOut.String(), "fake-secret") {
			t.Fatal("argument leaked")
		}
		if code == 0 && tc.args[0] == "print-effective" {
			var e configload.Effective
			if json.Unmarshal(out.Bytes(), &e) != nil || e.Config.Logging.Level != "warn" || e.Sources["logging.level"] != "flags" {
				t.Fatal(out.String())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".proxysieve")); !os.IsNotExist(err) {
		t.Fatal("validation created runtime data")
	}
}
