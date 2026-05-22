# Sshiitake: TUI SSH Tunnel Manager

**Status:** Draft
**Date:** 2026-05-17
**Author:** Adam Neilson

## Summary

A small, cross-platform terminal app for managing SSH tunnels. Picks up where macOS's *SSH Tunnel Manager* (Tynsoe.org, abandoned) left off, but works wherever you have a terminal: dev box, jump host, tablet via Termius, tmux session on a server.

The pitch in one sentence: **define your forwards once, see at a glance which are up, and toggle them with a keystroke.**

## Implementation status

- **Phase 1 shipped (2026-05-17):** single tunnel via `ssht up <name>`, TOML config, in-process SSH, CI matrix.
- **Phase 1.5 shipped (2026-05-18):** real `~/.ssh/known_hosts` verification, `ErrKeyMismatch` / `ErrHostNotInKnownHosts` sentinels.
- **Phase 2 shipped (2026-05-22):** manager-driven multi-tunnel orchestration, group selectors, per-tunnel metrics (bytes-in/out + latency ring), per-tunnel log ring, `ssht up --bare` newline-delimited JSON event stream, and non-loopback `local_host` rejection at validate time. Phase 2 details live in `docs/plans/2026-05-18-phase-2-manager.md` and `CHANGELOG.md`.

Phase 2 limitations (planned for Phase 3+): no Bubble Tea TUI, no `fsnotify` hot-reload of config, no subprocess SSH fallback, no auto-reconnect when a tunnel drops.

## Motivation

Existing tools sit at two extremes:

- **Too low-level:** `ssh -L ...`, `autossh`, hand-rolled bash. Works for one tunnel; doesn't scale to "bring up my whole work stack."
- **Too much:** TunnelForge bundles VPN-replacement, TLS obfuscation, DNS leak protection, Telegram bots, server hardening. Different threat model, different audience.

Nothing in between, especially nothing cross-platform with a real TUI, since STM went unmaintained. Sshiitake fills that gap.

## Goals

1. Define tunnels (local, remote, dynamic) in a single config file
2. Group tunnels and toggle a group as a unit
3. Show real-time status: up/down, latency, bytes/sec, sparkline
4. Auto-reconnect with backoff when a tunnel drops
5. Read identity (host, user, key, ProxyJump) from `~/.ssh/config`
6. Work as both interactive TUI and scriptable CLI
7. Run on macOS and Linux (one static binary). Windows is deferred indefinitely; Bubble Tea works there but we're not investing in the testing surface for v1
8. Be approachable to a first-time TUI hacker, with a clean codebase and good docs

## Non-goals (explicitly out of scope)

- **VPN-style features**: kill switch, DNS leak protection, iptables manipulation. Sshiitake is a dev tool, not a privacy tool. These features have a different threat model and cause sharp edges on dev machines (locking yourself out at 2am).
- **Censorship-evasion**: TLS obfuscation, PSK, stunnel integration. Different product.
- **Remote control bots**: Telegram, Discord, Slack. Out of scope.
- **Server-side configuration**: sshd hardening, fail2ban, sysctl. Out of scope.
- **Daemon mode** (v1): tunnels live for the life of the process. Run inside tmux for persistence. May revisit in v2 if user demand is real.
- **Writing back to `~/.ssh/config`**: sshiitake only reads ssh config. Tunnel orchestration metadata lives in sshiitake's own config.
- **Built-in tutorial**: link to good external docs.

## Stack decisions

| Decision | Choice | Reason |
|---|---|---|
| Language | Go | Bubble Tea is the most mature TUI toolkit in 2026; trivial cross-compile; learning slope is gentle |
| TUI library | Charm's Bubble Tea + Lipgloss + Bubbles | Cohesive ecosystem, great defaults, used by gh-cli, k9s, lazydocker, glow |
| SSH library | `golang.org/x/crypto/ssh` (primary), spawn `ssh` (fallback) | Native library = full control over reconnect, in-process port forwarding, connection events. Fallback to subprocess for users with exotic SSH config (custom ProxyCommand, certificate edge cases) |
| Config format | TOML | Comments supported, less footgun than YAML, more readable than JSON |
| Process model | Single process, foreground | KISS. Tmux solves persistence. |
| Persistence | None for tunnels themselves; config in `~/.config/sshiitake/tunnels.toml` | Tunnels are intentionally session-scoped |
| Binary name | `ssht` | 4 chars; reads as "ssh + t". Full `sshiitake` is the package, brand, and docs name. First three keystrokes match `ssh` so muscle memory transfers |
| Licence | MIT | Matches the rest of the TUI tooling ecosystem (gh, k9s, lazygit, ripgrep, charm tools); keeps packaging friction low for distros and corporate users |

