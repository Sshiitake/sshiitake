# Sshiitake v1 Phase 1: Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a CLI binary `ssht` that can open a single SSH tunnel defined in `~/.config/sshiitake/tunnels.toml` and forward a local port through it, including identity merging from `~/.ssh/config`. No TUI, no groups, no metrics, no hot-reload yet. The foundation everything else sits on.

**Architecture:** Go binary using `golang.org/x/crypto/ssh` for in-process SSH, `kevinburke/ssh_config` for `~/.ssh/config` parsing, `BurntSushi/toml` for the project config, and `spf13/cobra` for the CLI surface. Two internal packages: `config` (load, validate, resolve against ssh config) and `tunnel` (dial, forward, lifecycle). All SSH-touching tests use an in-process test SSH server fixture; no Docker, no external network.

**Tech Stack:**
- Go 1.22+
- `golang.org/x/crypto/ssh` (in-process SSH client + server-side test fixture)
- `github.com/kevinburke/ssh_config` (parse `~/.ssh/config`)
- `github.com/BurntSushi/toml` (TOML config)
- `github.com/spf13/cobra` (CLI subcommands)
- `github.com/stretchr/testify` (`assert` + `require` for readable test assertions)
- `golangci-lint` (linting in CI)
- `govulncheck` (vulnerability scanning in CI)

**Out of scope for Phase 1 (covered by later phases):**
- TUI (Bubble Tea, sparklines, detail view) - Phase 3
- Groups, multi-tunnel management - Phase 2
- Hot-reload via `fsnotify` - Phase 4
- Subprocess `ssh` fallback - Phase 4
- Auto-reconnect - v1.1 (post-launch)
- Metrics ring buffer, log buffer - Phase 2
- `--bare` JSON stream output - Phase 2

---

## File Structure

After Phase 1 the repo looks like this. Files marked `(exists)` are already in the repo from the bootstrapping commits.

```
sshiitake/
├── .editorconfig                              [new]
├── .gitignore                                 (exists)
├── .golangci.yml                              [new]
├── .github/
│   └── workflows/
│       ├── gitleaks.yml                       (exists)
│       └── go.yml                             [new]
├── LICENSE                                    (exists)
├── README.md                                  (exists, modified by Task 14)
├── cmd/
│   └── ssht/
│       ├── main.go                            [new]
│       ├── up.go                              [new]
│       ├── check.go                           [new]
│       ├── version.go                         [new]
│       └── main_test.go                       [new]
├── docs/                                      (exists)
├── go.mod                                     [new]
├── go.sum                                     [new]
├── internal/
│   ├── config/
│   │   ├── config.go                          [new]
│   │   ├── load.go                            [new]
│   │   ├── resolve.go                         [new]
│   │   ├── validate.go                        [new]
│   │   ├── config_test.go                     [new]
│   │   ├── load_test.go                       [new]
│   │   ├── resolve_test.go                    [new]
│   │   └── validate_test.go                   [new]
│   └── tunnel/
│       ├── tunnel.go                          [new]
│       ├── local.go                           [new]
│       ├── tunnel_test.go                     [new]
│       ├── local_test.go                      [new]
│       └── testserver_test.go                 [new]
└── testdata/
    ├── ssh_config_sample                      [new]
    ├── tunnels-valid.toml                     [new]
    └── tunnels-invalid.toml                   [new]
```

### Boundaries

- `cmd/ssht` is the only place that knows about command-line flags and the user. It depends on `internal/config` and `internal/tunnel`; nothing else.
- `internal/config` owns config types, TOML loading, validation, and merging with `~/.ssh/config`. It does NOT know about SSH protocol, sockets, or tunnels. It produces a `ResolvedTunnel` value that has everything `internal/tunnel` needs.
- `internal/tunnel` owns the actual network work: SSH dial, accept loop on the local port, byte copying. It does NOT know about TOML, ssh_config, or the CLI. It accepts `ResolvedTunnel` and runs.
- Tests live next to the code they test. Anything that talks SSH uses the in-process test server fixture in `tunnel/testserver_test.go`.

---

## Pre-flight

Before starting tasks, confirm the toolchain.

- [ ] **Verify Go is installed (1.22 or later)**

Run: `go version`
Expected output contains `go1.22` or higher. If not installed: `brew install go`.

- [ ] **Verify golangci-lint is installed**

Run: `golangci-lint --version`
Expected: a version string. If not installed: `brew install golangci-lint`.

- [ ] **Confirm in the right repo**

Run: `cd ~/Projects/sshiitake && git log --oneline -5`
Expected: top commit is the design-spec open-questions resolution. Working directory is clean.

---

## Task 1: Bootstrap Go module and CI

**Files:**
- Create: `go.mod` (via `go mod init`)
- Create: `.golangci.yml`
- Create: `.editorconfig`
- Create: `cmd/ssht/main.go` (smoke main, replaced in Task 10)
- Create: `cmd/ssht/main_test.go`
- Create: `.github/workflows/go.yml`

- [ ] **Step 1: Initialise Go module**

Run:
```bash
cd ~/Projects/sshiitake
go mod init github.com/Sshiitake/sshiitake
```

Expected: creates `go.mod` with module path `github.com/Sshiitake/sshiitake` and go version line.

- [ ] **Step 2: Write the smoke test**

Create `cmd/ssht/main_test.go`:

```go
package main

import "testing"

func TestSmoke(t *testing.T) {
	// Smoke test: package compiles and runs the test runner.
	// Replaced with real CLI tests in later tasks.
	if 1+1 != 2 {
		t.Fatal("arithmetic is broken")
	}
}
```

- [ ] **Step 3: Write the smoke main**

Create `cmd/ssht/main.go`:

```go
// Package main is the ssht CLI entry point.
package main

import "fmt"

func main() {
	fmt.Println("ssht: see https://github.com/Sshiitake/sshiitake")
}
```

- [ ] **Step 4: Run test, expect pass**

Run: `go test ./cmd/ssht/`
Expected: `ok ... 0.00Xs`

- [ ] **Step 5: Confirm build works**

Run: `go build -o /tmp/ssht ./cmd/ssht && /tmp/ssht`
Expected: prints `ssht: see https://github.com/Sshiitake/sshiitake`

- [ ] **Step 6: Add `.golangci.yml`**

Create `.golangci.yml`:

```yaml
run:
  timeout: 3m

linters:
  enable:
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    - gofmt
    - goimports
    - misspell
    - revive
    - bodyclose
    - gocritic

linters-settings:
  revive:
    rules:
      - name: exported
      - name: error-return
      - name: error-naming

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
```

- [ ] **Step 7: Add `.editorconfig`**

Create `.editorconfig`:

```ini
root = true

[*]
charset = utf-8
end_of_line = lf
indent_style = tab
insert_final_newline = true
trim_trailing_whitespace = true

[*.{md,yml,yaml}]
indent_style = space
indent_size = 2

[*.toml]
indent_style = space
indent_size = 2
```

- [ ] **Step 8: Run golangci-lint**

Run: `golangci-lint run ./...`
Expected: no findings.

- [ ] **Step 9: Add the Go CI workflow**

Create `.github/workflows/go.yml`:

```yaml
name: go

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
      - uses: golangci/golangci-lint-action@v6
        with:
          version: v1.59

  test:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
      - run: go test -race ./...

  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
      - run: go build -o /tmp/ssht ./cmd/ssht

  vuln:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
      - run: go install golang.org/x/vuln/cmd/govulncheck@latest
      - run: govulncheck ./...
```

- [ ] **Step 10: Commit**

```bash
cd ~/Projects/sshiitake
git add go.mod .golangci.yml .editorconfig .github/workflows/go.yml cmd/ssht/main.go cmd/ssht/main_test.go
git commit -m "chore: scaffold Go module, CI workflow, smoke main"
```

---

## Task 2: Config types

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTunnelType_String(t *testing.T) {
	tests := []struct {
		in   TunnelType
		want string
	}{
		{TypeLocal, "local"},
		{TypeRemote, "remote"},
		{TypeDynamic, "dynamic"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, tc.in.String())
	}
}

