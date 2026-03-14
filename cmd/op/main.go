// Package main is the entry point for the op CLI —
// the unified Organic Programming dispatcher.
package main

import (
	"fmt"
	"os"

	"github.com/organic-programming/grace-op/internal/cli"
)

var (
	version = "v0.3.2"
	commit  = "unknown" // set via: -ldflags "-X main.commit=..."
)

func main() {
	if len(os.Args) < 2 {
		cli.PrintUsage()
		os.Exit(0)
	}

	versionStr := version
	if commit != "unknown" && len(commit) >= 7 {
		versionStr = version + " (" + commit[:7] + ")"
	}

	code := cli.Run(os.Args[1:], versionStr)
	os.Exit(code)
}

func init() {
	// Ensure clean error output without log prefixes.
	_ = fmt.Sprintf("") //nolint - keep init for future use
}
