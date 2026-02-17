// Package main is the entry point for the op CLI —
// the unified Organic Programming dispatcher.
package main

import (
	"fmt"
	"os"

	"github.com/organic-programming/grace-op/internal/cli"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		cli.PrintUsage()
		os.Exit(0)
	}

	code := cli.Run(os.Args[1:], version)
	os.Exit(code)
}

func init() {
	// Ensure clean error output without log prefixes.
	fmt.Sprintf("") //nolint - keep init for future use
}
