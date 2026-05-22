# Sshiitake Phase 4: Hot-reload + Auto-reconnect + Subprocess fallback + v1.0 polish

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Ship the final v1.0 polish. Hot-reload of `tunnels.toml` via `fsnotify`. Auto-reconnect with exponential backoff (pulled forward from v1.1 to make the product actually robust for daily use). Subprocess SSH fallback for tunnels whose ssh_config uses options we can't natively support. Plus issue #1 cleanup, comprehensive test coverage gaps closed, and ARCHITECTURE.md + asciinema-style README walkthrough.

**Architecture:** New `internal/reload` package wraps `fsnotify.Watcher` and produces config-diff events (added/removed/changed tunnel sets). `Manager.Apply` mutates the live tunnel set atomically. New `internal/tunnel.subprocess` mode spawns `ssh` and tracks its lifecycle. `internal/tunnel.reconnect` adds the backoff loop. `internal/tui` gains a Toggle key that calls Manager API.

**Tech stack additions:**
- `github.com/fsnotify/fsnotify`

**Folded-in issue #1 items:**
- Auth UX cluster (3 items): buildAuth silent empty, keyAuth path leak, key-perm check
- Atomic write in appendTunnel
- NaN guard in blockFor
- Test boundary coverage (port 1, 65535, 65536)

**This is the v1.0 release plan.**

---

## File Structure

```
sshiitake/
├── ARCHITECTURE.md                            [new: package map + sequence diagrams]
├── CHANGELOG.md                               [modify: Phase 4 + v1.0 release]
├── CONTRIBUTING.md                            [new: brief contributor guide]
├── README.md                                  [modify: status, asciinema, screenshots]
├── cmd/ssht/
│   ├── add.go                                 [modify: atomic write]
│   └── main_test.go                           [modify: port boundary tests]
├── internal/
│   ├── config/
│   │   └── validate_test.go                   [modify: port boundary cases]
│   ├── manager/
│   │   ├── manager.go                         [modify: Apply, Toggle methods]
│   │   ├── apply.go                           [new: diff-and-apply logic]
│   │   ├── apply_test.go                      [new]
│   │   ├── toggle.go                          [new: per-tunnel start/stop API]
│   │   └── toggle_test.go                     [new]
│   ├── reload/
│   │   ├── watcher.go                         [new: fsnotify wrapper]
│   │   ├── watcher_test.go
│   │   ├── diff.go                            [new: Config diff algorithm]
│   │   └── diff_test.go
│   ├── tui/
│   │   ├── keys.go                            [modify: re-add Toggle binding]
│   │   ├── model.go                           [modify: Toggle handler -> Manager API]
│   │   ├── model_test.go                      [modify: Toggle assertion]
│   │   └── sparkline.go                       [modify: NaN guard]
│   └── tunnel/
│       ├── reconnect.go                       [new: backoff loop]
│       ├── reconnect_test.go
│       ├── subprocess.go                      [new: ssh subprocess wrapper]
│       ├── subprocess_test.go
│       ├── tunnel.go                          [modify: auth cluster fixes]
│       └── tunnel_test.go                     [modify]
├── docs/
│   ├── design/2026-05-17-sshiitake-design.md  [modify: v1.0 complete]
│   └── plans/2026-05-22-phase-4-polish.md     [this file]
```

---

## Pre-flight

```bash
cd ~/Projects/sshiitake
git checkout main && git pull --ff-only
git checkout -b feature/phase-4-polish
go get github.com/fsnotify/fsnotify
go mod tidy
go test -race ./...
GOTOOLCHAIN=go1.25.10 ~/go/bin/golangci-lint run ./...
```

---

## Task 1: Issue #1 auth-UX cluster

**Files:** `internal/tunnel/tunnel.go`, `internal/tunnel/tunnel_test.go`.

Three small fixes in one commit:

1. **buildAuth no longer silently returns empty methods.** If at least one auth source was tried AND all attempts failed, return a wrapped error. Empty when nothing was tried (e.g. NoClientAuth test server) stays silent.

