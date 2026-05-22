# Sshiitake Phase 2: Manager + Groups + Metrics + `--bare` JSON

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to execute task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Ship multi-tunnel orchestration. `ssht up work-stack` brings up a named group of tunnels concurrently. `ssht up --bare api-prod` streams newline-delimited JSON events to stdout for status-bar integration. The single-tunnel-blocking model of Phase 1 becomes a special case of the Manager-driven model.

**Architecture:** New `internal/manager` package owns `[]*tunnel.Tunnel` and exposes an event stream. New `internal/metrics` package provides per-tunnel ring buffers (latency + bytes-in/out time series) consumed by future TUI sparklines and by the JSON event stream. New `internal/logbuffer` package holds the per-tunnel log ring. `cmd/ssht/up.go` becomes a thin shell around `manager.Manager`.

**Tech stack:** No new external deps. Uses `golang.org/x/sync/errgroup` for parallel start (transitive dep already present via x/crypto). Builds on Phase 1's `internal/config` and `internal/tunnel`.

**Out of scope (Phase 3+):**
- Bubble Tea TUI (Phase 3)
- Hot-reload via fsnotify (Phase 4)
- Subprocess SSH fallback (Phase 4)
- Auto-reconnect (v1.1, post-launch)
- Per-tunnel JSON status file for TUI mode (Phase 3)

**Folded-in issue #1 items (touching the same code paths):**
- LocalHost=0.0.0.0 footgun (Lens 3): trivial validator change, fits in this PR.
- govulncheck pin (Lens 5): one-line workflow tweak.
- cmd/ssht in-process server lacks wg cleanup (Lens 5): the e2e test in this phase rewrites the helper anyway.

---

## File Structure

```
sshiitake/
├── cmd/ssht/
│   ├── bare.go                    [new: --bare JSON stream emitter]
│   ├── bare_test.go               [new]
│   ├── up.go                      [modify: accept multiple tunnels/group, plumb manager]
│   ├── main_test.go               [modify: rewrite TestUp_endToEnd against manager]
│   ├── version.go                 [no change]
│   ├── check.go                   [no change]
│   ├── errors.go                  [no change]
│   ├── hostkey.go                 [no change]
│   └── hostkey_test.go            [no change]
├── internal/
│   ├── config/
│   │   ├── validate.go            [modify: warn on non-loopback LocalHost]
│   │   └── validate_test.go       [modify: assert the warning]
│   ├── logbuffer/                 [new package]
│   │   ├── buffer.go              [in-memory ring of log lines]
│   │   └── buffer_test.go
│   ├── manager/                   [new package]
│   │   ├── events.go              [Event types: TunnelStateChange, MetricsUpdate, Log]
│   │   ├── events_test.go
│   │   ├── groups.go              [Group resolution]
│   │   ├── groups_test.go
│   │   ├── manager.go             [Manager type: owns tunnels, runs lifecycle]
│   │   └── manager_test.go
│   ├── metrics/                   [new package]
│   │   ├── ring.go                [generic time-series ring buffer]
│   │   ├── ring_test.go
│   │   ├── tracker.go             [per-tunnel: bytes-in/out counters + latency ring]
│   │   └── tracker_test.go
│   └── tunnel/
│       ├── local.go               [modify: thread metrics counter into pipeOneConn]
│       ├── tunnel.go              [modify: store metrics tracker; expose Metrics()]
│       └── *_test.go              [no behavioural change; signature updates]
├── .github/workflows/
│   └── go.yml                     [modify: govulncheck pin -> @v1 (rolling within v1.x)]
├── docs/
│   ├── design/2026-05-17-sshiitake-design.md  [modify: Phase 2 shipped]
│   └── plans/2026-05-18-phase-2-manager.md     [this file]
├── README.md                      [modify: groups + --bare + status-bar example]
└── CHANGELOG.md                   [new: phase-tracked history]
```

### Boundaries

- `manager` is the only place that touches multiple tunnels at once. `tunnel` stays single-tunnel.
- `metrics` and `logbuffer` are pure data containers. No `tunnel` or `manager` import.
- `cmd/ssht` only knows: parse flags → ask manager to do a thing → wait.
- `manager` emits events on a chan; consumers (TUI in Phase 3, `--bare` in this phase) subscribe.

---

## Pre-flight

- [ ] **Verify on `main` and clean working tree**

```bash
cd ~/Projects/sshiitake
git checkout main && git pull --ff-only
git status -sb   # expect: ## main...origin/main
```

- [ ] **Create the Phase 2 branch**

```bash
git checkout -b feature/phase-2-manager
```

- [ ] **Sanity-check baseline tests + lint**

```bash
go test -race ./...
GOTOOLCHAIN=go1.25.10 ~/go/bin/golangci-lint run ./...
```
Expected: all pass, 0 lint findings.

---

## Task 1: Metrics ring buffer

**Files:**
- Create: `internal/metrics/ring.go`
- Create: `internal/metrics/ring_test.go`

A generic-ish time-series ring for sparkline-friendly recent values.

- [ ] **Step 1: Write the failing test**

Create `internal/metrics/ring_test.go`:

```go
package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRing_belowCapacityKeepsAll(t *testing.T) {
	r := NewRing(5)
	now := time.Unix(0, 0)
	for i := 0; i < 3; i++ {
		r.Add(now.Add(time.Duration(i)*time.Second), float64(i))
	}
	got := r.Snapshot()
	assert.Len(t, got, 3)
	assert.Equal(t, 0.0, got[0].Value)
	assert.Equal(t, 2.0, got[2].Value)
}

func TestRing_overCapacityDropsOldest(t *testing.T) {
	r := NewRing(3)
	now := time.Unix(0, 0)
	for i := 0; i < 5; i++ {
		r.Add(now.Add(time.Duration(i)*time.Second), float64(i))
	}
	got := r.Snapshot()
	assert.Len(t, got, 3)
	assert.Equal(t, 2.0, got[0].Value, "oldest two should be dropped")
	assert.Equal(t, 4.0, got[2].Value)
}

func TestRing_concurrentSafeAddAndSnapshot(t *testing.T) {
	r := NewRing(100)
	done := make(chan struct{})

	// Writer
	go func() {
		now := time.Unix(0, 0)
		for i := 0; i < 1000; i++ {
			r.Add(now.Add(time.Duration(i)*time.Millisecond), float64(i))
		}
		close(done)
	}()

	// Reader hammering Snapshot
	for {
		select {
		case <-done:
			snap := r.Snapshot()
			assert.LessOrEqual(t, len(snap), 100)
			return
		default:
			_ = r.Snapshot()
		}
	}
}

func TestRing_zeroCapacityPanics(t *testing.T) {
	assert.Panics(t, func() { NewRing(0) })
}
```

- [ ] **Step 2: Run test, expect compile failure**

```bash
go test ./internal/metrics/
```
Expected: `undefined: NewRing` / `Ring`.

- [ ] **Step 3: Implement the ring**

Create `internal/metrics/ring.go`:

```go
// Package metrics provides per-tunnel counters and time-series ring
// buffers used by the manager event stream and (eventually) the TUI
// sparklines. The package has no dependency on tunnel or manager —
// the callers wire it in.
package metrics

import (
	"sync"
	"time"
)

// Sample is one (time, value) point.
type Sample struct {
	At    time.Time
	Value float64
}

// Ring is a fixed-capacity ring buffer of Samples. Safe for concurrent
// Add and Snapshot.
type Ring struct {
	mu       sync.Mutex
	capacity int
	samples  []Sample // length grows up to capacity, then we overwrite
	next     int      // write index for the next Add
	full     bool
}

// NewRing constructs a ring of the given capacity. Capacity must be > 0.
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		panic("metrics.NewRing: capacity must be > 0")
	}
	return &Ring{capacity: capacity, samples: make([]Sample, capacity)}
}

// Add records a sample.
func (r *Ring) Add(at time.Time, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples[r.next] = Sample{At: at, Value: value}
	r.next++
	if r.next == r.capacity {
		r.next = 0
		r.full = true
	}
}

// Snapshot returns a copy of the samples in chronological order.
func (r *Ring) Snapshot() []Sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]Sample, r.next)
		copy(out, r.samples[:r.next])
		return out
	}
	out := make([]Sample, r.capacity)
	copy(out, r.samples[r.next:])
	copy(out[r.capacity-r.next:], r.samples[:r.next])
	return out
}
```

- [ ] **Step 4: Run test, expect pass**

