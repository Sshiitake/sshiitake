package main

import (
	"errors"
	"strings"
)

// classifyError maps an error to a process exit code:
//
//	0   - no error
//	1   - configuration error (bad TOML, missing tunnel, validation)
//	2   - SSH or network error (handshake, dial, host key)
//	130 - interrupted by signal (handled separately in main)
func classifyError(err error) int {
	if err == nil {
		return 0
	}
	// Security-critical: host-key issues route to exit 2 regardless of
	// how the wrapper formats the message.
	if errors.Is(err, ErrKeyMismatch) || errors.Is(err, ErrHostNotInKnownHosts) {
		return 2
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "load "),
		strings.Contains(msg, "validate"),
		strings.Contains(msg, "not found in"),
		strings.Contains(msg, "no such file"),
		strings.Contains(msg, "unknown type"),
		strings.Contains(msg, "out of range"):
		return 1
	case strings.Contains(msg, "ssh "),
		strings.Contains(msg, "dial "),
		strings.Contains(msg, "handshake"),
		strings.Contains(msg, "host key"):
		return 2
	default:
		return 1
	}
}