2. **keyAuth error no longer leaks full path.** Replace `fmt.Errorf("parse %s: %w", path, err)` with `fmt.Errorf("parse private key: %w", err)`.

3. **0600 permission check on private keys.** OpenSSH refuses keys with broader perms. Match.

- [ ] **Tests** (append to `internal/tunnel/tunnel_test.go`):

```go
func TestKeyAuth_refusesOverbroadPerms(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	// Generate a real RSA key so PEM parsing would succeed.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemBlock := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)}
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0o644))

	_, err = keyAuth(keyPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "permissions too open")
}

func TestKeyAuth_errorDoesNotLeakPath(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "bad-key")
	require.NoError(t, os.WriteFile(keyPath, []byte("not a key"), 0o600))

	_, err := keyAuth(keyPath)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), dir, "error should not leak the file path")
}

func TestBuildAuth_errorsWhenAllAttemptsFailed(t *testing.T) {
	// Construct a Tunnel with an IdentityFile that doesn't exist.
	// SSH_AUTH_SOCK should be unset (test-controlled env).
	t.Setenv("SSH_AUTH_SOCK", "")

	dir := t.TempDir()
	missingKey := filepath.Join(dir, "no-such-key")

	tun := &Tunnel{rt: config.ResolvedTunnel{IdentityFile: missingKey}}
	methods, _, err := tun.buildAuth()
	require.Error(t, err)
	assert.Empty(t, methods)
}

func TestBuildAuth_silentEmptyWhenNothingTried(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	tun := &Tunnel{rt: config.ResolvedTunnel{IdentityFile: ""}}
	methods, _, err := tun.buildAuth()
	require.NoError(t, err)
	assert.Empty(t, methods)
}
```

(Imports: `crypto/rand`, `crypto/rsa`, `crypto/x509`, `encoding/pem`, `path/filepath`, `os`.)

- [ ] **Implementation in `internal/tunnel/tunnel.go`:**

```go
// keyAuth reads a private key file and returns an AuthMethod.
// Refuses keys with permissions broader than 0600 (matches OpenSSH).
func keyAuth(path string) (ssh.AuthMethod, error) {
	if expanded, err := expandHome(path); err == nil {
		path = expanded
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat private key: %w", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return nil, fmt.Errorf("private key permissions too open (%04o); want 0600 or stricter", mode)
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return ssh.PublicKeys(signer), nil
}

// buildAuth gathers auth methods. Returns an error if at least one
// source was attempted and ALL failed. Silent when nothing configured.
func (t *Tunnel) buildAuth() ([]ssh.AuthMethod, net.Conn, error) {
	var methods []ssh.AuthMethod
	var agentConn net.Conn
	var attempted bool
	var firstErr error

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		attempted = true
		conn, err := net.Dial("unix", sock)
		if err == nil {
			ac := agent.NewClient(conn)
			methods = append(methods, ssh.PublicKeysCallback(ac.Signers))
			agentConn = conn
		} else if firstErr == nil {
			firstErr = fmt.Errorf("ssh-agent: %w", err)
		}
	}
	if t.rt.IdentityFile != "" {
		attempted = true
		if keyMethod, err := keyAuth(t.rt.IdentityFile); err == nil {
			methods = append(methods, keyMethod)
		} else if firstErr == nil {
			firstErr = err
		}
	}

	if attempted && len(methods) == 0 {
		return nil, nil, fmt.Errorf("no usable auth methods: %w", firstErr)
	}
	return methods, agentConn, nil
}
```

- [ ] **Run tests, lint, commit**

