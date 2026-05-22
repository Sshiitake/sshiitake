# sshiitake Architecture

A 5-minute tour of the codebase for contributors. The pitch in one
sentence is "define your SSH forwards once, see at a glance which are
up, toggle them with a keystroke," and the code organisation reflects
that: one CLI entry, one Bubble Tea TUI, one Manager that owns the
tunnel goroutines, one tunnel implementation, and supporting packages
for config, metrics, and hot-reload.

## Package map

| Path | Responsibility |
|---|---|
| `cmd/ssht` | CLI entry. Cobra subcommands (`up`, `add`, `config check`, `version`). Flag parsing, TTY detection, host-key callback construction. |
| `internal/tui` | Bubble Tea TUI. List view, detail view, help overlay, ASCII diagrams, sparklines, three themes. Subscribes to `manager.Event` channel; never touches SSH directly. |
| `internal/manager` | Owns a `map[string]*tunnelHandle`. Lifecycle: `New`, `Run`, `Apply` (hot-reload diff), `Toggle` (per-tunnel start/stop). Broadcasts events to N subscribers with drop-on-full per-channel semantics. |
| `internal/tunnel` | One SSH tunnel: dial, local port-forward, reconnect loop. `Start` blocks; `StartWithReconnect` wraps it with exponential backoff for transient errors. |
| `internal/config` | TOML schema (`tunnels.toml`), validator, and `ssh_config` resolver. Produces `ResolvedTunnel` values ready for `internal/tunnel`. |
| `internal/metrics` | Per-tunnel `Tracker`: atomic bytes counters + 60-sample latency ring buffer. |
| `internal/logbuffer` | Per-tunnel in-memory log ring (populated by tunnel goroutines, consumed by the TUI detail view). |
| `internal/reload` | `fsnotify.Watcher` wrapper with debounced `Changed` channel + `Diff(old, new)` returning an Added/Removed/Modified plan. |

Each package has a deliberate boundary: `manager` knows nothing about
Bubble Tea; `tui` knows nothing about SSH internals; `tunnel` knows
nothing about its siblings or how it was scheduled.

## Event flow

```
                  +----------------+
  tunnels.toml -->|  config.Load   |
                  +----------------+
                          |
                          v
                  +----------------+
                  | manager.New    |
                  |  resolves +    |
                  |  builds        |
                  |  handles map   |
                  +----------------+
                          |
                          v
        +-----------------------------------+
        |          manager.Run              |
        |   for each handle: spawnHandle    |
        |     ctx, cancel = WithCancel      |
        |     go tunnel.Start(...)          |
        |     go (publish state events)     |
        +-----------------------------------+
                          |
        +-----------------+-----------------+
        |                 |                 |
        v                 v                 v
   subs.publish     metrics.Tracker   logbuffer.Ring
        |
        v
  +-----------+     +----------+     +---------------+
  | TUI       |     | --bare   |     | --no-tui      |
  | (Subscribe| --> |  JSON    |     | human stream  |
  | 256-buf)  |     |  stream  |     |  (subscribe)  |
  +-----------+     +----------+     +---------------+

  Hot-reload:
  reload.Watcher --(debounce 200ms)--> Changed
       |
       v
  config.Load + Validate + reload.Diff(old, new) --> Plan
       |
       v
  manager.Apply: stop removed/modified, start added/modified

  User input (TUI):
  space --> onToggle(name) --> go manager.Toggle(name)
       --> cancel old goroutine OR spawn fresh tunnel.Tunnel
```

## Invariants

1. **Per-tunnel context isolation.** Every running tunnel derives its
   context from `m.runCtx` via `context.WithCancel`. Cancelling one
   handle's context never disturbs siblings. Cancelling `m.runCtx`
   stops everything.

2. **Event publication is drop-on-full per subscriber.** A slow consumer
   (paused TUI, blocked JSON pipe) cannot back-pressure the Manager. The
   subscriber's channel just loses the oldest events it didn't drain.