func TestConfig_TunnelByName(t *testing.T) {
	cfg := &Config{
		Tunnels: map[string]Tunnel{
			"api": {Host: "bastion", Type: TypeLocal, LocalPort: 8443},
		},
	}
	got, ok := cfg.TunnelByName("api")
	assert.True(t, ok)
	assert.Equal(t, "bastion", got.Host)

	_, ok = cfg.TunnelByName("nope")
	assert.False(t, ok)
}
```

- [ ] **Step 2: Add testify dependency**

Run: `go get github.com/stretchr/testify`
Expected: updates `go.mod` and `go.sum`.

- [ ] **Step 3: Run test, expect compile failure**

Run: `go test ./internal/config/`
Expected: build error - `undefined: TunnelType`, `TypeLocal`, etc.

- [ ] **Step 4: Implement the types**

Create `internal/config/config.go`:

```go
// Package config defines the on-disk schema for tunnels.toml and merges
// it with ~/.ssh/config to produce ResolvedTunnel values ready for
// internal/tunnel to use.
package config

// TunnelType is the kind of port forward.
type TunnelType string

const (
	TypeLocal   TunnelType = "local"
	TypeRemote  TunnelType = "remote"
	TypeDynamic TunnelType = "dynamic"
)

// String makes TunnelType satisfy fmt.Stringer.
func (t TunnelType) String() string { return string(t) }

// Config is the parsed contents of tunnels.toml.
type Config struct {
	Tunnels map[string]Tunnel `toml:"tunnels"`
	Groups  map[string]Group  `toml:"groups"`
}

// Tunnel is a single tunnel definition.
type Tunnel struct {
	Host       string     `toml:"host"`
	Type       TunnelType `toml:"type"`
	LocalHost  string     `toml:"local_host"`
	LocalPort  int        `toml:"local_port"`
	RemoteHost string     `toml:"remote_host"`
	RemotePort int        `toml:"remote_port"`
	Group      string     `toml:"group"`
}

// Group is a named collection of tunnels.
type Group struct {
	Description string `toml:"description"`
}

// TunnelByName returns the named tunnel, or false if not found.
func (c *Config) TunnelByName(name string) (Tunnel, bool) {
	t, ok := c.Tunnels[name]
	return t, ok
}
```

- [ ] **Step 5: Run test, expect pass**

Run: `go test ./internal/config/ -v`
Expected: both tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go go.mod go.sum
git commit -m "feat(config): add tunnel/group/config types"
```

---

## Task 3: Load TOML config from disk

**Files:**
- Create: `internal/config/load.go`
- Create: `internal/config/load_test.go`
- Create: `testdata/tunnels-valid.toml`

- [ ] **Step 1: Create the fixture**

Create `testdata/tunnels-valid.toml`:

```toml
[tunnels.api-prod]
host = "bastion-prod"
type = "local"
local_port = 8443
remote_host = "api.internal"
remote_port = 443
group = "work-stack"

[tunnels.socks-eu]
host = "eu-jump"
type = "dynamic"
local_port = 1080

[groups.work-stack]
description = "Production work stack"
```

- [ ] **Step 2: Write the failing test**

Create `internal/config/load_test.go`:

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_validFixture(t *testing.T) {
	cfg, err := Load("../../testdata/tunnels-valid.toml")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Len(t, cfg.Tunnels, 2)

	api, ok := cfg.TunnelByName("api-prod")
	require.True(t, ok)
	assert.Equal(t, "bastion-prod", api.Host)
	assert.Equal(t, TypeLocal, api.Type)
	assert.Equal(t, 8443, api.LocalPort)
	assert.Equal(t, "api.internal", api.RemoteHost)
	assert.Equal(t, 443, api.RemotePort)
	assert.Equal(t, "work-stack", api.Group)

	socks, ok := cfg.TunnelByName("socks-eu")
	require.True(t, ok)
	assert.Equal(t, TypeDynamic, socks.Type)
	assert.Equal(t, 1080, socks.LocalPort)

	require.Len(t, cfg.Groups, 1)
	assert.Equal(t, "Production work stack", cfg.Groups["work-stack"].Description)
}

func TestLoad_missingFile(t *testing.T) {
	_, err := Load("/nonexistent/path.toml")
	require.Error(t, err)
	assert.ErrorContains(t, err, "no such file")
}

func TestLoad_invalidTOML(t *testing.T) {
	tmpFile := t.TempDir() + "/bad.toml"
	require.NoError(t, writeFile(tmpFile, []byte("this is = not [ valid toml")))
	_, err := Load(tmpFile)
	require.Error(t, err)
}

func writeFile(path string, b []byte) error {
	return os.WriteFile(path, b, 0o600)
}
```

- [ ] **Step 3: Add missing import**

Edit `internal/config/load_test.go` to add `"os"` to the import block:

```go
import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 4: Run test, expect compile failure**

Run: `go test ./internal/config/ -v`
Expected: `undefined: Load`.

- [ ] **Step 5: Add BurntSushi/toml dependency**

Run: `go get github.com/BurntSushi/toml`

- [ ] **Step 6: Implement Load**

Create `internal/config/load.go`:

```go
package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Load reads and decodes a tunnels.toml file from path.
// It does NOT validate the config; call (*Config).Validate after loading.
func Load(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	if cfg.Tunnels == nil {
		cfg.Tunnels = map[string]Tunnel{}
	}
	if cfg.Groups == nil {
		cfg.Groups = map[string]Group{}
	}
	return &cfg, nil
}
```

- [ ] **Step 7: Run test, expect pass**

Run: `go test ./internal/config/ -v`
Expected: all three Load tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/config/load.go internal/config/load_test.go testdata/tunnels-valid.toml go.mod go.sum
git commit -m "feat(config): load tunnels.toml from disk"
```

---

## Task 4: Config validation

**Files:**
- Create: `internal/config/validate.go`
- Create: `internal/config/validate_test.go`
- Create: `testdata/tunnels-invalid.toml`

- [ ] **Step 1: Write the failing tests**

Create `internal/config/validate_test.go`:

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_validFixture(t *testing.T) {
	cfg, err := Load("../../testdata/tunnels-valid.toml")
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}

func TestValidate_emptyName(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"": {Host: "h", Type: TypeLocal, LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "tunnel name must not be empty")
}

func TestValidate_unknownType(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: "weird", LocalPort: 1234},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown type")
}

func TestValidate_portOutOfRange(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalPort: 70000, RemoteHost: "r", RemotePort: 80},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "local_port")
}

func TestValidate_localMissingRemote(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalPort: 1234},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "remote_host")
}

func TestValidate_dynamicNeedsOnlyLocalPort(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"socks": {Host: "h", Type: TypeDynamic, LocalPort: 1080},
	}}
	require.NoError(t, cfg.Validate())
}

func TestValidate_groupReferenceUnknown(t *testing.T) {
	cfg := &Config{
		Tunnels: map[string]Tunnel{
			"x": {Host: "h", Type: TypeDynamic, LocalPort: 1080, Group: "no-such-group"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "no-such-group")
}
```

- [ ] **Step 2: Run test, expect compile failure**

Run: `go test ./internal/config/ -run Validate -v`
Expected: `cfg.Validate undefined`.

- [ ] **Step 3: Implement Validate**

Create `internal/config/validate.go`:

