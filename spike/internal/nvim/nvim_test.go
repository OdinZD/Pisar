package nvim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestRedraw(t *testing.T) {
	p, out := newProc(t, nil)
	// Let the startup screen finish, or its output would pass for the redraw's.
	var last int
	stable := 0
	eventually(t, "idle TUI", func() bool {
		n := len(out.bytes())
		if n > 0 && n == last {
			stable++
		} else {
			stable = 0
		}
		last = n
		return stable >= 10 // 10 polls x testPoll without new output
	})
	before := len(out.bytes())
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	if err := p.Redraw(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "output after Redraw", func() bool { return len(out.bytes()) > before })
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
		// What xterm.js 6.0.0 really sent at startup with no key pressed
		// (captured from the trace of scripts/smoke.sh), in its three writes.
		"\x1b[?69;0$y",
		"\x1b[?2026;2$y\x1b[?2027;0$y\x1b[?2031;0$y\x1b[?2048;0$y\x1bP1$r0m\x1b\\\x1b[?1;2c" +
			"\x1b]11;rgb:ffff/ffff/ffff\x1b\\\x1b[0n\x1b[?1;2c",
		"\x1b[I", // focus in
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

// fakeClipboardLua is the "fake clipboard" example of :help g:clipboard: the
// provider keeps [lines, regtype] in g:foo and never starts a process.
const fakeClipboardLua = `
	vim.api.nvim_exec2([[
		let g:clipboard = {
		\   'name': 'pisar-fake',
		\   'copy': {
		\      '+': {lines, regtype -> extend(g:, {'foo': [lines, regtype]}) },
		\      '*': {lines, regtype -> extend(g:, {'foo': [lines, regtype]}) },
		\    },
		\   'paste': {
		\      '+': {-> get(g:, 'foo', [])},
		\      '*': {-> get(g:, 'foo', [])},
		\   },
		\ }
	]], {})
	return vim.fn.has("clipboard")`

// clipProc is a Process whose clipboard provider is the fake one.
type clipProc struct {
	*Process
	pbMarker string // created by the stub pbcopy/pbpaste if either is ever run
}

// newClipProc starts Neovim with a fake clipboard. Two layers keep the real
// system clipboard out of reach: g:clipboard is set before any provider use
// (providers load on the first has('clipboard') or register access), and
// pbcopy/pbpaste on PATH are stubs that only record that they were called.
func newClipProc(t *testing.T) *clipProc {
	t.Helper()
	bin := t.TempDir()
	marker := filepath.Join(bin, "pb-called")
	stub := "#!/bin/sh\necho called >> '" + marker + "'\ncat >/dev/null\n"
	for _, name := range []string{"pbcopy", "pbpaste"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(stub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := newProc(t, func(o *Options) {
		o.Env = append(o.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	})
	cp := &clipProc{Process: p, pbMarker: marker}

	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	var has int
	if err := p.execLua(ctx, "fake clipboard", &has, fakeClipboardLua); err != nil {
		t.Fatalf("set g:clipboard: %v", err)
	}
	if has != 1 {
		t.Fatalf("has('clipboard') = %d, want 1", has)
	}
	var name string
	if err := p.execLua(ctx, "provider", &name, `return vim.fn["provider#clipboard#Executable"]()`); err != nil {
		t.Fatal(err)
	}
	if name != "pisar-fake" {
		t.Fatalf("clipboard provider = %q, want the fake one", name)
	}
	t.Cleanup(func() {
		if _, err := os.Stat(marker); err == nil {
			t.Error("pbcopy or pbpaste was run: the fake clipboard did not cover everything")
		}
	})
	return cp
}

// fakeClip is the content of the fake clipboard; nil lines means untouched.
type fakeClip struct {
	Lines   []string
	Regtype string
}

func (cp *clipProc) clip(t *testing.T) *fakeClip {
	t.Helper()
	var raw string
	err := cp.execLua(context.Background(), "read fake clipboard", &raw, `
		if vim.g.foo == nil then return "null" end
		return vim.json.encode(vim.g.foo)`)
	if err != nil {
		t.Fatal(err)
	}
	var pair []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &pair); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	if pair == nil {
		return nil
	}
	if len(pair) != 2 {
		t.Fatalf("fake clipboard = %s, want [lines, regtype]", raw)
	}
	var c fakeClip
	if err := json.Unmarshal(pair[0], &c.Lines); err != nil {
		t.Fatalf("decode lines of %q: %v", raw, err)
	}
	if err := json.Unmarshal(pair[1], &c.Regtype); err != nil {
		t.Fatalf("decode regtype of %q: %v", raw, err)
	}
	return &c
}

func (p *Process) mode(t *testing.T) string {
	t.Helper()
	var m string
	if err := p.execLua(context.Background(), "mode", &m, `return vim.api.nvim_get_mode().mode`); err != nil {
		t.Fatal(err)
	}
	return m
}

// setBuffer replaces the current buffer's text and puts the cursor at 1,0.
func (p *Process) setBuffer(t *testing.T, lines ...string) {
	t.Helper()
	err := p.execLua(context.Background(), "set buffer", nil, `
		vim.api.nvim_buf_set_lines(0, 0, -1, false, ...)
		vim.api.nvim_win_set_cursor(0, { 1, 0 })`, lines)
	if err != nil {
		t.Fatal(err)
	}
}

// typeKeys sends keys through the PTY and waits until the mode is wantMode and
// the cursor is at wantPos (row 1-based, byte column), which proves Neovim
// has processed all of them: keys travel asynchronously from the RPC calls.
func (p *Process) typeKeys(t *testing.T, keys, wantMode string, wantPos [2]int) {
	t.Helper()
	if _, err := p.Write([]byte(keys)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "keys processed", func() bool {
		var st struct {
			Mode string
			Row  int
			Col  int
		}
		err := p.execLua(context.Background(), "state", &st, `
			local pos = vim.api.nvim_win_get_cursor(0)
			return { Mode = vim.api.nvim_get_mode().mode, Row = pos[1], Col = pos[2] }`)
		return err == nil && st.Mode == wantMode && st.Row == wantPos[0] && st.Col == wantPos[1]
	})
}

func TestCopyVisual(t *testing.T) {
	// Text with Croatian letters (two bytes each) so the selection is cut by
	// characters, not bytes.
	text := []string{"čćšžđ abc", "def ghi", "jkl mno"}
	// What the provider's copy function receives (:help clipboard): a linewise
	// yank carries a trailing empty line, and a block's regtype is just "b".
	tests := []struct {
		name    string
		keys    string
		pos     [2]int // cursor after the keys: row, byte column
		mode    string // visual mode that must be active before CopyVisual
		want    []string
		regtype string
	}{
		{"char", "vlll", [2]int{1, 6}, "v", []string{"čćšž"}, "v"},
		{"line", "Vj", [2]int{2, 0}, "V", []string{"čćšžđ abc", "def ghi", ""}, "V"},
		{"block", "\x16jll", [2]int{2, 2}, "\x16", []string{"čćš", "def", ""}, "b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newClipProc(t)
			p.setBuffer(t, text...)
			p.typeKeys(t, tc.keys, tc.mode, tc.pos)

			ctx, cancel := context.WithTimeout(context.Background(), testWait)
			defer cancel()
			ok, err := p.CopyVisual(ctx)
			if err != nil || !ok {
				t.Fatalf("CopyVisual = %v, %v; want true, nil", ok, err)
			}
			if m := p.mode(t); m != "n" {
				t.Errorf("mode after copy = %q, want n", m)
			}
			got := p.clip(t)
			if got == nil {
				t.Fatal("fake clipboard was not written")
			}
			if strings.Join(got.Lines, "\n") != strings.Join(tc.want, "\n") || got.Regtype != tc.regtype {
				t.Errorf("clipboard = %q %q, want %q %q", got.Lines, got.Regtype, tc.want, tc.regtype)
			}
		})
	}
}

func TestCopyNone(t *testing.T) {
	tests := []struct {
		name string
		keys string
		mode string
		pos  [2]int
	}{
		{"norm", "", "n", [2]int{1, 0}},
		{"ins", "i", "i", [2]int{1, 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newClipProc(t)
			p.setBuffer(t, "some text")
			if tc.keys != "" {
				p.typeKeys(t, tc.keys, tc.mode, tc.pos)
			}
			ctx, cancel := context.WithTimeout(context.Background(), testWait)
			defer cancel()
			ok, err := p.CopyVisual(ctx)
			if err != nil || ok {
				t.Fatalf("CopyVisual = %v, %v; want false, nil", ok, err)
			}
			if m := p.mode(t); m != tc.mode {
				t.Errorf("mode = %q, want %q", m, tc.mode)
			}
			if got := p.clip(t); got != nil {
				t.Errorf("fake clipboard = %+v, want untouched", got)
			}
		})
	}
}

func TestSaveUnnamed(t *testing.T) {
	tests := []struct {
		name string
		keys string
		mode string
	}{
		{"normal", "", "n"},
		{"insert", "i", "i"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProc(t, nil)
			p.setBuffer(t, "x")
			if tc.keys != "" {
				p.typeKeys(t, tc.keys, tc.mode, [2]int{1, 0})
			}
			ctx, cancel := context.WithTimeout(context.Background(), testWait)
			defer cancel()
			err := p.Save(ctx)
			if err == nil || !strings.Contains(err.Error(), "E32") {
				t.Fatalf("Save = %v, want an E32 error", err)
			}
			var msgs string
			if err := p.execLua(ctx, "messages", &msgs, `
				return vim.api.nvim_exec2("messages", { output = true }).output`); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(msgs, "E32") {
				t.Errorf(":messages = %q, want it to contain E32", msgs)
			}
			if m := p.mode(t); m != tc.mode {
				t.Errorf("mode = %q, want %q", m, tc.mode)
			}
		})
	}
}

func TestSaveModes(t *testing.T) {
	tests := []struct {
		name string
		keys string
		mode string
	}{
		{"insert", "i", "i"},
		{"visual", "v", "v"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProc(t, nil)
			path := filepath.Join(t.TempDir(), "m.md")
			if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), testWait)
			defer cancel()
			if err := p.OpenFile(ctx, path); err != nil {
				t.Fatal(err)
			}
			p.setBuffer(t, "nov\u00e1 linija")
			p.typeKeys(t, tc.keys, tc.mode, [2]int{1, 0})
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
			if string(data) != "nov\u00e1 linija\n" {
				t.Errorf("file = %q", data)
			}
			if m := p.mode(t); m != tc.mode {
				t.Errorf("mode = %q, want %q", m, tc.mode)
			}
		})
	}
}

// TestDarwinProvider checks which provider Neovim picks on its own. Neither
// call below runs pbcopy or pbpaste: the provider script only maps names to
// commands, and has('clipboard') only asks whether a provider exists.
func TestDarwinProvider(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	if _, err := exec.LookPath("pbcopy"); err != nil {
		t.Skip("pbcopy not found in PATH")
	}
	p, _ := newProc(t, nil)
	var res struct {
		Has      int
		Provider string
		Custom   bool
	}
	err := p.execLua(context.Background(), "provider", &res, `
		return {
			Custom = vim.g.clipboard ~= nil,
			Has = vim.fn.has("clipboard"),
			Provider = vim.fn["provider#clipboard#Executable"](),
		}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Custom {
		t.Error("g:clipboard is set; the test must see Neovim's own choice")
	}
	if res.Has != 1 || res.Provider != "pbcopy" {
		t.Errorf("has('clipboard') = %d, provider = %q; want 1 and pbcopy", res.Has, res.Provider)
	}
}
