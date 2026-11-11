package nvim

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	testWait   = 15 * time.Second // generous: CI machines can be slow
	testPoll   = 20 * time.Millisecond
	testStop   = 500 * time.Millisecond
	testAppTag = "pisar-test"
)

// output collects everything read from the PTY. Reading is required: a TUI
// blocks once the PTY buffer is full.
type output struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	done chan struct{} // closed when Read returned an error
	err  error
}

func (o *output) bytes() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]byte(nil), o.buf.Bytes()...)
}

// Test names are kept short on purpose: t.TempDir() embeds the test name, and
// the socket path inside it must fit sun_path (103 bytes on macOS, where
// $TMPDIR alone is about 50).

// newProc starts Neovim confined to t.TempDir() and registers cleanup. The
// returned output is already being read in the background.
func newProc(t *testing.T, mutate func(*Options)) (*Process, *output) {
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
	opts := Options{
		Bin:          "nvim",
		InitLua:      initLua,
		AppName:      testAppTag,
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
	}
	if mutate != nil {
		mutate(&opts)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	p, err := Start(ctx, opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	out := &output{done: make(chan struct{})}
	go func() {
		defer close(out.done)
		chunk := make([]byte, 4096)
		for {
			n, err := p.Read(chunk)
			out.mu.Lock()
			out.buf.Write(chunk[:n])
			out.mu.Unlock()
			if err != nil {
				out.err = err
				return
			}
		}
	}()
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), testWait)
		defer ccancel()
		if err := p.Close(cctx); err != nil {
			t.Errorf("cleanup Close: %v", err)
		}
		// Close must also end the reader: Read has to report EOF by then.
		select {
		case <-out.done:
		case <-time.After(testWait):
			t.Error("PTY reader still blocked after Close")
		}
	})
	return p, out
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

func waitEvent(t *testing.T, p *Process) Event {
	t.Helper()
	select {
	case ev, ok := <-p.Events():
		if !ok {
			t.Fatal("Events closed while waiting for an event")
		}
		return ev
	case <-time.After(testWait):
		t.Fatal("timeout waiting for an event")
	}
	return Event{}
}

func waitDone(t *testing.T, p *Process) {
	t.Helper()
	select {
	case <-p.Done():
	case <-time.After(testWait):
		t.Fatal("timeout waiting for Done")
	}
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func processExists(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// nvimAlive reports whether any process still has needle on its command line.
// The socket path is unique per test, so it identifies the TUI and the
// server process Neovim spawns.
func nvimAlive(t *testing.T, needle string) bool {
	t.Helper()
	err := exec.Command("pgrep", "-f", needle).Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return true
	case errors.As(err, &ee) && ee.ExitCode() == 1:
		return false
	default:
		t.Fatalf("pgrep: %v", err)
		return false
	}
}

func TestOpen(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"plain", "notes.md"},
		{"space", "my notes.md"},
		{"diac", "Čćžšđ bilješke.md"},
		{"pct", "100% sure.md"},
		{"hash", "a#b.md"},
		{"pipe", "a|b.md"},
		{"mix", "%#|\"'$HOME [x] *.md"},
	}
	p, _ := newProc(t, nil)
	dir := t.TempDir()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), testWait)
			defer cancel()
			if err := p.OpenFile(ctx, path); err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			got, err := p.CurrentFile(ctx)
			if err != nil {
				t.Fatalf("CurrentFile: %v", err)
			}
			if realPath(t, got) != realPath(t, path) {
				t.Errorf("CurrentFile = %q, want %q", got, path)
			}
		})
	}
}

func TestUnnamed(t *testing.T) {
	p, _ := newProc(t, nil)
	got, err := p.CurrentFile(context.Background())
	if err != nil || got != "" {
		t.Fatalf("CurrentFile = %q, %v; want empty, nil", got, err)
	}
}

