package tui

import "strings"

// sanitiseForTerminal removes control bytes and ANSI escape sequences
// from user-uncontrolled strings before rendering them to the TTY.
// Allows tab and newline; strips everything else outside printable ASCII
// + common UTF-8.
func sanitiseForTerminal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			// C0 control byte or DEL, drop.
		case r >= 0x80 && r < 0xa0:
			// C1 control byte block, drop.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