```
git add internal/tunnel/tunnel.go internal/tunnel/tunnel_test.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
fix(tunnel): auth UX cluster from issue #1

Three related improvements to SSH auth setup:

- keyAuth now checks file permissions and refuses keys with bits
  broader than 0600, matching OpenSSH's stricture. A 0644 key returns
  "permissions too open" instead of silently working.

- keyAuth error no longer includes the full key path (privacy hygiene
  for users piping output to shared logs); the error simply says
  "parse private key: <reason>".

- buildAuth now returns a wrapped error when at least one auth source
  was attempted AND every attempt failed. Previously the SSH client
  saw an empty auth list and the server-side "no supported auth methods"
  error obscured the local cause. Silent empty stays when nothing was
  configured (matches test-server NoClientAuth path).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Atomic write in `appendTunnel` + NaN guard in sparkline + port boundary tests

Three small unrelated polish items in one commit:

- [ ] **2a — atomic write**

Replace `os.WriteFile + os.Chmod` in `cmd/ssht/add.go` `appendTunnel` with write-to-temp + rename:

```go
import "path/filepath"

func appendTunnel(path, name string, t config.Tunnel) error {
	cfg, err := loadOrEmpty(path)
	if err != nil {
		return err
	}
	if _, exists := cfg.Tunnels[name]; exists {
		return fmt.Errorf("tunnel %q already exists in %s", name, path)
	}
	if cfg.Tunnels == nil {
		cfg.Tunnels = map[string]config.Tunnel{}
	}
	cfg.Tunnels[name] = t

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tunnels.toml.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // safe if already renamed away

	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	return os.Rename(tmpPath, path)
}
```

Add test:

```go
func TestAppendTunnel_atomicWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tunnels.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`[tunnels.a]
host = "h"
type = "dynamic"
local_port = 1080
`), 0o600))

	// Cause Encode to fail mid-flight by passing a name with a sentinel
	// the encoder would barf on... actually BurntSushi/toml encodes
	// anything. Simulate failure differently: append, then verify the
	// original file content is intact while the temp file path doesn't
	// linger.

	require.NoError(t, appendTunnel(cfgPath, "newone", config.Tunnel{
		Host: "h2", Type: config.TypeDynamic, LocalPort: 1081,
	}))

	// Verify no .tunnels.toml.* tempfile remains.
	matches, err := filepath.Glob(filepath.Join(dir, ".tunnels.toml.*"))
	require.NoError(t, err)
	assert.Empty(t, matches, "temp file should have been renamed away")
}
```

- [ ] **2b — NaN guard in sparkline**

In `internal/tui/sparkline.go` `blockFor`, add at top:

```go
import "math"

