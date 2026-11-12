// Package term connects a Neovim process to a frontend: PTY output becomes
// events (grouped, never cut inside a UTF-8 character), keystrokes and size
// changes flow the other way, and Neovim's notifications are passed on. It
// does not depend on the UI toolkit; the caller supplies an Emitter.
package term

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"Pisar/internal/nvim"
)

// maxDim is the largest terminal size the PTY's 16-bit winsize can hold.
const maxDim = 1<<16 - 1

// Emitter receives everything a Session has to tell the layer above. It is
// called from one goroutine at a time; calls arrive in order. Implementations
// must not call Session.Close (or wait for something that does) from inside a
// method: Close waits for a call in progress to return.
//
// The order between terminal output (Data) and Neovim's notifications (Saved,
// Active, Modified, FocusTree, Changed, TogglePreview, OpenSearch) is not defined, because they travel on two
// separate channels. Exited is always the last call for a process.
type Emitter interface {
	Data(chunk string)                   // terminal output; always valid UTF-8 on its own
	Saved(path string)                   // a buffer was written
	Active(path string, modified bool)   // another buffer became current ("" = no file)
	Modified(path string, modified bool) // the 'modified' state of a buffer changed
	FocusTree()                          // <leader>e
	Changed()                            // text of the current buffer changed or another buffer became current (only while the preview is on)
	TogglePreview()                      // <leader>p
	OpenSearch()                         // <leader>f
	Exited()                             // Neovim quit on its own; all output has been delivered
}

// Config configures a Session.
type Config struct {
	Nvim          nvim.Options  // Cols and Rows are set by Start; the rest is used as given
	FlushInterval time.Duration // longest a pending byte waits before it is emitted; required (> 0)
	MaxChunk      int           // pending bytes that force an immediate flush; required (> 0)
}

// Stats counts traffic since the Session was created.
type Stats struct {
	BytesOut     uint64 // terminal output bytes emitted through Data
	Chunks       uint64 // Data calls
	LargestChunk int    // bytes
	BytesIn      uint64 // bytes written to the PTY by Write
}

// ErrClosed is returned by every method after Close.
var ErrClosed = errors.New("term: session is closed")

// Session owns at most one Neovim process at a time and forwards its output
// and notifications to the Emitter. All methods are safe for concurrent use.
type Session struct {
	cfg  Config
	emit Emitter

	mu     sync.Mutex // guards run and closed; Start holds it while launching
	run    *run
	closed bool

	closeMu sync.Mutex // serializes Close, so a second Close waits for the first

	// emitMu serializes calls to emit (one goroutine at a time, so Data stays
	// in order) and guards stats and muted.
	emitMu sync.Mutex
	stats  Stats
	muted  bool // set by Close: nothing is emitted from then on

	bytesIn atomic.Uint64
}

// run is one Neovim process and the goroutines that serve it.
type run struct {
	p        *nvim.Process
	wg       sync.WaitGroup // output pump, event forwarder, supervisor
	outDone  chan struct{}  // closed when all output has been emitted
	evDone   chan struct{}  // closed when the event stream has ended
	finished chan struct{}  // closed by the supervisor as its last step
}

// NewSession returns a Session that has not started Neovim yet. The defaults
// of FlushInterval and MaxChunk live in internal/config, so zero is an error.
func NewSession(cfg Config, emit Emitter) (*Session, error) {
	if cfg.FlushInterval <= 0 {
		return nil, fmt.Errorf("term: invalid flush interval %v", cfg.FlushInterval)
	}
	if cfg.MaxChunk <= 0 {
		return nil, fmt.Errorf("term: invalid max chunk %d", cfg.MaxChunk)
	}
	if emit == nil {
		return nil, errors.New("term: emitter is required")
	}
	return &Session{cfg: cfg, emit: emit}, nil
}

func checkSize(cols, rows int) error {
	if cols < 1 || cols > maxDim || rows < 1 || rows > maxDim {
		return fmt.Errorf("term: invalid size %dx%d (each side must be 1..%d)", cols, rows, maxDim)
	}
	return nil
}