```bash
go test -race ./internal/metrics/ -v
```
Expected: 4 tests PASS, no races.

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(metrics): time-series ring buffer

Concurrent-safe fixed-capacity ring for (time, value) samples. Will
back per-tunnel bandwidth and latency tracking, and ultimately the
TUI sparklines.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Metrics tracker (per-tunnel counters + latency ring)

**Files:**
- Create: `internal/metrics/tracker.go`
- Create: `internal/metrics/tracker_test.go`

- [ ] **Step 1: Write the failing test**

```go
package metrics

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTracker_bytesCounters(t *testing.T) {
	tk := NewTracker()
	tk.AddBytesIn(100)
	tk.AddBytesIn(50)
	tk.AddBytesOut(200)

	in, out := tk.Bytes()
	assert.Equal(t, uint64(150), in)
	assert.Equal(t, uint64(200), out)
}

func TestTracker_latencyRing(t *testing.T) {
	tk := NewTracker()
	now := time.Unix(0, 0)
	tk.RecordLatency(now, 12*time.Millisecond)
	tk.RecordLatency(now.Add(time.Second), 18*time.Millisecond)

	snap := tk.LatencySnapshot()
	assert.Len(t, snap, 2)
	assert.InDelta(t, 12.0, snap[0].Value, 0.01, "milliseconds stored as float")
}

func TestTracker_concurrentSafe(t *testing.T) {
	tk := NewTracker()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); tk.AddBytesIn(1) }()
		go func() { defer wg.Done(); tk.AddBytesOut(1) }()
	}
	wg.Wait()

	in, out := tk.Bytes()
	assert.Equal(t, uint64(50), in)
	assert.Equal(t, uint64(50), out)
}
```

- [ ] **Step 2: Run test, expect compile failure**

```bash
go test ./internal/metrics/ -run Tracker
```
Expected: `undefined: NewTracker`.

- [ ] **Step 3: Implement the tracker**

Create `internal/metrics/tracker.go`:

```go
package metrics

import (
	"sync/atomic"
	"time"
)

// LatencyRingCapacity is the number of latency samples retained per
// tunnel. 60 samples at one-per-second gives a one-minute sparkline.
const LatencyRingCapacity = 60

// Tracker is a per-tunnel metrics container. Bytes counters use atomics
// (high-frequency writes from io.Copy goroutines). Latency uses the Ring.
type Tracker struct {
	bytesIn  atomic.Uint64
	bytesOut atomic.Uint64
	latency  *Ring
}

// NewTracker returns a fresh Tracker.
func NewTracker() *Tracker {
	return &Tracker{latency: NewRing(LatencyRingCapacity)}
}

// AddBytesIn records bytes received from the SSH side (remote -> local).
func (t *Tracker) AddBytesIn(n int64) {
	if n < 0 {
		return
	}
	t.bytesIn.Add(uint64(n))
}

// AddBytesOut records bytes sent to the SSH side (local -> remote).
func (t *Tracker) AddBytesOut(n int64) {
	if n < 0 {
		return
	}
	t.bytesOut.Add(uint64(n))
}

// Bytes returns (in, out).
func (t *Tracker) Bytes() (in, out uint64) {
	return t.bytesIn.Load(), t.bytesOut.Load()
}

// RecordLatency stores a latency sample (stored in milliseconds as float64).
func (t *Tracker) RecordLatency(at time.Time, d time.Duration) {
	t.latency.Add(at, float64(d)/float64(time.Millisecond))
}

// LatencySnapshot returns the latency ring contents in chronological order.
func (t *Tracker) LatencySnapshot() []Sample {
	return t.latency.Snapshot()
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/metrics/ -v
```
Expected: 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/tracker.go internal/metrics/tracker_test.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(metrics): per-tunnel Tracker with bytes counters + latency ring

Atomic counters for bytes-in/bytes-out (high-frequency writes from
io.Copy goroutines); 60-sample latency ring (one-minute sparkline at
one-sample-per-second).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Log buffer package

**Files:**
- Create: `internal/logbuffer/buffer.go`
- Create: `internal/logbuffer/buffer_test.go`

- [ ] **Step 1: Write the failing test**

```go
package logbuffer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBuffer_appendThenSnapshot(t *testing.T) {
	b := New(3)
	b.Append(time.Unix(1, 0), "one")
	b.Append(time.Unix(2, 0), "two")

	got := b.Snapshot()
	assert.Len(t, got, 2)
	assert.Equal(t, "one", got[0].Message)
	assert.Equal(t, "two", got[1].Message)
}

func TestBuffer_overCapacityDropsOldest(t *testing.T) {
	b := New(2)
	b.Append(time.Unix(1, 0), "one")
	b.Append(time.Unix(2, 0), "two")
	b.Append(time.Unix(3, 0), "three")

	got := b.Snapshot()
	assert.Len(t, got, 2)
	assert.Equal(t, "two", got[0].Message)
	assert.Equal(t, "three", got[1].Message)
}

func TestBuffer_concurrent(t *testing.T) {
	b := New(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Append(time.Now(), "x")
		}()
	}
	wg.Wait()
	assert.Len(t, b.Snapshot(), 50)
}

func TestBuffer_zeroCapacityPanics(t *testing.T) {
	assert.Panics(t, func() { New(0) })
}
```

- [ ] **Step 2: Confirm compile failure**

```bash
go test ./internal/logbuffer/
```
Expected: `undefined: New`.

- [ ] **Step 3: Implement**

Create `internal/logbuffer/buffer.go`:

```go
// Package logbuffer holds an in-memory ring of per-tunnel log lines.
// Consumed by future TUI detail-view and by the JSON event stream.
package logbuffer

import (
	"sync"
	"time"
)

// Entry is one log line with timestamp.
type Entry struct {
	At      time.Time
	Message string
}

// Buffer is a fixed-capacity ring of Entries, safe for concurrent use.
type Buffer struct {
	mu       sync.Mutex
	capacity int
	entries  []Entry
	next     int
	full     bool
}

// New constructs a Buffer with the given capacity. Capacity must be > 0.
func New(capacity int) *Buffer {
	if capacity <= 0 {
		panic("logbuffer.New: capacity must be > 0")
	}
	return &Buffer{capacity: capacity, entries: make([]Entry, capacity)}
}

// Append records a log line.
func (b *Buffer) Append(at time.Time, msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[b.next] = Entry{At: at, Message: msg}
	b.next++
	if b.next == b.capacity {
		b.next = 0
		b.full = true
	}
}

// Snapshot returns a copy of the entries in chronological order.
func (b *Buffer) Snapshot() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.full {
		out := make([]Entry, b.next)
		copy(out, b.entries[:b.next])
		return out
	}
	out := make([]Entry, b.capacity)
	copy(out, b.entries[b.next:])
	copy(out[b.capacity-b.next:], b.entries[:b.next])
	return out
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/logbuffer/ -v
```
Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/logbuffer/
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(logbuffer): per-tunnel in-memory log ring

Same shape as metrics.Ring but typed for log entries. Used by future
TUI detail-view and by the JSON event stream for log-tail playback.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Wire metrics counters into `forwardLocal`

**Files:**
- Modify: `internal/tunnel/local.go` (accept metrics.Tracker, call AddBytesIn/Out from io.Copy paths)
- Modify: `internal/tunnel/local_test.go` (verify counters)

- [ ] **Step 1: Write the failing test**

Append to `internal/tunnel/local_test.go`:

```go
import "github.com/Sshiitake/sshiitake/internal/metrics"

func TestForwardLocal_recordsByteCounts(t *testing.T) {
	sshAddr, hostKey := newTestSSHServer(t)

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

	cfg := &ssh.ClientConfig{User: "tester", HostKeyCallback: ssh.FixedHostKey(hostKey)}
	sshClient, err := ssh.Dial("tcp", sshAddr, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sshClient.Close() })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	tracker := metrics.NewTracker()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = forwardLocal(ctx, sshClient, listener, echo.Addr().String(), tracker) }()

	c, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	_, err = c.Write(make([]byte, 1000))
	require.NoError(t, err)
	buf := make([]byte, 1000)
	_, err = io.ReadFull(c, buf)
	require.NoError(t, err)
	_ = c.Close()

	// Allow the io.Copy goroutines to drain before reading counters.
	require.Eventually(t, func() bool {
		in, out := tracker.Bytes()
		return in >= 1000 && out >= 1000
	}, 2*time.Second, 50*time.Millisecond, "counters did not reach 1000 bytes")
}
```