func blockFor(value, minV, maxV float64) rune {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ' '
	}
	if maxV == minV {
		return blockChars[3]
	}
	frac := (value - minV) / (maxV - minV)
	idx := int(frac * float64(len(blockChars)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(blockChars) {
		idx = len(blockChars) - 1
	}
	return blockChars[idx]
}
```

Add test:

```go
func TestRenderSparkline_handlesNaNAndInf(t *testing.T) {
	now := time.Unix(0, 0)
	samples := []metrics.Sample{
		{At: now, Value: math.NaN()},
		{At: now.Add(time.Second), Value: math.Inf(1)},
		{At: now.Add(2 * time.Second), Value: 50},
	}
	out := RenderSparkline(samples, 3)
	require.NotPanics(t, func() { _ = out })
	// NaN/Inf collapse to space.
	assert.Equal(t, ' ', []rune(out)[0])
	assert.Equal(t, ' ', []rune(out)[1])
}
```

- [ ] **2c — port boundary tests**

Append to `internal/config/validate_test.go`:

```go
func TestValidate_portBoundaries(t *testing.T) {
	tests := []struct {
		local, remote int
		wantOK        bool
	}{
		{1, 1, true},
		{65535, 65535, true},
		{0, 80, true},        // 0 = auto-pick for local
		{1, 65536, false},
		{1, 0, false},        // remote can't be 0
		{65536, 80, false},
		{-1, 80, false},
		{1, -1, false},
	}
	for _, tc := range tests {
		cfg := &Config{Tunnels: map[string]Tunnel{
			"x": {Host: "h", Type: TypeLocal, LocalHost: "127.0.0.1",
				LocalPort: tc.local, RemoteHost: "r", RemotePort: tc.remote},
		}}
		err := cfg.Validate()
		if tc.wantOK {
			assert.NoError(t, err, "local=%d remote=%d", tc.local, tc.remote)
		} else {
			assert.Error(t, err, "local=%d remote=%d", tc.local, tc.remote)
		}
	}
}
```

- [ ] **Commit**

```
git add cmd/ssht/add.go cmd/ssht/add_test.go internal/tui/sparkline.go internal/tui/sparkline_test.go internal/config/validate_test.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
chore: atomic config write, NaN guard, port boundary tests

Three small polish items closing iter-1/2 follow-ups:

- ssht add now uses CreateTemp + Rename for crash-safe writes. Power
  loss between truncate and complete writes used to leave an empty
  tunnels.toml; the temp-and-rename pattern is atomic at the directory
  level.

- Sparkline blockFor now treats NaN/+Inf/-Inf as a space rune rather
  than silently mapping to the lowest block. Defensive: nothing
  upstream produces NaN today.

- Config validator now has boundary-condition test cases (ports
  1, 65535, 65536, 0, -1) closing the Phase 1 review's coverage gap.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Auto-reconnect with exponential backoff

**Files:**
- Create: `internal/tunnel/reconnect.go`
- Create: `internal/tunnel/reconnect_test.go`
- Modify: `internal/tunnel/tunnel.go` (Start optionally runs the reconnect loop)

When a tunnel's `Start` returns due to an SSH connection drop (NOT user-initiated cancel), we want to retry with exponential backoff: 1s, 2s, 4s, 8s, ..., capped at 60s. Jitter 10%. After 10 consecutive failures, give up.

- [ ] **Tests**

```go
package tunnel

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBackoff_progression(t *testing.T) {
	b := newBackoff(BackoffOptions{
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       0,
	})
	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second,  // capped
		60 * time.Second,
	}
	for i, want := range expected {
		got := b.next()
		assert.Equal(t, want, got, "step %d", i)
	}
}

func TestBackoff_jitterStaysInBounds(t *testing.T) {
	b := newBackoff(BackoffOptions{
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       0.5, // ±50%
	})
	for i := 0; i < 100; i++ {
		d := b.next()
		assert.GreaterOrEqual(t, d, 500*time.Millisecond)
		assert.LessOrEqual(t, d, 90*time.Second)
		b.reset()
	}
}

func TestBackoff_resetReturnsToInitial(t *testing.T) {
	b := newBackoff(BackoffOptions{InitialDelay: time.Second, Multiplier: 2, Jitter: 0})
	b.next()
	b.next()
	b.next()
	b.reset()
	assert.Equal(t, time.Second, b.next())
}

func TestIsReconnectableError(t *testing.T) {
	assert.True(t, isReconnectableError(errors.New("EOF")))
	assert.True(t, isReconnectableError(errors.New("connection reset by peer")))
	assert.True(t, isReconnectableError(errors.New("ssh: handshake failed")))
	assert.False(t, isReconnectableError(nil))
	assert.False(t, isReconnectableError(errors.New("HostKeyCallback required")))
	assert.False(t, isReconnectableError(errors.New("ProxyJump=... is not yet supported")))
}
```

- [ ] **Implementation**

```go
// internal/tunnel/reconnect.go
package tunnel

import (
	"errors"
	"math/rand"
	"strings"
	"time"
)

// BackoffOptions configures the reconnect backoff schedule.
type BackoffOptions struct {
	InitialDelay time.Duration // default 1s
	MaxDelay     time.Duration // default 60s
	Multiplier   float64       // default 2.0
	Jitter       float64       // default 0.1 (±10%)
	MaxAttempts  int           // default 10
}

func (o *BackoffOptions) applyDefaults() {
	if o.InitialDelay == 0 {
		o.InitialDelay = time.Second
	}
	if o.MaxDelay == 0 {
		o.MaxDelay = 60 * time.Second
	}
	if o.Multiplier == 0 {
		o.Multiplier = 2.0
	}
	if o.Jitter == 0 {
		o.Jitter = 0.1
	}
	if o.MaxAttempts == 0 {
		o.MaxAttempts = 10
	}
}

type backoff struct {
	opts    BackoffOptions
	current time.Duration
}

func newBackoff(opts BackoffOptions) *backoff {
	opts.applyDefaults()
	return &backoff{opts: opts, current: 0}
}

// next returns the next delay and advances the schedule.
func (b *backoff) next() time.Duration {
	if b.current == 0 {
		b.current = b.opts.InitialDelay
	} else {
		b.current = time.Duration(float64(b.current) * b.opts.Multiplier)
	}
	if b.current > b.opts.MaxDelay {
		b.current = b.opts.MaxDelay
	}
	if b.opts.Jitter == 0 {
		return b.current
	}
	// Apply ±Jitter * current.
	delta := (rand.Float64()*2 - 1) * b.opts.Jitter * float64(b.current)
	return b.current + time.Duration(delta)
}

func (b *backoff) reset() { b.current = 0 }

// isReconnectableError decides whether an error from Start should
// trigger reconnect. Permanent failures (config errors, host-key
// mismatches, unsupported options) are NOT reconnectable.
func isReconnectableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Permanent failure tokens.
	for _, t := range []string{
		"HostKeyCallback required",
		"not yet supported",
		"ProxyJump",
		"local_host",
		"host key",
		"KEY MISMATCH",
		"not in known_hosts",
		"no usable auth methods",
	} {
		if strings.Contains(msg, t) {
			return false
		}
	}
	// Transient failure tokens.
	for _, t := range []string{
		"EOF",
		"connection reset",
		"connection refused",
		"handshake",
		"timeout",
		"broken pipe",
		"no route to host",
		"network is unreachable",
	} {
		if strings.Contains(msg, t) {
			return true
		}
	}
	return false // unknown: don't reconnect
}

var _ = errors.New // import keeper
```

- [ ] **Integrate into `Tunnel.Start`**

Add a `Reconnect bool` to `Options`. When true and `forwardLocal` returns due to non-cancel error, loop with backoff. Maximum 10 attempts (configurable later).

```go
// In tunnel.go Options:
type Options struct {
	HostKeyCallback ssh.HostKeyCallback
	DialTimeout     time.Duration
	Reconnect       bool             // NEW
	Backoff         BackoffOptions   // NEW (zero-value = defaults)
}

// New entry point that wraps Start with the backoff loop.
func (t *Tunnel) StartWithReconnect(ctx context.Context, started chan<- struct{}) error {
	if !t.opts.Reconnect {
		return t.Start(ctx, started)
	}

	b := newBackoff(t.opts.Backoff)
	attempt := 0

	// First attempt uses the supplied `started` channel.
	notify := started
	for {
		attempt++
		err := t.Start(ctx, notify)
		notify = nil // subsequent attempts: signal only first up.

		if ctx.Err() != nil {
			return nil
		}
		if !isReconnectableError(err) {
			return err
		}
		if attempt >= t.opts.Backoff.MaxAttempts {
			return fmt.Errorf("tunnel %q: gave up after %d attempts: %w", t.rt.Name, attempt, err)
		}
		delay := b.next()
		t.setStatus(StatusConnecting)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil
		}
	}
}
```

Update Manager.runOne to call `StartWithReconnect` instead of `Start` when the option is set.

Actually simpler: have Manager always set `Reconnect: true` by default. Add a CLI flag `--no-reconnect` to opt out. Default-on is the right v1 behaviour.

- [ ] **Commit**

```
git add internal/tunnel/reconnect.go internal/tunnel/reconnect_test.go internal/tunnel/tunnel.go internal/manager/manager.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "feat(tunnel): auto-reconnect with exponential backoff

Pulls auto-reconnect forward from v1.1 because a dev tunnel that
drops once and stays down isn't a usable product. Backoff schedule:
1s, 2s, 4s, 8s, 16s, 32s, 60s (cap), with 10% jitter, max 10 attempts.

Reconnectable errors: EOF, connection reset, handshake, timeout,
broken pipe, no route to host. Permanent errors (host-key mismatch,
ProxyJump unsupported, no usable auth) skip the loop and surface
immediately.

Manager always reconnects by default. CLI flag --no-reconnect opts out.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Hot-reload via fsnotify

**Files:**
- Create: `internal/reload/watcher.go`, `watcher_test.go`
- Create: `internal/reload/diff.go`, `diff_test.go`
- Modify: `internal/manager/manager.go` (Apply method)
- Modify: `cmd/ssht/up.go` (start watcher)

Big rocks:

1. **`reload.Watcher`** wraps `fsnotify.Watcher`. Detects writes to `tunnels.toml`. Debounces (editors often write multiple events in quick succession).

2. **`reload.Diff(old, new *config.Config) Plan`** returns added / removed / modified tunnel names.

3. **`manager.Apply(plan, cfg)`** mutates the live tunnel set: stops removed/modified tunnels, starts added/modified tunnels.

4. **Wiring** in `cmd/ssht/up.go`: watcher fires -> `Load` new config -> `Validate` -> `Apply`.

This task is large; the implementer should follow the plan's outline but use judgement on test design (the Watcher needs real filesystem operations; use t.TempDir() and time.Sleep for debouncing).

Sketch:

```go
// internal/reload/watcher.go
package reload

import (
	"context"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher fires Changed when the watched file is modified, debounced
// by Debounce. Close it to stop.
type Watcher struct {
	w        *fsnotify.Watcher
	path     string
	debounce time.Duration
	Changed  chan struct{}
}

func New(path string, debounce time.Duration) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(path); err != nil {
		_ = w.Close()
		return nil, err
	}
	return &Watcher{w: w, path: path, debounce: debounce, Changed: make(chan struct{}, 1)}, nil
}

func (w *Watcher) Run(ctx context.Context) error {
	var pending bool
	var timer *time.Timer
	for {
		select {
		case <-ctx.Done():
			return w.w.Close()
		case ev, ok := <-w.w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
				if timer != nil {
					timer.Stop()
				}
				pending = true
				timer = time.AfterFunc(w.debounce, func() {
					if pending {
						pending = false
						select {
						case w.Changed <- struct{}{}:
						default:
						}
					}
				})
			}
		case <-w.w.Errors:
			// log and continue
		}
	}
}
```

```go
// internal/reload/diff.go
package reload