// Start launches Neovim with the given size and starts forwarding. If Neovim
// already runs (the frontend reloaded), it only resizes it and asks for a full
// repaint, so a fresh terminal gets the current screen.
func (s *Session) Start(ctx context.Context, cols, rows int) error {
	if err := checkSize(cols, rows); err != nil {
		return err
	}
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return ErrClosed
		}
		r := s.run
		if r == nil {
			err := s.launch(ctx, cols, rows)
			s.mu.Unlock()
			return err
		}
		select {
		case <-r.p.Done():
			// Exited, but its supervisor has not finished yet. Wait for that,
			// so the old Exited cannot arrive after the new process' output.
			s.mu.Unlock()
			select {
			case <-r.finished:
			case <-ctx.Done():
				return fmt.Errorf("term: start: %w", ctx.Err())
			}
			continue
		default:
		}
		s.mu.Unlock()
		if err := r.p.Resize(uint16(cols), uint16(rows)); err != nil {
			return fmt.Errorf("term: start: %w", err)
		}
		if err := r.p.Redraw(ctx); err != nil {
			return fmt.Errorf("term: start: %w", err)
		}
		return nil
	}
}

// launch starts a new process. s.mu must be held.
func (s *Session) launch(ctx context.Context, cols, rows int) error {
	opts := s.cfg.Nvim
	opts.Cols, opts.Rows = uint16(cols), uint16(rows) // range checked by Start
	p, err := nvim.Start(ctx, opts)
	if err != nil {
		return fmt.Errorf("term: start nvim: %w", err)
	}
	r := &run{
		p:        p,
		outDone:  make(chan struct{}),
		evDone:   make(chan struct{}),
		finished: make(chan struct{}),
	}
	s.run = r
	r.wg.Add(3)
	go func() {
		defer r.wg.Done()
		s.forwardOutput(r)
	}()
	go func() {
		defer r.wg.Done()
		s.forwardEvents(r)
	}()
	go func() {
		defer r.wg.Done()
		s.supervise(r)
	}()
	return nil
}

// Running reports whether a Neovim process is running right now.
func (s *Session) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.run == nil {
		return false
	}
	select {
	case <-s.run.p.Done():
		return false
	default:
		return true
	}
}

// deliver calls f with the emitter unless the Session is closed. Calls are
// serialized, which is what keeps the Emitter single-threaded.
func (s *Session) deliver(f func(Emitter)) {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	if s.muted {
		return
	}
	f(s.emit)
}

// forwardOutput ends when the process is gone and its output is drained.
func (s *Session) forwardOutput(r *run) {
	err := pump(r.p, s.cfg.FlushInterval, s.cfg.MaxChunk, func(chunk string) {
		s.deliver(func(e Emitter) {
			// Counted under emitMu, together with the call, so Stats and the
			// order of Data agree.
			s.stats.BytesOut += uint64(len(chunk))
			s.stats.Chunks++
			s.stats.LargestChunk = max(s.stats.LargestChunk, len(chunk))
			e.Data(chunk)
		})
	})
	if err != nil {
		slog.Warn("term: read pty", "err", err)
	}
	close(r.outDone)
}

// forwardEvents passes Neovim's notifications on, in the order they arrived.
// The range ends when nvim closes the stream, which it does after every
// notification sent before the process quit (see nvim.Process.Events).
func (s *Session) forwardEvents(r *run) {
	for ev := range r.p.Events() {
		switch ev.Kind {
		case nvim.EventSaved:
			s.deliver(func(e Emitter) { e.Saved(ev.Path) })
		case nvim.EventBufferEntered:
			s.deliver(func(e Emitter) { e.Active(ev.Path, ev.Modified) })
		case nvim.EventModifiedChanged:
			s.deliver(func(e Emitter) { e.Modified(ev.Path, ev.Modified) })
		case nvim.EventFocusTree:
			s.deliver(func(e Emitter) { e.FocusTree() })
		case nvim.EventChanged:
			s.deliver(func(e Emitter) { e.Changed() })
		case nvim.EventTogglePreview:
			s.deliver(func(e Emitter) { e.TogglePreview() })
		case nvim.EventOpenSearch:
			s.deliver(func(e Emitter) { e.OpenSearch() })
		}
	}
	close(r.evDone)
}