```go
package config

import (
	"errors"
	"fmt"
)

// Validate checks the config for internal consistency.
// Returns the first error encountered, with a wrapped message that
// identifies the offending tunnel.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config is nil")
	}
	for name, t := range c.Tunnels {
		if name == "" {
			return errors.New("tunnel name must not be empty")
		}
		if err := validateTunnel(t); err != nil {
			return fmt.Errorf("tunnel %q: %w", name, err)
		}
		if t.Group != "" {
			if _, ok := c.Groups[t.Group]; !ok {
				return fmt.Errorf("tunnel %q: references unknown group %q", name, t.Group)
			}
		}
	}
	return nil
}

func validateTunnel(t Tunnel) error {
	if t.Host == "" {
		return errors.New("host must not be empty")
	}
	switch t.Type {
	case TypeLocal, TypeRemote, TypeDynamic:
	default:
		return fmt.Errorf("unknown type %q (want local, remote, or dynamic)", t.Type)
	}
	if !validPort(t.LocalPort) {
		return fmt.Errorf("local_port %d out of range (1-65535)", t.LocalPort)
	}
	switch t.Type {
	case TypeLocal:
		if t.RemoteHost == "" {
			return errors.New("local tunnel requires remote_host")
		}
		if !validPort(t.RemotePort) {
			return fmt.Errorf("remote_port %d out of range (1-65535)", t.RemotePort)
		}
	case TypeRemote:
		if !validPort(t.RemotePort) {
			return fmt.Errorf("remote_port %d out of range (1-65535)", t.RemotePort)
		}
	case TypeDynamic:
		// only local_port required
	}
	return nil
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }
```

- [ ] **Step 4: Run test, expect pass**

Run: `go test ./internal/config/ -v`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/validate.go internal/config/validate_test.go
git commit -m "feat(config): validate types, ports, and group references"
```

---

## Task 5: Resolve against `~/.ssh/config`

**Files:**
- Create: `internal/config/resolve.go`
- Create: `internal/config/resolve_test.go`
- Create: `testdata/ssh_config_sample`

- [ ] **Step 1: Create the ssh_config fixture**

Create `testdata/ssh_config_sample`:

```
Host bastion-prod
    HostName 203.0.113.10
    User adam
    Port 2200
    IdentityFile ~/.ssh/id_ed25519_work

Host eu-jump
    HostName 198.51.100.42
    User adam
    IdentityFile ~/.ssh/id_ed25519_eu

Host *.internal
    User svc
    ProxyJump bastion-prod
```

- [ ] **Step 2: Write the failing tests**

Create `internal/config/resolve_test.go`:

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve_matchingHost(t *testing.T) {
	tun := Tunnel{
		Host: "bastion-prod", Type: TypeLocal, LocalPort: 8443,
		RemoteHost: "api.internal", RemotePort: 443,
	}
	r, err := ResolveWithSSHConfig(tun, "../../testdata/ssh_config_sample")
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.10", r.SSHHost)
	assert.Equal(t, 2200, r.SSHPort)
	assert.Equal(t, "adam", r.SSHUser)
	assert.Contains(t, r.IdentityFile, "id_ed25519_work")
	assert.Empty(t, r.ProxyJump)

	assert.Equal(t, TypeLocal, r.Type)
	assert.Equal(t, 8443, r.LocalPort)
	assert.Equal(t, "api.internal:443", r.RemoteAddr)
}

func TestResolve_fallbackDefaults(t *testing.T) {
	tun := Tunnel{
		Host: "unmatched-host.example.com", Type: TypeDynamic, LocalPort: 1080,
	}
	r, err := ResolveWithSSHConfig(tun, "../../testdata/ssh_config_sample")
	require.NoError(t, err)

	// No match in ssh_config: SSHHost falls back to the literal hostname.
	assert.Equal(t, "unmatched-host.example.com", r.SSHHost)
	assert.Equal(t, 22, r.SSHPort)
}

func TestResolve_proxyJump(t *testing.T) {
	tun := Tunnel{
		Host: "api.internal", Type: TypeLocal, LocalPort: 8443,
		RemoteHost: "127.0.0.1", RemotePort: 443,
	}
	r, err := ResolveWithSSHConfig(tun, "../../testdata/ssh_config_sample")
	require.NoError(t, err)
	assert.Equal(t, "bastion-prod", r.ProxyJump)
	assert.Equal(t, "svc", r.SSHUser)
}
```

- [ ] **Step 3: Run test, expect compile failure**

Run: `go test ./internal/config/ -run Resolve -v`
Expected: `undefined: ResolveWithSSHConfig`.

- [ ] **Step 4: Add ssh_config dependency**

Run: `go get github.com/kevinburke/ssh_config`

- [ ] **Step 5: Implement Resolve**

Create `internal/config/resolve.go`:

```go
package config

import (
	"fmt"
	"os"
	"strconv"

	sshcfg "github.com/kevinburke/ssh_config"
)

// ResolvedTunnel is a Tunnel merged with the relevant identity fields
// from ~/.ssh/config. internal/tunnel consumes this directly.
type ResolvedTunnel struct {
	Name string

	// SSH connection identity (from ssh_config, with fallbacks)
	SSHHost      string
	SSHPort      int
	SSHUser      string
	IdentityFile string
	ProxyJump    string

	// Forward details
	Type       TunnelType
	LocalHost  string
	LocalPort  int
	RemoteAddr string // "host:port" for local/remote, empty for dynamic
}

// ResolveWithSSHConfig merges a tunnel definition with the entries in
// the ssh config file at sshConfigPath. The path may be empty to use
// the default ~/.ssh/config.
func ResolveWithSSHConfig(t Tunnel, sshConfigPath string) (ResolvedTunnel, error) {
	if sshConfigPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ResolvedTunnel{}, fmt.Errorf("home dir: %w", err)
		}
		sshConfigPath = home + "/.ssh/config"
	}

	cfg, err := openSSHConfig(sshConfigPath)
	if err != nil {
		return ResolvedTunnel{}, err
	}

	host, err := cfg.Get(t.Host, "HostName")
	if err != nil || host == "" {
		host = t.Host
	}
	portStr, _ := cfg.Get(t.Host, "Port")
	port := 22
	if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
		port = p
	}
	user, _ := cfg.Get(t.Host, "User")
	identity, _ := cfg.Get(t.Host, "IdentityFile")
	proxy, _ := cfg.Get(t.Host, "ProxyJump")

	r := ResolvedTunnel{
		SSHHost:      host,
		SSHPort:      port,
		SSHUser:      user,
		IdentityFile: identity,
		ProxyJump:    proxy,
		Type:         t.Type,
		LocalHost:    t.LocalHost,
		LocalPort:    t.LocalPort,
	}

	switch t.Type {
	case TypeLocal:
		r.RemoteAddr = fmt.Sprintf("%s:%d", t.RemoteHost, t.RemotePort)
	case TypeRemote:
		r.RemoteAddr = fmt.Sprintf("%s:%d", t.LocalHost, t.LocalPort)
	}

	if r.LocalHost == "" {
		r.LocalHost = "127.0.0.1"
	}
	return r, nil
}

func openSSHConfig(path string) (*sshcfg.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open ssh config: %w", err)
	}
	defer f.Close()
	return sshcfg.Decode(f)
}
```

- [ ] **Step 6: Run test, expect pass**

Run: `go test ./internal/config/ -v`
Expected: all Resolve tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/config/resolve.go internal/config/resolve_test.go testdata/ssh_config_sample go.mod go.sum
git commit -m "feat(config): resolve tunnel host against ~/.ssh/config"
```

---

## Task 6: In-process SSH test server fixture

**Files:**
- Create: `internal/tunnel/testserver_test.go`
- Create: `internal/tunnel/tunnel.go` (stub; real impl in next task)

This task sets up the test infrastructure every following SSH test relies on. The server accepts any auth and supports `direct-tcpip` channels (what local forwarding needs).

- [ ] **Step 1: Create the package with an empty entry file**

Create `internal/tunnel/tunnel.go`:

```go
// Package tunnel opens a single SSH tunnel using golang.org/x/crypto/ssh
// and forwards a local port through it. It is consumed by cmd/ssht.
//
// Phase 1 supports only local forwards; remote and dynamic land in
// Phase 2.
package tunnel
```

- [ ] **Step 2: Write the meta-test**

Create `internal/tunnel/testserver_test.go`:

```go
package tunnel

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// newTestSSHServer starts an in-process SSH server on a random
// localhost port. It accepts any client, supports direct-tcpip
// channels, and shuts down at the end of the test.
//
// Returns the listening "host:port" and the host key so tests can
// pin it in their client configs.
func newTestSSHServer(t *testing.T) (addr string, hostKey ssh.PublicKey) {
	t.Helper()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(rsaKey)
	require.NoError(t, err)

	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)

	go acceptLoop(t, ln, cfg, &wg)

	return ln.Addr().String(), signer.PublicKey()
}