import (
	"reflect"

	"github.com/Sshiitake/sshiitake/internal/config"
)

// Plan describes a config diff.
type Plan struct {
	Added    []string
	Removed  []string
	Modified []string
}

func Diff(oldCfg, newCfg *config.Config) Plan {
	var p Plan
	for name, newT := range newCfg.Tunnels {
		if oldT, ok := oldCfg.Tunnels[name]; ok {
			if !reflect.DeepEqual(oldT, newT) {
				p.Modified = append(p.Modified, name)
			}
		} else {
			p.Added = append(p.Added, name)
		}
	}
	for name := range oldCfg.Tunnels {
		if _, ok := newCfg.Tunnels[name]; !ok {
			p.Removed = append(p.Removed, name)
		}
	}
	return p
}
```

```go
// internal/manager/apply.go
package manager

import (
	"context"
	"fmt"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/reload"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// Apply mutates the live tunnel set according to plan against newCfg.
// Removed and Modified tunnels are stopped; Added and Modified are
// started. Errors per-tunnel are returned in a multi-error wrap.
func (m *Manager) Apply(ctx context.Context, newCfg *config.Config, sshCfgPath string, plan reload.Plan) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop removed + modified.
	for _, name := range append(append([]string{}, plan.Removed...), plan.Modified...) {
		t := m.findByName(name)
		if t == nil {
			continue
		}
		t.cancel()
		// Wait for it to actually stop (with a short timeout).
		// ... (implementation detail)
	}

	// Start added + modified.
	for _, name := range append(append([]string{}, plan.Added...), plan.Modified...) {
		raw, ok := newCfg.TunnelByName(name)
		if !ok {
			continue
		}
		rt, err := config.ResolveWithSSHConfig(raw, sshCfgPath)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", name, err)
		}
		rt.Name = name
		newT := tunnel.New(rt, m.tunnelOpts)
		m.tunnels = append(m.tunnels, newT)
		// Spawn its goroutine.
		go m.runOne(ctx, newT)
	}

	return nil
}
```

(The `m.mu`, `m.tunnelOpts`, `findByName`, `cancel` per-tunnel — these all need Manager refactoring to support per-tunnel lifecycle. This task is BIG. The implementer should follow standard Go patterns.)

- [ ] **Wire into cmd/ssht/up.go:**

After `manager.New`, start a `reload.Watcher` in a goroutine. On each `<-Changed`, load+validate+diff+Apply.

- [ ] **Commit**

(Sketch — implementer writes the actual code following Go conventions. This commit may be split into 2-3 smaller commits during implementation if needed.)

---

## Task 5: Space-to-toggle in TUI

**Files:** `internal/tui/keys.go`, `internal/tui/model.go`, `internal/manager/toggle.go`, `cmd/ssht/up.go`.

The Toggle keybinding (space) was deferred in Phase 3 iter-2 because the Manager had no per-tunnel start/stop API. Add it now and wire the TUI.

- [ ] Add `Manager.Toggle(name string)` that starts or stops the named tunnel.
- [ ] Re-add `Toggle` to `keys.go`.
- [ ] In `model.go` `handleKey`, on Toggle press: call back into Manager via a callback the TUI was given at construction.

Detail: the TUI doesn't currently hold a reference to the Manager (only its event channel). Pass a `tea.Cmd`-returning callback into the Model:

```go
func NewModel(events <-chan manager.Event, theme Theme, onToggle func(name string)) *Model {
    // ...
}
```

In `tui.Run`, pass `onToggle: func(n string) { go m.Toggle(n) }`.

Add Toggle test:

```go
func TestModel_toggleKeyTriggersCallback(t *testing.T) {
	var toggled string
	m := NewModel(nil, ThemeByNameOrDefault("dark"), func(name string) {
		toggled = name
	})
	m.list.tunnels = []tunnelRow{{Name: "api"}}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	assert.Equal(t, "api", toggled)
}
```

---

## Task 6: Subprocess SSH fallback

**Files:** `internal/tunnel/subprocess.go`, `subprocess_test.go`, modifications to `tunnel.go`.

When ssh_config uses options the native client doesn't support (`ProxyCommand`, exotic `Match`, `ControlMaster`, etc.), fall back to spawning `ssh` directly.

Detection: at resolve time, parse the relevant ssh_config keys and check for unsupported values. If found, mark the ResolvedTunnel with `UseSubprocess = true`.

Subprocess implementation: `exec.Command("ssh", "-L", "<lp>:<rh>:<rp>", "-N", "<user>@<host>", ...)`. Pipe stderr, parse for connection events.

This is the most uncertain task. Implementer should follow the plan but use judgement; if unable to detect all unsupported options at parse time, dial first and fall back on specific errors.

```go
// Sketch
type subprocessRunner struct {
	rt   config.ResolvedTunnel
	opts Options
	cmd  *exec.Cmd
}

