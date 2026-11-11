package term

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"Pisar/spike/internal/nvim"
)

const (
	testWait = 15 * time.Second // generous: CI machines can be slow
	testPoll = 20 * time.Millisecond
	testSize = 80 // columns; rows are 24
)

// Test names are kept short on purpose: t.TempDir() embeds the test name, and
// the unix socket path inside it must fit sun_path (103 bytes on macOS).

type event struct{ name, data string }

// recorder is the fake Emitter. It also notices overlapping emit calls, which
// the Session promises never to make.
type recorder struct {
	mu      sync.Mutex
	events  []event
	inEmit  bool
	overlap bool
}

func (r *recorder) emit(name, data string) {
	r.mu.Lock()
	if r.inEmit {
		r.overlap = true
	}
	r.inEmit = true
	r.mu.Unlock()

	time.Sleep(50 * time.Microsecond) // widen the window for an overlap to show

	r.mu.Lock()
	r.inEmit = false
	r.events = append(r.events, event{name, data})
	r.mu.Unlock()
}

func (r *recorder) snapshot() []event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event(nil), r.events...)
}

func (r *recorder) count(name string) int {
	n := 0
	for _, e := range r.snapshot() {
		if e.name == name {
			n++
		}
	}
	return n
}

// newSession returns a Session whose Neovim is confined to t.TempDir().
func newSession(t *testing.T) (*Session, *recorder) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not found in PATH")
	}
	root := t.TempDir()
	mk := func(name string) string {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	initLua, err := filepath.Abs(filepath.Join("..", "..", "nvim", "init.lua"))
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	s := NewSession(Config{Nvim: nvim.Options{
		Bin:          "nvim",
		InitLua:      initLua,
		AppName:      "pisar-test",
		SocketDir:    root,
		StartTimeout: testWait,
		StopTimeout:  testWait,
		// Everything Neovim writes (config, shada, swap, logs, its temp dir) stays in root.
		Env: []string{
			"XDG_CONFIG_HOME=" + mk("c"),
			"XDG_DATA_HOME=" + mk("d"),
			"XDG_STATE_HOME=" + mk("s"),
			"XDG_CACHE_HOME=" + mk("k"),
			"TMPDIR=" + mk("t"),
		},
	}}, rec.emit)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testWait)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Errorf("cleanup Close: %v", err)
		}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if rec.overlap {
			t.Error("emit was called from two goroutines at once")
		}
	})
	return s, rec
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	t.Cleanup(cancel)
	return ctx
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(testPoll)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func pidOf(t *testing.T, s *Session) int {
	t.Helper()
	p, err := s.current()
	if err != nil {
		t.Fatal(err)
	}
	return p.Pid()
}

func TestData(t *testing.T) {
	s, rec := newSession(t)
	if err := s.Start(testCtx(t), testSize, 24); err != nil {
		t.Fatal(err)
	}
	eventually(t, "terminal output", func() bool { return rec.count(EventData) > 0 })

	// Stats are updated before emit is called, so they can lead the recorder
	// for a moment; they must agree once the output is quiet.
	eventually(t, "stats to match the events", func() bool {
		var bytes, chunks, largest int
		for _, e := range rec.snapshot() {
			if e.name != EventData {
				continue
			}
			bytes += len(e.data)
			chunks++
			largest = max(largest, len(e.data))
		}
		st := s.Stats()
		return st.BytesOut == uint64(bytes) && st.Chunks == uint64(chunks) && st.LargestChunk == largest
	})
	for i, e := range rec.snapshot() {
		if e.name == EventData && !utf8.ValidString(e.data) {
			t.Errorf("payload %d is not valid UTF-8: %q", i, e.data)
		}
	}
	if st := s.Stats(); st.BytesIn != 0 {
		t.Errorf("BytesIn = %d before any Write", st.BytesIn)
	}
}

func TestSave(t *testing.T) {
	s, rec := newSession(t)
	ctx := testCtx(t)
	if err := s.Start(ctx, testSize, 24); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	const keys = "Ođšžčć\x1b" // O opens a line above and enters insert mode
	if err := s.Write(keys); err != nil {
		t.Fatal(err)
	}
	// The keys are processed asynchronously, so save until the text is in the file.
	eventually(t, "typed text on disk", func() bool {
		if err := s.Save(ctx); err != nil {
			t.Fatalf("Save: %v", err)
		}
		data, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(data), "đšžčć")
	})
	eventually(t, "saved event", func() bool {
		for _, e := range rec.snapshot() {
			if e.name == EventSaved && realPath(t, e.data) == realPath(t, path) {
				return true
			}
		}
		return false
	})
	if got, want := s.Stats().BytesIn, uint64(len(keys)); got != want {
		t.Errorf("BytesIn = %d, want %d", got, want)
	}
}