Update other existing test calls to `forwardLocal` to pass `nil` as the tracker (backwards-compatible).

- [ ] **Step 2: Confirm compile failure**

```bash
go test ./internal/tunnel/
```
Expected: `too many arguments in call to forwardLocal`.

- [ ] **Step 3: Modify `internal/tunnel/local.go`**

Add `tracker *metrics.Tracker` as the last parameter. Wire byte counts through. Replace the function and `pipeOneConn`:

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

	"github.com/Sshiitake/sshiitake/internal/metrics"
)

// forwardLocal serves ln, dialling each accepted connection through
// sshClient to remoteAddr ("host:port"). It returns when ctx is
// cancelled. If tracker is non-nil, bytes-in and bytes-out are recorded.
func forwardLocal(ctx context.Context, sshClient *ssh.Client, ln net.Listener, remoteAddr string, tracker *metrics.Tracker) error {
	var (
		mu     sync.Mutex
		active = make(map[net.Conn]struct{})
	)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		mu.Lock()
		for c := range active {
			_ = c.Close()
		}
		mu.Unlock()
		_ = sshClient.Close()
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
		mu.Lock()
		active[local] = struct{}{}
		mu.Unlock()
		wg.Add(1)
		go func(local net.Conn) {
			defer wg.Done()
			defer func() {
				mu.Lock()
				delete(active, local)
				mu.Unlock()
			}()
			pipeOneConn(sshClient, local, remoteAddr, tracker)
		}(local)
	}
}

func pipeOneConn(sshClient *ssh.Client, local net.Conn, remoteAddr string, tracker *metrics.Tracker) {
	defer func() { _ = local.Close() }()
	remote, err := sshClient.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer func() { _ = remote.Close() }()

	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(remote, local)
		if tracker != nil {
			tracker.AddBytesOut(n)
		}
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(local, remote)
		if tracker != nil {
			tracker.AddBytesIn(n)
		}
		done <- struct{}{}
	}()
	<-done
}
```

- [ ] **Step 4: Update existing test call sites**

In `internal/tunnel/local_test.go`, the older `TestForwardLocal_passesBytes` calls `forwardLocal(...)` with 4 args. Add `nil` as the 5th:

```go
errCh <- forwardLocal(ctx, sshClient, listener, echo.Addr().String(), nil)
```

Same for `TestForwardLocal_closesClientOnCancel`.

- [ ] **Step 5: Tests pass**

```bash
go test -race ./internal/tunnel/ -v
```
Expected: all tunnel tests PASS, including new `TestForwardLocal_recordsByteCounts`.

- [ ] **Step 6: Commit**

```bash
git add internal/tunnel/local.go internal/tunnel/local_test.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(tunnel): record bytes-in/out through forwardLocal

Optional *metrics.Tracker parameter; counters are AddBytesIn'd /
AddBytesOut'd at the end of each io.Copy direction. Nil tracker is
a no-op for callers that don't care (existing tests).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Expose `Tracker` on `Tunnel`

**Files:**
- Modify: `internal/tunnel/tunnel.go` (add Tracker field; getter; pass into forwardLocal)
- Modify: `internal/tunnel/tunnel_test.go` (assert Tracker accessibility)

- [ ] **Step 1: Write the failing test**

Append to `internal/tunnel/tunnel_test.go`:

```go
import "github.com/Sshiitake/sshiitake/internal/metrics"

func TestTunnel_metricsAccessible(t *testing.T) {
	sshAddr, hostKey := newTestSSHServer(t)
	host, port := splitHostPort(t, sshAddr)

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
		Name: "echo", SSHHost: host, SSHPort: port, SSHUser: "tester",
		Type: config.TypeLocal, LocalHost: "127.0.0.1", LocalPort: 0,
		RemoteAddr: echo.Addr().String(),
	}
	tun := New(rt, Options{HostKeyCallback: ssh.FixedHostKey(hostKey), DialTimeout: 2 * time.Second})

	// Metrics tracker is non-nil even before Start.
	require.NotNil(t, tun.Metrics())

	in, out := tun.Metrics().Bytes()
	assert.Equal(t, uint64(0), in)
	assert.Equal(t, uint64(0), out)

	// Start, push bytes through, confirm counters tick up.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() { errCh <- tun.Start(ctx, started) }()
	<-started

	c, err := net.Dial("tcp", tun.LocalAddr())
	require.NoError(t, err)
	_, _ = c.Write([]byte("hello"))
	buf := make([]byte, 5)
	_, err = io.ReadFull(c, buf)
	require.NoError(t, err)
	_ = c.Close()

	require.Eventually(t, func() bool {
		in, out := tun.Metrics().Bytes()
		return in >= 5 && out >= 5
	}, 2*time.Second, 50*time.Millisecond, "tracker did not accumulate via tunnel")

	cancel()
	<-errCh
}
```

- [ ] **Step 2: Modify `internal/tunnel/tunnel.go`**

Add a `metrics` field to `Tunnel`, initialise in `New`, and expose `Metrics()`. Thread the tracker through `Start`'s call to `forwardLocal`. Updated code:

```go
// In imports (top of tunnel.go), add:
// 	"github.com/Sshiitake/sshiitake/internal/metrics"

type Tunnel struct {
	rt   config.ResolvedTunnel
	opts Options

	mu        sync.Mutex
	status    Status
	localAddr string

	metrics *metrics.Tracker
}

func New(rt config.ResolvedTunnel, opts Options) *Tunnel {
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 10 * time.Second
	}
	return &Tunnel{
		rt:      rt,
		opts:    opts,
		status:  StatusDown,
		metrics: metrics.NewTracker(),
	}
}

// Metrics returns the per-tunnel metrics tracker. Non-nil after New.
func (t *Tunnel) Metrics() *metrics.Tracker { return t.metrics }
```

In `Start`, change the `forwardLocal(...)` call to:

```go
err = forwardLocal(ctx, client, ln, t.rt.RemoteAddr, t.metrics)
```

- [ ] **Step 3: Run tests**

```bash
go test -race ./internal/tunnel/ -v
```
Expected: all tunnel tests PASS, including new `TestTunnel_metricsAccessible`.

- [ ] **Step 4: Lint**

```bash
GOTOOLCHAIN=go1.25.10 ~/go/bin/golangci-lint run ./...
```
Expected: 0 issues.

- [ ] **Step 5: Commit**

```bash
git add internal/tunnel/tunnel.go internal/tunnel/tunnel_test.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(tunnel): own a Tracker; expose Metrics() accessor

Each Tunnel gets its own metrics.Tracker (allocated in New). Wired
into forwardLocal automatically. Consumers (manager, TUI, --bare)
call tun.Metrics() to read counters and latency snapshots.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Manager package scaffold

**Files:**
- Create: `internal/manager/manager.go`
- Create: `internal/manager/manager_test.go`

- [ ] **Step 1: Write the failing test**

```go
package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func TestNew_emptyConfig(t *testing.T) {
	m, err := New(&config.Config{Tunnels: map[string]config.Tunnel{}}, "", Options{})
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Len(t, m.Tunnels(), 0)
}

func TestNew_singleTunnelLoaded(t *testing.T) {
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"api": {Host: "localhost", Type: config.TypeLocal,
				LocalPort: 18443, RemoteHost: "127.0.0.1", RemotePort: 80},
		},
	}
	m, err := New(cfg, "", Options{HostKeyVerification: false})
	require.NoError(t, err)
	assert.Len(t, m.Tunnels(), 1)
	assert.Equal(t, "api", m.Tunnels()[0].Name())
}

