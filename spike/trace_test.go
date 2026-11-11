package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"Pisar/spike/internal/term"
)

// lineRe is the documented trace format: <RFC3339Nano> <kind> <detail>.
var lineRe = regexp.MustCompile(`^(\S+) (\w+) (.*)$`)

// readTrace returns the "<kind> <detail>" part of every line, checking the
// timestamp of each one on the way.
func readTrace(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		m := lineRe.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("line %q does not match <time> <kind> <detail>", line)
		}
		if _, err := time.Parse(time.RFC3339Nano, m[1]); err != nil {
			t.Fatalf("line %q: bad timestamp: %v", line, err)
		}
		out = append(out, m[2]+" "+m[3])
	}
	return out
}

func newTestService(t *testing.T, path string) *TerminalService {
	t.Helper()
	tr, err := newTracer(path)
	if err != nil {
		t.Fatalf("newTracer: %v", err)
	}
	session := term.NewSession(term.Config{}, func(string, string) {})
	return NewTerminalService(session, "", "dom", tr)
}

func TestTracerDisabledCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, "")

	svc.Trace("hello")
	if err := svc.Write("x"); err == nil {
		t.Error("Write with no process: got nil error, want one")
	}
	if err := svc.closeTrace(); err != nil {
		t.Errorf("closeTrace: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("directory has %d entries, want 0", len(entries))
	}
}

func TestTracerEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.log")
	svc := newTestService(t, path)

	// Escape sequences must stay visible, and a newline must not split a line.
	if err := svc.Write("\x1b[?62;4c\r"); err == nil {
		t.Error("Write with no process: got nil error, want one")
	}
	if err := svc.Resize(120, 30); err == nil {
		t.Error("Resize with no process: got nil error, want one")
	}
	if err := svc.Start(context.Background(), 0, 0); err == nil {
		t.Error("Start with an invalid size: got nil error, want one")
	}
	svc.Trace("cell 8x21\nsecond")
	if err := svc.closeTrace(); err != nil {
		t.Fatalf("closeTrace: %v", err)
	}
	// After close nothing more is written.
	svc.Trace("late")

	got := readTrace(t, path)
	want := []string{
		`write "\x1b[?62;4c\r"`,
		`resize cols=120 rows=30`,
		`start cols=0 rows=0 err=term: invalid size 0x0 (each side must be 1..65535)`,
		`ui cell 8x21\nsecond`,
		`stats bytesOut=0 chunks=0 largest=0 bytesIn=0`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

func TestTracerConcurrentWritesStayWholeLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.log")
	svc := newTestService(t, path)

	const workers, perWorker = 8, 50
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWorker {
				svc.Trace("0123456789012345678901234567890123456789")
			}
		}()
	}
	wg.Wait()
	if err := svc.closeTrace(); err != nil {
		t.Fatal(err)
	}

	got := readTrace(t, path) // fails on any malformed line
	if want := workers*perWorker + 1; len(got) != want {
		t.Errorf("got %d lines, want %d", len(got), want)
	}
}

func TestNewTracerOpenError(t *testing.T) {
	tr, err := newTracer(filepath.Join(t.TempDir(), "missing-dir", "trace.log"))
	if err == nil {
		t.Fatal("got nil error, want one")
	}
	if tr == nil {
		t.Fatal("tracer is nil; a disabled one is expected so callers need no check")
	}
	tr.logf("ui", "ignored") // must not panic
}

func TestRenderer(t *testing.T) {
	tests := []struct{ env, want string }{
		{"", "dom"},
		{"dom", "dom"},
		{"webgl", "webgl"},
		{"WEBGL", "dom"},
		{"canvas", "dom"},
	}
	for _, tt := range tests {
		svc := NewTerminalService(term.NewSession(term.Config{}, func(string, string) {}), "", tt.env, nil)
		if got := svc.Renderer(); got != tt.want {
			t.Errorf("Renderer() with %q = %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestErrText(t *testing.T) {
	if got := errText(nil); got != "<nil>" {
		t.Errorf("errText(nil) = %q", got)
	}
	if got := errText(errors.New("boom")); got != "boom" {
		t.Errorf("errText = %q", got)
	}
}
