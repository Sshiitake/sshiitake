# sshiitake

A TUI SSH tunnel manager. Define your forwards once, see at a glance which are
up, toggle them with a keystroke.

> **Status: pre-implementation.** Design has been agreed; no code yet. See the
> design spec at [`docs/design/2026-05-17-sshiitake-design.md`](docs/design/2026-05-17-sshiitake-design.md).

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

The installed command is `ssht` — first three keystrokes match `ssh`, so the
muscle memory transfers.

## Roadmap

See the [design spec](docs/design/2026-05-17-sshiitake-design.md) for the v1
feature set, architecture, and explicit non-goals.

## Licence

MIT. See [`LICENSE`](LICENSE).