func TestNew_unknownNameFails(t *testing.T) {
	cfg := &config.Config{Tunnels: map[string]config.Tunnel{}}
	_, err := New(cfg, "", Options{Selectors: []string{"nope"}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "not found")
}
```

(`Name()` on Tunnel will be added in Task 7 since Tunnel doesn't yet expose it as a method. For now this test will compile failure on `m.Tunnels()[0].Name()` — fix in next task.)

- [ ] **Step 2: Confirm compile failure**

Expected: `undefined: New`, `undefined: Options`, `m.Tunnels` undefined.

- [ ] **Step 3: Implement `internal/manager/manager.go`**

```go
// Package manager orchestrates multiple tunnels: it loads them from
// config (optionally filtered by selector names or group), drives the
// lifecycle in parallel, and broadcasts events to subscribers.
//
// Phase 2 covers structural support; the consumer wiring (CLI --bare
// stream, future TUI) is in cmd/ssht.
package manager

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// Options configures Manager construction.
type Options struct {
	// Selectors filters the tunnels to manage. An empty slice means "all
	// tunnels in config". A name matches a single tunnel; a group name
	// expands to its members.
	Selectors []string

	// HostKeyCallback is required for production use. Tests can leave
	// it nil and set HostKeyVerification=false to skip dial-time host
	// key checks (those run inside Tunnel.dial).
	HostKeyCallback ssh.HostKeyCallback

	// HostKeyVerification, when false, lets the Manager construct
	// tunnels without a host-key callback. ONLY for tests.
	HostKeyVerification bool
}

// Manager owns a set of tunnels and exposes lifecycle controls + event
// subscription.
type Manager struct {
	tunnels []*tunnel.Tunnel
	subs    *subscribers // wired in Task 8
	mu      sync.Mutex   // protects subs membership
}

// New builds a Manager from config + ssh config path + options.
func New(cfg *config.Config, sshConfigPath string, opts Options) (*Manager, error) {
	names, err := resolveSelectors(cfg, opts.Selectors)
	if err != nil {
		return nil, err
	}

	tunnels := make([]*tunnel.Tunnel, 0, len(names))
	for _, name := range names {
		raw, ok := cfg.TunnelByName(name)
		if !ok {
			return nil, fmt.Errorf("tunnel %q not found in config", name)
		}
		rt, err := config.ResolveWithSSHConfig(raw, sshConfigPath)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", name, err)
		}
		rt.Name = name
		tunnels = append(tunnels, tunnel.New(rt, tunnel.Options{
			HostKeyCallback: opts.HostKeyCallback,
		}))
	}

	return &Manager{
		tunnels: tunnels,
		subs:    newSubscribers(),
	}, nil
}

// Tunnels returns the tunnels owned by this manager. The slice is a
// shallow copy; mutating its order is fine.
func (m *Manager) Tunnels() []*tunnel.Tunnel {
	out := make([]*tunnel.Tunnel, len(m.tunnels))
	copy(out, m.tunnels)
	return out
}

// resolveSelectors returns the ordered list of tunnel names to manage,
// expanding group names. Empty selectors -> all tunnels.
func resolveSelectors(cfg *config.Config, selectors []string) ([]string, error) {
	if len(selectors) == 0 {
		names := make([]string, 0, len(cfg.Tunnels))
		for n := range cfg.Tunnels {
			names = append(names, n)
		}
		return names, nil
	}

	var out []string
	for _, sel := range selectors {
		if _, ok := cfg.Groups[sel]; ok {
			out = append(out, tunnelsInGroup(cfg, sel)...)
			continue
		}
		if _, ok := cfg.Tunnels[sel]; ok {
			out = append(out, sel)
			continue
		}
		return nil, fmt.Errorf("selector %q: not found (neither tunnel nor group)", sel)
	}
	return out, nil
}

// subscribers is wired in Task 8; stub here to keep the file compiling.
type subscribers struct{ mu sync.Mutex }

func newSubscribers() *subscribers { return &subscribers{} }

// _ = context.Background  // satisfy import for now; removed when Start lands.
var _ = context.Background
```

Also need `tunnelsInGroup`. Add to a stub file `internal/manager/groups.go`:

```go
package manager

import "github.com/Sshiitake/sshiitake/internal/config"

// tunnelsInGroup returns the ordered names of tunnels whose Group field
// matches the given group name.
func tunnelsInGroup(cfg *config.Config, group string) []string {
	var out []string
	for name, t := range cfg.Tunnels {
		if t.Group == group {
			out = append(out, name)
		}
	}
	return out
}
```

- [ ] **Step 4: Tests fail on `.Name()`**

```bash
go test ./internal/manager/
```
Expected: `tun.Name undefined`. This is the next task's job to fix.

- [ ] **Step 5: Stub `Name()` on Tunnel for now**

Add to `internal/tunnel/tunnel.go`:

```go
// Name returns the configured name of this tunnel.
func (t *Tunnel) Name() string { return t.rt.Name }
```

- [ ] **Step 6: Tests pass**

```bash
go test -race ./internal/manager/ -v
```
Expected: 3 manager tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/manager/ internal/tunnel/tunnel.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(manager): scaffold with selector resolution

Manager owns []*tunnel.Tunnel. New() resolves selectors (tunnel name or
group name) against config + ssh_config; empty selectors = all tunnels.
Subscribers stub is in place ready for events in next task.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Groups: expand-and-deduplicate

**Files:**
- Modify: `internal/manager/groups.go`
- Create: `internal/manager/groups_test.go`

- [ ] **Step 1: Write the failing test**

```go
package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func TestTunnelsInGroup_empty(t *testing.T) {
	cfg := &config.Config{}
	assert.Empty(t, tunnelsInGroup(cfg, "anything"))
}

func TestTunnelsInGroup_filtersByMembership(t *testing.T) {
	cfg := &config.Config{Tunnels: map[string]config.Tunnel{
		"a": {Group: "work"},
		"b": {Group: "personal"},
		"c": {Group: "work"},
	}}
	got := tunnelsInGroup(cfg, "work")
	assert.ElementsMatch(t, []string{"a", "c"}, got)
}