## Architecture

```
┌───────────────────────────────────────────────────────────┐
│                       ssht (single process)                │
│                                                           │
│  ┌─────────────┐    ┌──────────────────┐   ┌──────────┐   │
│  │   CLI       │    │   Bubble Tea     │   │  Config  │   │
│  │   commands  │    │   TUI            │   │  loader  │   │
│  │             │    │                  │   │          │   │
│  │  up/down/   │    │  list, detail,   │   │ tunnels  │   │
│  │  status/    │    │  logs, help      │   │  .toml + │   │
│  │  logs       │    │                  │   │  ssh cfg │   │
│  └──────┬──────┘    └────────┬─────────┘   └────┬─────┘   │
│         │                    │                  │         │
│         └─────────┬──────────┴──────────┬───────┘         │
│                   ▼                     ▼                 │
│           ┌─────────────────────────────────────┐         │
│           │       Tunnel Manager (core)         │         │
│           │                                     │         │
│           │  TunnelState (per tunnel goroutine) │         │
│           │   ─ dial / forward / reconnect      │         │
│           │   ─ metrics ring buffer             │         │
│           │   ─ event stream → TUI / CLI        │         │
│           └─────────────────────────────────────┘         │
│                          │                                │
└──────────────────────────┼────────────────────────────────┘
                           ▼
                ┌──────────────────────┐
                │  golang.org/x/       │
                │  crypto/ssh          │
                │                      │
                │  (or spawn ssh)      │
                └──────────────────────┘
```

### Internal units

- **`config`** - load + validate TOML, parse `~/.ssh/config` for host identity, merge into resolved tunnel definitions
- **`tunnel`** - per-tunnel goroutine: dial, forward, reconnect with backoff, emit events
- **`manager`** - owns all tunnel goroutines, exposes start/stop/list, broadcasts events to subscribers
- **`tui`** - Bubble Tea model, subscribes to manager events
- **`cli`** - non-interactive commands, also subscribes to manager events
- **`metrics`** - ring buffer per tunnel (latency, bytes-in, bytes-out) for sparklines and JSON status

Each unit has a clear boundary: `manager` doesn't know about Bubble Tea; `tui` doesn't know how SSH works; `tunnel` doesn't know about other tunnels or groups.

## Config model

`~/.config/sshiitake/tunnels.toml`:

```toml
# A single tunnel
[tunnels.api-prod]
host = "bastion-prod"          # resolved via ~/.ssh/config (User, IdentityFile, ProxyJump)
type = "local"                 # local | remote | dynamic
local_port = 8443
remote_host = "api.internal"   # used by local/remote
remote_port = 443
group = "work-stack"
auto_reconnect = true          # default true

# A dynamic SOCKS proxy
[tunnels.socks-eu]
host = "eu-jump"
type = "dynamic"
local_port = 1080

# A reverse tunnel
[tunnels.webhook-test]
host = "myvps"
type = "remote"
remote_port = 9090
local_host = "127.0.0.1"
local_port = 3000

# Groups (named collections, atomic start/stop)
[groups.work-stack]
description = "Production work stack: api, db, redis"

[groups.personal]
description = "Home services"
```

`~/.ssh/config` is consulted (read-only) for `Host`, `HostName`, `User`, `Port`, `IdentityFile`, `ProxyJump`. If a tunnel's `host` isn't in ssh config and isn't a resolvable DNS name with sensible defaults, sshiitake reports a config error at startup.

No secrets in `tunnels.toml`. Keys come from ssh-agent or the path declared in ssh config.

## CLI surface

Each `ssht` invocation is an independent foreground process that owns its own tunnels. There is no daemon, no IPC, no shared state between invocations. Run `ssht` inside tmux for persistence. Close the process, tunnels close with it. This is the absolute simplest model and the one we commit to.

