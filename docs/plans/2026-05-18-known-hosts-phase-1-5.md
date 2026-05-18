# Phase 1.5: known_hosts host-key verification

> **For agentic workers:** Use superpowers:subagent-driven-development to execute. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Replace the `SSHT_TEST_HOSTKEY`-only host-key verification (Phase 1) with full `~/.ssh/known_hosts` integration, so `ssht up <name>` works against real SSH hosts without environment hacks.

**Architecture:** Extend `buildHostKeyCallback` in `cmd/ssht/up.go`. Chain: (1) `SSHT_TEST_HOSTKEY` env var (preserved for tests), then (2) `--known-hosts` flag or `~/.ssh/known_hosts`. Use `golang.org/x/crypto/ssh/knownhosts.New` for the heavy lifting. On `KeyError`, return a clear actionable message including the `ssh-keyscan` command needed.

**Tech stack:** `golang.org/x/crypto/ssh/knownhosts` (already in indirect dep tree via x/crypto). No new modules.

**Scope:** This is the most-blocking item from issue #1 (the Phase 2 follow-ups). Pulled forward because day-1 users hit the limitation immediately.

---

## File Structure

```
sshiitake/
├── cmd/ssht/
│   ├── up.go          [modify: extend buildHostKeyCallback, add --known-hosts flag]
│   ├── hostkey.go     [new: knownhosts callback + error wrapping (split from up.go for clarity)]
│   ├── hostkey_test.go [new: test the chain logic]
│   └── main_test.go   [no change — existing TestUp_endToEnd uses SSHT_TEST_HOSTKEY]
├── README.md          [modify: drop the SSHT_TEST_HOSTKEY note from Quick Start]
└── docs/design/
    └── 2026-05-17-sshiitake-design.md  [modify: move known_hosts from Phase 4 → Phase 1.5 shipped]
```

### Boundaries

- `hostkey.go` owns the callback construction and the error-message formatting. Tested in isolation.
- `up.go` only knows "give me a HostKeyCallback for this tunnel" — it doesn't care which strategy.
- Wire the `--known-hosts` flag through cobra; default value is empty, in which case the callback resolves `~/.ssh/known_hosts`.

---

## Task 1: Tests for the host-key callback chain

**Files:** Create `cmd/ssht/hostkey_test.go`.

- [ ] **Step 1: Write the tests**

```go
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestBuildHostKey_envVarWins(t *testing.T) {
	pub := genHostKey(t)
	t.Setenv("SSHT_TEST_HOSTKEY", base64.StdEncoding.EncodeToString(pub.Marshal()))

	cb, err := buildHostKeyCallback("/nonexistent/known_hosts")
	require.NoError(t, err)
	require.NotNil(t, cb)

	// Callback should accept the pinned key without consulting the file.
	require.NoError(t, cb("host:22", nil, pub))
}

func TestBuildHostKey_knownHostsHit(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	pub := genHostKey(t)
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")

	line := "hudson " + pub.Type() + " " + base64.StdEncoding.EncodeToString(pub.Marshal()) + "\n"
	require.NoError(t, os.WriteFile(khPath, []byte(line), 0o600))

	cb, err := buildHostKeyCallback(khPath)
	require.NoError(t, err)

	// Simulate the SSH client invoking the callback with the matching key.
	addr := &fakeAddr{net: "tcp", str: "1.2.3.4:22"}
	require.NoError(t, cb("hudson:22", addr, pub))
}

func TestBuildHostKey_knownHostsMissing_returnsClearError(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	pub := genHostKey(t)
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	require.NoError(t, os.WriteFile(khPath, []byte(""), 0o600))

	cb, err := buildHostKeyCallback(khPath)
	require.NoError(t, err)

	addr := &fakeAddr{net: "tcp", str: "1.2.3.4:22"}
	err = cb("hudson:22", addr, pub)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hudson")
	assert.Contains(t, err.Error(), "ssh-keyscan", "error should tell user how to fix it")
}

func TestBuildHostKey_keyMismatch_returnsSecurityWarning(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	saved := genHostKey(t)
	presented := genHostKey(t) // different key for the same host

	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	line := "hudson " + saved.Type() + " " + base64.StdEncoding.EncodeToString(saved.Marshal()) + "\n"
	require.NoError(t, os.WriteFile(khPath, []byte(line), 0o600))

	cb, err := buildHostKeyCallback(khPath)
	require.NoError(t, err)

	addr := &fakeAddr{net: "tcp", str: "1.2.3.4:22"}
	err = cb("hudson:22", addr, presented)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KEY MISMATCH",
		"mismatched keys must surface as a loud security warning, not a generic error")
}

func TestBuildHostKey_noFile(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	cb, err := buildHostKeyCallback("/no/such/file/known_hosts")
	require.Error(t, err)
	require.Nil(t, cb)
	assert.Contains(t, err.Error(), "known_hosts")
}

// ----- helpers -----

func genHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(rsaKey)
	require.NoError(t, err)
	return signer.PublicKey()
}

type fakeAddr struct{ net, str string }

func (f *fakeAddr) Network() string { return f.net }
func (f *fakeAddr) String() string  { return f.str }
```

- [ ] **Step 2: Run tests to confirm compile failure**

Run: `go test ./cmd/ssht/`
Expected: `buildHostKeyCallback` already exists but takes 0 args; signature mismatch is the failure.

---

## Task 2: Implement the new `buildHostKeyCallback` with known_hosts chain

**Files:** Create `cmd/ssht/hostkey.go`. Modify `cmd/ssht/up.go` to remove the old `buildHostKeyCallback` and call the new one with the known-hosts path resolved from the `--known-hosts` flag (or default `~/.ssh/known_hosts`).