func TestResolveSelectors_groupAndTunnelDedup(t *testing.T) {
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"a": {Group: "work"},
			"b": {Group: "work"},
		},
		Groups: map[string]config.Group{"work": {}},
	}
	// Selecting both the group AND a tunnel inside the group should
	// not yield duplicates.
	got, err := resolveSelectors(cfg, []string{"work", "a"})
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b"}, got)
}
```

- [ ] **Step 2: Update `resolveSelectors` for dedup**

Edit `internal/manager/manager.go`'s `resolveSelectors` to dedupe:

```go
func resolveSelectors(cfg *config.Config, selectors []string) ([]string, error) {
	if len(selectors) == 0 {
		names := make([]string, 0, len(cfg.Tunnels))
		for n := range cfg.Tunnels {
			names = append(names, n)
		}
		return names, nil
	}

	seen := make(map[string]struct{})
	var out []string
	add := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}

	for _, sel := range selectors {
		if _, ok := cfg.Groups[sel]; ok {
			for _, n := range tunnelsInGroup(cfg, sel) {
				add(n)
			}
			continue
		}
		if _, ok := cfg.Tunnels[sel]; ok {
			add(sel)
			continue
		}
		return nil, fmt.Errorf("selector %q: not found (neither tunnel nor group)", sel)
	}
	return out, nil
}
```

- [ ] **Step 3: Run tests**

```bash
go test -race ./internal/manager/ -v
```
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/manager/manager.go internal/manager/groups_test.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(manager): dedupe selectors when group + member overlap

resolveSelectors now uses a seen-set; passing both "work-stack" and
"api-prod" (a member) returns just the group members once.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Event types + subscribers

**Files:**
- Create: `internal/manager/events.go`
- Create: `internal/manager/events_test.go`
- Modify: `internal/manager/manager.go` (replace subscribers stub)

- [ ] **Step 1: Write the failing test**

```go
package manager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func TestSubscribe_receivesEvents(t *testing.T) {
	subs := newSubscribers()

	ch := subs.Subscribe(8)
	defer subs.Unsubscribe(ch)

	subs.publish(Event{
		Type:       EventTunnelState,
		TunnelName: "api-prod",
		Timestamp:  time.Unix(123, 0),
		Status:     tunnel.StatusUp,
	})

	select {
	case e := <-ch:
		assert.Equal(t, EventTunnelState, e.Type)
		assert.Equal(t, "api-prod", e.TunnelName)
		assert.Equal(t, tunnel.StatusUp, e.Status)
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestSubscribe_slowConsumerDropsEvents(t *testing.T) {
	subs := newSubscribers()

	ch := subs.Subscribe(1) // capacity 1
	defer subs.Unsubscribe(ch)

	// Three publishes with a 1-element buffer; two drops expected.
	subs.publish(Event{Type: EventTunnelState, TunnelName: "a"})
	subs.publish(Event{Type: EventTunnelState, TunnelName: "b"})
	subs.publish(Event{Type: EventTunnelState, TunnelName: "c"})

	got := <-ch
	assert.Equal(t, "a", got.TunnelName, "first event is the only one delivered")

	select {
	case <-ch:
		t.Fatal("expected channel empty after drop")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestUnsubscribe_closesChannel(t *testing.T) {
	subs := newSubscribers()
	ch := subs.Subscribe(1)

	subs.Unsubscribe(ch)
	// publish to ensure no panic on closed channel send
	require.NotPanics(t, func() {
		subs.publish(Event{Type: EventTunnelState})
	})
}
```

- [ ] **Step 2: Confirm compile failure**

Expected: `undefined: Event`, `EventTunnelState`, `subs.Subscribe`, etc.

- [ ] **Step 3: Implement `internal/manager/events.go`**

```go
package manager

import (
	"sync"
	"time"

	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// EventType discriminates events on the stream. The numeric values are
// not stable; the JSON encoding uses the string form (see MarshalJSON
// in the cmd/ssht/bare.go encoder).
type EventType int

const (
	EventUnknown EventType = iota
	EventTunnelState
	EventMetrics
	EventLog
)

func (et EventType) String() string {
	switch et {
	case EventTunnelState:
		return "tunnel_state"
	case EventMetrics:
		return "metrics"
	case EventLog:
		return "log"
	default:
		return "unknown"
	}
}

// Event is one message on the manager's event stream.
type Event struct {
	Type       EventType
	TunnelName string
	Timestamp  time.Time

	// EventTunnelState
	Status tunnel.Status

	// EventMetrics
	BytesIn   uint64
	BytesOut  uint64
	LatencyMs float64 // 0 if no recent sample

	// EventLog
	Message string
}

// subscribers fan-out events to N consumer channels with per-channel
// drop-on-full semantics.
type subscribers struct {
	mu      sync.Mutex
	chans   map[chan Event]struct{}
	closed  bool
}

func newSubscribers() *subscribers {
	return &subscribers{chans: make(map[chan Event]struct{})}
}

// Subscribe returns a buffered channel that will receive events.
// Capacity is bufferSize; when the buffer is full, new events for THIS
// subscriber are dropped silently. Pick capacity according to how
// patient the consumer is (TUI: 256; --bare: 256; tests: small to
// exercise drops).
func (s *subscribers) Subscribe(bufferSize int) chan Event {
	if bufferSize <= 0 {
		bufferSize = 1
	}
	ch := make(chan Event, bufferSize)
	s.mu.Lock()
	s.chans[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes ch from the subscriber set and closes it.
func (s *subscribers) Unsubscribe(ch chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.chans[ch]; !ok {
		return
	}
	delete(s.chans, ch)
	close(ch)
}

// publish fans out e to all current subscribers. Slow subscribers drop.
func (s *subscribers) publish(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	for ch := range s.chans {
		select {
		case ch <- e:
		default:
			// drop
		}
	}
}

// closeAll closes every active subscription. Called on Manager shutdown.
func (s *subscribers) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for ch := range s.chans {
		close(ch)
		delete(s.chans, ch)
	}
}
```

Modify `internal/manager/manager.go`: remove the stub `subscribers struct{ mu sync.Mutex }` and the `_ = context.Background` line.

- [ ] **Step 4: Tests pass**

```bash
go test -race ./internal/manager/ -v
```
Expected: 6 manager tests PASS (3 from Task 6 + 3 new).

- [ ] **Step 5: Commit**

```bash
git add internal/manager/
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(manager): event types + drop-on-full pub/sub

Subscribers get a buffered channel; slow consumers drop events rather
than block the publisher. Event types cover tunnel state changes,
metric snapshots, and log lines.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Manager.Run — parallel tunnel lifecycle

**Files:**
- Modify: `internal/manager/manager.go` (add Run, helpers)
- Modify: `internal/manager/manager_test.go` (e2e test)

- [ ] **Step 1: Write the integration test**

Append to `internal/manager/manager_test.go`:

```go
import (
	"context"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// TestRun_multipleTunnelsStartConcurrently constructs an in-process
// SSH server, two tunnels pointing at two echo servers, runs the
// manager, exercises both tunnels, then cancels the context and
// confirms graceful shutdown.
func TestRun_multipleTunnelsStartConcurrently(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echo1 := startEchoServer(t)
	echo2 := startEchoServer(t)

	host, port := splitHostPort(t, sshAddr)
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"echo1": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo1), RemotePort: echoPort(echo1)},
			"echo2": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo2), RemotePort: echoPort(echo2)},
		},
		Groups: map[string]config.Group{},
	}
	// Inline ssh-config: map "host:port" alias to itself, override SSHPort.
	sshCfgPath := writeTempSSHConfig(t, host, port)

	m, err := New(cfg, sshCfgPath, Options{
		HostKeyCallback:     ssh.FixedHostKey(hostKey),
		HostKeyVerification: true,
	})
	require.NoError(t, err)
	require.Len(t, m.Tunnels(), 2)

	ch := m.Subscribe(64)
	defer m.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- m.Run(ctx) }()

	// Wait for both tunnels to report Up via the event stream.
	upCount := 0
	deadline := time.After(5 * time.Second)
	for upCount < 2 {
		select {
		case e := <-ch:
			if e.Type == EventTunnelState && e.Status == tunnel.StatusUp {
				upCount++
			}
		case <-deadline:
			t.Fatalf("only %d tunnels reported Up", upCount)
		}
	}

	// Both tunnels should now be usable.
	for _, tun := range m.Tunnels() {
		require.NotEmpty(t, tun.LocalAddr())
		c, err := net.Dial("tcp", tun.LocalAddr())
		require.NoError(t, err)
		_, err = c.Write([]byte("ping"))
		require.NoError(t, err)
		buf := make([]byte, 4)
		_, err = io.ReadFull(c, buf)
		require.NoError(t, err)
		require.Equal(t, "ping", string(buf))
		_ = c.Close()
	}

	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("manager did not stop")
	}
}
```

Add the test helpers (referenced above) to a new file `internal/manager/helpers_test.go`. Most of these reuse the patterns from `internal/tunnel/testserver_test.go`:

```go
package manager

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func startTestSSHServer(t *testing.T) (addr string, hostKey ssh.PublicKey) {
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
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				handleManagerTestConn(c, cfg)
			}()
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func handleManagerTestConn(c net.Conn, cfg *ssh.ServerConfig) {
	defer c.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			_ = ch.Reject(ssh.UnknownChannelType, "")
			continue
		}
		go func(ch ssh.NewChannel) {
			var msg struct {
				DestAddr   string
				DestPort   uint32
				OriginAddr string
				OriginPort uint32
			}
			if err := ssh.Unmarshal(ch.ExtraData(), &msg); err != nil {
				_ = ch.Reject(ssh.ConnectionFailed, "")
				return
			}
			remote, err := net.Dial("tcp", fmt.Sprintf("%s:%d", msg.DestAddr, msg.DestPort))
			if err != nil {
				_ = ch.Reject(ssh.ConnectionFailed, err.Error())
				return
			}
			channel, reqs2, err := ch.Accept()
			if err != nil {
				_ = remote.Close()
				return
			}
			go ssh.DiscardRequests(reqs2)
			go func() { _, _ = io.Copy(channel, remote); _ = channel.Close() }()
			go func() { _, _ = io.Copy(remote, channel); _ = remote.Close() }()
		}(ch)
	}
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

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	pi, err := strconv.Atoi(p)
	require.NoError(t, err)
	return h, pi
}

func echoHost(addr string) string { h, _, _ := net.SplitHostPort(addr); return h }
func echoPort(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	pi, _ := strconv.Atoi(p)
	return pi
}

func writeTempSSHConfig(t *testing.T, host string, port int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh_config")
	content := fmt.Sprintf("Host %s\n    HostName %s\n    Port %d\n    User tester\n", host, host, port)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
```

Also update `internal/manager/manager_test.go`'s existing tests that used `New(...)` without HostKeyCallback. The Task 6 tests `TestNew_emptyConfig`, etc., need `HostKeyVerification: false` and either `nil` for HostKeyCallback. That's already the test's intent — but Tunnel won't accept a nil callback by default (the existing tunnel.Tunnel.dial check). The fix: skip dial validation. Manager construction doesn't call dial, just `tunnel.New`. So Tunnel.New is fine with HostKeyCallback=nil. The dial check is only at runtime. Good.

- [ ] **Step 2: Implement `Manager.Run`**

Append to `internal/manager/manager.go`:

```go
import (
	// ... existing imports ...
	"golang.org/x/sync/errgroup"
)

// Subscribe returns a buffered event channel.
func (m *Manager) Subscribe(buf int) chan Event { return m.subs.Subscribe(buf) }

// Unsubscribe closes the channel.
func (m *Manager) Unsubscribe(ch chan Event) { m.subs.Unsubscribe(ch) }

// Run starts all tunnels concurrently and blocks until ctx is
// cancelled. Returns the first error from any tunnel that fails fatally
// (e.g. dial error); other tunnels are stopped before Run returns.
// On graceful shutdown (ctx cancel), returns nil.
func (m *Manager) Run(ctx context.Context) error {
	defer m.subs.closeAll()

	g, ctx := errgroup.WithContext(ctx)
	for _, t := range m.tunnels {
		t := t // capture
		g.Go(func() error {
			return m.runOne(ctx, t)
		})
	}
	if err := g.Wait(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func (m *Manager) runOne(ctx context.Context, t *tunnel.Tunnel) error {
	started := make(chan struct{})

	stateCh := make(chan struct{})
	go func() {
		defer close(stateCh)
		// Watch for the started signal so we can emit the Up event.
		<-started
		m.subs.publish(Event{
			Type:       EventTunnelState,
			TunnelName: t.Name(),
			Timestamp:  time.Now().UTC(),
			Status:     tunnel.StatusUp,
		})
	}()

	err := t.Start(ctx, started)

	// Emit Down state (regardless of error vs graceful).
	m.subs.publish(Event{
		Type:       EventTunnelState,
		TunnelName: t.Name(),
		Timestamp:  time.Now().UTC(),
		Status:     tunnel.StatusDown,
	})

	<-stateCh // make sure the stateCh goroutine is collected
	return err
}
```

Add `"time"` and `"golang.org/x/sync/errgroup"` to imports. Get the latter:

```bash
go get golang.org/x/sync/errgroup
```

- [ ] **Step 3: Run tests**

```bash
go test -race ./internal/manager/ -v
```
Expected: all PASS, including `TestRun_multipleTunnelsStartConcurrently`.

- [ ] **Step 4: Commit**

```bash
git add internal/manager/ go.mod go.sum
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(manager): Run starts all tunnels concurrently and broadcasts state

errgroup-driven parallel start; first fatal tunnel error stops the
rest. Up / Down events are published as state transitions.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Manager metrics ticker

**Files:**
- Modify: `internal/manager/manager.go` (add startMetricsTicker)
- Modify: `internal/manager/manager_test.go` (assert metrics events arrive)

- [ ] **Step 1: Append test**

```go
func TestRun_emitsMetricsEvents(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echo := startEchoServer(t)
	host, port := splitHostPort(t, sshAddr)
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"e": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo), RemotePort: echoPort(echo)},
		},
	}
	m, err := New(cfg, writeTempSSHConfig(t, host, port), Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey), HostKeyVerification: true,
	})
	require.NoError(t, err)

	ch := m.Subscribe(64)
	defer m.Unsubscribe(ch)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- m.Run(ctx) }()

	// Wait for Up.
	requireEventEventually(t, ch, EventTunnelState, tunnel.StatusUp, 3*time.Second)

	// Push traffic so metrics actually have something to report.
	c, err := net.Dial("tcp", m.Tunnels()[0].LocalAddr())
	require.NoError(t, err)
	_, _ = c.Write(make([]byte, 1024))
	buf := make([]byte, 1024)
	_, _ = io.ReadFull(c, buf)
	_ = c.Close()

	// Expect at least one EventMetrics within a reasonable window.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Type == EventMetrics && e.BytesIn > 0 {
				cancel()
				<-runErr
				return
			}
		case <-deadline:
			t.Fatal("no EventMetrics with bytes received")
		}
	}
}

