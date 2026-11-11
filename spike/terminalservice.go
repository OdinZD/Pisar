package main

import (
	"context"
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

	mu          sync.Mutex
	initialFile string // opened once, after the first successful Start
}

// NewTerminalService wraps session. initialFile, if not empty, is opened
// after the first successful Start.
func NewTerminalService(session *term.Session, initialFile string) *TerminalService {
	return &TerminalService{session: session, initialFile: initialFile}
}

// Start launches Neovim at the given size, or repaints it if it already runs.
// Wails passes ctx and cancels it with the request, and callTimeout bounds it.
func (t *TerminalService) Start(ctx context.Context, cols, rows int) error {
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
func (t *TerminalService) Write(data string) error { return t.session.Write(data) }

// Resize tells Neovim the terminal size changed.
func (t *TerminalService) Resize(cols, rows int) error { return t.session.Resize(cols, rows) }

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

// Stats returns the output and input byte counters.
func (t *TerminalService) Stats() term.Stats { return t.session.Stats() }
