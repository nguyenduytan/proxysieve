// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

// Command proxysieve is the ProxySieve traffic gateway's CLI entry point.
package main

import (
	"os"

	"github.com/nguyenduytan/proxysieve/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
