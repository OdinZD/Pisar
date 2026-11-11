package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Change is one raw notification from a Source.
type Change struct {
	Path   string // absolute path, as the source reports it
	Dir    bool   // the path is a directory
	Rescan bool   // the source lost track (overflow, root moved or deleted): anything may have changed
}

// Source delivers raw change notifications for one directory tree.
// Defined here, at the consumer, so tests can replace it.
type Source interface {
	// Start watches root until ctx is cancelled. The returned channel is
	// closed once the source has fully stopped.
	Start(ctx context.Context, root string) (<-chan []Change, error)
}

// WatchOptions configures a Watcher.
type WatchOptions struct {
	Source   Source
	Debounce time.Duration   // quiet time after the last relevant change; > 0
	MaxDelay time.Duration   // upper bound for postponing while changes keep coming; >= Debounce
	OnChange func(id string) // called with the project id; never concurrently with itself
}

// ErrWatcherClosed is returned by Add after Close.
var ErrWatcherClosed = errors.New("project: watcher is closed")

// Watcher turns raw Source notifications into one debounced OnChange call per
// burst of relevant changes. OnChange must not call back into the Watcher:
// Remove and Close wait for a running OnChange and would deadlock.
type Watcher struct {
	opts WatchOptions

	ctx    context.Context // parent of every project context; cancelled by Close
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// cbMu serialises OnChange across all projects.
	cbMu sync.Mutex

	mu       sync.Mutex
	closed   bool
	projects map[string]*watched
}

type watched struct {
	id       string
	realRoot string // root with symlinks resolved: sources report real paths
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{} // closed when the project goroutine has finished
	dead     atomic.Bool   // the source stopped on its own
}

// NewWatcher validates opts and returns a Watcher with no projects.
func NewWatcher(opts WatchOptions) (*Watcher, error) {
	switch {
	case opts.Source == nil:
		return nil, errors.New("project: watcher needs a Source")
	case opts.OnChange == nil:
		return nil, errors.New("project: watcher needs an OnChange callback")
	case opts.Debounce <= 0:
		return nil, fmt.Errorf("project: Debounce must be positive, got %v", opts.Debounce)
	case opts.MaxDelay < opts.Debounce:
		return nil, fmt.Errorf("project: MaxDelay %v is shorter than Debounce %v", opts.MaxDelay, opts.Debounce)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Watcher{opts: opts, ctx: ctx, cancel: cancel, projects: make(map[string]*watched)}, nil
}

// Add starts watching root for the project id. Adding an id that is already
// watched is an error.
func (w *Watcher) Add(id, root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("project: watch %q: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project: watch %q: not a directory", root)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("project: watch %q: resolve symlinks: %w", root, err)
	}

	// Start runs under the lock so that Add, Remove and Close cannot interleave
	// with a half-registered project; starting a stream is quick.
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWatcherClosed
	}
	if old, ok := w.projects[id]; ok {
		if !old.dead.Load() {
			return fmt.Errorf("project: %q is already watched", id)
		}
		// Its source is gone; let it finish and take its place.
		old.cancel()
		<-old.done
		delete(w.projects, id)
	}

	ctx, cancel := context.WithCancel(w.ctx)
	ch, err := w.opts.Source.Start(ctx, realRoot)
	if err != nil {
		cancel()
		return fmt.Errorf("project: start watching %q: %w", root, err)
	}
	p := &watched{id: id, realRoot: realRoot, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	w.projects[id] = p
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.run(p, ch)
	}()
	return nil
}

// Remove stops watching a project. After it returns, OnChange is not called
// for that id any more. Unknown id: no-op.
func (w *Watcher) Remove(id string) {
	w.mu.Lock()
	p, ok := w.projects[id]
	delete(w.projects, id)
	w.mu.Unlock()
	if !ok {
		return
	}
	p.cancel()
	<-p.done
}

// Watching reports whether id is currently watched.
func (w *Watcher) Watching(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, ok := w.projects[id]
	return ok && !p.dead.Load()
}

// Close stops everything and waits for all goroutines. After it returns,
// OnChange is never called again. Idempotent.
func (w *Watcher) Close() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	w.cancel()
	w.wg.Wait()
}

// run is the goroutine of one project: it reads the source, debounces, and
// delivers. It returns only after the source channel is closed.
func (w *Watcher) run(p *watched, ch <-chan []Change) {
	defer close(p.done)

	// The timer is idle until the first relevant change.
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var (
		pending bool
		first   time.Time // arrival of the first undelivered change
	)

	for {
		select {
		case <-p.ctx.Done():
			drain(ch) // the source may block on send until it sees the cancellation
			return

		case batch, ok := <-ch:
			if !ok {
				if p.ctx.Err() != nil {
					return
				}
				// The stream died by itself: tell the layer above once, so it
				// can refresh and decide what to do. Dead is set first so that
				// Watching is already false inside OnChange.
				p.dead.Store(true)
				w.deliver(p)
				return
			}
			if !w.anyRelevant(p, batch) {
				continue
			}
			now := time.Now()
			if !pending {
				pending, first = true, now
			}
			// Quiet period after this change, capped so that a constant stream
			// of changes cannot postpone the refresh forever.
			deadline := now.Add(w.opts.Debounce)
			if limit := first.Add(w.opts.MaxDelay); deadline.After(limit) {
				deadline = limit
			}
			timer.Reset(time.Until(deadline))

		case <-timer.C:
			pending = false
			// While OnChange runs, this goroutine does not read the source, so
			// newer changes wait in the channel and start a new window after it.
			w.deliver(p)
		}
	}
}

// deliver calls OnChange unless the project was removed or the watcher closed
// in the meantime; the check happens under cbMu, so no call starts after
// Remove or Close has cancelled the context and returned.
func (w *Watcher) deliver(p *watched) {
	w.cbMu.Lock()
	defer w.cbMu.Unlock()
	if p.ctx.Err() != nil {
		return
	}
	w.opts.OnChange(p.id)
}

func drain(ch <-chan []Change) {
	for range ch {
	}
}

func (w *Watcher) anyRelevant(p *watched, batch []Change) bool {
	for _, c := range batch {
		if relevant(p.realRoot, c) {
			return true
		}
	}
	return false
}

// relevant reports whether a change can alter what the tree shows. It mirrors
// the rules of Scan: hidden entries, "~$" lock files and unsupported file
// kinds never appear in the tree.
func relevant(realRoot string, c Change) bool {
	if c.Rescan {
		return true
	}
	rel, err := filepath.Rel(realRoot, c.Path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if rel == "." {
		// An event for the root directory itself carries no tree change (its
		// children report their own events), and FSEvents emits one for the
		// root's own creation just before a stream starts. A moved or deleted
		// root arrives as Rescan.
		return false
	}
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	name := filepath.Base(rel)
	if strings.HasPrefix(name, "~$") {
		return false
	}
	if c.Dir {
		return true
	}
	_, ok := KindOf(name)
	return ok
}
