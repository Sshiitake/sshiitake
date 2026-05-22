// Package reload provides hot-reload primitives for tunnels.toml: a
// debounced fsnotify wrapper that emits a single Changed event per burst
// of writes, and a diff function that produces an Added/Removed/Modified
// plan against the previous config.
//
// The Manager consumes these to mutate the live tunnel set without
// requiring a process restart.
package reload

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultDebounce is the recommended debounce window for typical editors
// (vim, VSCode, neovim with backup-write strategies). 200ms swallows the
// remove/create/write trio without making the user-visible reload feel
// laggy.
const DefaultDebounce = 200 * time.Millisecond

// Watcher watches a single file for writes and emits at most one
// Changed event per debounce window. Editor patterns that swap a file
// (write-to-temp + rename + remove-old) are handled by re-adding the
// watch when the file disappears.
type Watcher struct {
	w        *fsnotify.Watcher
	path     string
	debounce time.Duration

	// Changed receives struct{} after each debounced burst. Buffer is 1
	// and sends are non-blocking; if the consumer is slow we coalesce
	// further events into the single pending slot.
	Changed chan struct{}
}

// New creates a Watcher for the given path with the supplied debounce
// window. Pass DefaultDebounce unless tests need something different.
//
// The path's parent directory is also watched so a remove-then-create
// (atomic save pattern used by vim, VSCode, and our own appendTunnel)
// keeps producing events; on Remove of the target we re-Add it once it
// reappears.
func New(path string, debounce time.Duration) (*Watcher, error) {
	if debounce <= 0 {
		debounce = DefaultDebounce
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// Watching the parent directory rather than the file itself catches
	// editor write-to-temp + rename patterns: fsnotify on the file
	// invalidates when the inode is replaced, but directory watches keep
	// firing.
	if err := w.Add(filepath.Dir(path)); err != nil {
		_ = w.Close()
		return nil, err
	}
	return &Watcher{
		w:        w,
		path:     path,
		debounce: debounce,
		Changed:  make(chan struct{}, 1),
	}, nil
}

// Run blocks until ctx is cancelled. It dispatches debounced Changed
// events to w.Changed. The underlying fsnotify.Watcher is closed on
// return.
//
// Errors from fsnotify (other than channel-closed) are silently
// dropped; callers that care about diagnostic visibility can wrap Run
// with a logger. We don't surface them through Run's error because the
// design intent is "hot reload is best-effort; failure of the watcher
// must not take down the running tunnels."
func (w *Watcher) Run(ctx context.Context) error {
	var (
		mu      sync.Mutex
		timer   *time.Timer
		pending bool
	)
	fire := func() {
		mu.Lock()
		pending = false
		mu.Unlock()
		// Non-blocking send: if a previous event hasn't been consumed,
		// coalesce silently. The consumer will pick up the latest config
		// state via config.Load when it processes the signal.
		select {
		case w.Changed <- struct{}{}:
		default:
		}
	}

	defer func() {
		mu.Lock()
		if timer != nil {
			timer.Stop()
		}
		mu.Unlock()
		_ = w.w.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.w.Events:
			if !ok {
				return nil
			}
			// Filter to events on our target file. fsnotify on the parent
			// directory will surface events for siblings too (e.g. the
			// .tunnels.toml.* temp files appendTunnel uses).
			if filepath.Clean(ev.Name) != filepath.Clean(w.path) {
				continue
			}
			// Some editors save by renaming a temp file over the target.
			// fsnotify on Linux can lose the watch on the file inode; we
			// watch the parent directory to avoid that pitfall, so no
			// re-Add dance is needed here.
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			mu.Lock()
			pending = true
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.debounce, func() {
				mu.Lock()
				stillPending := pending
				mu.Unlock()
				if stillPending {
					fire()
				}
			})
			mu.Unlock()
		case err, ok := <-w.w.Errors:
			if !ok {
				return nil
			}
			// Filter out the closed-watcher sentinel that surfaces when we
			// Close the watcher concurrent with the consumer reading
			// Errors. Real errors are otherwise ignored: hot reload is
			// advisory.
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				// Overflow means we may have missed events; force a fire so
				// the consumer at least reloads once.
				fire()
				continue
			}
		}
	}
}