func acceptLoop(t *testing.T, ln net.Listener, cfg *ssh.ServerConfig, wg *sync.WaitGroup) {
	for {
		tcpConn, err := ln.Accept()
		if err != nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			handleConn(t, tcpConn, cfg)
		}()
	}
}

func handleConn(t *testing.T, tcpConn net.Conn, cfg *ssh.ServerConfig) {
	defer tcpConn.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			_ = ch.Reject(ssh.UnknownChannelType, "only direct-tcpip is supported")
			continue
		}
		go handleDirectTCPIP(t, ch)
	}
}

// directTCPIPMsg is the payload of a direct-tcpip channel open request,
// per RFC 4254 §7.2.
type directTCPIPMsg struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func handleDirectTCPIP(t *testing.T, ch ssh.NewChannel) {
	var msg directTCPIPMsg
	if err := ssh.Unmarshal(ch.ExtraData(), &msg); err != nil {
		_ = ch.Reject(ssh.ConnectionFailed, "bad payload")
		return
	}
	target := fmt.Sprintf("%s:%d", msg.DestAddr, msg.DestPort)

	remote, err := net.Dial("tcp", target)
	if err != nil {
		_ = ch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	channel, reqs, err := ch.Accept()
	if err != nil {
		_ = remote.Close()
		return
	}
	go ssh.DiscardRequests(reqs)

	go func() {
		_, _ = io.Copy(channel, remote)
		_ = channel.Close()
	}()
	go func() {
		_, _ = io.Copy(remote, channel)
		_ = remote.Close()
	}()
}

// TestNewTestSSHServer verifies the fixture itself: a client should
// be able to open an SSH connection and request a direct-tcpip
// channel that connects to an echo server we start in-process.
func TestNewTestSSHServer(t *testing.T) {
	addr, hostKey := newTestSSHServer(t)

	// Start an echo server the SSH server will connect to.
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = echoLn.Close() })
	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()

	clientCfg := &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.FixedHostKey(hostKey),
	}
	client, err := ssh.Dial("tcp", addr, clientCfg)
	require.NoError(t, err)
	defer client.Close()

	conn, err := client.Dial("tcp", echoLn.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	require.Equal(t, "ping", string(buf))
}
```

- [ ] **Step 3: Run test, expect pass**

Run: `go test ./internal/tunnel/ -v -run TestNewTestSSHServer`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tunnel/tunnel.go internal/tunnel/testserver_test.go
git commit -m "test(tunnel): in-process SSH server fixture for forward tests"
```

---

## Task 7: Tunnel: SSH dial

**Files:**
- Modify: `internal/tunnel/tunnel.go`
- Create: `internal/tunnel/tunnel_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tunnel/tunnel_test.go`:

```go
package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func TestTunnel_dial_connects(t *testing.T) {
	addr, hostKey := newTestSSHServer(t)
	host, port := splitHostPort(t, addr)

	rt := config.ResolvedTunnel{
		Name:      "test",
		SSHHost:   host,
		SSHPort:   port,
		SSHUser:   "tester",
		Type:      config.TypeLocal,
		LocalHost: "127.0.0.1",
		LocalPort: 0, // not used in this test
	}
	opts := Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		DialTimeout:     2 * time.Second,
	}
	tun := New(rt, opts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := tun.dial(ctx)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NoError(t, client.Close())
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	p, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return host, p
}
```

- [ ] **Step 2: Add missing imports**

The test uses `net` and `strconv`. Edit the import block in `tunnel_test.go`:

```go
import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
)
```

- [ ] **Step 3: Run test, expect compile failure**

Run: `go test ./internal/tunnel/`
Expected: `undefined: Options`, `undefined: New`, `tun.dial undefined`.

- [ ] **Step 4: Add crypto/ssh dependency**

Run: `go get golang.org/x/crypto/ssh`

- [ ] **Step 5: Implement Tunnel and dial**

Replace `internal/tunnel/tunnel.go` with:

```go
// Package tunnel opens a single SSH tunnel using golang.org/x/crypto/ssh
// and forwards a local port through it.
//
// Phase 1 supports only local forwards; remote and dynamic land in
// Phase 2.
package tunnel

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
)

// Options carries cross-cutting settings shared across tunnels.
type Options struct {
	// HostKeyCallback is required. In production use
	// ssh.InsecureIgnoreHostKey() is forbidden; supply
	// knownhosts.New or ssh.FixedHostKey.
	HostKeyCallback ssh.HostKeyCallback

	// DialTimeout is the maximum time for the initial TCP+SSH handshake.
	// If zero, defaults to 10s.
	DialTimeout time.Duration
}

// Tunnel represents a single configured tunnel. Construct with New,
// then call Start to bring it up.
type Tunnel struct {
	rt   config.ResolvedTunnel
	opts Options
}

// New constructs a Tunnel. It does not connect.
func New(rt config.ResolvedTunnel, opts Options) *Tunnel {
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 10 * time.Second
	}
	return &Tunnel{rt: rt, opts: opts}
}

// dial opens an SSH client connection. The caller owns the returned
// client and must Close() it.
func (t *Tunnel) dial(ctx context.Context) (*ssh.Client, error) {
	if t.opts.HostKeyCallback == nil {
		return nil, fmt.Errorf("HostKeyCallback required")
	}
	auth, err := t.buildAuth()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User:            t.rt.SSHUser,
		Auth:            auth,
		HostKeyCallback: t.opts.HostKeyCallback,
		Timeout:         t.opts.DialTimeout,
	}

	addr := net.JoinHostPort(t.rt.SSHHost, strconv.Itoa(t.rt.SSHPort))

	d := net.Dialer{Timeout: t.opts.DialTimeout}
	tcpConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", addr, err)
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, cfg)
	if err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	return ssh.NewClient(clientConn, chans, reqs), nil
}

func (t *Tunnel) buildAuth() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Try ssh-agent if SSH_AUTH_SOCK is set.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if agentMethod, err := agentAuth(sock); err == nil {
			methods = append(methods, agentMethod)
		}
	}

	// Try the configured IdentityFile.
	if t.rt.IdentityFile != "" {
		if keyMethod, err := keyAuth(t.rt.IdentityFile); err == nil {
			methods = append(methods, keyMethod)
		}
	}

	// Test servers use NoClientAuth; clients sending an empty Auth list
	// are accepted in that case.
	return methods, nil
}
```

- [ ] **Step 6: Add ssh-agent and key helpers**

Append to `internal/tunnel/tunnel.go`:

```go
// agentAuth returns an ssh.AuthMethod backed by the ssh-agent at sock.
func agentAuth(sock string) (ssh.AuthMethod, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeysCallback(newAgentClient(conn).Signers), nil
}

// keyAuth reads a private key from disk and returns it as an AuthMethod.
func keyAuth(path string) (ssh.AuthMethod, error) {
	if expanded, err := expandHome(path); err == nil {
		path = expanded
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return ssh.PublicKeys(signer), nil
}

func expandHome(p string) (string, error) {
	if len(p) == 0 || p[0] != '~' {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + p[1:], nil
}
```

- [ ] **Step 7: Add the agent client helper**

Add the import `"golang.org/x/crypto/ssh/agent"` and define `newAgentClient`. Replace the `agentAuth` function with:

```go
func agentAuth(sock string) (ssh.AuthMethod, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	ac := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ac.Signers), nil
}
```

And remove the call to `newAgentClient` (it was a placeholder). Final import block in `tunnel.go`:

