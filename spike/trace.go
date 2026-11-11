package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// Environment variables that switch on spike diagnostics and pick the renderer.
const (
	envTrace    = "PISAR_SPIKE_TRACE"
	envRenderer = "PISAR_SPIKE_RENDERER"
)

// tracer appends one line per event to a file. It is a diagnostic for the
// spike: with no file configured every method is a no-op, and a failed write
// is logged, never fatal. The zero value and a nil *tracer are both disabled.
type tracer struct {
	mu sync.Mutex // serializes writes, so lines never interleave
	f  *os.File   // nil when disabled or after close
}

// newTracer opens path for appending. An empty path returns a disabled tracer
// and opens nothing.
func newTracer(path string) (*tracer, error) {
	if path == "" {
		return &tracer{}, nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return &tracer{}, fmt.Errorf("open trace file: %w", err)
	}
	return &tracer{f: f}, nil
}

// lineBreaks keeps one event on one line, whatever the frontend sends.
var lineBreaks = strings.NewReplacer("\r", `\r`, "\n", `\n`)

// logf writes "<RFC3339Nano> <kind> <detail>".
func (t *tracer) logf(kind, format string, args ...any) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f == nil {
		return
	}
	detail := lineBreaks.Replace(fmt.Sprintf(format, args...))
	if _, err := fmt.Fprintf(t.f, "%s %s %s\n", time.Now().Format(time.RFC3339Nano), kind, detail); err != nil {
		log.Printf("trace: write: %v", err)
	}
}

// close flushes and closes the file; later logf calls do nothing.
func (t *tracer) close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f == nil {
		return nil
	}
	err := t.f.Close()
	t.f = nil
	if err != nil {
		return fmt.Errorf("close trace file: %w", err)
	}
	return nil
}

// errText renders an error for a trace line: "<nil>" for none.
func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// normalizeRenderer maps the environment value to one of the two renderers
// the frontend knows. Anything but "webgl" is the DOM renderer.
func normalizeRenderer(v string) string {
	if v == "webgl" {
		return "webgl"
	}
	return "dom"
}