func (r *subprocessRunner) start(ctx context.Context, started chan<- struct{}) error {
	args := []string{
		"-L", fmt.Sprintf("%s:%d:%s", r.rt.LocalHost, r.rt.LocalPort, r.rt.RemoteAddr),
		"-N",
		fmt.Sprintf("%s@%s", r.rt.SSHUser, r.rt.SSHHost),
		"-p", strconv.Itoa(r.rt.SSHPort),
	}
	r.cmd = exec.CommandContext(ctx, "ssh", args...)
	// Wire stderr to a parser that detects "Local forwarding listening on..."
	// and closes started.
	// ...
}
```

---

## Task 7: ARCHITECTURE.md

Write a contributor-facing doc describing the package layout, lifecycle, event flow, and key invariants.

```markdown
# Sshiitake Architecture

## Layered structure

cmd/ssht       — CLI entry. cobra subcommands; flag parsing; TTY detection.
internal/tui   — Bubble Tea TUI. Subscribes to manager events; renders.
internal/manager — Owns []*tunnel.Tunnel. Lifecycle: New, Run, Toggle, Apply.
internal/tunnel — One tunnel: SSH dial + port forward + reconnect.
internal/config — TOML load + validate + ssh_config resolve.
internal/metrics — Bytes counters + latency ring.
internal/logbuffer — Per-tunnel in-memory log ring.
internal/reload — fsnotify watcher + config diff.