func TestSave(t *testing.T) {
	p, _ := newProc(t, nil)
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	if err := p.OpenFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	// Keystrokes go through the PTY like a user's: insert text, leave insert mode.
	if _, err := p.Write([]byte("Onew line\x1b")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "typed text in buffer", func() bool {
		var lines []string
		err := p.execLua(ctx, "lines", &lines, `return vim.api.nvim_buf_get_lines(0, 0, -1, false)`)
		return err == nil && len(lines) > 0 && lines[0] == "new line"
	})
	if err := p.Save(ctx); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ev := waitEvent(t, p)
	if ev.Kind != EventSaved || realPath(t, ev.Path) != realPath(t, path) {
		t.Errorf("event = %+v, want saved %q", ev, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new line\nold\n" {
		t.Errorf("file = %q", data)
	}
}

func TestColonW(t *testing.T) {
	p, _ := newProc(t, nil)
	path := filepath.Join(t.TempDir(), "w.md")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	if err := p.OpenFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte(":w\r")); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, p)
	if ev.Kind != EventSaved || realPath(t, ev.Path) != realPath(t, path) {
		t.Errorf("event = %+v, want saved %q", ev, path)
	}
}

func TestRead(t *testing.T) {
	_, out := newProc(t, nil)
	eventually(t, "TUI output", func() bool { return len(out.bytes()) > 0 })
}

func TestResize(t *testing.T) {
	p, _ := newProc(t, nil)
	if err := p.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	eventually(t, "&columns=100 &lines=30", func() bool {
		var got []int
		err := p.execLua(ctx, "size", &got, `return { vim.o.columns, vim.o.lines }`)
		return err == nil && len(got) == 2 && got[0] == 100 && got[1] == 30
	})
	if err := p.Resize(0, 10); err == nil {
		t.Error("Resize(0, 10) succeeded, want error")
	}
}

func TestSize(t *testing.T) {
	p, _ := newProc(t, func(o *Options) { o.Cols, o.Rows = 90, 28 })
	var got []int
	if err := p.execLua(context.Background(), "size", &got, `return { vim.o.columns, vim.o.lines }`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 90 || got[1] != 28 {
		t.Errorf("size = %v, want [90 28]", got)
	}
}

func TestClose(t *testing.T) {
	p, _ := newProc(t, nil)
	pid := p.Pid()
	sock := p.sock
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("socket should exist while running: %v", err)
	}
	if !nvimAlive(t, sock) {
		t.Fatal("pgrep does not see the running Neovim; the check below would prove nothing")
	}
	start := time.Now()
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	t.Logf("Close took %v", time.Since(start))
	if processExists(pid) {
		t.Errorf("pid %d still exists", pid)
	}
	if nvimAlive(t, sock) {
		t.Error("a process with the socket path on its command line is still alive")
	}
	select {
	case <-p.Done():
	default:
		t.Error("Done not closed after Close")
	}
	if _, err := os.Stat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket file still there: %v", err)
	}
	if _, ok := <-p.Events(); ok {
		t.Error("Events not closed after Close")
	}
	if err := p.Close(context.Background()); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	if got := p.stoppedBy.Load(); got != 1 {
		t.Errorf("stopped by stage %d, want 1 (RPC quit)", got)
	}
}

func TestExit(t *testing.T) {
	p, out := newProc(t, nil)
	pid := p.Pid()
	if _, err := p.Write([]byte(":qa!\r")); err != nil {
		t.Fatal(err)
	}
	waitDone(t, p)
	if processExists(pid) {
		t.Errorf("pid %d still exists", pid)
	}
	// Events closes on its own too.
	select {
	case _, ok := <-p.Events():
		if ok {
			t.Error("unexpected event")
		}
	case <-time.After(testWait):
		t.Fatal("Events not closed after Neovim exited")
	}
	ctx := context.Background()
	if err := p.OpenFile(ctx, filepath.Join(t.TempDir(), "x.md")); !errors.Is(err, ErrNotRunning) {
		t.Errorf("OpenFile = %v, want ErrNotRunning", err)
	}
	if _, err := p.CurrentFile(ctx); !errors.Is(err, ErrNotRunning) {
		t.Errorf("CurrentFile = %v, want ErrNotRunning", err)
	}
	if err := p.Save(ctx); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Save = %v, want ErrNotRunning", err)
	}
	if _, err := p.Write([]byte("x")); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Write = %v, want ErrNotRunning", err)
	}
	if err := p.Resize(80, 24); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Resize = %v, want ErrNotRunning", err)
	}
	// The reader sees the rest of the output, then io.EOF.
	select {
	case <-out.done:
		if !errors.Is(out.err, io.EOF) {
			t.Errorf("Read ended with %v, want io.EOF", out.err)
		}
	case <-time.After(testWait):
		t.Fatal("Read did not end after Neovim exited")
	}
	if err := p.Close(ctx); err != nil {
		t.Errorf("Close after self-exit = %v, want nil", err)
	}
	if got := p.stoppedBy.Load(); got != 0 {
		t.Errorf("stoppedBy = %d, want 0 (nobody stopped it)", got)
	}
}

