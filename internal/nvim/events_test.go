package nvim

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// tempDir is t.TempDir() with symlinks resolved: Neovim reports /private/var/...
// where the OS temp dir says /var/..., and the tests compare paths exactly.
func tempDir(t *testing.T) string {
	t.Helper()
	return realPath(t, t.TempDir())
}

func mkFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	t.Cleanup(cancel)
	return ctx
}

// wantEvents reads len(want) events and requires exactly these, in this order.
func wantEvents(t *testing.T, p *Process, want ...Event) {
	t.Helper()
	for i, w := range want {
		if got := waitEvent(t, p); got != w {
			t.Fatalf("event #%d = %+v, want %+v", i, got, w)
		}
	}
}

// collectUntil reads events until done reports true for what was read so far.
func collectUntil(t *testing.T, p *Process, done func([]Event) bool) []Event {
	t.Helper()
	var got []Event
	for !done(got) {
		got = append(got, waitEvent(t, p))
	}
	return got
}

func hasEvent(evs []Event, want Event) bool { return slices.Contains(evs, want) }

func TestStartSize(t *testing.T) {
	if _, err := os.Stat(os.Getenv("HOME")); err != nil {
		t.Skip("no HOME")
	}
	valid := func(dir string) Options {
		return Options{Bin: "nvim", InitLua: "/nonexistent/init.lua", AppName: testAppTag, SocketDir: dir,
			Cols: 80, Rows: 24, StartTimeout: time.Second, StopTimeout: time.Second}
	}
	tests := []struct {
		name   string
		mutate func(*Options)
	}{
		{"cols", func(o *Options) { o.Cols = 0 }},
		{"rows", func(o *Options) { o.Rows = 0 }},
		{"start", func(o *Options) { o.StartTimeout = 0 }},
		{"stop", func(o *Options) { o.StopTimeout = 0 }},
		{"negstop", func(o *Options) { o.StopTimeout = -time.Second }},
		{"noinit", func(o *Options) { o.InitLua = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			o := valid(dir)
			tc.mutate(&o)
			p, err := Start(context.Background(), o)
			if err == nil {
				_ = p.Close(context.Background())
				t.Fatal("Start succeeded, want an error")
			}
			if nvimAlive(t, dir) {
				t.Error("a process was started although the options are invalid")
			}
		})
	}
}

func TestEntered(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "a\n")
	b := mkFile(t, dir, "b.md", "b\n")
	c := filepath.Join(dir, "c.md")

	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a})

	steps := []struct {
		keys string
		want []Event
	}{
		{":edit " + b + "\r", []Event{{Kind: EventBufferEntered, Path: b}}},
		{":bprevious\r", []Event{{Kind: EventBufferEntered, Path: a}}},
		{":enew\r", []Event{{Kind: EventBufferEntered, Path: ""}}},
		// :saveas renames the current buffer: it is announced under its new name.
		{":saveas " + c + "\r", []Event{{Kind: EventBufferEntered, Path: c}, {Kind: EventSaved, Path: c}}},
		{":help\r", []Event{{Kind: EventBufferEntered, Path: ""}}},
	}
	for _, s := range steps {
		if _, err := p.Write([]byte(s.keys)); err != nil {
			t.Fatal(err)
		}
		wantEvents(t, p, s.want...)
	}
}

func TestModEvents(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "a\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a})

	steps := []struct {
		keys string
		want []Event
	}{
		{"ix\x1b", []Event{{Kind: EventModifiedChanged, Path: a, Modified: true}}},
		{":w\r", []Event{{Kind: EventSaved, Path: a}, {Kind: EventModifiedChanged, Path: a}}},
		// Change, then undo back to the saved state.
		{"iy\x1b", []Event{{Kind: EventModifiedChanged, Path: a, Modified: true}}},
		{"u", []Event{{Kind: EventModifiedChanged, Path: a}}},
	}
	for _, s := range steps {
		if _, err := p.Write([]byte(s.keys)); err != nil {
			t.Fatal(err)
		}
		wantEvents(t, p, s.want...)
	}
}

// TestOrder types many edits and writes in one go; the events must come in the
// order things happened.
func TestOrder(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "a\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	const rounds = 30
	want := []Event{{Kind: EventBufferEntered, Path: a}}
	var keys strings.Builder
	for range rounds {
		keys.WriteString("ix\x1b:w\r")
		want = append(want,
			Event{Kind: EventModifiedChanged, Path: a, Modified: true},
			Event{Kind: EventSaved, Path: a},
			Event{Kind: EventModifiedChanged, Path: a})
	}
	if _, err := p.Write([]byte(keys.String())); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, want...)
}

