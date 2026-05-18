# sshiitake

A TUI SSH tunnel manager. Define your forwards once, see at a glance which are
up, toggle them with a keystroke.

> **Status: Phase 1 (Foundation) shipped, no TUI yet.** The CLI can bring up
> a single tunnel from `~/.config/sshiitake/tunnels.toml`. TUI, groups,
> auto-reconnect, and hot-reload land in later phases.

## Quick Start

Build from source:

```bash
go install github.com/Sshiitake/sshiitake/cmd/ssht@latest
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