// supervise reports the end of a process that quit on its own. It runs after
// all output and all events were emitted, which makes Exited the last call.
func (s *Session) supervise(r *run) {
	defer close(r.finished)
	<-r.outDone
	<-r.evDone

	// The process has exited; Close only releases the PTY.
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Nvim.StopTimeout)
	defer cancel()
	if err := r.p.Close(ctx); err != nil {
		slog.Warn("term: release exited nvim", "err", err)
	}

	s.mu.Lock()
	if s.run == r {
		s.run = nil // clear before the call, so a Start after Exited launches anew
	}
	s.mu.Unlock()
	s.deliver(func(e Emitter) { e.Exited() })
}

// proc returns the running process, or an error that wraps ErrClosed or
// nvim.ErrNotRunning.
func (s *Session) proc() (*nvim.Process, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	if s.run == nil {
		return nil, fmt.Errorf("term: no nvim process: %w", nvim.ErrNotRunning)
	}
	return s.run.p, nil
}

// Write sends keystrokes (already encoded by the terminal) to Neovim.
func (s *Session) Write(data string) error {
	p, err := s.proc()
	if err != nil {
		return err
	}
	n, err := p.Write([]byte(data))
	s.bytesIn.Add(uint64(n))
	if err != nil {
		return fmt.Errorf("term: write: %w", err)
	}
	return nil
}

// Resize tells Neovim the terminal size changed.
func (s *Session) Resize(cols, rows int) error {
	if err := checkSize(cols, rows); err != nil {
		return err
	}
	p, err := s.proc()
	if err != nil {
		return err
	}
	if err := p.Resize(uint16(cols), uint16(rows)); err != nil {
		return fmt.Errorf("term: %w", err)
	}
	return nil
}

// OpenFile edits path in Neovim's current window.
func (s *Session) OpenFile(ctx context.Context, path string) error {
	p, err := s.proc()
	if err != nil {
		return err
	}
	return p.OpenFile(ctx, path)
}

// Current returns the current buffer. Without a process it returns an empty
// Buffer and nil: there is nothing to report, and that is not a failure.
func (s *Session) Current(ctx context.Context) (nvim.Buffer, error) {
	p, err := s.proc()
	if errors.Is(err, nvim.ErrNotRunning) {
		return nvim.Buffer{}, nil
	}
	if err != nil {
		return nvim.Buffer{}, err
	}
	b, err := p.Current(ctx)
	if errors.Is(err, nvim.ErrNotRunning) {
		return nvim.Buffer{}, nil // quit between proc and the call
	}
	return b, err
}

// Modified returns the buffers with unsaved changes. Without a process it
// returns an empty list and nil: no Neovim means nothing unsaved, which is
// what the application's shutdown check needs to know.
func (s *Session) Modified(ctx context.Context) ([]nvim.Buffer, error) {
	p, err := s.proc()
	if errors.Is(err, nvim.ErrNotRunning) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	bufs, err := p.Modified(ctx)
	if errors.Is(err, nvim.ErrNotRunning) {
		return nil, nil // quit between proc and the call
	}
	return bufs, err
}

// Buffers returns the listed buffers that have a file. Without a process it
// returns an empty list and nil: no Neovim means no open documents.
func (s *Session) Buffers(ctx context.Context) ([]nvim.Buffer, error) {
	p, err := s.proc()
	if errors.Is(err, nvim.ErrNotRunning) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	bufs, err := p.Buffers(ctx)
	if errors.Is(err, nvim.ErrNotRunning) {
		return nil, nil // quit between proc and the call
	}
	return bufs, err
}

// Wipe removes the buffer that holds the file at path; see nvim.Process.Wipe.
// Without a process it reports false and nil: there is nothing to close.
func (s *Session) Wipe(ctx context.Context, path string) (bool, error) {
	p, err := s.proc()
	if errors.Is(err, nvim.ErrNotRunning) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	was, err := p.Wipe(ctx, path)
	if errors.Is(err, nvim.ErrNotRunning) {
		return false, nil // quit between proc and the call
	}
	return was, err
}