func TestModifiedOpen(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "a\n")
	b := mkFile(t, dir, "b.md", "b\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	p.setBuffer(t, "changed")
	// 'hidden' lets the modified buffer stay loaded in the background.
	if err := p.OpenFile(ctx, b); err != nil {
		t.Fatalf("OpenFile with a modified current buffer: %v", err)
	}
	cur, err := p.Current(ctx)
	if err != nil || cur != (Buffer{Path: b}) {
		t.Fatalf("Current = %+v, %v; want %q unmodified", cur, err, b)
	}
	mod, err := p.Modified(ctx)
	if err != nil || !slices.Equal(mod, []Buffer{{Path: a, Modified: true}}) {
		t.Fatalf("Modified = %+v, %v; want only %q", mod, err, a)
	}
}

func TestModifiedList(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	mod, err := p.Modified(ctx)
	if err != nil || len(mod) != 0 {
		t.Fatalf("Modified at start = %+v, %v; want empty", mod, err)
	}

	p.setBuffer(t, "unnamed text")
	mod, err = p.Modified(ctx)
	if err != nil || !slices.Equal(mod, []Buffer{{Modified: true}}) {
		t.Fatalf("Modified = %+v, %v; want one unnamed buffer", mod, err)
	}

	if _, err := p.Write([]byte(":help\r")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the help buffer to be current", func() bool {
		var bt string
		err := p.execLua(ctx, "buftype", &bt, `return vim.bo.buftype`)
		return err == nil && bt == "help"
	})
	cur, err := p.Current(ctx)
	if err != nil || cur.Path != "" {
		t.Fatalf("Current in help = %+v, %v; want an empty path", cur, err)
	}
	mod, err = p.Modified(ctx)
	if err != nil || !slices.Equal(mod, []Buffer{{Modified: true}}) {
		t.Fatalf("Modified with help open = %+v, %v; want only the unnamed buffer", mod, err)
	}
}

func TestSaveAll(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "a\n")
	b := mkFile(t, dir, "b.md", "b\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	p.setBuffer(t, "new a")
	if err := p.OpenFile(ctx, b); err != nil { // a stays in the background
		t.Fatal(err)
	}
	p.setBuffer(t, "new b")
	// Nothing was written yet, so no saved event can be among these.
	collectUntil(t, p, func(evs []Event) bool {
		return hasEvent(evs, Event{Kind: EventModifiedChanged, Path: a, Modified: true}) &&
			hasEvent(evs, Event{Kind: EventModifiedChanged, Path: b, Modified: true})
	})

	if _, err := p.Write([]byte("i")); err != nil { // insert mode must survive SaveAll
		t.Fatal(err)
	}
	p.typeKeys(t, "", "i", [2]int{1, 0})
	if err := p.SaveAll(ctx); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	for path, want := range map[string]string{a: "new a\n", b: "new b\n"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
	evs := collectUntil(t, p, func(evs []Event) bool {
		return hasEvent(evs, Event{Kind: EventModifiedChanged, Path: a}) &&
			hasEvent(evs, Event{Kind: EventModifiedChanged, Path: b})
	})
	saved := 0
	for _, ev := range evs {
		switch ev.Kind {
		case EventSaved:
			saved++
		case EventBufferEntered:
			t.Errorf("SaveAll switched buffers: %+v", ev)
		}
	}
	if saved != 2 || !hasEvent(evs, Event{Kind: EventSaved, Path: a}) || !hasEvent(evs, Event{Kind: EventSaved, Path: b}) {
		t.Errorf("events = %+v, want one saved event per file", evs)
	}
	if cur, err := p.Current(ctx); err != nil || cur != (Buffer{Path: b}) {
		t.Errorf("Current = %+v, %v; want %q", cur, err, b)
	}
	if m := p.mode(t); m != "i" {
		t.Errorf("mode = %q, want i", m)
	}
	if mod, err := p.Modified(ctx); err != nil || len(mod) != 0 {
		t.Errorf("Modified after SaveAll = %+v, %v; want empty", mod, err)
	}
}

func TestSaveAllUnnamed(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "a\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	p.setBuffer(t, "new a")
	if err := p.execLua(ctx, "enew", nil, `vim.cmd.enew()`); err != nil {
		t.Fatal(err)
	}
	p.setBuffer(t, "scratch")

	err := p.SaveAll(ctx)
	if !errors.Is(err, ErrUnnamedBuffer) {
		t.Fatalf("SaveAll = %v, want ErrUnnamedBuffer", err)
	}
	if got, err := os.ReadFile(a); err != nil || string(got) != "new a\n" {
		t.Errorf("a.md = %q, %v; the named buffer must be written anyway", got, err)
	}
	mod, err := p.Modified(ctx)
	if err != nil || !slices.Equal(mod, []Buffer{{Modified: true}}) {
		t.Errorf("Modified = %+v, %v; want only the unnamed buffer", mod, err)
	}
	// The message on the screen is Pisar's own, in Croatian, not Neovim's E141.
	var msgs string
	if err := p.execLua(ctx, "messages", &msgs, `
		return vim.api.nvim_exec2("messages", { output = true }).output`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msgs, "Dokument bez naziva") || !strings.Contains(msgs, "nije spremljen") || strings.Contains(msgs, "E141") {
		t.Errorf("messages = %q, want the Croatian text about a buffer without a file name", msgs)
	}
}

func TestSaveAllRO(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "ro.md", "a\n")
	if err := os.Chmod(a, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(a, 0o600) })
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	p.setBuffer(t, "changed")

	err := p.SaveAll(ctx)
	if err == nil || errors.Is(err, ErrUnnamedBuffer) {
		t.Fatalf("SaveAll = %v, want a write error", err)
	}
	if got, rerr := os.ReadFile(a); rerr != nil || string(got) != "a\n" {
		t.Errorf("file = %q, %v; must be untouched", got, rerr)
	}
	var msgs string
	if err := p.execLua(ctx, "messages", &msgs, `
		return vim.api.nvim_exec2("messages", { output = true }).output`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msgs, "E45") {
		t.Errorf(":messages = %q, want the write error", msgs)
	}
}

