# Contributing

Thanks for considering a contribution! sshiitake is a small Go project;
PRs welcome.

## Setup

```bash
git clone https://github.com/Sshiitake/sshiitake
cd sshiitake
go test -race ./...
```

The Go toolchain is pinned via `go.mod` (currently 1.25). CI runs the
same `go test -race` invocation on Linux and macOS.

## Code style

- Run `golangci-lint run ./...` before commit (config in `.golangci.yml`).
  The CI pipeline pins `GOTOOLCHAIN=go1.25.10`; use the same locally.
- No em dashes anywhere in source, docs, or commit messages (project
  style).
- TDD per task; tests live next to code in the same package.
- One feature per commit; commit messages are imperative-mood single
  sentences, optionally followed by a body.

## PRs

- Branch off `main`, one feature per PR.
- CI (lint, test, build, gitleaks, govulncheck) must be green before
  merge.
- Cover behaviour changes with tests at the lowest level that
  meaningfully exercises them. Manager-level tests use the in-process
  SSH server fixture in `internal/manager/helpers_test.go`; copy that
  pattern for new lifecycle tests.
- For non-trivial changes (multi-file, schema, behavioural), request a
  red-team / review-team pass before merging.

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for the package map, event flow,
and a worked example of adding a new event type.

## Reporting bugs

Open an issue with:
- A short title describing the symptom.
- The `ssht version` output.
- A minimal `tunnels.toml` that reproduces.
- The full stderr / log output (redact secrets; sshiitake should not
  print key material but double-check before pasting).

## Reporting security issues

Please do NOT open a public issue for suspected security problems.
Email the maintainer at the address in the project metadata, or use
GitHub's private vulnerability reporting.