```
ssht                         Launch TUI (interactive use)
ssht up <name|group>         Launch TUI with these tunnels pre-started
ssht up <name|group> --bare  Bring up tunnels, log to stdout, no TUI (for tmux/scripts)
ssht add                     Interactive wizard, writes to tunnels.toml then exits
ssht config check            Validate tunnels.toml + ssh config refs
ssht version
```

Notably *absent*: `ssht down`, `ssht status`, `ssht logs` as separate commands. The TUI is the control surface. To stop tunnels, quit the process (or press `space` in the TUI). To see status, look at the TUI (or the status file, below).

**Status-bar integration.** `ssht --bare` streams newline-delimited JSON events on stdout: pipe to whatever you want (`ssht --bare work | sketchybar-renderer`). TUI mode optionally writes the same stream to `--status-file=PATH`, off by default. No global socket, no PID file, no surprises.

## TUI design

### List view (default)

```
┌─ ssht  ──────────────────────────────────── 4/6 up ─┐
│  ▾ GROUP  work-stack          [up]   3/3  ▲ 12K/s  │
│    ● api-prod     :8443 → bastion-prod  12ms ▁▂▄▅▄▂│
│    ● pg-replica   :5432 → bastion-prod   8ms ▁▁▂▃▂▁│
│    ● redis        :6379 → bastion-prod   9ms ▁▂▂▂▂▁│
│                                                    │
│  ▾ GROUP  personal            [partial] 1/2        │
│    ● nas-web      :8080 → home          45ms ▁▂▁▁▁▁│
│    ○ pi-ssh       :2222 → home          (retry 4s) │
│                                                    │
│  ▾ ad-hoc                                          │
│    ● socks-eu     :1080 → eu-jump (dynamic SOCKS5) │
│                                                    │
├────────────────────────────────────────────────────┤
│ space toggle  g group  a add  l logs  s speed  ? │
└────────────────────────────────────────────────────┘
```

- `●` green = up, `○` red = down, `◐` amber = reconnecting
- Each row has a 6-cell sparkline of recent bandwidth
- Latency colour-coded: green <50ms, yellow <200ms, red >=200ms

### Detail view

Pressed `enter` on a tunnel:

```
┌─ api-prod  ──────────────────────────────── [up] ──┐
│                                                    │
│  Type:           local forward                     │
│  Host:           bastion-prod (via jumpbox)        │
│  Forward:        127.0.0.1:8443 → api.internal:443 │
│  Uptime:         2h 14m                            │
│  Sent:           1.2 MB    Recv:  18.4 MB          │
│  Latency:        12ms ▁▂▄▅▄▂▃▂▁▁▂▃▂▁▁▂▃▂▁▁▂▃▂▁    │
│  Bandwidth:      12 KB/s ▁▂▄▅▄▂▃▂▁▁▂▃▂▁▁▂▃▂▁▁▂▃   │
│                                                    │
│  Recent logs:                                      │
│  14:22:01  connected (handshake 184ms)             │
│  14:22:01  listening on 127.0.0.1:8443             │
│  14:22:43  proxied: GET /v2/recipes (200, 31ms)    │
│  14:22:48  proxied: GET /v2/users (200, 28ms)      │
│                                                    │
├────────────────────────────────────────────────────┤
│ space toggle  l logs(full)  s speed-test  esc back │
└────────────────────────────────────────────────────┘
```

### Keybindings

| Key | Action |
|---|---|
| `space` | Toggle tunnel or group |
| `g` | Operate on group (start/stop all) |
| `a` | Add tunnel (wizard) |
| `e` | Edit tunnel (open `$EDITOR` on tunnels.toml at the right line; sshiitake hot-reloads the config on save) |
| `d` | Delete tunnel (confirm) |
| `l` | Logs |
| `s` | Speed test through tunnel |
| `/` | Filter |
| `j` / `k` / arrows | Move |
| `?` | Help overlay (incl. ASCII diagrams of tunnel types) |
| `q` | Quit (prompts if tunnels active) |

## Features

### v1

