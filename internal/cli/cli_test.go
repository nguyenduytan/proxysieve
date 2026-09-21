package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/buildinfo"
)

func TestRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		code int
		out  string
		err  string
	}{
		{"no args", nil, 0, "Usage:", ""},
		{"help", []string{"help"}, 0, "Tony Nguyen", ""},
		{"help flag", []string{"--help"}, 0, "Usage:", ""},
		{"short help", []string{"-h"}, 0, "Usage:", ""},
		{"version", []string{"version"}, 0, "ProxySieve 0.0.0-dev", ""},
		{"version alias", []string{"--version"}, 0, "Author: Tony Nguyen", ""},
		{"start invalid flags", []string{"start", "--bogus"}, 2, "", "INVALID_USAGE"},
		{"extension missing call", []string{"extension"}, 2, "", "INVALID_USAGE"},
		{"unknown", []string{"not-a-command"}, 2, "", "INVALID_USAGE"},
		{"bad version flag", []string{"version", "--bogus"}, 2, "", "INVALID_USAGE"},
		{"extra version args", []string{"version", "--json", "extra"}, 2, "", "INVALID_USAGE"},
		{"extra help args", []string{"help", "extra"}, 2, "", "INVALID_USAGE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out, errOut bytes.Buffer
			if code := Run(tc.args, &out, &errOut); code != tc.code {
				t.Fatalf("exit = %d, want %d", code, tc.code)
			}
			checkOutput(t, out.String(), tc.out)
			checkOutput(t, errOut.String(), tc.err)
		})
	}
}

func checkOutput(t *testing.T, got, want string) {
	t.Helper()
	if (want == "" && got != "") || !strings.Contains(got, want) {
		t.Errorf("output = %q, want substring %q (empty means no output)", got, want)
	}
}

func TestVersionJSON(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	if code := Run([]string{"version", "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var info buildinfo.Info
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info != buildinfo.Current() || errOut.Len() != 0 {
		t.Fatalf("unexpected version output: %+v, stderr %q", info, errOut.String())
	}
}

func TestArgumentsAreNotEchoed(t *testing.T) {
	t.Parallel()
	secret := "http://demo:fake-password@example.invalid:8080"
	for _, args := range [][]string{{secret}, {"version", secret}, {"help", secret}, {"start", secret}} {
		var out, errOut bytes.Buffer
		if Run(args, &out, &errOut) == 0 {
			t.Fatal("invalid invocation succeeded")
		}
		if strings.Contains(out.String()+errOut.String(), secret) {
			t.Fatal("argument leaked")
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken output") }

func TestOutputFailures(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"help"}, {"version"}, {"version", "--json"}} {
		if code := Run(args, brokenWriter{}, brokenWriter{}); code != 1 {
			t.Errorf("args %v: exit = %d, want 1", args, code)
		}
	}
}
