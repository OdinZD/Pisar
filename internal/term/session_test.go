package term

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"Pisar/internal/nvim"
)

const (
	testWait = 15 * time.Second // generous: CI machines can be slow
	testPoll = 20 * time.Millisecond
	testSize = 80 // columns; rows are 24
	testRows = 24
)

// Test names are kept short on purpose: t.TempDir() embeds the test name, and
// the unix socket path inside it must fit sun_path (103 bytes on macOS).

// Names of the Emitter methods, as recorded.
const (
	evData          = "data"
	evSaved         = "saved"
	evActive        = "active"
	evModified      = "modified"
	evFocus         = "focus"
	evChanged       = "changed"
	evTogglePreview = "toggle-preview"
	evOpenSearch    = "open-search"
	evExited        = "exited"
)

// call is one recorded Emitter call. s is the chunk for Data and the path for
// Saved, Active and Modified; on is the boolean of Active and Modified.
type call struct {
	name string
	s    string
	on   bool
}

// recorder is the fake Emitter. It records calls in order and notices
// overlapping calls, which the Session promises never to make.
type recorder struct {
	mu      sync.Mutex
	calls   []call
	inEmit  bool
	overlap bool
}

func (r *recorder) record(c call) {
	r.mu.Lock()
	if r.inEmit {
		r.overlap = true
	}
	r.inEmit = true
	r.mu.Unlock()

	time.Sleep(50 * time.Microsecond) // widen the window for an overlap to show

	r.mu.Lock()
	r.inEmit = false
	r.calls = append(r.calls, c)
	r.mu.Unlock()
}

func (r *recorder) Data(chunk string)             { r.record(call{name: evData, s: chunk}) }
func (r *recorder) Saved(path string)             { r.record(call{name: evSaved, s: path}) }
func (r *recorder) Active(path string, on bool)   { r.record(call{name: evActive, s: path, on: on}) }
func (r *recorder) Modified(path string, on bool) { r.record(call{name: evModified, s: path, on: on}) }
func (r *recorder) FocusTree()                    { r.record(call{name: evFocus}) }
func (r *recorder) Changed()                      { r.record(call{name: evChanged}) }
func (r *recorder) TogglePreview()                { r.record(call{name: evTogglePreview}) }
func (r *recorder) OpenSearch()                   { r.record(call{name: evOpenSearch}) }
func (r *recorder) Exited()                       { r.record(call{name: evExited}) }

func (r *recorder) snapshot() []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]call(nil), r.calls...)
}

func (r *recorder) count(name string) int {
	n := 0
	for _, c := range r.snapshot() {
		if c.name == name {
			n++
		}
	}
	return n
}

// index returns the position of the first call of the given kind and path
// (compared as real paths) and state, or -1. Data is never matched by it.
func (r *recorder) index(t *testing.T, name, path string, on bool) int {
	t.Helper()
	for i, c := range r.snapshot() {
		if c.name == name && c.on == on && realPath(t, c.s) == realPath(t, path) {
			return i
		}
	}
	return -1
}

// newSession returns a Session whose Neovim is confined to t.TempDir().
func newSession(t *testing.T) (*Session, *recorder) {
	t.Helper()
	return newSessionInit(t, "")
}

// newSessionInit is newSession with extraLua appended to the shipped init.lua
// (through a wrapper file in the test's directory).
func newSessionInit(t *testing.T, extraLua string) (*Session, *recorder) {
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
	if extraLua != "" {
		wrapper := filepath.Join(root, "i.lua")
		code := "dofile(" + strconv.Quote(initLua) + ")\n" + extraLua
		if err := os.WriteFile(wrapper, []byte(code), 0o600); err != nil {
			t.Fatal(err)
		}
		initLua = wrapper
	}
	rec := &recorder{}
	s, err := NewSession(Config{
		Nvim: nvim.Options{
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
		},
		FlushInterval: testFlushInterval,
		MaxChunk:      testMaxChunk,
	}, rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testWait)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Errorf("cleanup Close: %v", err)
		}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if rec.overlap {
			t.Error("the emitter was called from two goroutines at once")
		}
	})
	return s, rec
}

