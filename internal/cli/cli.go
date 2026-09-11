// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the command boundary without process-global flag state.
package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/nguyenduytan/proxysieve/internal/buildinfo"
)

const help = `ProxySieve — Smart traffic control for paid proxies.
Created by Tony Nguyen. Licensed under Apache-2.0.

Usage:
  proxysieve help
  proxysieve version [--json]

Status: repository bootstrap; gateway and dashboard are not available yet.
The start command will be enabled when the gateway milestone is implemented.
`

// Run executes a command and returns a process exit code: 0 success, 1 output
// failure/unavailable feature, or 2 invalid usage. It never opens a listener.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return writeHelp(stdout)
	}
	switch args[0] {
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
	case "start":
		// A bootstrap must not silently open an unauthenticated or direct proxy.
		_, _ = io.WriteString(stderr, "GATEWAY_NOT_IMPLEMENTED: start is unavailable in the bootstrap build. No listener was opened.\n")
		return 1
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
