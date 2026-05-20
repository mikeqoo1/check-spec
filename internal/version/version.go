// Package version exposes build-time version strings populated by ldflags.
//
// See Makefile for the -X assignments at build time.
package version

// Version is the semver tag from `git describe --tags`, or "dev" for
// development builds.
var Version = "dev"

// Commit is the short git SHA.
var Commit = "none"

// Date is the build timestamp in UTC.
var Date = "unknown"
