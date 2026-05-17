// Package tunnel opens a single SSH tunnel using golang.org/x/crypto/ssh
// and forwards a local port through it. It is consumed by cmd/ssht.
//
// Phase 1 supports only local forwards; remote and dynamic land in
// Phase 2.
package tunnel