func requireEventEventually(t *testing.T, ch chan Event, et EventType, st tunnel.Status, dur time.Duration) {
	t.Helper()
	deadline := time.After(dur)
	for {
		select {
		case e := <-ch:
			if e.Type == et && e.Status == st {
				return
			}
		case <-deadline:
			t.Fatalf("did not see %v / %v", et, st)
		}
	}
}
```

- [ ] **Step 2: Implement the ticker**

Add to `internal/manager/manager.go`:

```go
// metricsTickInterval determines how often Manager emits an EventMetrics
// snapshot per tunnel. 1s is conservative; the TUI sparkline needs at
// most one sample per second at the 60-cell width.
const metricsTickInterval = time.Second

func (m *Manager) startMetricsTicker(ctx context.Context) {
	go func() {
		t := time.NewTicker(metricsTickInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				for _, tun := range m.tunnels {
					if tun.Status() != tunnel.StatusUp {
						continue
					}
					in, out := tun.Metrics().Bytes()
					var lat float64
					if snap := tun.Metrics().LatencySnapshot(); len(snap) > 0 {
						lat = snap[len(snap)-1].Value
					}
					m.subs.publish(Event{
						Type:       EventMetrics,
						TunnelName: tun.Name(),
						Timestamp:  now.UTC(),
						BytesIn:    in,
						BytesOut:   out,
						LatencyMs:  lat,
					})
				}
			}
		}
	}()
}
```

In `Run`, start the ticker before the errgroup:

```go
func (m *Manager) Run(ctx context.Context) error {
	defer m.subs.closeAll()

	m.startMetricsTicker(ctx)

	g, ctx := errgroup.WithContext(ctx)
	// ... rest unchanged
}
```

- [ ] **Step 3: Tests pass**

```bash
go test -race ./internal/manager/ -v
```
Expected: all PASS including `TestRun_emitsMetricsEvents`.

- [ ] **Step 4: Commit**

```bash
git add internal/manager/manager.go internal/manager/manager_test.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(manager): per-second metrics ticker emits EventMetrics

For each Up tunnel, snapshot bytes-in/bytes-out + last latency sample
and publish via the subscriber fan-out. Off when the tunnel is not Up.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: CLI: `ssht up` accepts multiple tunnels + groups

**Files:**
- Modify: `cmd/ssht/up.go` (use Manager, accept N args)
- Modify: `cmd/ssht/main_test.go` (update e2e to manager-driven; remove reserveLocalPort hack)

