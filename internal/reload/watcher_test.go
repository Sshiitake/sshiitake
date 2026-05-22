package reload

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile is a convenience for the watcher tests; uses 0o600 to match
// the production atomic-write path's mode.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestWatcher_singleWriteEmits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnels.toml")
	writeFile(t, path, "# initial\n")

	// 50ms debounce keeps the test fast; production uses 200ms.
	w, err := New(path, 50*time.Millisecond)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(runDone)
	}()

	// Tiny pause so the watcher goroutine is scheduled before the write.
	// Without this the write event can race the Add() call's completion
	// on some platforms.
	time.Sleep(10 * time.Millisecond)

	writeFile(t, path, "# changed\n")

	select {
	case <-w.Changed:
		// OK
	case <-time.After(time.Second):
		t.Fatal("did not receive Changed after write")
	}

	cancel()
	<-runDone
}

func TestWatcher_rapidWritesDebounce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnels.toml")
	writeFile(t, path, "# initial\n")

	w, err := New(path, 100*time.Millisecond)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(runDone)
	}()

	time.Sleep(10 * time.Millisecond)

	// Three writes within the debounce window. Should collapse to a
	// single Changed event.
	for i := 0; i < 3; i++ {
		writeFile(t, path, "# burst\n")
		time.Sleep(15 * time.Millisecond)
	}

	// Wait for the debounce to elapse plus a small grace period.
	select {
	case <-w.Changed:
		// OK
	case <-time.After(time.Second):
		t.Fatal("did not receive Changed after burst")
	}

	// Now assert no SECOND Changed event arrives within the same
	// debounce window (i.e. the burst genuinely collapsed). 300ms is
	// enough for any stray timer to have fired.
	select {
	case <-w.Changed:
		t.Fatal("received a second Changed; burst was not debounced")
	case <-time.After(300 * time.Millisecond):
		// OK: only one event from the burst.
	}

	cancel()
	<-runDone
}

func TestWatcher_separateBurstsEmitSeparately(t *testing.T) {
	// Confirm the debounce is per-burst, not a global rate limit.
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnels.toml")
	writeFile(t, path, "# initial\n")

	w, err := New(path, 50*time.Millisecond)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(runDone)
	}()

	time.Sleep(10 * time.Millisecond)

	// First burst
	writeFile(t, path, "# first\n")
	select {
	case <-w.Changed:
	case <-time.After(time.Second):
		t.Fatal("missed first Changed")
	}

	// Wait well past the debounce window, then write again.
	time.Sleep(150 * time.Millisecond)

	writeFile(t, path, "# second\n")
	select {
	case <-w.Changed:
	case <-time.After(time.Second):
		t.Fatal("missed second Changed")
	}

	cancel()
	<-runDone
}

func TestWatcher_ctxCancelReturnsCleanly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnels.toml")
	writeFile(t, path, "# x\n")

	w, err := New(path, 50*time.Millisecond)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	var (
		mu      sync.Mutex
		runErr  error
		runDone = make(chan struct{})
	)
	go func() {
		err := w.Run(ctx)
		mu.Lock()
		runErr = err
		mu.Unlock()
		close(runDone)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.NoError(t, runErr, "Run should return nil on ctx cancel")
}

func TestWatcher_atomicRenameStillFires(t *testing.T) {
	// Editor pattern: write to temp, fsync, rename over the target.
	// appendTunnel does this. The watcher must still emit Changed.
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnels.toml")
	writeFile(t, path, "# original\n")

	w, err := New(path, 50*time.Millisecond)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(runDone)
	}()

	time.Sleep(10 * time.Millisecond)

	tmpPath := filepath.Join(dir, ".tunnels.toml.tmp")
	writeFile(t, tmpPath, "# replaced\n")
	require.NoError(t, os.Rename(tmpPath, path))

	select {
	case <-w.Changed:
		// OK
	case <-time.After(time.Second):
		t.Fatal("did not receive Changed after atomic rename")
	}

	cancel()
	<-runDone
}
