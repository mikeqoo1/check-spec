// Command check-spec audits whether Arceus-generated code matches its spec.
//
// Usage:
//
//	check-spec verify --change <id> [--base <ref>] [--head <ref>]
//	check-spec list
//	check-spec version
//
// See `check-spec verify --help` for the full flag set, and README.md for
// the GitHub Action wrapper.
package main

import (
	"os"

	"github.com/mikeqoo1/check-spec/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