func TestFocus(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "one\ntwo\nthree\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a})

	// Normal mode: the mapping notifies and does nothing else. Without it a
	// space would move the cursor.
	if _, err := p.Write([]byte(" e")); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, Event{Kind: EventFocusTree})
	p.typeKeys(t, "", "n", [2]int{1, 0})
	if cur, err := p.Current(ctx); err != nil || cur != (Buffer{Path: a}) {
		t.Errorf("Current = %+v, %v", cur, err)
	}

	// Insert mode: a space and "e" are text.
	if _, err := p.Write([]byte("i e")); err != nil {
		t.Fatal(err)
	}
	evs := collectUntil(t, p, func(evs []Event) bool {
		return hasEvent(evs, Event{Kind: EventModifiedChanged, Path: a, Modified: true})
	})
	if hasEvent(evs, Event{Kind: EventFocusTree}) {
		t.Error("focus event in insert mode")
	}
	var lines []string
	eventually(t, "text in buffer", func() bool {
		err := p.execLua(ctx, "lines", &lines, `return vim.api.nvim_buf_get_lines(0, 0, 1, false)`)
		return err == nil && len(lines) == 1 && lines[0] == " eone"
	})
}

func TestIsolation(t *testing.T) {
	var root string
	p, _ := newProc(t, func(o *Options) { root = o.SocketDir })
	var paths []string
	err := p.execLua(testCtx(t), "stdpath", &paths, `
		return { vim.fn.stdpath("config"), vim.fn.stdpath("data"), vim.fn.stdpath("state"), vim.fn.stdpath("cache") }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 {
		t.Fatalf("stdpath = %v", paths)
	}
	for _, path := range paths {
		if !strings.HasPrefix(path, root+string(filepath.Separator)) &&
			!strings.HasPrefix(path, realPath(t, root)+string(filepath.Separator)) {
			t.Errorf("%s is outside the test directory %s", path, root)
		}
	}
}

func TestInitClean(t *testing.T) {
	p, _ := newProc(t, nil)
	var st struct {
		Errmsg   string
		Messages string
		Hidden   bool
		TermGUI  bool
		Bg       string
		Leader   string
	}
	err := p.execLua(testCtx(t), "init state", &st, `
		return {
			Errmsg = vim.v.errmsg,
			Messages = vim.api.nvim_exec2("messages", { output = true }).output,
			Hidden = vim.o.hidden,
			TermGUI = vim.o.termguicolors,
			Bg = vim.o.background,
			Leader = vim.g.mapleader,
		}`)
	if err != nil {
		t.Fatal(err)
	}
	if st.Errmsg != "" {
		t.Errorf("v:errmsg = %q", st.Errmsg)
	}
	if regexp.MustCompile(`\bE\d+:`).MatchString(st.Messages) {
		t.Errorf(":messages has an error: %q", st.Messages)
	}
	if !st.Hidden || !st.TermGUI || st.Bg != "light" || st.Leader != " " {
		t.Errorf("hidden=%v termguicolors=%v background=%q leader=%q", st.Hidden, st.TermGUI, st.Bg, st.Leader)
	}
}
