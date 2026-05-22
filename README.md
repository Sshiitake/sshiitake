# sshiitake

A TUI SSH tunnel manager. Define your forwards once, see at a glance which are
up, toggle them with a keystroke.

> **Status: Phase 1 + 1.5 + 2 shipped, no TUI yet.** The CLI brings up one or
> more tunnels (or a named group) from `~/.config/sshiitake/tunnels.toml`, with
> `~/.ssh/known_hosts` verification and per-tunnel metrics. `--bare` streams
> JSON events for status-bar integration. TUI lands in Phase 3.

## Quick Start

Build from source:

```bash
go install github.com/Sshiitake/sshiitake/cmd/ssht@latest
```

`go install` puts the binary in `$GOPATH/bin` (default `~/go/bin`). If `ssht`
isn't found after install, add it to your `PATH`:

```bash
export PATH="$HOME/go/bin:$PATH"   # add to ~/.zshrc or ~/.bashrc
```

Create `~/.config/sshiitake/tunnels.toml`:

```toml
[tunnels.api-prod]
host = "bastion-prod"           # must exist in ~/.ssh/config
type = "local"
local_port = 8443
remote_host = "api.internal"
remote_port = 443
```

Validate, then run:

```bash
ssht config check     # exits 0 if everything resolves
ssht up api-prod      # blocks; Ctrl-C to stop
```

Bring up several tunnels at once:

```bash
ssht up api-prod pg-replica redis
```

Bring up a named group (defined under `[groups.<name>]` in your config):

```bash
ssht up work-stack
```

Stream newline-delimited JSON events for a status bar (SketchyBar, xbar,
Waybar, tmux status, anything line-buffered):

```bash
ssht up --bare api-prod | sketchybar-renderer
```

> **First-time use:** `ssht up <name>` reads `~/.ssh/known_hosts` to verify the
> server. If the host isn't there yet, ssht tells you exactly what to run:
>
> ```
> ssh-keyscan -H hudson >> ~/.ssh/known_hosts
> ```
>
> For non-standard SSH ports:
>
> ```
> ssh-keyscan -H -p 2200 hudson >> ~/.ssh/known_hosts
> ```
>
> Always verify the printed fingerprint matches the server's reported one
> before trusting it.

## Flags

`ssht up` and `ssht config check` accept:

| Flag | Default | Purpose |
|---|---|---|
| `--config` | `~/.config/sshiitake/tunnels.toml` | Path to your tunnels.toml |
| `--ssh-config` | `~/.ssh/config` | Path to ssh_config (read-only, used for host identity) |
| `--known-hosts` | `~/.ssh/known_hosts` | Path to known_hosts (used for host-key verification) |
| `--bare` | (off) | Stream newline-delimited JSON events to stdout (no human-friendly output) |

## Why

Existing tools sit at two extremes:

- **Too low-level.** `ssh -L ...`, `autossh`, hand-rolled bash. Works for one
  tunnel; doesn't scale to "bring up my whole work stack."
- **Too much.** Tools that bundle VPN-replacement, TLS obfuscation, DNS leak
  protection, and Telegram bots. Different threat model, different audience.

`sshiitake` fills the gap STM (Tynsoe.org's *SSH Tunnel Manager*) left when it
went unmaintained: a focused, cross-platform terminal app for the developer use
case.

## Binary

The installed command is `ssht` - first three keystrokes match `ssh`, so the
muscle memory transfers.

## Roadmap

See the [design spec](docs/design/2026-05-17-sshiitake-design.md) for the v1
feature set, architecture, and explicit non-goals.

## Security

### Host-key verification

`ssht up` refuses to connect if the server is not in `~/.ssh/known_hosts`.
The error message tells you exactly which `ssh-keyscan` command to run.

If the server's key has changed since you saved it, ssht prints a loud
`KEY MISMATCH` warning and refuses to connect. That's the protection
against an active MITM. Don't paper over it without understanding why
the key changed.

### The `SSHT_TEST_HOSTKEY` env var

If you set `SSHT_TEST_HOSTKEY` (base64 of an `ssh.PublicKey`), ssht
trusts that pinned key INSTEAD of consulting `~/.ssh/known_hosts`. This
is intended for the integration test fixture only. The binary logs a
`WARNING` to stderr if you set it in a non-test run, because anyone with
control of your environment could otherwise silently disable host-key
verification. Don't set it in production scripts.

### Secret scanning

Every push and pull request is scanned for accidentally-committed secrets by
[gitleaks](https://github.com/gitleaks/gitleaks) (see
[`.github/workflows/gitleaks.yml`](.github/workflows/gitleaks.yml)). A weekly
scheduled run repeats the scan in case rule definitions change.

To run the same scan locally before pushing:

```bash
brew install gitleaks
gitleaks detect --source . --verbose --redact
```

## Licence

MIT. See [`LICENSE`](LICENSE).