```go
import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/Sshiitake/sshiitake/internal/config"
)
```

- [ ] **Step 8: Run test, expect pass**

Run: `go test ./internal/tunnel/ -v -run TestTunnel_dial`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tunnel/tunnel.go internal/tunnel/tunnel_test.go go.mod go.sum
git commit -m "feat(tunnel): SSH dial with agent + key auth"
```

---

## Task 8: Tunnel: local forward (accept loop)

**Files:**
- Create: `internal/tunnel/local.go`
- Create: `internal/tunnel/local_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tunnel/local_test.go`:

```go
package tunnel

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestForwardLocal_passesBytes(t *testing.T) {
	sshAddr, hostKey := newTestSSHServer(t)

	// Echo server: the "remote" service we're tunnelling to.
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()

	// Open a real SSH client (the same Dial the tunnel package uses).
	clientCfg := &ssh.ClientConfig{
		User:            "tester",
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		Timeout:         2 * time.Second,
	}
	sshClient, err := ssh.Dial("tcp", sshAddr, clientCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sshClient.Close() })

	// Start the local forward.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- forwardLocal(ctx, sshClient, listener, echo.Addr().String())
	}()

	// Connect to the local port, send bytes, expect echo.
	c, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Write([]byte("hello"))
	require.NoError(t, err)
	buf := make([]byte, 5)
	_, err = io.ReadFull(c, buf)
	require.NoError(t, err)
	require.Equal(t, "hello", string(buf))

	// Cancel and confirm forwardLocal returns nil (graceful).
	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("forwardLocal did not return after cancel")
	}
}
```

- [ ] **Step 2: Run test, expect compile failure**

Run: `go test ./internal/tunnel/ -run TestForwardLocal`
Expected: `undefined: forwardLocal`.

- [ ] **Step 3: Implement forwardLocal**

Create `internal/tunnel/local.go`:

```go
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// forwardLocal serves ln, dialling each accepted connection through
// sshClient to remoteAddr ("host:port"). It returns when ctx is
// cancelled. Errors that occur while serving a single connection are
// not returned; they are swallowed silently in Phase 1. Phase 2 wires
// a logger in.
func forwardLocal(ctx context.Context, sshClient *ssh.Client, ln net.Listener, remoteAddr string) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		local, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		wg.Add(1)
		go func(local net.Conn) {
			defer wg.Done()
			pipeOneConn(sshClient, local, remoteAddr)
		}(local)
	}
}

func pipeOneConn(sshClient *ssh.Client, local net.Conn, remoteAddr string) {
	defer local.Close()
	remote, err := sshClient.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, local)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		done <- struct{}{}
	}()
	<-done
}
```

- [ ] **Step 4: Run test, expect pass**

Run: `go test ./internal/tunnel/ -v -run TestForwardLocal`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tunnel/local.go internal/tunnel/local_test.go
git commit -m "feat(tunnel): local port forward through SSH client"
```

---

## Task 9: Tunnel: Start / Stop / Status

**Files:**
- Modify: `internal/tunnel/tunnel.go`
- Modify: `internal/tunnel/tunnel_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tunnel/tunnel_test.go`:

```go
func TestTunnel_lifecycle(t *testing.T) {
	sshAddr, hostKey := newTestSSHServer(t)
	host, port := splitHostPort(t, sshAddr)

	// Echo target.
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()

	rt := config.ResolvedTunnel{
		Name:       "echo",
		SSHHost:    host,
		SSHPort:    port,
		SSHUser:    "tester",
		Type:       config.TypeLocal,
		LocalHost:  "127.0.0.1",
		LocalPort:  0, // 0 = pick a free port
		RemoteAddr: echo.Addr().String(),
	}
	opts := Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		DialTimeout:     2 * time.Second,
	}
	tun := New(rt, opts)

	assert.Equal(t, StatusDown, tun.Status())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- tun.Start(ctx, started)
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("tunnel did not start in 3s")
	}

	assert.Equal(t, StatusUp, tun.Status())

	// Use the tunnel.
	c, err := net.Dial("tcp", tun.LocalAddr())
	require.NoError(t, err)
	_, _ = c.Write([]byte("ok"))
	buf := make([]byte, 2)
	_, err = io.ReadFull(c, buf)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(buf))
	_ = c.Close()

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("tunnel did not stop")
	}
	assert.Equal(t, StatusDown, tun.Status())
}
```

The test file imports need `"io"` added. Edit imports if not present.

- [ ] **Step 2: Run test, expect compile failure**

Run: `go test ./internal/tunnel/ -run TestTunnel_lifecycle`
Expected: undefined `StatusUp`, `StatusDown`, `tun.Start`, `tun.LocalAddr`.

- [ ] **Step 3: Add Status, Start, LocalAddr to Tunnel**

Add to `internal/tunnel/tunnel.go` (top of file, after the imports):

```go
// Status describes a tunnel's current state.
type Status int

const (
	StatusDown Status = iota
	StatusConnecting
	StatusUp
	StatusStopping
)

func (s Status) String() string {
	switch s {
	case StatusDown:
		return "down"
	case StatusConnecting:
		return "connecting"
	case StatusUp:
		return "up"
	case StatusStopping:
		return "stopping"
	default:
		return "unknown"
	}
}
```

Then extend the `Tunnel` struct and add new methods. Replace the existing `Tunnel` struct and `New` with:

```go
type Tunnel struct {
	rt   config.ResolvedTunnel
	opts Options

	mu        sync.Mutex
	status    Status
	localAddr string
}

func New(rt config.ResolvedTunnel, opts Options) *Tunnel {
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 10 * time.Second
	}
	return &Tunnel{rt: rt, opts: opts, status: StatusDown}
}

// Status returns the current tunnel state.
func (t *Tunnel) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

// LocalAddr returns the actual listen address ("host:port") once Start
// has succeeded. Empty before then.
func (t *Tunnel) LocalAddr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.localAddr
}

func (t *Tunnel) setStatus(s Status) {
	t.mu.Lock()
	t.status = s
	t.mu.Unlock()
}

func (t *Tunnel) setLocalAddr(a string) {
	t.mu.Lock()
	t.localAddr = a
	t.mu.Unlock()
}
```

Add `"sync"` to the import block.

- [ ] **Step 4: Implement Start**

Append to `internal/tunnel/tunnel.go`:

```go
// Start dials the SSH server, opens the local listener, and forwards
// connections. It blocks until ctx is cancelled or a fatal error
// occurs. The `started` channel is closed once the listener is
// accepting connections (use to gate on "tunnel is up" in tests and
// CLI mode).
func (t *Tunnel) Start(ctx context.Context, started chan<- struct{}) error {
	if t.rt.Type != config.TypeLocal {
		return fmt.Errorf("tunnel %q: type %q not supported in Phase 1 (local only)",
			t.rt.Name, t.rt.Type)
	}

	t.setStatus(StatusConnecting)

	client, err := t.dial(ctx)
	if err != nil {
		t.setStatus(StatusDown)
		return fmt.Errorf("dial %s: %w", t.rt.Name, err)
	}
	defer client.Close()

	listenAddr := net.JoinHostPort(t.rt.LocalHost, strconv.Itoa(t.rt.LocalPort))
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		t.setStatus(StatusDown)
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	t.setLocalAddr(ln.Addr().String())
	t.setStatus(StatusUp)
	if started != nil {
		close(started)
	}

	err = forwardLocal(ctx, client, ln, t.rt.RemoteAddr)

	t.setStatus(StatusStopping)
	_ = ln.Close()
	t.setStatus(StatusDown)
	t.setLocalAddr("")

	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}
```

- [ ] **Step 5: Run test, expect pass**

Run: `go test ./internal/tunnel/ -v`
Expected: all tunnel tests PASS, including `TestTunnel_lifecycle`.

- [ ] **Step 6: Run race detector**

Run: `go test -race ./internal/tunnel/`
Expected: PASS, no data race warnings.

- [ ] **Step 7: Commit**

