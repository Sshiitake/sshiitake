// Package main is the ssht CLI entry point.
package main

import (
	"fmt"
	"os"
)

// These are set at build time via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ssht: "+err.Error())
		os.Exit(classifyError(err))
	}
}