// SetReadOnly makes the buffer of the file at path read-only or writable; see
// nvim.Process.SetReadOnly.
func (s *Session) SetReadOnly(ctx context.Context, path string, readOnly bool) error {
	p, err := s.proc()
	if err != nil {
		return err
	}
	return p.SetReadOnly(ctx, path, readOnly)
}

// Reload reads the file at path again into its buffer, current or not; see
// nvim.Process.Reload.
func (s *Session) Reload(ctx context.Context, path string) error {
	p, err := s.proc()
	if err != nil {
		return err
	}
	return p.Reload(ctx, path)
}

// SetPreview switches Neovim's change notifications on or off; see
// nvim.Process.SetPreview. Without a process it fails with nvim.ErrNotRunning
// (wrapped), like the other methods that change Neovim.
func (s *Session) SetPreview(ctx context.Context, on bool) error {
	p, err := s.proc()
	if err != nil {
		return err
	}
	return p.SetPreview(ctx, on)
}

// BufferText returns the unsaved text of the current buffer and its
// description in one step; see nvim.Process.BufferText. Without a process it
// fails with nvim.ErrNotRunning (wrapped).
func (s *Session) BufferText(ctx context.Context, maxBytes int) ([]byte, nvim.Buffer, error) {
	p, err := s.proc()
	if err != nil {
		return nil, nvim.Buffer{}, err
	}
	return p.BufferText(ctx, maxBytes)
}

// Format runs a Markdown formatting action ("bold", "italic", "heading" or
// "list") on the current buffer; see nvim.Process.Format. An unknown kind fails
// with nvim.ErrUnknownFormat, no process with nvim.ErrNotRunning (both
// wrapped).
func (s *Session) Format(ctx context.Context, kind string) error {
	p, err := s.proc()
	if err != nil {
		return err
	}
	return p.Format(ctx, kind)
}

// Colorscheme reloads colors/pisar.lua in the running Neovim and repaints; see
// nvim.Process.Colorscheme, in particular what it returns while a prompt is
// open. Without a process it fails with nvim.ErrNotRunning (wrapped).
func (s *Session) Colorscheme(ctx context.Context) error {
	p, err := s.proc()
	if err != nil {
		return err
	}
	return p.Colorscheme(ctx)
}

// UserConfig reports what became of the user's own user.lua when the running
// Neovim started; see nvim.Process.UserConfig. Without a process it is empty.
func (s *Session) UserConfig() nvim.UserConfig {
	p, err := s.proc()
	if err != nil {
		return nvim.UserConfig{}
	}
	return p.UserConfig()
}

// Save writes the current buffer to its file.
func (s *Session) Save(ctx context.Context) error {
	p, err := s.proc()
	if err != nil {
		return err
	}
	return p.Save(ctx)
}

// SaveAll writes every modified buffer; see nvim.Process.SaveAll.
func (s *Session) SaveAll(ctx context.Context) error {
	p, err := s.proc()
	if err != nil {
		return err
	}
	return p.SaveAll(ctx)
}

// CopyVisual yanks Neovim's visual selection into the system clipboard. It
// reports false when Neovim is not in a visual mode.
func (s *Session) CopyVisual(ctx context.Context) (bool, error) {
	p, err := s.proc()
	if err != nil {
		return false, err
	}
	return p.CopyVisual(ctx)
}

// Stats returns the traffic counters.
func (s *Session) Stats() Stats {
	s.emitMu.Lock()
	st := s.stats
	s.emitMu.Unlock()
	st.BytesIn = s.bytesIn.Load()
	return st
}

// Close stops Neovim (discarding unsaved changes) and waits for the
// goroutines. After it returns the emitter is never called again, even if
// stopping failed. It does not call Exited. It is idempotent.
func (s *Session) Close(ctx context.Context) error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	r := s.run
	s.run = nil
	s.mu.Unlock()

	// Taking emitMu waits for a call in progress; after that none can start.
	s.emitMu.Lock()
	s.muted = true
	s.emitMu.Unlock()

	if r == nil {
		return nil
	}
	if err := r.p.Close(ctx); err != nil {
		// The process may still run, so its goroutines may never end: do not wait for them.
		return fmt.Errorf("term: close: %w", err)
	}
	r.wg.Wait()
	return nil
}