func TestRestart(t *testing.T) {
	s, rec := newSession(t)
	ctx := testCtx(t)
	if err := s.Start(ctx, testSize, 24); err != nil {
		t.Fatal(err)
	}
	pid := pidOf(t, s)
	eventually(t, "terminal output", func() bool { return rec.count(EventData) > 0 })
	before := rec.count(EventData)

	if err := s.Start(ctx, 100, 30); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if got := pidOf(t, s); got != pid {
		t.Errorf("pid = %d after second Start, want %d", got, pid)
	}
	// Ask Neovim itself for its size, through the keyboard like a user would.
	// Resize reaches it as a signal, so repeat until it has caught up.
	sizeFile := filepath.Join(t.TempDir(), "size")
	eventually(t, "&columns=100 &lines=30", func() bool {
		cmd := ":call writefile([&columns . ' ' . &lines], '" + sizeFile + "')\r"
		if err := s.Write(cmd); err != nil {
			t.Fatal(err)
		}
		time.Sleep(testPoll)
		data, err := os.ReadFile(sizeFile)
		return err == nil && strings.TrimSpace(string(data)) == "100 30"
	})
	if rec.count(EventData) <= before {
		t.Error("no output after the second Start")
	}
}

func TestQuit(t *testing.T) {
	s, rec := newSession(t)
	ctx := testCtx(t)
	if err := s.Start(ctx, testSize, 24); err != nil {
		t.Fatal(err)
	}
	pid := pidOf(t, s)
	eventually(t, "terminal output", func() bool { return rec.count(EventData) > 0 })

	if err := s.Write(":qa!\r"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "exit event", func() bool { return rec.count(EventExit) > 0 })

	// Exactly one, and nothing after it. Give a stray second one time to show up.
	time.Sleep(300 * time.Millisecond)
	events := rec.snapshot()
	if n := rec.count(EventExit); n != 1 {
		t.Errorf("%d exit events, want 1", n)
	}
	if last := events[len(events)-1]; last.name != EventExit || last.data != "" {
		t.Errorf("last event = %+v, want an empty exit event", last)
	}

	if err := s.Write("x"); !errors.Is(err, nvim.ErrNotRunning) {
		t.Errorf("Write after exit: %v, want ErrNotRunning", err)
	}
	if err := s.Resize(80, 24); !errors.Is(err, nvim.ErrNotRunning) {
		t.Errorf("Resize after exit: %v, want ErrNotRunning", err)
	}
	if err := s.OpenFile(ctx, "x"); !errors.Is(err, nvim.ErrNotRunning) {
		t.Errorf("OpenFile after exit: %v, want ErrNotRunning", err)
	}
	if err := s.Save(ctx); !errors.Is(err, nvim.ErrNotRunning) {
		t.Errorf("Save after exit: %v, want ErrNotRunning", err)
	}

	before := rec.count(EventData)
	if err := s.Start(ctx, testSize, 24); err != nil {
		t.Fatalf("Start after exit: %v", err)
	}
	if got := pidOf(t, s); got == pid {
		t.Errorf("same pid %d after restart", pid)
	}
	eventually(t, "output of the new process", func() bool { return rec.count(EventData) > before })
}

func TestClose(t *testing.T) {
	s, rec := newSession(t)
	ctx := testCtx(t)
	if err := s.Start(ctx, testSize, 24); err != nil {
		t.Fatal(err)
	}
	pid := pidOf(t, s)
	eventually(t, "terminal output", func() bool { return rec.count(EventData) > 0 })

	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("kill(pid, 0) = %v, want ESRCH", err)
	}
	n := len(rec.snapshot())
	time.Sleep(300 * time.Millisecond)
	if got := len(rec.snapshot()); got != n {
		t.Errorf("%d events arrived after Close returned", got-n)
	}
	if rec.count(EventExit) != 0 {
		t.Error("Close emitted an exit event")
	}

	if err := s.Close(ctx); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := s.Start(ctx, testSize, 24); !errors.Is(err, ErrClosed) {
		t.Errorf("Start after Close: %v, want ErrClosed", err)
	}
	if err := s.Write("x"); !errors.Is(err, ErrClosed) {
		t.Errorf("Write after Close: %v, want ErrClosed", err)
	}
	if err := s.Resize(80, 24); !errors.Is(err, ErrClosed) {
		t.Errorf("Resize after Close: %v, want ErrClosed", err)
	}
	if err := s.OpenFile(ctx, "x"); !errors.Is(err, ErrClosed) {
		t.Errorf("OpenFile after Close: %v, want ErrClosed", err)
	}
	if err := s.Save(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("Save after Close: %v, want ErrClosed", err)
	}
}

func TestCloseIdle(t *testing.T) {
	s, _ := newSession(t)
	if err := s.Close(testCtx(t)); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(testCtx(t), testSize, 24); !errors.Is(err, ErrClosed) {
		t.Errorf("Start after Close: %v, want ErrClosed", err)
	}
}

func TestBadSize(t *testing.T) {
	s, rec := newSession(t)
	ctx := testCtx(t)
	for _, tc := range []struct{ cols, rows int }{{0, 10}, {80, 70000}, {70000, 24}, {-1, 24}, {80, 0}} {
		if err := s.Start(ctx, tc.cols, tc.rows); err == nil {
			t.Errorf("Start(%d, %d) succeeded", tc.cols, tc.rows)
		}
	}
	if _, err := s.current(); !errors.Is(err, nvim.ErrNotRunning) {
		t.Errorf("a process exists after invalid sizes: %v", err)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Errorf("%d events for a Session that never started", n)
	}
	if err := s.Resize(0, 10); err == nil {
		t.Error("Resize(0, 10) succeeded")
	}
}

func TestNotStarted(t *testing.T) {
	s, _ := newSession(t)
	if err := s.Write("x"); !errors.Is(err, nvim.ErrNotRunning) {
		t.Errorf("Write before Start: %v, want ErrNotRunning", err)
	}
}
