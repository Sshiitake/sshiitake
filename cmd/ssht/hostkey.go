package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// buildHostKeyCallback returns the host-key verification callback used by
// the SSH client. The strategy is:
//
//  1. If SSHT_TEST_HOSTKEY is set, return a ssh.FixedHostKey for that
//     pinned key. (Used by integration tests and one-off pinning.)
//  2. Otherwise, return a callback backed by the file at knownHostsPath.
//     Missing files and missing entries surface as actionable errors;
//     mismatched keys surface as a loud security warning.
//
// knownHostsPath may be empty; in that case the default resolves to
// $HOME/.ssh/known_hosts.
func buildHostKeyCallback(knownHostsPath string) (ssh.HostKeyCallback, error) {
	if pinned := os.Getenv("SSHT_TEST_HOSTKEY"); pinned != "" {
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
// the server's key disagrees with the saved one.
func wrapKnownHostsCallback(inner ssh.HostKeyCallback, khPath string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := inner(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			// keyErr.Want is non-nil and non-empty => host has a saved key
			// that disagrees with the presented one (MITM or rotation).
			if len(keyErr.Want) > 0 {
				return fmt.Errorf(
					"KEY MISMATCH for %s — saved key disagrees with the host's current key. "+
						"This could be a server reinstall, key rotation, or an active MITM. "+
						"If you trust the change, remove the old entry: "+
						"`ssh-keygen -R %s -f %s`, then re-add with `ssh-keyscan -H %s >> %s`",
					hostname, hostKeyHostname(hostname), khPath,
					hostKeyHostname(hostname), khPath,
				)
			}
			// keyErr.Want empty => host not in known_hosts at all.
			return fmt.Errorf(
				"host %s not in %s. To trust it, run: "+
					"`ssh-keyscan -H %s >> %s` (verify the fingerprint matches the server's reported one first)",
				hostKeyHostname(hostname), khPath,
				hostKeyHostname(hostname), khPath,
			)
		}

		// Any other failure mode (malformed file, IO error mid-read).
		return fmt.Errorf("known_hosts %s: %w", khPath, err)
	}
}

// hostKeyHostname strips the trailing ":port" from a hostname that
// the SSH library passes to the callback (which is "host:port" form).
// known_hosts entries are typically keyed by hostname only.
func hostKeyHostname(hostnamePort string) string {
	if host, _, err := net.SplitHostPort(hostnamePort); err == nil {
		return host
	}
	return hostnamePort
}
