// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

// Package buildinfo exposes public-safe binary provenance, without environment data.
package buildinfo

import "runtime"

// These values are set with -ldflags by release builds.
var (
	Version = "0.0.0-dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Author identifies the project's creator and maintainer.
const Author = "Tony Nguyen"

// Info contains only metadata safe to expose in CLI/API output.
type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
	Author    string `json:"author"`
	License   string `json:"license"`
}

// Current returns a snapshot, not mutable shared application state.
func Current() Info {
	return Info{
		Name: "ProxySieve", Version: Version, Commit: Commit, BuildDate: Date,
		GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Author: Author, License: "Apache-2.0",
	}
}