- [ ] **Step 1: Rewrite `cmd/ssht/up.go`**

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func upCmd() *cobra.Command {
	var (
		cfgPath        string
		sshCfgPath     string
		knownHostsPath string
		listenFile     string
		bare           bool
	)
	cmd := &cobra.Command{
		Use:   "up <name|group>...",
		Short: "Bring up one or more tunnels (or a group) and run until interrupted",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			hostKeyCB, err := buildHostKeyCallback(knownHostsPath)
			if err != nil {
				return err
			}

			m, err := manager.New(cfg, sshCfgPath, manager.Options{
				Selectors:           args,
				HostKeyCallback:     hostKeyCB,
				HostKeyVerification: true,
			})
			if err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()

			eventCh := m.Subscribe(256)
			defer m.Unsubscribe(eventCh)

			runErr := make(chan error, 1)
			go func() { runErr <- m.Run(ctx) }()

			if bare {
				return streamBareEvents(cmd, eventCh, runErr, m)
			}

			return streamHumanEvents(cmd, eventCh, runErr, m, listenFile)
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", defaultConfigPath(), "path to tunnels.toml")
	cmd.Flags().StringVar(&sshCfgPath, "ssh-config", "", "path to ssh_config (default ~/.ssh/config)")
	cmd.Flags().StringVar(&knownHostsPath, "known-hosts", "", "path to known_hosts (default ~/.ssh/known_hosts)")
	cmd.Flags().StringVar(&listenFile, "listen-file", "", "test-only: write first tunnel listen addr here")
	_ = cmd.Flags().MarkHidden("listen-file")
	cmd.Flags().BoolVar(&bare, "bare", false, "stream newline-delimited JSON events to stdout; no human-friendly output")
	return cmd
}

// streamHumanEvents prints state changes to the user until ctx is done
// or Run returns.
func streamHumanEvents(cmd *cobra.Command, eventCh chan manager.Event, runErr chan error, m *manager.Manager, listenFile string) error {
	out := cmd.OutOrStdout()
	upCount := 0
	totalTunnels := len(m.Tunnels())

	for {
		select {
		case e, ok := <-eventCh:
			if !ok {
				return <-runErr
			}
			switch e.Type {
			case manager.EventTunnelState:
				switch e.Status {
				case tunnel.StatusUp:
					upCount++
					for _, tun := range m.Tunnels() {
						if tun.Name() == e.TunnelName {
							fmt.Fprintf(out, "tunnel %q up on %s\n", e.TunnelName, tun.LocalAddr())
							if listenFile != "" && upCount == 1 {
								_ = os.WriteFile(listenFile, []byte(tun.LocalAddr()), 0o600)
							}
							break
						}
					}
				case tunnel.StatusDown:
					fmt.Fprintf(out, "tunnel %q down\n", e.TunnelName)
				}
			}
			_ = totalTunnels
		case err := <-runErr:
			return err
		}
	}
}
```

`streamBareEvents` will be added in Task 12. For now stub it:

```go
func streamBareEvents(cmd *cobra.Command, eventCh chan manager.Event, runErr chan error, m *manager.Manager) error {
	return fmt.Errorf("--bare is not yet wired up; see Task 12")
}
```

(time imported but not used here; remove that import if unused.)

- [ ] **Step 2: Update `cmd/ssht/main_test.go` TestUp_endToEnd**

Replace the old single-tunnel test with a manager-driven version that doesn't need `reserveLocalPort`:

```go
func TestUp_endToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e in short mode")
	}

	sshAddr, hostKey := newInProcSSHServer(t)
	host, port := splitHostPortHelper(t, sshAddr)
	echoAddr := startEchoServer(t)

	dir := t.TempDir()
	cfgPath := dir + "/tunnels.toml"
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`
[tunnels.echo]
host = "%s"
type = "local"
local_port = 18443
remote_host = "%s"
remote_port = %s
`, host, echoHost(echoAddr), echoPort(echoAddr))), 0o600))

	sshCfgPath := dir + "/ssh_config"
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(fmt.Sprintf(`
Host %s
    HostName %s
    Port %d
    User tester
`, host, host, port)), 0o600))

	t.Setenv("SSHT_TEST_HOSTKEY", base64HostKey(hostKey))

	listenFile := dir + "/listen.txt"
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"up", "echo",
		"--config", cfgPath,
		"--ssh-config", sshCfgPath,
		"--listen-file", listenFile,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()

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
	}, 8*time.Second, 50*time.Millisecond, "tunnel forwarding never succeeded")

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("up did not exit on context cancel")
	}
}
```

Note: `local_port = 18443` is a fixed high port. If it's in use locally, the test fails. The previous race-prone `reserveLocalPort` helper is gone. (Future polish: make the validator accept `local_port = 0` and the Manager surface the bound port; out of scope here.)

Remove the `reserveLocalPort` helper from `cmd/ssht/main_test.go` since it's no longer used.

- [ ] **Step 3: Tests pass**

```bash
go test -race ./...
```
Expected: all packages PASS.

- [ ] **Step 4: Manual smoke**

```bash
go build -o /tmp/ssht ./cmd/ssht
SSHT_TEST_HOSTKEY=... /tmp/ssht up echo --config /tmp/test.toml --ssh-config /tmp/test_ssh
```
Expect: "tunnel \"echo\" up on 127.0.0.1:18443"

- [ ] **Step 5: Commit**

```bash
git add cmd/ssht/up.go cmd/ssht/main_test.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(cli): ssht up accepts multiple tunnels/groups via Manager

The up command now constructs a manager.Manager from one or more
selectors (tunnel names or group names), runs them concurrently, and
reports state via the event stream. Single-tunnel use is now a special
case of N-tunnel. Removed the reserveLocalPort flake helper.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: `--bare` JSON event stream

**Files:**
- Create: `cmd/ssht/bare.go` (streamBareEvents implementation + JSON marshalling)
- Create: `cmd/ssht/bare_test.go`
- Modify: `cmd/ssht/up.go` (replace the stub)

- [ ] **Step 1: Write the failing test**

Create `cmd/ssht/bare_test.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func TestBare_marshalsEvent(t *testing.T) {
	var buf bytes.Buffer
	enc := newBareEncoder(&buf)

	err := enc.write(manager.Event{
		Type:       manager.EventTunnelState,
		TunnelName: "api-prod",
		Timestamp:  time.Unix(1700000000, 0).UTC(),
		Status:     tunnel.StatusUp,
	})
	require.NoError(t, err)

	line := strings.TrimSpace(buf.String())
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &obj))
	assert.Equal(t, "tunnel_state", obj["type"])
	assert.Equal(t, "api-prod", obj["tunnel"])
	assert.Equal(t, "up", obj["status"])
	// Ends with a newline so the consumer can line-buffer.
	assert.True(t, strings.HasSuffix(buf.String(), "\n"))
}

func TestBare_metricsEvent(t *testing.T) {
	var buf bytes.Buffer
	enc := newBareEncoder(&buf)

	err := enc.write(manager.Event{
		Type:       manager.EventMetrics,
		TunnelName: "api-prod",
		Timestamp:  time.Unix(1700000001, 0).UTC(),
		BytesIn:    1024,
		BytesOut:   2048,
		LatencyMs:  12.3,
	})
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &obj))
	assert.Equal(t, "metrics", obj["type"])
	assert.InDelta(t, 1024.0, obj["bytes_in"], 0.01)
	assert.InDelta(t, 2048.0, obj["bytes_out"], 0.01)
	assert.InDelta(t, 12.3, obj["latency_ms"], 0.01)
}

func TestBare_unknownEventTypeReturnsError(t *testing.T) {
	var buf bytes.Buffer
	enc := newBareEncoder(&buf)
	err := enc.write(manager.Event{Type: manager.EventUnknown})
	require.Error(t, err)
}
```

- [ ] **Step 2: Implement `cmd/ssht/bare.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// bareEnvelope is the on-wire JSON schema for --bare events.
// Stable across patch versions; minor changes add optional fields.
type bareEnvelope struct {
	Type      string  `json:"type"`
	Tunnel    string  `json:"tunnel,omitempty"`
	Timestamp string  `json:"ts"`
	Status    string  `json:"status,omitempty"`
	BytesIn   uint64  `json:"bytes_in,omitempty"`
	BytesOut  uint64  `json:"bytes_out,omitempty"`
	LatencyMs float64 `json:"latency_ms,omitempty"`
	Message   string  `json:"message,omitempty"`
}

type bareEncoder struct{ w io.Writer }

func newBareEncoder(w io.Writer) *bareEncoder { return &bareEncoder{w: w} }

func (b *bareEncoder) write(e manager.Event) error {
	env := bareEnvelope{
		Type:      e.Type.String(),
		Tunnel:    e.TunnelName,
		Timestamp: e.Timestamp.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	switch e.Type {
	case manager.EventTunnelState:
		env.Status = statusString(e.Status)
	case manager.EventMetrics:
		env.BytesIn = e.BytesIn
		env.BytesOut = e.BytesOut
		env.LatencyMs = e.LatencyMs
	case manager.EventLog:
		env.Message = e.Message
	default:
		return fmt.Errorf("bare: unknown event type %d", e.Type)
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("bare: marshal: %w", err)
	}
	if _, err := b.w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("bare: write: %w", err)
	}
	return nil
}

func statusString(s tunnel.Status) string {
	switch s {
	case tunnel.StatusDown:
		return "down"
	case tunnel.StatusConnecting:
		return "connecting"
	case tunnel.StatusUp:
		return "up"
	case tunnel.StatusStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

func streamBareEvents(cmd *cobra.Command, eventCh chan manager.Event, runErr chan error, _ *manager.Manager) error {
	enc := newBareEncoder(cmd.OutOrStdout())
	_ = context.Background // future expansion
	for {
		select {
		case e, ok := <-eventCh:
			if !ok {
				return <-runErr
			}
			if err := enc.write(e); err != nil {
				// In bare mode, marshal errors are fatal — the consumer
				// expects a coherent stream.
				return err
			}
		case err := <-runErr:
			return err
		}
	}
}
```

- [ ] **Step 3: Tests pass**

```bash
go test -race ./cmd/ssht/ -v
```
Expected: all PASS including 3 new bare tests.

- [ ] **Step 4: Manual smoke**

```bash
go build -o /tmp/ssht ./cmd/ssht
SSHT_TEST_HOSTKEY=$(...) /tmp/ssht up echo --config /tmp/x.toml --bare | head -5
```
Expect: newline-delimited JSON lines, one per event.

- [ ] **Step 5: Commit**