// started is newSession plus a successful Start and the first output.
func started(t *testing.T) (*Session, *recorder, context.Context) {
	t.Helper()
	s, rec := newSession(t)
	ctx := testCtx(t)
	if err := s.Start(ctx, testSize, testRows); err != nil {
		t.Fatal(err)
	}
	eventually(t, "terminal output", func() bool { return rec.count(evData) > 0 })
	return s, rec, ctx
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
	if path == "" {
		return ""
	}
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// writeFile creates a file in a fresh t.TempDir().
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func pidOf(t *testing.T, s *Session) int {
	t.Helper()
	p, err := s.proc()
	if err != nil {
		t.Fatal(err)
	}
	return p.Pid()
}

func TestNewSession(t *testing.T) {
	good := Config{FlushInterval: time.Millisecond, MaxChunk: 1}
	tests := []struct {
		name string
		cfg  Config
		emit Emitter
		ok   bool
	}{
		{"valid", good, &recorder{}, true},
		{"zero interval", Config{MaxChunk: 1}, &recorder{}, false},
		{"negative interval", Config{FlushInterval: -1, MaxChunk: 1}, &recorder{}, false},
		{"zero max chunk", Config{FlushInterval: time.Millisecond}, &recorder{}, false},
		{"negative max chunk", Config{FlushInterval: time.Millisecond, MaxChunk: -1}, &recorder{}, false},
		{"nil emitter", good, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewSession(tc.cfg, tc.emit)
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v, want success: %v", err, tc.ok)
			}
			if tc.ok != (s != nil) {
				t.Errorf("session = %v with err = %v", s, err)
			}
		})
	}
}

func TestData(t *testing.T) {
	s, rec, _ := started(t)

	// Stats are updated together with the call, so they must agree with the
	// recorder once the output is quiet.
	eventually(t, "stats to match the calls", func() bool {
		var bytes, chunks, largest int
		for _, c := range rec.snapshot() {
			if c.name != evData {
				continue
			}
			bytes += len(c.s)
			chunks++
			largest = max(largest, len(c.s))
		}
		st := s.Stats()
		return st.BytesOut == uint64(bytes) && st.Chunks == uint64(chunks) && st.LargestChunk == largest
	})
	for i, c := range rec.snapshot() {
		if c.name == evData && !utf8.ValidString(c.s) {
			t.Errorf("payload %d is not valid UTF-8: %q", i, c.s)
		}
	}
	if st := s.Stats(); st.BytesIn != 0 {
		t.Errorf("BytesIn = %d before any Write", st.BytesIn)
	}
}

func TestSave(t *testing.T) {
	s, rec, ctx := started(t)
	path := writeFile(t, "doc.md", "old\n")
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
	eventually(t, "saved call", func() bool { return rec.index(t, evSaved, path, false) >= 0 })
	if got, want := s.Stats().BytesIn, uint64(len(keys)); got != want {
		t.Errorf("BytesIn = %d, want %d", got, want)
	}
}

func TestRestart(t *testing.T) {
	s, rec, ctx := started(t)
	pid := pidOf(t, s)
	before := rec.count(evData)

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
	if rec.count(evData) <= before {
		t.Error("no output after the second Start")
	}
}

// quit makes Neovim leave on its own and waits for the Exited call.
func quit(t *testing.T, s *Session, rec *recorder, cmd string, wantExits int) {
	t.Helper()
	if err := s.Write(cmd); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Exited", func() bool { return rec.count(evExited) >= wantExits })
}

func TestQuit(t *testing.T) {
	s, rec, ctx := started(t)
	pid := pidOf(t, s)

	quit(t, s, rec, ":qa!\r", 1)

	// Exactly one, and nothing after it. Give a stray second one time to show up.
	time.Sleep(300 * time.Millisecond)
	calls := rec.snapshot()
	if n := rec.count(evExited); n != 1 {
		t.Errorf("%d Exited calls, want 1", n)
	}
	if last := calls[len(calls)-1]; last.name != evExited {
		t.Errorf("last call = %+v, want Exited", last)
	}

	before := rec.count(evData)
	if err := s.Start(ctx, testSize, testRows); err != nil {
		t.Fatalf("Start after exit: %v", err)
	}
	if got := pidOf(t, s); got == pid {
		t.Errorf("same pid %d after restart", pid)
	}
	eventually(t, "output of the new process", func() bool { return rec.count(evData) > before })

	// The second process ends the same way: one more Exited, again the last call.
	quit(t, s, rec, ":qa!\r", 2)
	time.Sleep(300 * time.Millisecond)
	calls = rec.snapshot()
	if n := rec.count(evExited); n != 2 {
		t.Errorf("%d Exited calls, want 2", n)
	}
	if last := calls[len(calls)-1]; last.name != evExited {
		t.Errorf("last call = %+v, want Exited", last)
	}
}

