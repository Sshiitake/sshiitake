# Sahjhan: TUI SSH Tunnel Manager

**Status:** Draft
**Date:** 2026-05-17
**Author:** Adam Neilson

## Summary

A small, cross-platform terminal app for managing SSH tunnels. Picks up where macOS's *SSH Tunnel Manager* (Tynsoe.org, abandoned) left off, but works wherever you have a terminal: dev box, jump host, tablet via Termius, tmux session on a server.

The pitch in one sentence: **define your forwards once, see at a glance which are up, and toggle them with a keystroke.**

## Motivation

Existing tools sit at two extremes:

- **Too low-level:** `ssh -L ...`, `autossh`, hand-rolled bash. Works for one tunnel; doesn't scale to "bring up my whole work stack."
- **Too much:** TunnelForge bundles VPN-replacement, TLS obfuscation, DNS leak protection, Telegram bots, server hardening. Different threat model, different audience.

Nothing in between, especially nothing cross-platform with a real TUI, since STM went unmaintained. Sahjhan fills that gap.

## Goals

1. Define tunnels (local, remote, dynamic) in a single config file
2. Group tunnels and toggle a group as a unit
3. Show real-time status: up/down, latency, bytes/sec, sparkline
4. Auto-reconnect with backoff when a tunnel drops
5. Read identity (host, user, key, ProxyJump) from `~/.ssh/config`
6. Work as both interactive TUI and scriptable CLI
7. Run on macOS, Linux, Windows (one static binary)
8. Be approachable to a first-time TUI hacker, with a clean codebase and good docs

## Non-goals (explicitly out of scope)

- **VPN-style features**: kill switch, DNS leak protection, iptables manipulation. Sahjhan is a dev tool, not a privacy tool. These features have a different threat model and cause sharp edges on dev machines (locking yourself out at 2am).
- **Censorship-evasion**: TLS obfuscation, PSK, stunnel integration. Different product.
- **Remote control bots**: Telegram, Discord, Slack. Out of scope.
- **Server-side configuration**: sshd hardening, fail2ban, sysctl. Out of scope.
- **Daemon mode** (v1): tunnels live for the life of the process. Run inside tmux for persistence. May revisit in v2 if user demand is real.
- **Writing back to `~/.ssh/config`**: sahjhan only reads ssh config. Tunnel orchestration metadata lives in sahjhan's own config.
- **Built-in tutorial**: link to good external docs.

## Stack decisions

| Decision | Choice | Reason |
|---|---|---|
| Language | Go | Bubble Tea is the most mature TUI toolkit in 2026; trivial cross-compile; learning slope is gentle |
| TUI library | Charm's Bubble Tea + Lipgloss + Bubbles | Cohesive ecosystem, great defaults, used by gh-cli, k9s, lazydocker, glow |
| SSH library | `golang.org/x/crypto/ssh` (primary), spawn `ssh` (fallback) | Native library = full control over reconnect, in-process port forwarding, connection events. Fallback to subprocess for users with exotic SSH config (custom ProxyCommand, certificate edge cases) |
| Config format | TOML | Comments supported, less footgun than YAML, more readable than JSON |
| Process model | Single process, foreground | KISS. Tmux solves persistence. |
| Persistence | None for tunnels themselves; config in `~/.config/sahjhan/tunnels.toml` | Tunnels are intentionally session-scoped |
| Binary name | `shn` | 3 chars, like `gh` / `fd` / `jq`. Full `sahjhan` is the package, brand, and docs name |
| License | GPLv3 or MIT (TBD — see open questions) | |

## Architecture