```bash
git add cmd/ssht/bare.go cmd/ssht/bare_test.go cmd/ssht/up.go
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
feat(cli): --bare streams newline-delimited JSON events

ssht up --bare emits one JSON object per line on stdout for each event
the manager publishes (state change, metrics tick, log). Stable schema
(see cmd/ssht/bare.go bareEnvelope) consumable by SketchyBar, xbar,
Waybar, tmux status, or any line-buffered shell pipe.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Issue #1 folded items

**Files:**
- Modify: `internal/config/validate.go` (LocalHost warning)
- Modify: `internal/config/validate_test.go`
- Modify: `.github/workflows/go.yml` (govulncheck pin -> @v1)

- [ ] **Step 1: Test the LocalHost validation**

Append to `internal/config/validate_test.go`:

```go
func TestValidate_localHostLoopbackOK(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalHost: "127.0.0.1",
			LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	require.NoError(t, cfg.Validate())
}

func TestValidate_localHostNonLoopbackRejected(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalHost: "0.0.0.0",
			LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "local_host")
	assert.ErrorContains(t, err, "0.0.0.0")
}

func TestValidate_localHostIPv6LoopbackOK(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalHost: "::1",
			LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	require.NoError(t, cfg.Validate())
}

func TestValidate_localHostExternalRejected(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalHost: "192.168.1.50",
			LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	require.Error(t, cfg.Validate())
}
```

- [ ] **Step 2: Implement the check**

Modify `internal/config/validate.go`'s `validateTunnel`. Add at the top of the function:

```go
import "net"   // add to imports

func validateTunnel(t Tunnel) error {
	if t.Host == "" {
		return errors.New("host must not be empty")
	}
	if t.LocalHost != "" {
		if !isLoopback(t.LocalHost) {
			return fmt.Errorf("local_host %q is not a loopback address; "+
				"binding to non-loopback exposes the tunnel to the network. "+
				"If you really want this, ask for it in a future feature: "+
				"expose_to_network = true (not yet implemented)", t.LocalHost)
		}
	}
	// ... rest unchanged
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
```

- [ ] **Step 3: Pin govulncheck**

In `.github/workflows/go.yml`, find the `vuln` job's install step and change:

```yaml
      - run: go install golang.org/x/vuln/cmd/govulncheck@v1.3.0
```

to:

```yaml
      - run: go install golang.org/x/vuln/cmd/govulncheck@v1
```

- [ ] **Step 4: Tests pass**

```bash
go test -race ./...
GOTOOLCHAIN=go1.25.10 ~/go/bin/golangci-lint run ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/config/validate.go internal/config/validate_test.go .github/workflows/go.yml
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
chore: fold in issue #1 items: LocalHost validation, govulncheck pin

- Validator rejects non-loopback local_host (closes the 0.0.0.0
  network-exposure footgun flagged in the Phase 1 red-team review).
- govulncheck pin moved from @v1.3.0 to @v1 so the analyser tracks
  the vuln DB protocol as it rolls.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: README + design spec + CHANGELOG

**Files:**
- Modify: `README.md`
- Modify: `docs/design/2026-05-17-sshiitake-design.md`
- Create: `CHANGELOG.md`

- [ ] **Step 1: Update README**

Find the status banner and replace:

```markdown
> **Status: Phase 1 + 1.5 + 2 shipped, no TUI yet.** The CLI brings up
> one or more tunnels (or a named group) from `~/.config/sshiitake/tunnels.toml`,
> with `~/.ssh/known_hosts` verification and per-tunnel metrics. `--bare`
> streams JSON events for status-bar integration. TUI lands in Phase 3.
```

Update Quick Start to show multi-tunnel + group + --bare:

```markdown
Bring up a single tunnel:

\`\`\`bash
ssht up api-prod
\`\`\`

Bring up a named group (defined under `[groups.<name>]` in your config):

\`\`\`bash
ssht up work-stack
\`\`\`

Stream JSON events for a status bar:

\`\`\`bash
ssht up --bare api-prod | sketchybar-renderer
\`\`\`
```

(Keep the existing first-time-use callout and Flags table; add `--bare` to the Flags table.)

- [ ] **Step 2: Update design spec**

In `docs/design/2026-05-17-sshiitake-design.md`, find the "Phase 2" outline section and replace its content with a brief "shipped" note. Add a "Phase 2 limitations" subsection listing what's still in Phase 3+ (no TUI, no hot-reload, no auto-reconnect).

- [ ] **Step 3: Create CHANGELOG.md**

```markdown
# Changelog

All notable changes to sshiitake by phase. Pre-1.0 versions follow the
phased plan in `docs/design/`.

## Phase 2 — 2026-05-18

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

## Phase 1.5 — 2026-05-18

### Added
- `~/.ssh/known_hosts` host-key verification via `knownhosts.New`.
- `--known-hosts` flag.
- Sentinel errors `ErrKeyMismatch`, `ErrHostNotInKnownHosts` (route to
  exit code 2).
- Loud stderr warning when `SSHT_TEST_HOSTKEY` is set in a non-test binary.

## Phase 1 — 2026-05-17

### Added
- Initial release. Single tunnel via `ssht up <name>`.
- TOML config (`~/.config/sshiitake/tunnels.toml`).
- `~/.ssh/config` integration for host identity.
- In-process SSH (golang.org/x/crypto/ssh) with ssh-agent + key auth.
- CLI: `version`, `config check`, `up`.
- CI: gitleaks, lint, test (linux + macos), build, vuln scan.
```

- [ ] **Step 4: Verify**

```bash
git status -sb
```

Should show README.md, docs/design/2026-05-17-sshiitake-design.md, CHANGELOG.md modified/created.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/design/2026-05-17-sshiitake-design.md CHANGELOG.md
git -c user.email="claude@seamonster.co.uk" -c user.name="Adam Neilson" \
  commit -m "$(cat <<'EOF'
docs: Phase 2 status update, multi-tunnel quickstart, CHANGELOG

README banner + Quick Start now show multi-tunnel + group + --bare.
Design spec marks Phase 2 shipped. New CHANGELOG.md tracks phased
deliveries for users reading github releases.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

- [ ] **All tests pass with race detector**

```bash
go test -race ./...
```

- [ ] **Lint clean**

```bash
GOTOOLCHAIN=go1.25.10 ~/go/bin/golangci-lint run ./...
```

- [ ] **Local govulncheck clean (if installed)**

```bash
govulncheck ./...
```

- [ ] **Manual smoke: group + multi-tunnel + --bare**

```bash
cat > /tmp/test.toml <<'EOF'
[tunnels.a]
host = "myhost"
type = "local"
local_port = 19001
remote_host = "127.0.0.1"
remote_port = 22
group = "test"

[tunnels.b]
host = "myhost"
type = "local"
local_port = 19002
remote_host = "127.0.0.1"
remote_port = 80
group = "test"

[groups.test]
description = "Smoke group"
EOF

# Set SSHT_TEST_HOSTKEY for a real reachable test host first.
ssht up test --config /tmp/test.toml         # Group form
ssht up a b --config /tmp/test.toml          # Multi-name form
ssht up a --bare --config /tmp/test.toml     # JSON stream
```

- [ ] **Push and open PR**

```bash
git push -u origin feature/phase-2-manager   # or via SSH remote if HTTPS auth flakes
gh pr create --base main --head feature/phase-2-manager \
  --title "Phase 2: Manager + groups + metrics + --bare JSON" \
  --body-file docs/plans/2026-05-18-phase-2-manager.md
```

- [ ] **Red-team review (the /goal gate)**

After CI green, run review-team skill against the PR diff. Address blockers; iterate until accepted. Merge.

---

## Success criteria

1. `ssht up a b c` brings up three tunnels concurrently. Time-to-up << time-to-up-sequentially.
2. `ssht up work-stack` brings up all tunnels with `group = "work-stack"`.
3. `ssht up --bare api-prod` emits valid newline-delimited JSON; downstream consumers can `jq` it.
4. Per-tunnel bandwidth counters tick up as bytes flow.
5. `local_host = "0.0.0.0"` is rejected at validate time with a clear "exposes to network" error.
6. All tests pass with `-race`. Lint clean. govulncheck clean. CI matrix (ubuntu + macos) green.
7. Red-team review-team accepts.

## Known limitations after Phase 2

- No TUI (Phase 3).
- No hot-reload of config (Phase 4).
- No subprocess SSH fallback (Phase 4).
- No auto-reconnect when a tunnel drops (v1.1, post-launch).
- Single fixed-port tests still use a hardcoded port (validator rejects `local_port = 0`); converting the validator to accept 0 as "auto-pick" is a small Phase 3 cleanup.
- `--bare` JSON schema is v1; future minor changes add optional fields, never rename existing ones.