- Local / remote / dynamic forwards
- Named tunnels and groups
- Live status (up/down), latency, bandwidth, uptime
- Sparkline bandwidth + latency
- Per-tunnel log buffer (in-memory ring, configurable size)
- Read identity from `~/.ssh/config`
- Host-key verification via `~/.ssh/known_hosts` (shipped Phase 1.5)
- Jump host chains via `ProxyJump`
- Config validation (`ssht config check`)
- Hot-reload of `tunnels.toml` on save (file-watch via `fsnotify`)
- CLI mode (interactive TUI + `--bare` stdout streaming)
- JSON status for status-bar integration
- Theme support (built-in dark, light, high-contrast)
- ASCII tunnel-type diagrams in help

When a tunnel drops in v1 it stays down; the user gets a clear "down: <reason>" indicator and can press `space` to bring it back up. Auto-reconnect arrives in v1.1.

### v1.1

- **Auto-reconnect** with exponential backoff + jitter. The "reconnecting" status (amber `◐` indicator in the mockup) lights up here, not in v1.
- Network-change awareness on macOS and Linux: when the default route changes, tunnels in reconnect-backoff retry immediately.

### v1.5 (post-launch, prioritise by user feedback)

- **Speed test** - pump configurable payload through tunnel, report throughput
- **Conditional tunnels** - `only_when_ssid = "OfficeWifi"`, `only_when_reachable = "10.0.0.1"`
- **Share-as-URL** - encode a tunnel definition (no secrets) as a `sshiitake://...` URL for Slack/team sharing
- **Mouse support** - clicks, drag-to-reorder
- **Notifications** - desktop notification on tunnel down / reconnect, via OS-native paths (`osascript`, `notify-send`)
- **Windows support** - test, fix the rough edges, document supported terminals (likely Windows Terminal only)

### v2 maybe (no commitment)

- Daemon mode (if persistence becomes a real pain)
- Plugins / scriptable hooks (`on_connect`, `on_disconnect`)
- Web UI

## Resolved decisions

The five open questions in the original draft of this spec have been answered:

1. **Licence: MIT.** Matches the rest of the TUI ecosystem and keeps packaging friction low.
2. **Windows TTY: deferred indefinitely.** v1 targets macOS and Linux only. Windows support shows up in v1.5 if there's demand and a contributor willing to own the testing surface.
3. **Auto-reconnect: not v1.** v1 ships without auto-reconnect to keep the initial surface small and the failure modes obvious. Reconnect logic lands in v1.1 with backoff, jitter, and network-change awareness.
4. **Hot-reload: yes.** The running process watches `tunnels.toml` via `fsnotify` and reloads on save. Pressing `e` opens `$EDITOR` at the relevant line; the diff applies on save without restarting tunnels that are unchanged.
5. **In-process SSH with fallback.** Default to `golang.org/x/crypto/ssh` for full event-level control. Detect at parse time when a tunnel's resolved ssh config uses an option we don't natively support (`ProxyCommand`, exotic `Match` blocks, custom `KexAlgorithms`, `ControlMaster yes` from outside), and fall back to spawning `ssh` for that specific tunnel. Mixed-mode is fine; the manager doesn't care.

## Risks

- **`golang.org/x/crypto/ssh` quirks.** It's correct but lower-level than OpenSSH's client; some config options (`ControlMaster`, `Compression`, custom `KexAlgorithms`) need explicit handling. Mitigation: fallback to spawning `ssh` when an option we don't support is detected.
- **macOS network-change detection.** No clean cross-platform API. Polling `route get default` works but is ugly. Mitigation: poll once a second when at least one tunnel is in reconnect-backoff; otherwise idle.
- **Scope creep.** Every user will want their pet feature (and TunnelForge is the cautionary tale). Mitigation: be loud about non-goals in the README from day one.

## Success criteria

v1:
1. A new user can install, define their first tunnel, and bring it up in under 5 minutes
2. Running `ssht` on a config with 20 tunnels uses <50 MB RSS and <1% CPU when idle
3. JSON status integrates with at least one status bar (SketchyBar) out of the box, documented
4. Hot-reload applies a config change in under 500ms with no flicker for unchanged tunnels

v1.1:
5. Auto-reconnect recovers within 10 seconds of network restoration

Adoption:
6. 500 GitHub stars within 6 months of v1 launch (vanity, but a useful signal of "are people interested?")