func TestWQ(t *testing.T) {
	p, _ := newProc(t, nil)
	path := filepath.Join(t.TempDir(), "q.md")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	if err := p.OpenFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte(":wq\r")); err != nil {
		t.Fatal(err)
	}
	waitDone(t, p)
	// The consumer starts reading only after the process is gone: the save
	// must still be delivered before the channel closes.
	ev, ok := <-p.Events()
	if !ok || ev.Kind != EventSaved {
		t.Fatalf("got %+v, %v; want the save event", ev, ok)
	}
	if _, ok := <-p.Events(); ok {
		t.Error("Events should be closed after the save event")
	}
}

func TestHung(t *testing.T) {
	p, _ := newProc(t, func(o *Options) { o.StopTimeout = testStop })
	pid := p.Pid()
	sock := p.sock
	// A busy Lua loop blocks the server's event loop completely: it neither
	// answers RPC requests nor notices that the TUI is gone. (`:sleep` would
	// not do, Neovim keeps serving RPC during it.)
	if _, err := p.Write([]byte(":lua while true do end\r")); err != nil {
		t.Fatal(err)
	}
	// Prove it is hung: a request must stop completing.
	eventually(t, "the server to stop answering", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		return errors.Is(p.execLua(ctx, "probe", nil, `return 1`), context.DeadlineExceeded)
	})

	start := time.Now()
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	took := time.Since(start)
	got := p.stoppedBy.Load()
	t.Logf("Close took %v, stopped by stage %d", took, got)
	if took > 6*testStop {
		t.Errorf("Close took %v, want a few stages at most", took)
	}
	if processExists(pid) {
		t.Errorf("pid %d still exists", pid)
	}
	if nvimAlive(t, sock) {
		t.Error("server process survived Close")
	}
	if got < 2 {
		t.Errorf("stopped by stage %d, want >= 2 (signals)", got)
	}
}

func TestCloseCtx(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if processExists(p.Pid()) {
		t.Error("process survived Close with a cancelled context")
	}
}

func TestStartErr(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not found in PATH")
	}
	initLua, err := filepath.Abs(filepath.Join("..", "..", "nvim", "init.lua"))
	if err != nil {
		t.Fatal(err)
	}
	base := func(t *testing.T) Options {
		dir := t.TempDir()
		return Options{Bin: "nvim", InitLua: initLua, AppName: testAppTag, SocketDir: dir,
			Env: []string{
				"XDG_CONFIG_HOME=" + dir, "XDG_DATA_HOME=" + dir,
				"XDG_STATE_HOME=" + dir, "XDG_CACHE_HOME=" + dir, "TMPDIR=" + dir,
			}}
	}

	tests := []struct {
		name   string
		mutate func(t *testing.T, o *Options) context.Context
		is     error
	}{
		{"nobin", func(t *testing.T, o *Options) context.Context {
			o.Bin = "nvim-does-not-exist-pisar"
			return context.Background()
		}, exec.ErrNotFound},
		{"longpath", func(t *testing.T, o *Options) context.Context {
			o.SocketDir = filepath.Join(o.SocketDir, strings.Repeat("d", 100))
			if err := os.Mkdir(o.SocketDir, 0o700); err != nil {
				t.Fatal(err)
			}
			return context.Background()
		}, ErrSocketPathTooLong},
		{"cancel", func(t *testing.T, o *Options) context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}, context.Canceled},
		{"timeout", func(t *testing.T, o *Options) context.Context {
			// Too short for Neovim to come up: the spawned process must be cleaned up.
			o.StartTimeout = time.Millisecond
			return context.Background()
		}, context.DeadlineExceeded},
		{"midstart", func(t *testing.T, o *Options) context.Context {
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
			t.Cleanup(cancel)
			return ctx
		}, context.DeadlineExceeded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := base(t)
			ctx := tc.mutate(t, &o)
			start := time.Now()
			p, err := Start(ctx, o)
			if err == nil {
				_ = p.Close(context.Background())
				t.Fatal("Start succeeded, want an error")
			}
			if !errors.Is(err, tc.is) {
				t.Errorf("err = %v, want it to wrap %v", err, tc.is)
			}
			if took := time.Since(start); took > testWait {
				t.Errorf("Start took %v", took)
			}
			if nvimAlive(t, o.SocketDir) {
				t.Error("a Neovim process started by this test is still alive")
			}
		})
	}
}