```
┌───────────────────────────────────────────────────────────┐
│                       shn (single process)                │
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

- **`config`** — load + validate TOML, parse `~/.ssh/config` for host identity, merge into resolved tunnel definitions
- **`tunnel`** — per-tunnel goroutine: dial, forward, reconnect with backoff, emit events
- **`manager`** — owns all tunnel goroutines, exposes start/stop/list, broadcasts events to subscribers
- **`tui`** — Bubble Tea model, subscribes to manager events
- **`cli`** — non-interactive commands, also subscribes to manager events
- **`metrics`** — ring buffer per tunnel (latency, bytes-in, bytes-out) for sparklines and JSON status

Each unit has a clear boundary: `manager` doesn't know about Bubble Tea; `tui` doesn't know how SSH works; `tunnel` doesn't know about other tunnels or groups.

## Config model

`~/.config/sahjhan/tunnels.toml`:

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

`~/.ssh/config` is consulted (read-only) for `Host`, `HostName`, `User`, `Port`, `IdentityFile`, `ProxyJump`. If a tunnel's `host` isn't in ssh config and isn't a resolvable DNS name with sensible defaults, sahjhan reports a config error at startup.

No secrets in `tunnels.toml`. Keys come from ssh-agent or the path declared in ssh config.

## CLI surface

Each `shn` invocation is an independent foreground process that owns its own tunnels. There is no daemon, no IPC, no shared state between invocations. Run `shn` inside tmux for persistence. Close the process, tunnels close with it. This is the absolute simplest model and the one we commit to.

```
shn                         Launch TUI (interactive use)
shn up <name|group>         Launch TUI with these tunnels pre-started
shn up <name|group> --bare  Bring up tunnels, log to stdout, no TUI (for tmux/scripts)
shn add                     Interactive wizard, writes to tunnels.toml then exits
shn config check            Validate tunnels.toml + ssh config refs
shn version
```

Notably *absent*: `shn down`, `shn status`, `shn logs` as separate commands. The TUI is the control surface. To stop tunnels, quit the process (or press `space` in the TUI). To see status, look at the TUI (or the status file, below).

**Status-bar integration.** `shn --bare` streams newline-delimited JSON events on stdout: pipe to whatever you want (`shn --bare work | sketchybar-renderer`). TUI mode optionally writes the same stream to `--status-file=PATH`, off by default. No global socket, no PID file, no surprises.

## TUI design

### List view (default)

```
┌─ shn  ──────────────────────────────────── 4/6 up ─┐
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
│  Auto-reconnect: yes                               │
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
| `e` | Edit tunnel (open `$EDITOR` on tunnels.toml at the right line) |
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
- Auto-reconnect with exponential backoff + jitter
- Live status (up/down/reconnecting), latency, bandwidth, uptime
- Sparkline bandwidth + latency
- Per-tunnel log buffer (in-memory ring, configurable size)
- Read identity from `~/.ssh/config`
- Jump host chains via `ProxyJump`
- Config validation (`shn config check`)
- CLI mode (up/down/status/logs)
- JSON status for status-bar integration
- Theme support (built-in dark, light, high-contrast)
- ASCII tunnel-type diagrams in help

### v1.5 (post-launch, prioritise by user feedback)

- **Speed test** — pump configurable payload through tunnel, report throughput
- **Conditional tunnels** — `only_when_ssid = "OfficeWifi"`, `only_when_reachable = "10.0.0.1"`
- **Share-as-URL** — encode a tunnel definition (no secrets) as a `sahjhan://...` URL for Slack/team sharing
- **Mouse support** — clicks, drag-to-reorder
- **Notifications** — desktop notification on tunnel down / reconnect, via OS-native paths (`osascript`, `notify-send`)

### v2 maybe (no commitment)

- Daemon mode (if persistence becomes a real pain)
- Plugins / scriptable hooks (`on_connect`, `on_disconnect`)
- Web UI

## Open questions

1. **Licence.** GPLv3 (TunnelForge's choice, ensures fork-with-source) vs MIT (broader adoption, friendlier to packaging). Soft lean toward MIT for a tool people will package widely.
2. **Windows TTY quality.** Bubble Tea works on Windows but the experience is noticeably better in WSL / Windows Terminal than legacy conhost. Decide whether to officially support Windows or "supports Windows Terminal."
3. **First release scope.** Should v1 ship without auto-reconnect (smaller surface, simpler reasoning) and add it in v1.1, or is auto-reconnect table stakes?
4. **Config edit semantics.** When `e` opens `$EDITOR`, should sahjhan hot-reload on save, or require a reload command? Hot-reload is nicer but adds file-watch complexity.
5. **In-process forwarding vs subprocess `ssh`.** Start with in-process via `crypto/ssh` and fall back to spawning `ssh` only when needed, or always spawn `ssh` (simpler, less control)?

## Risks

- **`golang.org/x/crypto/ssh` quirks.** It's correct but lower-level than OpenSSH's client; some config options (`ControlMaster`, `Compression`, custom `KexAlgorithms`) need explicit handling. Mitigation: fallback to spawning `ssh` when an option we don't support is detected.
- **macOS network-change detection.** No clean cross-platform API. Polling `route get default` works but is ugly. Mitigation: poll once a second when at least one tunnel is in reconnect-backoff; otherwise idle.
- **Scope creep.** Every user will want their pet feature (and TunnelForge is the cautionary tale). Mitigation: be loud about non-goals in the README from day one.

## Success criteria

1. A new user can install, define their first tunnel, and bring it up in under 5 minutes
2. Running `shn` on a config with 20 tunnels uses <50 MB RSS and <1% CPU when idle
3. Auto-reconnect recovers within 10 seconds of network restoration
4. JSON status integrates with at least one status bar (SketchyBar) out of the box, documented
5. 500 GitHub stars within 6 months of launch (vanity, but a useful signal of "are people interested?")