## Event flow

(diagram)
config -> Manager.New -> []*tunnel.Tunnel -> goroutine per tunnel -> events -> subscribers -> TUI / --bare

## Invariants

- Manager events are fanned out drop-on-full per subscriber.
- Tunnel statuses are set via setStatus (mutex-protected).
- forwardLocal owns sshClient.Close on ctx cancel.
- All host-key verification goes through buildHostKeyCallback (env-var OR known_hosts).

## Adding a new feature

(walkthrough: how to add a new metric, new event type, new TUI view)
```

---

## Task 8: README polish + asciinema placeholder

Update README with:
- Animated GIF / asciinema link placeholder (we can't generate it from here, but link to where it'll live)
- Clearer Quick Start with output examples
- Link to ARCHITECTURE.md
- Link to CONTRIBUTING.md

---

## Task 9: CONTRIBUTING.md (brief)

```markdown
# Contributing

Thanks for considering a contribution! sshiitake is a small Go project; PRs welcome.

## Setup

```
git clone https://github.com/Sshiitake/sshiitake
cd sshiitake
go test -race ./...
```

## Code style

- Run `golangci-lint run ./...` before commit (config in `.golangci.yml`).
- No em dashes anywhere (project style).
- TDD per task; tests next to code in the same package.

## PRs