```bash
git add internal/tunnel/tunnel.go internal/tunnel/tunnel_test.go
git commit -m "feat(tunnel): Start/Stop lifecycle with Status reporting"
```

---

## Task 10: CLI: cobra root + version

**Files:**
- Modify: `cmd/ssht/main.go` (replace the smoke main)
- Create: `cmd/ssht/version.go`
- Modify: `cmd/ssht/main_test.go`

- [ ] **Step 1: Write the failing test**

Replace `cmd/ssht/main_test.go` with:

```go
package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"version"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, stdout.String(), "ssht")
}

func TestRootHelp(t *testing.T) {
	var stdout bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())
	out := stdout.String()
	assert.Contains(t, out, "ssht")
	assert.Contains(t, out, "version")
}
```

- [ ] **Step 2: Run test, expect compile failure**

Run: `go test ./cmd/ssht/`
Expected: `undefined: rootCmd`.

- [ ] **Step 3: Add cobra dependency**

Run: `go get github.com/spf13/cobra`

- [ ] **Step 4: Replace the smoke main**

Replace `cmd/ssht/main.go`:

```go
// Package main is the ssht CLI entry point.
package main

import (
	"fmt"
	"os"
)

// These are set at build time via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Add the rootCmd and version command**

Create `cmd/ssht/version.go`:

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ssht",
		Short: "A TUI SSH tunnel manager",
		Long: `ssht is a small, focused SSH tunnel manager.
Define your forwards once in ~/.config/sshiitake/tunnels.toml,
bring them up with ` + "`ssht up <name>`" + `.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(versionCmd())
	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ssht version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(),
				"ssht %s (commit %s, built %s)\n", version, commit, date)
			return nil
		},
	}
}
```

- [ ] **Step 6: Run test, expect pass**

Run: `go test ./cmd/ssht/ -v`
Expected: both tests PASS.

- [ ] **Step 7: Confirm build**

Run: `go build -o /tmp/ssht ./cmd/ssht && /tmp/ssht version`
Expected: `ssht dev (commit none, built unknown)`

- [ ] **Step 8: Commit**

```bash
git add cmd/ssht/main.go cmd/ssht/version.go cmd/ssht/main_test.go go.mod go.sum
git commit -m "feat(cli): cobra root command and ssht version"
```

---

## Task 11: CLI: `ssht config check`

**Files:**
- Create: `cmd/ssht/check.go`
- Modify: `cmd/ssht/main_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/ssht/main_test.go`:

```go
func TestConfigCheck_validFixture(t *testing.T) {
	var stdout bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{
		"config", "check",
		"--config", "../../testdata/tunnels-valid.toml",
		"--ssh-config", "../../testdata/ssh_config_sample",
	})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, stdout.String(), "OK")
}

func TestConfigCheck_missingFile(t *testing.T) {
	var stderr bytes.Buffer
	cmd := rootCmd()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"config", "check",
		"--config", "/nonexistent.toml",
	})
	err := cmd.Execute()
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test, expect compile/runtime failure**

Run: `go test ./cmd/ssht/`
Expected: cobra reports `unknown command "config"`.

- [ ] **Step 3: Implement the config subcommand tree**

Create `cmd/ssht/check.go`:

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func configCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate the tunnels.toml file",
	}
	c.AddCommand(configCheckCmd())
	return c
}

func configCheckCmd() *cobra.Command {
	var (
		cfgPath    string
		sshCfgPath string
	)
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate tunnels.toml and resolve hosts against ~/.ssh/config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			for name, t := range cfg.Tunnels {
				rt, err := config.ResolveWithSSHConfig(t, sshCfgPath)
				if err != nil {
					return fmt.Errorf("resolve %q: %w", name, err)
				}
				_ = rt // Phase 1: presence is sufficient; we just want no errors.
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK: %d tunnels, %d groups\n",
				len(cfg.Tunnels), len(cfg.Groups))
			return nil
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", defaultConfigPath(), "path to tunnels.toml")
	cmd.Flags().StringVar(&sshCfgPath, "ssh-config", "", "path to ssh_config (default ~/.ssh/config)")
	return cmd
}
```

- [ ] **Step 4: Add defaultConfigPath helper and wire the subcommand**

Add to the bottom of `cmd/ssht/version.go` (just to keep helpers in one file):

```go
import "os"

func defaultConfigPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.config/sshiitake/tunnels.toml"
	}
	return "tunnels.toml"
}
```

Wait - that file already has an import block; just add `"os"` if not present.

Then modify `rootCmd()` in `cmd/ssht/version.go` to register the config subcommand:

```go
func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ssht",
		Short: "A TUI SSH tunnel manager",
		Long: `ssht is a small, focused SSH tunnel manager.
Define your forwards once in ~/.config/sshiitake/tunnels.toml,
bring them up with ` + "`ssht up <name>`" + `.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(versionCmd())
	root.AddCommand(configCmd())
	return root
}
```

- [ ] **Step 5: Run test, expect pass**

Run: `go test ./cmd/ssht/ -v`
Expected: both new tests PASS.

- [ ] **Step 6: Smoke-test the CLI manually**

Run:
```bash
go build -o /tmp/ssht ./cmd/ssht
/tmp/ssht config check \
  --config testdata/tunnels-valid.toml \
  --ssh-config testdata/ssh_config_sample
```
Expected: `OK: 2 tunnels, 1 groups`

- [ ] **Step 7: Commit**

```bash
git add cmd/ssht/check.go cmd/ssht/version.go cmd/ssht/main_test.go
git commit -m "feat(cli): ssht config check"
```

---

## Task 12: CLI: `ssht up <name>`

**Files:**
- Create: `cmd/ssht/up.go`
- Modify: `cmd/ssht/main_test.go`

This is the keystone task: end-to-end bring-up of one tunnel, with signal-handled shutdown.

- [ ] **Step 1: Write the integration test**

Append to `cmd/ssht/main_test.go`:

```go
// To keep the up test self-contained (no external SSH server), it
// uses the in-process test server fixture from internal/tunnel via
// a small test-only TOML fixture written to a temp dir at runtime.
//
// This test exercises the same code path the user does: build a
// rootCmd, set args, Execute. It verifies the tunnel actually
// forwards bytes by dialling the local port.

func TestUp_endToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e in short mode")
	}

	sshAddr, hostKey := newInProcSSHServer(t) // helper defined in this test file
	host, port := splitHostPortHelper(t, sshAddr)

	echoAddr := startEchoServer(t)

	dir := t.TempDir()
	cfgPath := dir + "/tunnels.toml"
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`
[tunnels.echo]
host = "%s"
type = "local"
local_port = 0
remote_host = "%s"
remote_port = %s
`, host, echoHost(echoAddr), echoPort(echoAddr))), 0o600))

	// Write a minimal ssh_config that pins the port (no IdentityFile,
	// no User — the test server doesn't care).
	sshCfgPath := dir + "/ssh_config"
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(fmt.Sprintf(`
Host %s
    HostName %s
    Port %d
    User tester
`, host, host, port)), 0o600))

	// Pin the host key for the test by setting the env var the up
	// command reads (see Step 3 below).
	t.Setenv("SSHT_TEST_HOSTKEY", base64HostKey(hostKey))

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"up", "echo",
		"--config", cfgPath,
		"--ssh-config", sshCfgPath,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()

	// Wait a beat for the tunnel to come up, then send.
	require.Eventually(t, func() bool {
		c, err := net.Dial("tcp", listenAddrForTunnel(t, dir))
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 3*time.Second, 50*time.Millisecond, "tunnel did not open local port")

	// Cancel context; expect clean exit.
	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("up did not exit on context cancel")
	}
}
```

This test references several helpers that need to be defined in this test file. Add them at the bottom of `main_test.go`. The key trick is that we can't call `internal/tunnel`'s test-only helpers (different package), so we redefine equivalents in this file.

- [ ] **Step 2: Add the test helpers**

