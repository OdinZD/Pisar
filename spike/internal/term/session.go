// Package term connects a Neovim process to a frontend: PTY output becomes
// events (grouped, never cut inside a UTF-8 character), keystrokes and size
// changes flow the other way. It does not depend on the UI toolkit; the
// caller supplies an Emitter.
package term

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"Pisar/spike/internal/nvim"
)

// Event names shared with the frontend.
const (
	EventData  = "pty:data"   // payload string: a chunk of terminal output, always valid UTF-8 on its own
	EventSaved = "nvim:saved" // payload string: absolute path of the saved file
	EventExit  = "nvim:exit"  // payload string: always ""
	EventCopy  = "ui:copy"    // payload string: always ""; the frontend should copy the terminal's own selection
)

// maxDim is the largest terminal size the PTY's 16-bit winsize can hold.
const maxDim = 1<<16 - 1

// releaseTimeout bounds releasing the PTY of a process that already exited.
const releaseTimeout = 5 * time.Second

// Emitter delivers one event to the frontend. Defined here, at the consumer.
type Emitter func(name string, data string)

// Config configures a Session.
type Config struct {
	Nvim          nvim.Options  // Cols and Rows are set by Start; the rest is used as given
	FlushInterval time.Duration // longest a pending byte waits before it is emitted; 0 = package default
	MaxChunk      int           // pending bytes that force an immediate flush; 0 = package default
}

// Stats counts traffic since the Session was created.
type Stats struct {
	BytesOut     uint64 // terminal output bytes emitted in EventData payloads
	Chunks       uint64 // EventData events emitted
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

	// emitMu serializes calls to emit (one goroutine at a time, so EventData
	// stay in order) and guards stats and muted.
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

// NewSession returns a Session that has not started Neovim yet.
func NewSession(cfg Config, emit Emitter) *Session {
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = defaultFlushInterval
	}
	if cfg.MaxChunk <= 0 {
		cfg.MaxChunk = defaultMaxChunk
	}
	return &Session{cfg: cfg, emit: emit}
}

// Start launches Neovim with the given size and starts forwarding. If Neovim
// already runs (the frontend reloaded), it only resizes it and asks for a full
// repaint, so a fresh terminal gets the current screen.
func (s *Session) Start(ctx context.Context, cols, rows int) error {
	if cols < 1 || cols > maxDim || rows < 1 || rows > maxDim {
		return fmt.Errorf("term: invalid size %dx%d (each side must be 1..%d)", cols, rows, maxDim)
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
			// Exited, but its supervisor has not cleared it yet. Wait for that,
			// so the old EventExit cannot arrive after the new process' output.
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

// forwardOutput ends when the process is gone and its output is drained.
func (s *Session) forwardOutput(r *run) {
	err := pump(r.p, s.cfg.FlushInterval, s.cfg.MaxChunk, func(chunk string) {
		s.send(EventData, chunk)
	})
	if err != nil {
		slog.Warn("term: read pty", "err", err)
	}
	close(r.outDone)
}

func (s *Session) forwardEvents(r *run) {
	for ev := range r.p.Events() { // closed when the process is gone
		if ev.Kind == nvim.EventSaved {
			s.send(EventSaved, ev.Path)
		}
	}
	close(r.evDone)
}

// supervise reports the end of a process that quit on its own. It must run
// after all output was emitted, which makes EventExit the last event.
func (s *Session) supervise(r *run) {
	defer close(r.finished)
	<-r.outDone
	<-r.evDone

	// The process has exited; Close only releases the PTY.
	ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
	defer cancel()
	if err := r.p.Close(ctx); err != nil {
		slog.Warn("term: release exited nvim", "err", err)
	}

	s.mu.Lock()
	if s.run == r {
		s.run = nil // clear before the event, so a Start after EventExit launches anew
	}
	s.mu.Unlock()
	s.send(EventExit, "")
}

// send delivers one event unless the Session is closed.
func (s *Session) send(name, data string) {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	if s.muted {
		return
	}
	if name == EventData {
		s.stats.BytesOut += uint64(len(data))
		s.stats.Chunks++
		s.stats.LargestChunk = max(s.stats.LargestChunk, len(data))
	}
	s.emit(name, data)
}

// current returns the running process, or an error that wraps ErrClosed or
// nvim.ErrNotRunning.
func (s *Session) current() (*nvim.Process, error) {
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
	p, err := s.current()
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
	if cols < 1 || cols > maxDim || rows < 1 || rows > maxDim {
		return fmt.Errorf("term: invalid size %dx%d (each side must be 1..%d)", cols, rows, maxDim)
	}
	p, err := s.current()
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
	p, err := s.current()
	if err != nil {
		return err
	}
	return p.OpenFile(ctx, path)
}

// Save writes the current buffer to its file.
func (s *Session) Save(ctx context.Context) error {
	p, err := s.current()
	if err != nil {
		return err
	}
	return p.Save(ctx)
}

// CopyVisual yanks Neovim's visual selection into the system clipboard. It
// reports false when Neovim is not in a visual mode.
func (s *Session) CopyVisual(ctx context.Context) (bool, error) {
	p, err := s.current()
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
// goroutines. After it returns emit is never called again, even if stopping
// failed. It does not emit EventExit. It is idempotent.
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

	// Taking emitMu waits for an emit in progress; after that none can start.
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