func TestRunning(t *testing.T) {
	s, rec := newSession(t)
	ctx := testCtx(t)
	if s.Running() {
		t.Error("Running before Start")
	}
	if err := s.Start(ctx, testSize, testRows); err != nil {
		t.Fatal(err)
	}
	if !s.Running() {
		t.Error("not Running after Start")
	}
	quit(t, s, rec, ":qa!\r", 1)
	if s.Running() {
		t.Error("Running after Exited")
	}
	if err := s.Start(ctx, testSize, testRows); err != nil {
		t.Fatal(err)
	}
	if !s.Running() {
		t.Error("not Running after the second Start")
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Running() {
		t.Error("Running after Close")
	}
}

func TestActive(t *testing.T) {
	s, rec, ctx := started(t)
	a := writeFile(t, "a.md", "a\n")
	b := writeFile(t, "b.md", "b\n")

	if err := s.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Active(a, false)", func() bool { return rec.index(t, evActive, a, false) >= 0 })

	if err := s.Write(":edit " + b + "\r"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Active(b, false)", func() bool { return rec.index(t, evActive, b, false) >= 0 })
}

func TestModEvents(t *testing.T) {
	s, rec, ctx := started(t)
	a := writeFile(t, "a.md", "a\n")
	if err := s.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Write("Ox\x1b"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Modified(a, true)", func() bool { return rec.index(t, evModified, a, true) >= 0 })

	if err := s.Save(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Saved(a) and Modified(a, false)", func() bool {
		return rec.index(t, evSaved, a, false) >= 0 && rec.index(t, evModified, a, false) >= 0
	})
	// init.lua sends the saved notification first, the state change right after it.
	if saved, off := rec.index(t, evSaved, a, false), rec.index(t, evModified, a, false); saved > off {
		t.Errorf("Saved at %d after Modified(false) at %d", saved, off)
	}
}

func TestFocus(t *testing.T) {
	s, rec, _ := started(t)
	if err := s.Write(" e"); err != nil { // <leader>e in normal mode
		t.Fatal(err)
	}
	eventually(t, "FocusTree", func() bool { return rec.count(evFocus) > 0 })
	time.Sleep(300 * time.Millisecond)
	if n := rec.count(evFocus); n != 1 {
		t.Errorf("%d FocusTree calls, want 1", n)
	}
}

func TestPreview(t *testing.T) {
	s, rec, ctx := started(t)
	a := writeFile(t, "a.md", "one\n")
	if err := s.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Active(a, false)", func() bool { return rec.index(t, evActive, a, false) >= 0 })

	if err := s.Write(" p"); err != nil { // <leader>p in normal mode
		t.Fatal(err)
	}
	eventually(t, "TogglePreview", func() bool { return rec.count(evTogglePreview) > 0 })
	if err := s.Write(" f"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "OpenSearch", func() bool { return rec.count(evOpenSearch) > 0 })

	// Off: an edit gives no Changed.
	if err := s.Write("x"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Modified(a, true)", func() bool { return rec.index(t, evModified, a, true) >= 0 })
	time.Sleep(300 * time.Millisecond)
	if n := rec.count(evChanged); n != 0 {
		t.Fatalf("%d Changed calls with the preview off", n)
	}

	// On: the first one comes from SetPreview itself, the next from the edit.
	if err := s.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Changed after SetPreview", func() bool { return rec.count(evChanged) == 1 })
	if err := s.Write("x"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Changed after an edit", func() bool { return rec.count(evChanged) == 2 })
	text, buf, err := s.BufferText(ctx, 1<<20)
	if err != nil || string(text) != "e\n" || realPath(t, buf.Path) != realPath(t, a) || !buf.Modified {
		t.Errorf("BufferText = %q, %+v, %v; want the unsaved \"e\" of a", text, buf, err)
	}
	if _, _, err := s.BufferText(ctx, 1); !errors.Is(err, nvim.ErrBufferTooLarge) {
		t.Errorf("BufferText over the limit: %v, want ErrBufferTooLarge", err)
	}

	if err := s.SetPreview(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Write("x"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := rec.count(evChanged); n != 2 {
		t.Errorf("%d Changed calls in all, want 2", n)
	}
	if n, m := rec.count(evTogglePreview), rec.count(evOpenSearch); n != 1 || m != 1 {
		t.Errorf("TogglePreview %d, OpenSearch %d calls, want 1 each", n, m)
	}
}

func TestForward(t *testing.T) {
	s, _, ctx := started(t)
	a := writeFile(t, "a.md", "a\n")
	if err := s.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	cur, err := s.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if realPath(t, cur.Path) != realPath(t, a) || cur.Modified {
		t.Errorf("Current = %+v, want %s unmodified", cur, a)
	}

	if err := s.Write("Oq\x1b"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a in Modified", func() bool {
		bufs, err := s.Modified(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(bufs) == 1 && realPath(t, bufs[0].Path) == realPath(t, a) && bufs[0].Modified
	})
	if err := s.SaveAll(ctx); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	if bufs, err := s.Modified(ctx); err != nil || len(bufs) != 0 {
		t.Errorf("Modified after SaveAll = %v, %v; want none", bufs, err)
	}
	if data, err := os.ReadFile(a); err != nil || !strings.Contains(string(data), "q") {
		t.Errorf("file after SaveAll = %q, %v", data, err)
	}

	// A modified buffer without a file cannot be saved.
	if err := s.Write(":enew\rOz\x1b"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "ErrUnnamedBuffer", func() bool {
		err := s.SaveAll(ctx)
		return errors.Is(err, nvim.ErrUnnamedBuffer)
	})
}

func TestBuffers(t *testing.T) {
	s, rec, ctx := started(t)
	a := writeFile(t, "a.md", "a\n")
	b := writeFile(t, "b.md", "b\n")
	for _, f := range []string{a, b} {
		if err := s.OpenFile(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	bufs, err := s.Buffers(ctx)
	if err != nil || len(bufs) != 2 || realPath(t, bufs[0].Path) != realPath(t, a) || realPath(t, bufs[1].Path) != realPath(t, b) {
		t.Fatalf("Buffers = %+v, %v; want a, b", bufs, err)
	}

	eventually(t, "Active(b, false)", func() bool { return rec.index(t, evActive, b, false) >= 0 })
	was, err := s.Wipe(ctx, b)
	if err != nil || !was {
		t.Fatalf("Wipe(b) = %v, %v; want true, nil", was, err)
	}
	// The buffer that takes the place of b is announced after b was.
	eventually(t, "Active(a, false) after Wipe", func() bool {
		calls := rec.snapshot()
		for i := len(calls) - 1; i >= 0; i-- {
			if calls[i].name == evActive {
				return calls[i].s != "" && realPath(t, calls[i].s) == realPath(t, a)
			}
		}
		return false
	})
	bufs, err = s.Buffers(ctx)
	if err != nil || len(bufs) != 1 || realPath(t, bufs[0].Path) != realPath(t, a) {
		t.Fatalf("Buffers after Wipe = %+v, %v; want only a", bufs, err)
	}

	if err := s.Write("Oq\x1b"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a modified", func() bool {
		m, err := s.Modified(ctx)
		return err == nil && len(m) == 1
	})
	if _, err := s.Wipe(ctx, a); !errors.Is(err, nvim.ErrBufferModified) {
		t.Fatalf("Wipe of a modified buffer: %v, want ErrBufferModified", err)
	}
}

// SetReadOnly and Reload reach the buffer of a file that is not on screen.
func TestReadOnlyReload(t *testing.T) {
	s, _, ctx := started(t)
	a := writeFile(t, "a.md", "a\n")
	b := writeFile(t, "b.md", "b\n")
	for _, f := range []string{a, b} {
		if err := s.OpenFile(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetReadOnly(ctx, a, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("from outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReadOnly(ctx, a, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(ctx, "/nonexistent/x.md"); !errors.Is(err, nvim.ErrNoBuffer) {
		t.Errorf("Reload of an unknown file: %v, want ErrNoBuffer", err)
	}
}

func TestNoProc(t *testing.T) {
	check := func(t *testing.T, s *Session, ctx context.Context, want error) {
		t.Helper()
		errs := map[string]error{
			"Save":        s.Save(ctx),
			"SaveAll":     s.SaveAll(ctx),
			"OpenFile":    s.OpenFile(ctx, "x"),
			"Write":       s.Write("x"),
			"Resize":      s.Resize(80, 24),
			"SetReadOnly": s.SetReadOnly(ctx, "x", true),
			"Reload":      s.Reload(ctx, "x"),
			"SetPreview":  s.SetPreview(ctx, true),
			"Colorscheme": s.Colorscheme(ctx),
		}
		_, errs["CopyVisual"] = s.CopyVisual(ctx)
		_, _, errs["BufferText"] = s.BufferText(ctx, 100)
		for name, err := range errs {
			if !errors.Is(err, want) {
				t.Errorf("%s: %v, want %v", name, err, want)
			}
		}
	}
	empty := func(t *testing.T, s *Session, ctx context.Context) {
		t.Helper()
		if bufs, err := s.Modified(ctx); err != nil || len(bufs) != 0 {
			t.Errorf("Modified = %v, %v; want empty, nil", bufs, err)
		}
		if b, err := s.Current(ctx); err != nil || b != (nvim.Buffer{}) {
			t.Errorf("Current = %+v, %v; want empty, nil", b, err)
		}
		if bufs, err := s.Buffers(ctx); err != nil || len(bufs) != 0 {
			t.Errorf("Buffers = %v, %v; want empty, nil", bufs, err)
		}
		if was, err := s.Wipe(ctx, "/x/a.md"); err != nil || was {
			t.Errorf("Wipe = %v, %v; want false, nil", was, err)
		}
	}

	s, rec := newSession(t)
	ctx := testCtx(t)
	t.Run("before Start", func(t *testing.T) {
		check(t, s, ctx, nvim.ErrNotRunning)
		empty(t, s, ctx)
	})
	if err := s.Start(ctx, testSize, testRows); err != nil {
		t.Fatal(err)
	}
	quit(t, s, rec, ":qa!\r", 1)
	t.Run("after exit", func(t *testing.T) {
		check(t, s, ctx, nvim.ErrNotRunning)
		empty(t, s, ctx)
	})
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Run("after Close", func(t *testing.T) {
		check(t, s, ctx, ErrClosed)
		if _, err := s.Modified(ctx); !errors.Is(err, ErrClosed) {
			t.Errorf("Modified after Close: %v, want ErrClosed", err)
		}
		if _, err := s.Current(ctx); !errors.Is(err, ErrClosed) {
			t.Errorf("Current after Close: %v, want ErrClosed", err)
		}
		if _, err := s.Buffers(ctx); !errors.Is(err, ErrClosed) {
			t.Errorf("Buffers after Close: %v, want ErrClosed", err)
		}
		if _, err := s.Wipe(ctx, "/x/a.md"); !errors.Is(err, ErrClosed) {
			t.Errorf("Wipe after Close: %v, want ErrClosed", err)
		}
		if err := s.Start(ctx, testSize, testRows); !errors.Is(err, ErrClosed) {
			t.Errorf("Start after Close: %v, want ErrClosed", err)
		}
	})
}

func TestWQ(t *testing.T) {
	s, rec, ctx := started(t)
	a := writeFile(t, "a.md", "a\n")
	if err := s.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Write("Ow\x1b"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Modified(a, true)", func() bool { return rec.index(t, evModified, a, true) >= 0 })

	quit(t, s, rec, ":wq\r", 1)
	time.Sleep(300 * time.Millisecond)

	calls := rec.snapshot()
	if n := rec.count(evExited); n != 1 {
		t.Errorf("%d Exited calls, want 1", n)
	}
	last := len(calls) - 1
	if calls[last].name != evExited {
		t.Fatalf("last call = %+v, want Exited", calls[last])
	}
	if i := rec.index(t, evSaved, a, false); i < 0 || i > last {
		t.Errorf("Saved(a) at %d, want before Exited at %d", i, last)
	}
	if i := rec.index(t, evModified, a, false); i < 0 || i > last {
		t.Errorf("Modified(a, false) at %d, want before Exited at %d", i, last)
	}
	if data, err := os.ReadFile(a); err != nil || !strings.Contains(string(data), "w") {
		t.Errorf("file after :wq = %q, %v", data, err)
	}
}

func TestClose(t *testing.T) {
	s, rec, ctx := started(t)
	pid := pidOf(t, s)

	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("kill(pid, 0) = %v, want ESRCH", err)
	}
	n := len(rec.snapshot())
	time.Sleep(300 * time.Millisecond)
	if got := len(rec.snapshot()); got != n {
		t.Errorf("%d calls arrived after Close returned", got-n)
	}
	if rec.count(evExited) != 0 {
		t.Error("Close called Exited")
	}
	if err := s.Close(ctx); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestCloseIdle(t *testing.T) {
	s, _ := newSession(t)
	if err := s.Close(testCtx(t)); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(testCtx(t), testSize, testRows); !errors.Is(err, ErrClosed) {
		t.Errorf("Start after Close: %v, want ErrClosed", err)
	}
}

func TestCloseBusy(t *testing.T) {
	s, rec, ctx := started(t)
	// A loop that keeps repainting; sleep lets Neovim serve the quit request
	// between iterations, so Close does not have to escalate to signals.
	if err := s.Write(":for i in range(3000) | call setline(1, repeat('x', i % 70)) | redraw | sleep 2m | endfor\r"); err != nil {
		t.Fatal(err)
	}
	start := rec.count(evData)
	eventually(t, "output of the loop", func() bool { return rec.count(evData) > start+5 })

	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	n := len(rec.snapshot())
	time.Sleep(300 * time.Millisecond)
	if got := len(rec.snapshot()); got != n {
		t.Errorf("%d calls arrived after Close returned", got-n)
	}
	if rec.count(evExited) != 0 {
		t.Error("Close called Exited")
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
	if s.Running() {
		t.Error("a process exists after invalid sizes")
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Errorf("%d calls for a Session that never started", n)
	}
	if err := s.Resize(0, 10); err == nil {
		t.Error("Resize(0, 10) succeeded")
	}
}

func TestCopyVisual(t *testing.T) {
	// The fake clipboard writes the yanked lines to a file, so the test can read
	// them without RPC access to Neovim. It is set in init.lua, before any use of
	// the clipboard provider, so the system clipboard is never touched.
	clipFile := filepath.Join(t.TempDir(), "clip")
	s, _ := newSessionInit(t, `
		local file = `+strconv.Quote(clipFile)+`
		vim.g.clipboard = {
			name = "pisar-fake",
			copy = {
				["+"] = function(lines) vim.fn.writefile(lines, file) end,
				["*"] = function(lines) vim.fn.writefile(lines, file) end,
			},
			paste = {
				["+"] = function() return { {}, "v" } end,
				["*"] = function() return { {}, "v" } end,
			},
		}`)
	ctx := testCtx(t)
	if err := s.Start(ctx, testSize, testRows); err != nil {
		t.Fatal(err)
	}
	doc := writeFile(t, "d.md", "čaj\n")
	if err := s.OpenFile(ctx, doc); err != nil {
		t.Fatal(err)
	}

	ok, err := s.CopyVisual(ctx)
	if err != nil || ok {
		t.Fatalf("CopyVisual in normal mode = %v, %v; want false, nil", ok, err)
	}
	if _, err := os.Stat(clipFile); err == nil {
		t.Fatal("the fake clipboard was written outside visual mode")
	}

	// "v" selects the character under the cursor. Keys reach Neovim
	// asynchronously, so retry until the visual mode is on.
	if err := s.Write("v"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a visual selection", func() bool {
		ok, err := s.CopyVisual(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	})
	got, err := os.ReadFile(clipFile)
	if err != nil {
		t.Fatalf("fake clipboard not written: %v", err)
	}
	if string(got) != "č\n" {
		t.Errorf("clipboard = %q, want %q", got, "č\n")
	}

	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CopyVisual(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("CopyVisual after Close = %v, want ErrClosed", err)
	}
}

func TestConc(t *testing.T) {
	s, _, ctx := started(t)

	const workers, rounds = 8, 40
	var wg sync.WaitGroup
	fail := make(chan error, workers*rounds)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range rounds {
				var err error
				switch (w + i) % 5 {
				case 0:
					err = s.Write("\x1b")
				case 1:
					err = s.Resize(60+i%30, 20+i%10)
				case 2:
					_ = s.Stats()
				case 3:
					if !s.Running() {
						fail <- errors.New("not Running while Neovim runs")
					}
				case 4:
					_, err = s.Current(ctx)
				}
				if err != nil {
					fail <- err
				}
			}
		}()
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		t.Error(err)
	}
}

func TestFmt(t *testing.T) {
	idle, _ := newSession(t)
	if err := idle.Format(testCtx(t), "bold"); !errors.Is(err, nvim.ErrNotRunning) {
		t.Errorf("Format before Start: %v, want ErrNotRunning", err)
	}

	s, _, ctx := started(t)
	a := writeFile(t, "a.md", "jedna riječ\n")
	if err := s.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Format(ctx, "underline"); !errors.Is(err, nvim.ErrUnknownFormat) {
		t.Errorf("Format(underline): %v, want ErrUnknownFormat", err)
	}
	if err := s.Format(ctx, "list"); err != nil {
		t.Fatal(err)
	}
	if err := s.Format(ctx, "heading"); err != nil {
		t.Fatal(err)
	}
	text, _, err := s.BufferText(ctx, 1<<20)
	if err != nil || string(text) != "# jedna riječ\n" {
		t.Errorf("text after list and heading = %q, %v; want the heading", text, err)
	}
}

// Keymaps reaches the running Neovim and lists a mapping that has a description,
// and without a process it fails with ErrNotRunning.
func TestKeys(t *testing.T) {
	idle, _ := newSession(t)
	if _, err := idle.Keymaps(testCtx(t), 10, 10); !errors.Is(err, nvim.ErrNotRunning) {
		t.Errorf("Keymaps before Start: %v, want ErrNotRunning", err)
	}

	s, _ := newSessionInit(t, `vim.keymap.set("n", "<leader>q", function() end, { desc = "session test" })`)
	ctx := testCtx(t)
	if err := s.Start(ctx, testSize, testRows); err != nil {
		t.Fatal(err)
	}
	got, err := s.Keymaps(ctx, 50, 50)
	if err != nil {
		t.Fatal(err)
	}
	want := nvim.Keymap{Mode: "n", Keys: "<Space>q", Desc: "session test"}
	if !slices.Contains(got.Maps, want) {
		t.Errorf("Keymaps = %+v, want it to hold %+v", got.Maps, want)
	}
	if got.Groups == nil {
		t.Error("Groups is nil")
	}
	if _, err := s.Keymaps(ctx, 0, 50); err == nil {
		t.Error("Keymaps with maxMaps 0: no error")
	}
}

// Colorscheme reaches the running Neovim: ColorScheme autocmd records the
// 'background' the file set, once for each call. The file is a copy of the
// generator's output, changed from light to dark between the calls.
func TestScheme(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "colorscheme", name+".lua"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	rtp := t.TempDir()
	file := filepath.Join(rtp, "colors", "pisar.lua")
	if err := os.Mkdir(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	put := func(text string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(t.TempDir(), "seen")
	s, _ := newSessionInit(t, `
		vim.opt.runtimepath:prepend(`+strconv.Quote(rtp)+`)
		vim.api.nvim_create_autocmd("ColorScheme", {
			callback = function() vim.fn.writefile({ vim.o.background }, `+strconv.Quote(log)+`, "a") end,
		})`)
	ctx := testCtx(t)
	if err := s.Start(ctx, testSize, testRows); err != nil {
		t.Fatal(err)
	}

	put(read("pisar"))
	if err := s.Colorscheme(ctx); err != nil {
		t.Fatal(err)
	}
	put(read("dracula"))
	if err := s.Colorscheme(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "light\ndark\n" {
		t.Errorf("backgrounds seen = %q, want light then dark", got)
	}

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := s.Colorscheme(ctx); err == nil || errors.Is(err, nvim.ErrNotRunning) {
		t.Errorf("Colorscheme without the file = %v, want an error from Neovim", err)
	}
}
