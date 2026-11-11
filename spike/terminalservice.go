package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"Pisar/spike/internal/term"
)

// callTimeout bounds every blocking call from the frontend (starting Neovim,
// opening and saving a file). It is the only place this limit is defined.
const callTimeout = 10 * time.Second

// TerminalService is the Wails-facing surface of a term.Session. Wails binds
// its exported methods, so the frontend can call them; it adds nothing else.
type TerminalService struct {
	session *term.Session

	trace    *tracer // may be nil or disabled
	renderer string  // "dom" or "webgl"

	mu          sync.Mutex
	initialFile string // opened once, after the first successful Start

	// setClipboard puts text on the system clipboard and reports success. The
	// Wails clipboard needs the application, which is created after the
	// service, so main sets it through useClipboard.
	setClipboard func(text string) bool
}

// NewTerminalService wraps session. initialFile, if not empty, is opened
// after the first successful Start. renderer is the frontend's starting
// renderer (see normalizeRenderer); tr may be nil.
func NewTerminalService(session *term.Session, initialFile, renderer string, tr *tracer) *TerminalService {
	return &TerminalService{
		session:     session,
		initialFile: initialFile,
		renderer:    normalizeRenderer(renderer),
		trace:       tr,
	}
}

// Start launches Neovim at the given size, or repaints it if it already runs.
// Wails passes ctx and cancels it with the request, and callTimeout bounds it.
func (t *TerminalService) Start(ctx context.Context, cols, rows int) (err error) {
	// Logged after the initial file is open, so a trace line with err=<nil>
	// means Neovim is up and showing the file.
	defer func() { t.trace.logf("start", "cols=%d rows=%d err=%s", cols, rows, errText(err)) }()
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := t.session.Start(ctx, cols, rows); err != nil {
		return err
	}

	t.mu.Lock()
	path := t.initialFile
	t.initialFile = ""
	t.mu.Unlock()
	if path == "" {
		return nil
	}
	if err := t.session.OpenFile(ctx, path); err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	return nil
}

// Write sends keystrokes, already encoded by the terminal, to Neovim.
func (t *TerminalService) Write(data string) error {
	t.trace.logf("write", "%q", data)
	return t.session.Write(data)
}

// Resize tells Neovim the terminal size changed.
func (t *TerminalService) Resize(cols, rows int) error {
	t.trace.logf("resize", "cols=%d rows=%d", cols, rows)
	return t.session.Resize(cols, rows)
}

// Renderer reports which xterm.js renderer to start with: "dom" or "webgl".
// It comes from the environment variable PISAR_SPIKE_RENDERER (default "dom";
// any other value is treated as "dom").
func (t *TerminalService) Renderer() string { return t.renderer }

// Trace appends one line from the frontend to the trace file. No-op when tracing is off.
func (t *TerminalService) Trace(msg string) { t.trace.logf("ui", "%s", msg) }

// closeTrace writes the final traffic counters and closes the trace file. Call
// it after the session is closed, so the counters are final.
func (t *TerminalService) closeTrace() error {
	st := t.session.Stats()
	t.trace.logf("stats", "bytesOut=%d chunks=%d largest=%d bytesIn=%d",
		st.BytesOut, st.Chunks, st.LargestChunk, st.BytesIn)
	return t.trace.close()
}

// OpenFile edits the file at path.
func (t *TerminalService) OpenFile(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return t.session.OpenFile(ctx, path)
}

// Save writes the current buffer to its file.
func (t *TerminalService) Save(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return t.session.Save(ctx)
}

// useClipboard sets the function CopyText uses. Unexported, so Wails does not bind it.
func (t *TerminalService) useClipboard(set func(text string) bool) {
	t.mu.Lock()
	t.setClipboard = set
	t.mu.Unlock()
}

// CopyText puts text on the system clipboard (used for the terminal's own selection).
// Empty text leaves the clipboard alone.
func (t *TerminalService) CopyText(text string) error {
	if text == "" {
		return nil
	}
	t.mu.Lock()
	set := t.setClipboard
	t.mu.Unlock()
	if set == nil {
		return errors.New("copy text: no clipboard available")
	}
	if !set(text) {
		return errors.New("copy text: the system clipboard refused the text")
	}
	return nil
}

// Stats returns the output and input byte counters.
func (t *TerminalService) Stats() term.Stats { return t.session.Stats() }
