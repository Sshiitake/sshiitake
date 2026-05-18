package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Sentinel errors for host-key verification outcomes. classifyError
// in errors.go uses errors.Is to route these to the right exit code,
// and tests assert behaviour via errors.Is rather than coupling to
// the exact display string.
var (
	// ErrKeyMismatch is wrapped into the error returned when known_hosts
	// has a saved key for the host but it does not match the key the
	// server presented. The most security-critical signal in the binary.
	ErrKeyMismatch = errors.New("ssh host key mismatch")

	// ErrHostNotInKnownHosts is wrapped when the host has no entry in
	// known_hosts at all.
	ErrHostNotInKnownHosts = errors.New("ssh host not in known_hosts")
)

// buildHostKeyCallback returns the host-key verification callback used by
// the SSH client. Strategy:
//
//  1. If SSHT_TEST_HOSTKEY is set (a base64-encoded ssh.PublicKey),
//     pin to that key. Intended for the integration test fixture only.
//     A loud warning is logged in non-test binaries to discourage
//     mis-use, since it bypasses known_hosts verification entirely.
//  2. Otherwise, the callback is backed by knownHostsPath. Missing
//     entries surface as actionable errors; mismatched keys surface
//     as a loud security warning.
//
// knownHostsPath may be empty; in that case the default resolves to
// $HOME/.ssh/known_hosts.
func buildHostKeyCallback(knownHostsPath string) (ssh.HostKeyCallback, error) {
	if pinned := os.Getenv("SSHT_TEST_HOSTKEY"); pinned != "" {
		if !testing.Testing() {
			fmt.Fprintln(os.Stderr,
				"ssht: WARNING - SSHT_TEST_HOSTKEY is set in a non-test binary; "+
					"this BYPASSES ~/.ssh/known_hosts verification. "+
					"If you didn't intend this, unset the variable.")
		}
		raw, err := base64.StdEncoding.DecodeString(pinned)
		if err != nil {
			return nil, fmt.Errorf("SSHT_TEST_HOSTKEY: %w", err)
		}
		pub, err := ssh.ParsePublicKey(raw)
		if err != nil {
			return nil, fmt.Errorf("SSHT_TEST_HOSTKEY: %w", err)
		}
		return ssh.FixedHostKey(pub), nil
	}

	if knownHostsPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("known_hosts: home dir: %w", err)
		}
		knownHostsPath = filepath.Join(home, ".ssh", "known_hosts")
	}

	if _, err := os.Stat(knownHostsPath); err != nil {
		return nil, fmt.Errorf("known_hosts: %w (run `ssh-keyscan -H <host> >> %s` first, "+
			"or pass --known-hosts <path>)", err, knownHostsPath)
	}

	khCallback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("known_hosts %s: %w", knownHostsPath, err)
	}

	return wrapKnownHostsCallback(khCallback, knownHostsPath), nil
}

// wrapKnownHostsCallback turns the raw knownhosts.New callback into one
// that returns user-friendly errors with the exact ssh-keyscan command
// needed to register a missing host, and a loud security warning when
// the server's key disagrees with the saved one. The errors wrap
// ErrKeyMismatch / ErrHostNotInKnownHosts so classifyError can route
// them to exit code 2 via errors.Is.
func wrapKnownHostsCallback(inner ssh.HostKeyCallback, khPath string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := inner(hostname, remote, key)
		if err == nil {
			return nil
		}

		host, port := splitHostKeyHostname(hostname)
		scanCmd := keyscanInvocation(host, port)
		target := keygenTarget(host, port)

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			// keyErr.Want is non-nil and non-empty => host has a saved key
			// that disagrees with the presented one (MITM or rotation).
			if len(keyErr.Want) > 0 {
				return fmt.Errorf(
					"KEY MISMATCH for %s - saved key disagrees with the host's current key. "+
						"This could be a server reinstall, key rotation, or an active MITM. "+
						"If you trust the change, remove the old entry: "+
						"`ssh-keygen -R %s -f %s`, then re-add with `%s >> %s`: %w",
					hostname, target, khPath, scanCmd, khPath, ErrKeyMismatch,
				)
			}
			// keyErr.Want empty => host not in known_hosts at all.
			return fmt.Errorf(
				"host %s not in %s. To trust it, run: "+
					"`%s >> %s` (verify the fingerprint matches the server's reported one first): %w",
				target, khPath, scanCmd, khPath, ErrHostNotInKnownHosts,
			)
		}

		// Any other failure mode (malformed file, IO error mid-read).
		return fmt.Errorf("known_hosts %s: %w", khPath, err)
	}
}

// splitHostKeyHostname returns (host, port). port is "" when the
// hostname was not in host:port form (defensive; the SSH library
// passes "host:port").
func splitHostKeyHostname(hostnamePort string) (host, port string) {
	if h, p, err := net.SplitHostPort(hostnamePort); err == nil {
		return h, p
	}
	return hostnamePort, ""
}

// keyscanInvocation builds the ssh-keyscan command that, when run,
// produces a known_hosts entry that matches the host the user is
// trying to reach. On port 22, no -p is needed. On non-standard
// ports, -p is required so the entry is keyed as [host]:port.
func keyscanInvocation(host, port string) string {
	if port == "" || port == "22" {
		return fmt.Sprintf("ssh-keyscan -H %s", host)
	}
	return fmt.Sprintf("ssh-keyscan -H -p %s %s", port, host)
}

// keygenTarget builds the host argument for ssh-keygen -R, which
// must match how known_hosts stores the entry. Port 22 is bare;
// non-standard ports use the [host]:port bracket form.
func keygenTarget(host, port string) string {
	if port == "" || port == "22" {
		return host
	}
	return fmt.Sprintf("[%s]:%s", host, port)
}