3. **HostKeyCallback is mandatory.** `tunnel.dial` refuses to proceed if
   `Options.HostKeyCallback == nil`. There is no
   `InsecureIgnoreHostKey` escape hatch in production code; the test
   fixture uses `SSHT_TEST_HOSTKEY` (loud stderr warning if set in a
   non-test binary).

4. **Status transitions go through `setStatus`.** A tunnel's `Status` is
   read/written under `t.mu`; the public `Status()` method takes the
   lock. Tests and TUI rendering rely on this being race-free.

5. **`Manager.Apply` and `Manager.Toggle` are idempotent on running state.**
   Applying an empty plan is a no-op. Toggling a tunnel that's already
   stopping is safe (the second cancel is a no-op; the wait sees the
   same closed `done` channel).

## Adding a new feature

### Adding a new event type

This is the most common addition. Worked example: adding a
`EventReconnectAttempt` event with attempt count + next-delay duration.

1. **Extend the enum and `String()`** in `internal/manager/events.go`:

   ```go
   const (
       // ...
       EventReconnectAttempt
   )

   func (et EventType) String() string {
       switch et {
       // ...
       case EventReconnectAttempt:
           return "reconnect_attempt"
       }
   }
   ```

2. **Add the typed payload fields** to the `Event` struct in the same
   file. Keep them flat (no nested structs) so the `--bare` JSON encoder
   in `cmd/ssht/bare.go` stays simple.

3. **Publish from the producer.** For reconnect events that's
   `StartWithReconnect` in `internal/tunnel/tunnel.go`. The Manager owns
   the subscriber set, so producers in `internal/tunnel` need a hook;
   the established pattern is to widen `tunnel.Options` with a
   `EventSink func(Event)` callback that `Manager.spawnHandle` populates.

4. **Update consumers.** The TUI list/detail models (`internal/tui/list.go`,
   `detail.go`) switch on `e.Type` in their `applyEvent` methods. Add a
   case. For the `--bare` path, extend the `bareEnvelope` struct in
   `cmd/ssht/bare.go` so the new fields show up in stdout.

5. **Tests.** Add a unit test next to the publisher. Add a manager-level
   test that subscribes and asserts the event is received. The pattern
   to copy is `TestApply_addsNewTunnel` in `internal/manager/apply_test.go`.

### Adding a new TUI view

Sub-models live in `internal/tui/<name>.go` with a constructor that takes
`(keyMap, Theme)` and an `applyEvent(manager.Event)` method if it cares
about the stream. Register the view-state constant in `model.go`'s
`const (...)` block, add a key binding (`keys.go`), and route from
`Model.handleKey`. The detail view (`internal/tui/detail.go`) is the
template to copy.

### Adding a new CLI subcommand

Add `cmd/ssht/<name>.go` with a `newXxxCmd() *cobra.Command` constructor;
register it in `main.go`. Long-running commands that drive the manager
follow the `up.go` pattern (Manager + Subscribe + signal-aware ctx +
either TUI or stream loop).

## Testing

The test suite favours unit tests with focused integration tests where
the interaction surface (SSH handshake, fsnotify wake-up, Bubble Tea
event loop) is the actual subject under test. `go test -race ./...`
runs cleanly across all packages.

### Coverage gaps documented for follow-up

- **Auto-reconnect end-to-end.** `internal/tunnel` covers the backoff
  arithmetic (`TestBackoff_progression`, `TestBackoff_jitterStaysInBounds`)
  and the permanent vs transient error classification
  (`TestIsReconnectableError*`), but does not exercise a real
  Start -> drop -> reconnect -> Start cycle against a live SSH server.
  Adding an integration test that restarts the test SSH server mid-Start
  is on the v1.1 backlog.

## Out of scope (deliberate)

Things the architecture explicitly does NOT do:

- No daemon, no IPC, no shared state between `ssht` invocations. Use
  tmux for persistence.
- No write-back to `~/.ssh/config`. We read it; we never modify it.
- No global socket / PID file / lock. The process owns its own tunnels;
  closing it closes them.

See `docs/design/2026-05-17-sshiitake-design.md` for the longer
"non-goals" list and the reasoning behind each.