Append helpers to `cmd/ssht/main_test.go`. Note: many of these duplicate code in `internal/tunnel/testserver_test.go`. That duplication is fine because test fixtures are scoped to a single test binary; we don't want to export test code from `internal/tunnel`.

```go
// (Helpers used by TestUp_endToEnd. Some duplicate code in
// internal/tunnel/testserver_test.go on purpose: test helpers should
// not be exported.)

func newInProcSSHServer(t *testing.T) (addr string, hostKey ssh.PublicKey) {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(rsaKey)
	require.NoError(t, err)

	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go inProcServerHandle(c, cfg)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func inProcServerHandle(c net.Conn, cfg *ssh.ServerConfig) {
	defer c.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			_ = ch.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		go inProcServeChannel(ch)
	}
}

func inProcServeChannel(ch ssh.NewChannel) {
	var msg struct {
		DestAddr   string
		DestPort   uint32
		OriginAddr string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(ch.ExtraData(), &msg); err != nil {
		_ = ch.Reject(ssh.ConnectionFailed, "bad payload")
		return
	}
	target := fmt.Sprintf("%s:%d", msg.DestAddr, msg.DestPort)
	remote, err := net.Dial("tcp", target)
	if err != nil {
		_ = ch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	channel, reqs, err := ch.Accept()
	if err != nil {
		_ = remote.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	go func() { _, _ = io.Copy(channel, remote); _ = channel.Close() }()
	go func() { _, _ = io.Copy(remote, channel); _ = remote.Close() }()
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()
	return ln.Addr().String()
}

func splitHostPortHelper(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	pi, err := strconv.Atoi(p)
	require.NoError(t, err)
	return h, pi
}

func echoHost(addr string) string {
	h, _, _ := net.SplitHostPort(addr)
	return h
}

func echoPort(addr string) string {
	_, p, _ := net.SplitHostPort(addr)
	return p
}

func base64HostKey(k ssh.PublicKey) string {
	return base64.StdEncoding.EncodeToString(k.Marshal())
}

// listenAddrForTunnel reads the listen address the up command wrote
// to a sidecar file. The up command writes this when --listen-file
// is set; the test passes that flag. See up.go.
func listenAddrForTunnel(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(dir + "/listen.txt")
	if err != nil {
		return "" // not ready yet
	}
	return string(data)
}
```

Add the missing imports to the top of `main_test.go`:

```go
import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)
```

- [ ] **Step 3: Implement the `up` command**

Create `cmd/ssht/up.go`:

```go
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func upCmd() *cobra.Command {
	var (
		cfgPath    string
		sshCfgPath string
		listenFile string // hidden: test-only, write the actual listen address here
	)
	cmd := &cobra.Command{
		Use:   "up <name>",
		Short: "Bring up a tunnel by name and run until interrupted",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			rawTunnel, ok := cfg.TunnelByName(name)
			if !ok {
				return fmt.Errorf("tunnel %q not found in %s", name, cfgPath)
			}
			rt, err := config.ResolveWithSSHConfig(rawTunnel, sshCfgPath)
			if err != nil {
				return err
			}
			rt.Name = name

			hostKeyCB, err := buildHostKeyCallback()
			if err != nil {
				return err
			}

			tun := tunnel.New(rt, tunnel.Options{
				HostKeyCallback: hostKeyCB,
				DialTimeout:     10 * time.Second,
			})

			ctx, cancel := signal.NotifyContext(cmd.Context(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()

			started := make(chan struct{})

			errCh := make(chan error, 1)
			go func() {
				errCh <- tun.Start(ctx, started)
			}()

			// Wait for the tunnel to be up so we can report its listen
			// addr, then continue blocking on errCh.
			select {
			case <-started:
				fmt.Fprintf(cmd.OutOrStdout(),
					"tunnel %q up on %s\n", name, tun.LocalAddr())
				if listenFile != "" {
					_ = os.WriteFile(listenFile, []byte(tun.LocalAddr()), 0o600)
				}
			case err := <-errCh:
				return err
			case <-ctx.Done():
				// Signal arrived before the tunnel came up.
				return nil
			}

			return <-errCh
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", defaultConfigPath(), "path to tunnels.toml")
	cmd.Flags().StringVar(&sshCfgPath, "ssh-config", "", "path to ssh_config (default ~/.ssh/config)")
	cmd.Flags().StringVar(&listenFile, "listen-file", "", "test-only: write listen addr to this path")
	_ = cmd.Flags().MarkHidden("listen-file")
	return cmd
}

// buildHostKeyCallback chooses the host-key verification strategy.
//
// In tests, SSHT_TEST_HOSTKEY pins a single base64-encoded host key.
// In production, this will use known_hosts (Phase 2). For Phase 1
// we deliberately FAIL if neither is set, to avoid silently accepting
// any host.
func buildHostKeyCallback() (ssh.HostKeyCallback, error) {
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
	// Phase 1 placeholder: until Phase 2 wires known_hosts, refuse
	// to run without an explicit key. Loud-and-safe beats quiet-and-broken.
	return nil, errors.New("host key verification not configured: " +
		"set SSHT_TEST_HOSTKEY for tests, or wait for Phase 2 known_hosts support")
}
```

- [ ] **Step 4: Wire the up command into the root**

Edit `cmd/ssht/version.go` so `rootCmd()` registers `up`:

```go
func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ssht",
		Short: "A TUI SSH tunnel manager",
		Long: `ssht is a small, focused SSH tunnel manager.
Define your forwards once in ~/.config/sshiitake/tunnels.toml,
bring them up with ` + "`ssht up <name>`" + `.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(versionCmd())
	root.AddCommand(configCmd())
	root.AddCommand(upCmd())
	return root
}
```

- [ ] **Step 5: Update the test to pass --listen-file**

Edit the `TestUp_endToEnd` test to include the `--listen-file` flag and to read the address from it before dialling. Find the `cmd.SetArgs` block and replace with:

```go
listenFile := dir + "/listen.txt"
cmd.SetArgs([]string{
	"up", "echo",
	"--config", cfgPath,
	"--ssh-config", sshCfgPath,
	"--listen-file", listenFile,
})
```

And update the `require.Eventually` block to dial what's in `listenFile`:

```go
require.Eventually(t, func() bool {
	data, err := os.ReadFile(listenFile)
	if err != nil || len(data) == 0 {
		return false
	}
	c, err := net.Dial("tcp", string(data))
	if err != nil {
		return false
	}
	defer c.Close()
	_, err = c.Write([]byte("ping"))
	if err != nil {
		return false
	}
	buf := make([]byte, 4)
	_, err = io.ReadFull(c, buf)
	return err == nil && string(buf) == "ping"
}, 5*time.Second, 50*time.Millisecond, "tunnel forwarding never succeeded")
```

Remove the `listenAddrForTunnel` helper from the test file - the inline read replaces it.

- [ ] **Step 6: Run the test**

Run: `go test ./cmd/ssht/ -v -run TestUp_endToEnd`
Expected: PASS.

- [ ] **Step 7: Run the full test suite**

Run: `go test -race ./...`
Expected: all PASS, no data races.

- [ ] **Step 8: Manual smoke (optional, requires a real SSH host)**

If you have a real SSH host you can reach:

```bash
go build -o /tmp/ssht ./cmd/ssht
# Create a minimal tunnels.toml pointing at it; set SSHT_TEST_HOSTKEY
# to the base64 of its host key (yes, ugly - Phase 2 fixes this with
# known_hosts).
```

For now, skip if it's awkward. The integration test against the in-process server is doing the heavy lifting.

- [ ] **Step 9: Commit**

```bash
git add cmd/ssht/up.go cmd/ssht/version.go cmd/ssht/main_test.go
git commit -m "feat(cli): ssht up <name> with signal-handled shutdown"
```

---

## Task 13: Friendlier errors and exit codes

**Files:**
- Create: `cmd/ssht/errors.go`
- Modify: `cmd/ssht/main.go`
- Modify: `cmd/ssht/main_test.go`

This task replaces the bare `fmt.Fprintln(os.Stderr, err); os.Exit(1)` pattern with categorised exit codes and one-line user-facing error messages.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/ssht/main_test.go`:

