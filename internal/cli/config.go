package cli

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"strings"

	"github.com/nguyenduytan/proxysieve/internal/configload"
)

type overrides map[string]string

func (overrides) String() string { return "key=value" }
func (o overrides) Set(value string) error {
	k, v, ok := strings.Cut(value, "=")
	if !ok || k == "" {
		return errUsage
	}
	if _, exists := o[k]; exists {
		return errUsage
	}
	o[k] = v
	return nil
}

func runConfig(args []string, stdout, stderr io.Writer, home string, env map[string]string) int {
	if len(args) == 0 || args[0] != "validate" && args[0] != "print-effective" {
		return usageError(stderr)
	}
	f := flag.NewFlagSet("config", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	file := f.String("file", "", "Explicit YAML file; omitted means defaults plus environment")
	values := overrides{}
	f.Var(values, "set", "Override a scalar dotted.path=value (repeatable)")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 {
		return usageError(stderr)
	}
	o := configload.Options{Home: home, Env: env, Flags: values}
	if *file != "" {
		r, err := os.Open(*file)
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_READ_FAILED: cannot open the requested configuration file.\n")
			return 1
		}
		defer func() { _ = r.Close() }()
		o.File = r
	}
	e, err := configload.Load(o)
	if err != nil {
		_, _ = io.WriteString(stderr, "CONFIG_INVALID: check schema version, field names, limits, duration strings, bind addresses and authentication requirements. No configuration was applied.\n")
		return 1
	}
	if args[0] == "validate" {
		if _, err = io.WriteString(stdout, "Configuration v1 is valid. No listeners opened or files modified.\n"); err != nil {
			return 1
		}
		return 0
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if enc.Encode(e) != nil {
		return 1
	}
	return 0
}

func environment() map[string]string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			values[k] = v
		}
	}
	return values
}