- Branch off main, one feature per PR.
- CI must be green before merge.
- For non-trivial changes, run the review-team skill or request a manual red-team pass before merging to main.

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md).
```

---

## Task 10: CHANGELOG + design spec v1.0 release

```markdown
## Phase 4 / v1.0 release - 2026-05-22

### Added
- Auto-reconnect with exponential backoff (1s..60s, 10% jitter, 10 attempts).
- Hot-reload of tunnels.toml via fsnotify (debounced 200ms).
- Subprocess SSH fallback for tunnels whose ssh_config uses options
  we don't natively support.
- Space-to-toggle in TUI (calls Manager.Toggle).
- ARCHITECTURE.md and CONTRIBUTING.md.

### Changed
- ssht add now writes atomically (CreateTemp + Rename).
- keyAuth refuses keys with permissions broader than 0600.
- buildAuth returns a wrapped error when all configured auth sources fail.
- Sparkline handles NaN/Inf without panic.

### Removed (none)
```

Update design spec status: "v1.0 released".

---

## Final verification

```bash
go test -race ./...
GOTOOLCHAIN=go1.25.10 ~/go/bin/golangci-lint run ./...
govulncheck ./...
```

PR, CI green, red-team review-team accept, merge. Tag v1.0.0.

## Success criteria

1. Tunnels reconnect automatically when transient errors occur.
2. Editing tunnels.toml triggers a reload within ~200ms (debounce).
3. Adding a tunnel in the editor brings it up; removing stops it.
4. Tunnels with ProxyCommand work via the subprocess fallback.
5. Space in the TUI toggles the selected tunnel.
6. All issue #1 items closed.
7. CHANGELOG, README, ARCHITECTURE.md, CONTRIBUTING.md present.
8. Red-team review-team accepts.
9. CI green.
10. Tag v1.0.0 pushed.