- [ ] **Step 1: Create `cmd/ssht/hostkey.go`**

```go
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
```

- [ ] **Step 2: Update `cmd/ssht/up.go`**

Remove the existing `buildHostKeyCallback` function (it's now in `hostkey.go`).

Add a `--known-hosts` flag and pass its value through. The `upCmd` function should look like (only the changed parts shown):

```go
func upCmd() *cobra.Command {
	var (
		cfgPath        string
		sshCfgPath     string
		knownHostsPath string // NEW
		listenFile     string
	)
	cmd := &cobra.Command{
		// ... unchanged ...
		RunE: func(cmd *cobra.Command, args []string) error {
			// ... unchanged ...

			hostKeyCB, err := buildHostKeyCallback(knownHostsPath) // CHANGED: pass path
			if err != nil {
				return err
			}

			// ... rest unchanged ...
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", defaultConfigPath(), "path to tunnels.toml")
	cmd.Flags().StringVar(&sshCfgPath, "ssh-config", "", "path to ssh_config (default ~/.ssh/config)")
	cmd.Flags().StringVar(&knownHostsPath, "known-hosts", "", "path to known_hosts (default ~/.ssh/known_hosts)") // NEW
	cmd.Flags().StringVar(&listenFile, "listen-file", "", "test-only: write listen addr to this path")
	_ = cmd.Flags().MarkHidden("listen-file")
	return cmd
}
```

- [ ] **Step 3: Run tests**

`go test ./cmd/ssht/ -v -run TestBuildHostKey` — all 5 new tests pass.
`go test ./cmd/ssht/ -v` — all 10 tests pass (5 existing + 5 new).
`go test -race ./...` — clean across all packages.
`~/go/bin/golangci-lint run ./...` — clean.

- [ ] **Step 4: Update error classification**

The classifier in `cmd/ssht/errors.go` already routes "host key" → exit code 2. Verify the new error messages contain a token the classifier matches. Quick read; no change expected.

- [ ] **Step 5: Manual smoke**

```bash
go build -o /tmp/ssht ./cmd/ssht

# Without any known_hosts setup — expect clear missing-host error
/tmp/ssht up dev-database

# After adding hudson to known_hosts
ssh-keyscan -H hudson >> ~/.ssh/known_hosts
/tmp/ssht up dev-database

# Expected: tunnel opens, listens on :8765 forwarding to hudson:localhost:5432
```

---

## Task 3: Update README and design spec

**Files:** `README.md`, `docs/design/2026-05-17-sshiitake-design.md`.

- [ ] **Step 1: README — drop the SSHT_TEST_HOSTKEY caveat**

Replace the "Note for Phase 1: Host-key verification currently requires..." callout with:

```markdown
> **First-time use:** `ssht up <name>` reads `~/.ssh/known_hosts` to verify the
> server. If the host isn't there yet, ssht tells you exactly what to run:
>
> ```
> ssh-keyscan -H hudson >> ~/.ssh/known_hosts
> ```
>
> Always verify the printed fingerprint matches the server's reported one
> before trusting it.
```

- [ ] **Step 2: Design spec — move known_hosts from Phase 4 to Phase 1.5**

In `docs/design/2026-05-17-sshiitake-design.md`, find the Phase 4 / known_hosts mention and update it: known_hosts shipped in Phase 1.5; the Phase 4 entry is now just "subprocess fallback + fsnotify hot-reload".

In the same file, in the "Phase 1 known limitations" section (or whatever it's called), remove the host-key bullet entirely.

---

## Task 4: Commit, push, red-team

- [ ] **Step 1: Single commit**

```bash
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(cli): known_hosts host-key verification (was Phase 4)

ssht up now reads ~/.ssh/known_hosts to verify SSH server identity,
making the binary actually usable against real hosts without the
SSHT_TEST_HOSTKEY env-var workaround required by the original Phase 1.

Strategy:
- SSHT_TEST_HOSTKEY env var still wins (preserves test fixture and
  one-off pinning).
- Otherwise, golang.org/x/crypto/ssh/knownhosts.New backs the callback
  against the path from --known-hosts or ~/.ssh/known_hosts.
- Missing entries: clear error with the exact ssh-keyscan command
  needed.
- Mismatched keys: loud "KEY MISMATCH" warning with rotation/MITM
  guidance.

Pulled forward from Phase 4 because day-one users hit the limitation
immediately (issue #1 follow-up).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 2: Push the branch**

```bash
git push -u origin feature/known-hosts
```

- [ ] **Step 3: Open PR for red-team**

After CI lands green, run red-team review per repo policy. Address any blockers. Merge to main once approved.

---

## Success criteria

1. `ssht up dev-database` works against `hudson` after a one-time `ssh-keyscan` — no env-var hacks needed.
2. Host not in known_hosts → clear actionable error telling user the exact command to fix it.
3. Server key mismatched against saved one → loud KEY MISMATCH warning, not a silent or generic error.
4. Tests at unit-level for all 4 callback states (env-var, hit, missing, mismatch) plus the no-file path.
5. All existing tests still pass with race detector.
6. CI lint, test (linux + macos), build, vuln, gitleaks all green.

## Known limitations after this lands

- `@cert-authority` markers in known_hosts are not handled (knownhosts.New treats them as regular entries; verification will fail for certificate-authority-signed host keys). Document as a Phase 2 followup if anyone hits it.
- Hashed known_hosts entries (the `|1|...` format ssh-keyscan -H produces) ARE supported by knownhosts.New, so the README's recommended `-H` flag works correctly.
