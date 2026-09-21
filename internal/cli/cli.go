// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the command boundary without process-global flag state.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nguyenduytan/proxysieve/internal/buildinfo"
)

var errUsage = errors.New("invalid command syntax")

const help = `ProxySieve — Smart traffic control for paid proxies.
Created by Tony Nguyen. Licensed under Apache-2.0.

Usage:
  proxysieve help
  proxysieve version [--json]
  proxysieve start [--file PATH]
  proxysieve doctor [--file PATH] [--json]
  proxysieve db status [--file PATH] [--json]
  proxysieve db migrate [--file PATH]
  proxysieve db compact [--file PATH]
  proxysieve backup --file PATH --path BACKUP
  proxysieve restore --file PATH --path BACKUP
  proxysieve export [--file PATH] --path EXPORT
  proxysieve import --path EXPORT --output CONFIG
  proxysieve import --path EXPORT --dry-run
  proxysieve session list --username USER [--admin URL] [--json]
  proxysieve session show --username USER [--admin URL] [--json] ID
  proxysieve session rotate --username USER [--admin URL] [--json] ID
  proxysieve session delete --username USER [--admin URL] ID
  proxysieve cache stats --username USER [--admin URL] [--json]
  proxysieve cache purge --username USER [--admin URL] [--json]
  proxysieve cache purge-domain --domain DOMAIN --username USER [--admin URL] [--json]
  proxysieve extension call --root DIR --manifest PATH --capability NAME --method NAME [--input JSON] [--timeout DURATION] [--attempts N]
  proxysieve inspect ca init [--file PATH]
  proxysieve inspect ca fingerprint [--file PATH]
  proxysieve inspect ca export --path CERTIFICATE [--file PATH]
  proxysieve inspect ca rotate [--file PATH]
  proxysieve config validate [--file PATH] [--set dotted.path=value]
  proxysieve config print-effective [--file PATH] [--set dotted.path=value]

Status: local development build; HTTP/SOCKS5 listeners, upstream routing,
authenticated dashboard and traffic accounting are available with fail-closed defaults.
`

// Run executes a command and returns a process exit code: 0 success, 1 output
// failure/unavailable feature, or 2 invalid usage. It never opens a listener.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return writeHelp(stdout)
	}
	switch args[0] {
	case "start":
		home, err := os.UserHomeDir()
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_HOME_UNAVAILABLE\n")
			return 1
		}
		return runStart(args[1:], stdout, stderr, home, environment())
	case "config":
		home, err := os.UserHomeDir()
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_HOME_UNAVAILABLE\n")
			return 1
		}
		return runConfig(args[1:], stdout, stderr, home, environment())
	case "doctor":
		home, err := os.UserHomeDir()
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_HOME_UNAVAILABLE\n")
			return 1
		}
		return runDoctor(args[1:], stdout, stderr, home, environment())
	case "db":
		home, err := os.UserHomeDir()
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_HOME_UNAVAILABLE\n")
			return 1
		}
		return runDB(args[1:], stdout, stderr, home, environment())
	case "backup", "restore":
		home, err := os.UserHomeDir()
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_HOME_UNAVAILABLE\n")
			return 1
		}
		return runOperations(append([]string{args[0]}, args[1:]...), stdout, stderr, home, environment())
	case "export", "import":
		home, err := os.UserHomeDir()
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_HOME_UNAVAILABLE\n")
			return 1
		}
		return runExport(append([]string{args[0]}, args[1:]...), stdout, stderr, home, environment())
	case "session":
		return runSession(args[1:], stdout, stderr, environment())
	case "cache":
		return runCache(args[1:], stdout, stderr, environment())
	case "extension":
		return runExtension(args[1:], stdout, stderr)
	case "inspect":
		home, err := os.UserHomeDir()
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_HOME_UNAVAILABLE\n")
			return 1
		}
		return runInspect(args[1:], stdout, stderr, home, environment())
	case "help", "--help", "-h":
		if len(args) != 1 {
			return usageError(stderr)
		}
		return writeHelp(stdout)
	case "version", "--version":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			return usageError(stderr)
		}
		info := buildinfo.Current()
		var err error
		if len(args) == 2 {
			err = json.NewEncoder(stdout).Encode(info)
		} else {
			_, err = fmt.Fprintf(stdout, "%s %s\nAuthor: %s\nLicense: %s\nCommit: %s\nBuilt: %s\nGo: %s\nPlatform: %s\n",
				info.Name, info.Version, info.Author, info.License, info.Commit,
				info.BuildDate, info.GoVersion, info.Platform)
		}
		if err != nil {
			return 1
		}
		return 0
	default:
		return usageError(stderr)
	}
}

func writeHelp(w io.Writer) int {
	if _, err := io.WriteString(w, help); err != nil {
		return 1
	}
	return 0
}

func usageError(w io.Writer) int {
	// Do not echo arbitrary arguments: future commands may contain credentials.
	_, _ = io.WriteString(w, "INVALID_USAGE: run proxysieve help for supported commands.\n")
	return 2
}