```go
func TestExitCode_configError(t *testing.T) {
	code := classifyError(fmt.Errorf("load %s: open: no such file", "tunnels.toml"))
	assert.Equal(t, 1, code)
}

func TestExitCode_sshError(t *testing.T) {
	code := classifyError(fmt.Errorf("dial tcp: handshake failed"))
	assert.Equal(t, 2, code)
}

func TestExitCode_nil(t *testing.T) {
	assert.Equal(t, 0, classifyError(nil))
}
```

- [ ] **Step 2: Run test, expect compile failure**

Run: `go test ./cmd/ssht/ -run TestExitCode`
Expected: `undefined: classifyError`.

- [ ] **Step 3: Implement error classification**

Create `cmd/ssht/errors.go`:

```go
package main

import "strings"

// classifyError maps an error to a process exit code:
//
//	0   - no error
//	1   - configuration error (bad TOML, missing tunnel, validation)
//	2   - SSH or network error (handshake, dial, host key)
//	130 - interrupted by signal (handled separately in main)
func classifyError(err error) int {
	if err == nil {
		return 0
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "load "),
		strings.Contains(msg, "validate"),
		strings.Contains(msg, "not found in"),
		strings.Contains(msg, "no such file"),
		strings.Contains(msg, "unknown type"),
		strings.Contains(msg, "out of range"):
		return 1
	case strings.Contains(msg, "ssh "),
		strings.Contains(msg, "dial "),
		strings.Contains(msg, "handshake"),
		strings.Contains(msg, "host key"):
		return 2
	default:
		return 1
	}
}
```

- [ ] **Step 4: Use the classifier in main**

Replace `main()` in `cmd/ssht/main.go`:

```go
func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ssht: " + err.Error())
		os.Exit(classifyError(err))
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/ssht/ -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/ssht/errors.go cmd/ssht/main.go cmd/ssht/main_test.go
git commit -m "feat(cli): categorise errors and exit codes"
```

---

## Task 14: Update README with quick-start

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update the README**

Read the current README, then replace the "Status: pre-implementation" block and add a Quick Start section:

```markdown
# sshiitake

A TUI SSH tunnel manager. Define your forwards once, see at a glance which are
up, toggle them with a keystroke.

> **Status: Phase 1 (Foundation) shipped, no TUI yet.** The CLI can bring up
> a single tunnel from `~/.config/sshiitake/tunnels.toml`. TUI, groups,
> auto-reconnect, and hot-reload land in later phases.

## Quick Start

```bash
# Install (once a release is cut)
brew install sshiitake/tap/sshiitake

# Or build from source
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
```

(Keep the existing Why, Binary, Security, and Licence sections beneath this.)

- [ ] **Step 2: Verify rendering**

Run: `cat README.md` and eyeball it. If your terminal renders markdown, that's a bonus.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(readme): Phase 1 quick-start"
```

---

## Final verification

Before declaring Phase 1 complete:

- [ ] **All tests pass with race detector**

Run: `go test -race ./...`
Expected: PASS across all packages.

- [ ] **Linter clean**

Run: `golangci-lint run ./...`
Expected: no findings.

- [ ] **Vuln scan clean**

Run: `govulncheck ./...`
Expected: no known vulns.

- [ ] **Push and confirm CI is green**

```bash
git push origin main
gh run watch
```
Expected: gitleaks + go lint/test/build/vuln all green.

- [ ] **Run a manual smoke test**

Create `~/.config/sshiitake/tunnels.toml` with one tunnel pointing at an SSH host you actually control (a homelab, a VPS, etc.). Verify `ssht config check` reports OK, then `ssht up <name>` brings up the tunnel and a `curl` against the local port reaches the remote service.

If you have no such host yet, defer this step. The integration test exercising the in-process SSH server gives high confidence.

---

## Phase 1 known limitations

These are intentional and documented in the plan rather than deferred-quietly:

1. **ProxyJump is parsed but not honoured.** Task 5 reads `ProxyJump` from `~/.ssh/config` into `ResolvedTunnel.ProxyJump`, but Task 7's `dial()` ignores it and connects directly to `SSHHost`. Chain dialling lands in Phase 2 alongside the manager.
2. **Local forwards only.** Remote (`-R`) and dynamic (`-D`) forwards return a clear "type not supported in Phase 1" error from `Tunnel.Start`. They land in Phase 2.
3. **No reconnect on drop.** If the SSH connection drops mid-session, the tunnel goes to `StatusDown` and `Start` returns. Auto-reconnect is v1.1 (post-launch).
4. **Single-tunnel.** `ssht up` runs one tunnel and blocks. Multi-tunnel and groups land in Phase 2.

Host-key verification via `~/.ssh/known_hosts` was pulled forward as Phase 1.5 (shipped 2026-05-18, see `docs/plans/2026-05-18-known-hosts-phase-1-5.md`).

If any of these limitations are blockers for what you want to demo first, raise them now and we'll fold the fix into Phase 1 before execution.

---

## Subsequent phases (outline only; detailed plans written later)

These plans are not written yet. Sketched here so the engineer reading Phase 1 knows where it fits.

### Phase 2: Manager and CLI extensions

Adds the `manager` package that owns multiple Tunnel instances, plus groups, the metrics ring buffer, and the `ssht --bare` JSON event stream. Replaces the single-tunnel-blocking model of Phase 1's `up` command with manager-driven start/stop.

Approx 12-15 tasks. Big rocks: `Manager` type, group atomic start/stop, metrics collection, JSON status schema, `--bare` mode, log buffer (in-memory ring).

### Phase 3: TUI

Adds the Bubble Tea TUI: list view, detail view, keybindings, theming, ASCII tunnel-type diagrams in help. Wires the manager's event stream to a Bubble Tea program. Adds the wizard for `ssht add`.

Approx 15-20 tasks. Big rocks: list model, detail model, sparkline rendering, key handling, themes, wizard.

### Phase 4: Hot-reload and subprocess fallback

Adds `fsnotify` watching on `tunnels.toml` with a diff-and-apply algorithm that only restarts changed tunnels. Adds the subprocess fallback for tunnels whose ssh-config uses unsupported options (`ProxyCommand`, exotic `Match`, etc.).

Approx 6-8 tasks. Big rocks: file-watch + reload loop, tunnel diff algorithm, subprocess wrapper. (known_hosts integration shipped in Phase 1.5.)

---

## Spec coverage check

Cross-reference against `docs/design/2026-05-17-sshiitake-design.md`. Phase 1 delivers, in part or in full:

- v1 goal: define tunnels in config (Tasks 2-4) ✓
- v1 goal: read identity from ~/.ssh/config (Task 5) ✓
- v1 goal: local forwards (Tasks 6-9) ✓
- v1 goal: config validation `ssht config check` (Tasks 4, 11) ✓
- v1 goal: CLI mode for bring-up (Task 12) ✓
- v1 goal: graceful shutdown on signals (Task 12) ✓
- v1 goal: clear errors and exit codes (Task 13) ✓
- v1 goal (deferred to Phase 2): remote and dynamic forwards
- v1 goal (deferred to Phase 2): groups
- v1 goal (deferred to Phase 2): live status, latency, bandwidth, sparkline
- v1 goal (deferred to Phase 2): per-tunnel log buffer
- v1 goal (deferred to Phase 2): JSON status / `--bare`
- v1 goal (deferred to Phase 3): TUI (all of it)
- v1 goal (deferred to Phase 4): hot-reload on save
- v1 goal (deferred to Phase 4): subprocess fallback
- v1.1 goal: auto-reconnect (post-launch; not Phase 1-4)

By the end of Phase 1 the project compiles, tests pass with race detector, CI is green, and a developer can open a tunnel and `curl` through it. That's the smallest demonstrable artefact and the foundation everything else depends on.