func TestSlow(t *testing.T) {
	p, _ := newProc(t, nil)
	dir := t.TempDir()
	var files []string
	for _, n := range []string{"a.md", "b.md", "c.md"} {
		f := filepath.Join(dir, n)
		if err := os.WriteFile(f, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	saves := 3 * eventBuffer
	for i := 0; i < saves; i++ {
		f := files[i%len(files)]
		if err := p.OpenFile(ctx, f); err != nil {
			t.Fatalf("OpenFile #%d: %v", i, err)
		}
		if err := p.Save(ctx); err != nil {
			t.Fatalf("Save #%d: %v (nobody reads Events)", i, err)
		}
	}
	last := files[(saves-1)%len(files)]

	start := time.Now()
	if err := p.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if took := time.Since(start); took > testWait/2 {
		t.Errorf("Close took %v with an unread Events channel", took)
	}

	// Only the newest eventBuffer events were kept, the newest of all among them.
	var got []Event
	for ev := range p.Events() {
		got = append(got, ev)
	}
	if len(got) != eventBuffer {
		t.Fatalf("kept %d events, want %d", len(got), eventBuffer)
	}
	if realPath(t, got[len(got)-1].Path) != realPath(t, last) {
		t.Errorf("newest kept event = %q, want %q", got[len(got)-1].Path, last)
	}
}

func TestOptionsDefaults(t *testing.T) {
	got := Options{}.withDefaults()
	if got.Cols != defaultCols || got.Rows != defaultRows ||
		got.StartTimeout != defaultStartTimeout || got.StopTimeout != defaultStopTimeout {
		t.Errorf("defaults not applied: %+v", got)
	}
	set := Options{Cols: 1, Rows: 2, StartTimeout: 3, StopTimeout: 4}.withDefaults()
	if set.Cols != 1 || set.Rows != 2 || set.StartTimeout != 3 || set.StopTimeout != 4 {
		t.Errorf("explicit values overridden: %+v", set)
	}
}

// TestLateReplies covers the duplicate answers the pump makes possible: the
// pump answers Neovim's terminal queries itself, and a terminal emulator
// attached to the same PTY answers them again, later. Such late replies must
// be swallowed as terminal responses and never end up as keystrokes.
func TestLateReplies(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	for _, reply := range []string{
		"\x1b[0n",                          // DSR: terminal OK
		"\x1b[?1;2c",                       // primary device attributes
		"\x1b]11;rgb:ffff/ffff/ffff\x1b\\", // OSC 11: background colour
		"\x1b[?2026;2$y",                   // DECRPM: synchronized output
	} {
		if _, err := p.Write([]byte(reply)); err != nil {
			t.Fatal(err)
		}
	}
	// Typed after the replies: once it shows up, the replies were processed too.
	if _, err := p.Write([]byte("imarker\x1b")); err != nil {
		t.Fatal(err)
	}
	var lines []string
	eventually(t, "marker in buffer", func() bool {
		err := p.execLua(ctx, "lines", &lines, `return vim.api.nvim_buf_get_lines(0, 0, -1, false)`)
		return err == nil && len(lines) > 0 && strings.Contains(lines[0], "marker")
	})
	if len(lines) != 1 || lines[0] != "marker" {
		t.Errorf("buffer = %q, want only %q: a terminal reply leaked in as keystrokes", lines, "marker")
	}
	var mode string
	eventually(t, "normal mode", func() bool {
		err := p.execLua(ctx, "mode", &mode, `return vim.api.nvim_get_mode().mode`)
		return err == nil && mode == "n"
	})
}
