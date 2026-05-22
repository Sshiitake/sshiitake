# Changelog

All notable changes to sshiitake by phase. Pre-1.0 versions follow the
phased plan in `docs/design/`.

## Phase 4 / v1.0 release - 2026-05-22

### Added
- Auto-reconnect with exponential backoff: 1s, 2s, 4s, 8s, 16s, 32s,
  60s (capped), 10% jitter, 10 attempts max. Reconnectable error tokens
  cover EOF, connection reset, handshake failure, timeout, broken pipe,
  no route to host, network unreachable. Permanent errors (host-key
  mismatch, unsupported config options, no usable auth) skip the loop.
  Enabled by default; `--no-reconnect` opts out.
- Hot-reload of `tunnels.toml` via `fsnotify`. Debounced 200ms. The
  watcher diffs old vs new config and applies an Added/Removed/Modified
  plan to the running Manager without disturbing unchanged tunnels.
  Invalid edits are logged to stderr with a `[reload]` prefix; running
  tunnels are not torn down. `--no-reload` opts out.
- `Manager.Apply` per-tunnel start/stop API used by hot-reload and the
  new TUI toggle binding.
- `Manager.Toggle(name)` cancels or restarts a single tunnel.
- TUI space-to-toggle: pressing space on a selected row starts or stops
  the underlying tunnel. The TUI dispatches to `Manager.Toggle` off the
  Bubble Tea event loop so the UI stays responsive while a cancel is
  waiting for its goroutine to exit.
- `ARCHITECTURE.md` (package map, event flow, invariants, "adding a new
  event type" walkthrough).
- `CONTRIBUTING.md` (setup, code style, PR conventions, security-report
  guidance).

### Changed
- `ssht add` now writes atomically (`CreateTemp` + `Rename`). Crash
  between truncate and complete write no longer leaves an empty
  `tunnels.toml`.
- `keyAuth` refuses private keys with permissions broader than 0600,
  matching OpenSSH's stricture.
- `keyAuth` error no longer leaks the full key path; the error is
  simply "parse private key: <reason>".
- `buildAuth` returns a wrapped error when at least one auth source was
  attempted and every attempt failed. Silent empty stays when nothing
  was configured.
- Sparkline `blockFor` handles `NaN` / `+Inf` / `-Inf` without panic
  (collapses to space rune).
- Config validator gains port boundary cases (1, 65535, 65536, 0, -1).

### Known limitations (v1.0)
- Subprocess SSH fallback is deferred to a future minor release. Until
  then, tunnels whose ssh_config requires `ProxyCommand`, exotic
  `Match` blocks, or `ControlMaster yes` are not supported.
- Auto-reconnect is unit-tested for backoff arithmetic and error
  classification but not end-to-end against a real SSH server restart.
  Full Start/drop/reconnect integration coverage is targeted for a
  future release.

## Phase 3 - 2026-05-22

### Added
- `internal/tui` package: Bubble Tea TUI with list view, detail view,
  help overlay, ASCII tunnel-type diagrams (local / remote / dynamic),
  sparkline-rendered latency history, three themes (dark, light,
  high-contrast).
- CLI: TUI is the default when stdout is a TTY. `--no-tui` opts back
  to the human stream. `--theme` selects the palette
  (dark / light / high-contrast).
- CLI: `ssht add` interactive wizard (huh-based) appends new tunnels
  to `tunnels.toml` without hand-editing. Refuses to overwrite an
  existing tunnel name.
- Dependencies: `github.com/charmbracelet/bubbletea`,
  `github.com/charmbracelet/lipgloss`, `github.com/charmbracelet/bubbles`,
  `github.com/charmbracelet/huh`, `github.com/mattn/go-isatty`.

### Known limitations (deferred to Phase 4+)
- No `fsnotify` hot-reload of `tunnels.toml`.
- No subprocess SSH fallback.
- No auto-reconnect (v1.1).
- Wizard does not auto-suggest hosts from `~/.ssh/config`.

## Phase 2 - 2026-05-22

### Added
- `internal/manager`: orchestrates multiple tunnels concurrently.
  - Selector resolution: tunnel name OR group name.
  - Event stream with `Subscribe(bufSize)` / `Unsubscribe(ch)`.
  - Per-second `EventMetrics` ticker per Up tunnel.
- `internal/metrics`: per-tunnel `Tracker` (atomic bytes counters + 60-sample latency ring).
- `internal/logbuffer`: per-tunnel in-memory log ring (consumed by Phase 3 TUI).
- CLI: `ssht up <names...>` accepts multiple tunnels and group names.
- CLI: `ssht up --bare` streams newline-delimited JSON events to stdout
  (schema: `cmd/ssht/bare.go` `bareEnvelope`).
- Config validator: rejects non-loopback `local_host` (prevents accidental
  network exposure).

### Changed
- `internal/tunnel/forwardLocal` now records bytes-in / bytes-out into an
  optional `*metrics.Tracker`. Backwards compatible (nil tracker = no-op).
- CI: `govulncheck` pinned to `@v1` (rolling within v1.x) for vuln DB
  protocol freshness.

### Removed
- `reserveLocalPort` helper from `cmd/ssht/main_test.go` (was a flake
  source; the new manager-driven test uses a fixed port instead).

## Phase 1.5 - 2026-05-18

### Added
- `~/.ssh/known_hosts` host-key verification via `knownhosts.New`.
- `--known-hosts` flag.
- Sentinel errors `ErrKeyMismatch`, `ErrHostNotInKnownHosts` (route to
  exit code 2).
- Loud stderr warning when `SSHT_TEST_HOSTKEY` is set in a non-test binary.

## Phase 1 - 2026-05-17

### Added
- Initial release. Single tunnel via `ssht up <name>`.
- TOML config (`~/.config/sshiitake/tunnels.toml`).
- `~/.ssh/config` integration for host identity.
- In-process SSH (golang.org/x/crypto/ssh) with ssh-agent + key auth.
- CLI: `version`, `config check`, `up`.
- CI: gitleaks, lint, test (linux + macos), build, vuln scan.
