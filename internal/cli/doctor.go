package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguyenduytan/proxysieve/internal/configload"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/config"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorReport struct {
	Checks []doctorCheck `json:"checks"`
}

func runDoctor(args []string, stdout, stderr io.Writer, home string, env map[string]string) int {
	f := flag.NewFlagSet("doctor", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	file := f.String("file", "", "Configuration file")
	jsonOutput := f.Bool("json", false, "Output machine-readable JSON")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return usageError(stderr)
	}
	o := configload.Options{Home: home, Env: env}
	if *file != "" {
		r, err := os.Open(*file)
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_READ_FAILED\n")
			return 1
		}
		defer func() { _ = r.Close() }()
		o.File = r
	}
	effective, err := configload.Load(o)
	if err != nil {
		return writeDoctor(stderr, stdout, *jsonOutput, doctorReport{Checks: []doctorCheck{{Name: "config", Status: "fail", Detail: "configuration was not accepted"}}}, true)
	}
	report := diagnose(context.Background(), effective.Config)
	critical := false
	for _, check := range report.Checks {
		if check.Status == "fail" {
			critical = true
		}
	}
	return writeDoctor(stderr, stdout, *jsonOutput, report, critical)
}

func diagnose(ctx context.Context, c config.Config) doctorReport {
	report := doctorReport{Checks: []doctorCheck{{Name: "config", Status: "pass", Detail: "configuration is valid"}}}
	report.Checks = append(report.Checks, checkDataDir(c.Server.DataDir))
	if c.Storage.Driver == "sqlite" {
		report.Checks = append(report.Checks, checkDatabase(ctx, c.Storage.Path))
	} else {
		report.Checks = append(report.Checks, doctorCheck{Name: "database", Status: "warn", Detail: "memory storage has no durable schema to inspect"})
	}
	for _, listener := range c.Listeners {
		report.Checks = append(report.Checks, checkBind(listener.Name, listener.Bind))
	}
	if c.Admin.Enabled {
		report.Checks = append(report.Checks, checkBind("admin", c.Admin.Bind))
	}
	return report
}

func checkDataDir(dir string) doctorCheck {
	if dir == "" {
		return doctorCheck{Name: "data_dir", Status: "fail", Detail: "data directory is empty"}
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return doctorCheck{Name: "data_dir", Status: "warn", Detail: "data directory does not exist yet"}
		}
		return doctorCheck{Name: "data_dir", Status: "fail", Detail: "data directory could not be inspected"}
	}
	if !info.IsDir() {
		return doctorCheck{Name: "data_dir", Status: "fail", Detail: "configured data path is not a directory"}
	}
	probe, err := os.CreateTemp(dir, ".proxysieve-doctor-*")
	if err != nil {
		return doctorCheck{Name: "data_dir", Status: "fail", Detail: "data directory is not writable"}
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return doctorCheck{Name: "data_dir", Status: "pass", Detail: "data directory is readable and writable"}
}

func checkDatabase(ctx context.Context, path string) doctorCheck {
	if path == "" || strings.HasPrefix(path, "file:") {
		return doctorCheck{Name: "database", Status: "fail", Detail: "database path is invalid"}
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return doctorCheck{Name: "database", Status: "warn", Detail: "database has not been created yet"}
		}
		return doctorCheck{Name: "database", Status: "fail", Detail: "database path could not be inspected"}
	}
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		return doctorCheck{Name: "database", Status: "fail", Detail: "database could not be opened or migrated"}
	}
	defer func() { _ = store.Close() }()
	status, err := store.Status(ctx)
	if err != nil {
		return doctorCheck{Name: "database", Status: "fail", Detail: "database status could not be read"}
	}
	return doctorCheck{Name: "database", Status: "pass", Detail: fmt.Sprintf("SQLite schema %d is ready", status.SchemaVersion)}
}

func checkBind(name, bind string) doctorCheck {
	listener, err := net.Listen("tcp", bind)
	if err != nil {
		return doctorCheck{Name: "listener." + name, Status: "fail", Detail: "configured address is unavailable"}
	}
	_ = listener.Close()
	return doctorCheck{Name: "listener." + name, Status: "pass", Detail: "configured address is available"}
}

func writeDoctor(stderr, stdout io.Writer, jsonOutput bool, report doctorReport, failed bool) int {
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			return 1
		}
	} else {
		for _, check := range report.Checks {
			if _, err := fmt.Fprintf(stdout, "[%s] %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Detail); err != nil {
				return 1
			}
		}
	}
	if failed {
		_, _ = io.WriteString(stderr, "DOCTOR_FAILED: one or more critical checks failed.\n")
		return 1
	}
	return 0
}

func doctorTestConfig(home string) config.Config {
	c := config.Defaults(home)
	c.Server.DataDir = filepath.Join(home, "data")
	c.Storage.Path = filepath.Join(c.Server.DataDir, "proxysieve.db")
	return c
}
