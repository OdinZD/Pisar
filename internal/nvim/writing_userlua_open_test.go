package nvim

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Tests of OpenUserLua in a real Neovim with the repository's own init.lua. The
// user's file lives in t.TempDir(). This file is named so that it sorts after
// writing_user_test.go: its tests start several Neovims, and the counter in the
// socket name must not grow for the tests of the files that come later (see the
// note in writing_test.go). The names of tests and subtests stay short for the
// same reason.

// openState is what the tests read about the current buffer.
type openState struct {
	Name     string
	Lines    []string
	Modified bool
	Row, Col int // cursor: row 1-based, byte column
}

func (p *Process) openState(t *testing.T) openState {
	t.Helper()
	var st openState
	p.lua(t, &st, `
		local pos = vim.api.nvim_win_get_cursor(0)
		return {
			Name = vim.api.nvim_buf_get_name(0),
			Lines = vim.api.nvim_buf_get_lines(0, 0, -1, false),
			Modified = vim.bo.modified,
			Row = pos[1], Col = pos[2],
		}`)
	return st
}

// reset throws away every buffer, with its changes, and clears the messages, so
// that the subtests of one process do not see each other's state.
func (p *Process) reset(t *testing.T) {
	t.Helper()
	p.lua(t, nil, `
		vim.cmd("silent! %bwipeout!")
		vim.cmd("messages clear")`)
}

func wantNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Lstat(%s) = %v, want ErrNotExist: opening must not create the file", path, err)
	}
}

func TestUserLuaOpen(t *testing.T) {
	tmpl := testTexts(t)["userLuaTemplate"]
	if tmpl == "" {
		t.Fatal("strings.json has no userLuaTemplate")
	}
	tmplLines := strings.Split(tmpl, "\n")
	p, _ := newProc(t, nil)

	t.Run("new", func(t *testing.T) {
		p.reset(t)
		path := filepath.Join(tempDir(t), "user.lua")
		if err := p.OpenUserLua(testCtx(t), path, tmpl); err != nil {
			t.Fatal(err)
		}
		st := p.openState(t)
		if st.Name != path {
			t.Errorf("buffer name = %q, want %q", st.Name, path)
		}
		if !slices.Equal(st.Lines, tmplLines) {
			t.Errorf("lines = %q, want the lines of the template", st.Lines)
		}
		if !st.Modified {
			t.Error("the buffer is not modified: a save would be skipped")
		}
		if st.Row != 1 || st.Col != 0 {
			t.Errorf("cursor = %d:%d, want 1:0", st.Row, st.Col)
		}
		wantNoFile(t, path)
		got := collectUntil(t, p, func(evs []Event) bool {
			return slices.ContainsFunc(evs, func(e Event) bool { return e.Kind == EventBufferEntered && e.Path == path })
		})
		if len(got) == 0 {
			t.Error("no events")
		}

		// The file comes into being with the user's save.
		if err := p.Save(testCtx(t)); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != tmpl+"\n" {
			t.Errorf("file after Save = %q, %v; want the template and a final newline", data, err)
		}
	})

	t.Run("exists", func(t *testing.T) {
		p.reset(t)
		path := mkFile(t, tempDir(t), "user.lua", "-- mine\nvim.g.x = 1\n")
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.OpenUserLua(testCtx(t), path, tmpl); err != nil {
			t.Fatal(err)
		}
		st := p.openState(t)
		if st.Name != path || !slices.Equal(st.Lines, []string{"-- mine", "vim.g.x = 1"}) || st.Modified {
			t.Errorf("buffer = %+v, want the unmodified file", st)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "-- mine\nvim.g.x = 1\n" || !after.ModTime().Equal(before.ModTime()) {
			t.Errorf("the file changed: %q, mtime %v -> %v", data, before.ModTime(), after.ModTime())
		}
	})

	t.Run("twice", func(t *testing.T) {
		p.reset(t)
		dir := tempDir(t)
		path := filepath.Join(dir, "user.lua")
		other := mkFile(t, dir, "other.md", "o\n")
		if err := p.OpenUserLua(testCtx(t), path, tmpl); err != nil {
			t.Fatal(err)
		}
		p.setBuffer(t, "-- moje")
		// While the buffer is the current one (a :edit would fail with E37).
		if err := p.OpenUserLua(testCtx(t), path, tmpl); err != nil {
			t.Fatalf("second open from the buffer itself: %v", err)
		}
		if st := p.openState(t); !slices.Equal(st.Lines, []string{"-- moje"}) || !st.Modified {
			t.Errorf("after the second open: %+v, want the user's change", st)
		}
		// And from another buffer, with the changed one in the background.
		if err := p.OpenFile(testCtx(t), other); err != nil {
			t.Fatal(err)
		}
		if err := p.OpenUserLua(testCtx(t), path, tmpl); err != nil {
			t.Fatalf("open from another buffer: %v", err)
		}
		if st := p.openState(t); st.Name != path || !slices.Equal(st.Lines, []string{"-- moje"}) || !st.Modified {
			t.Errorf("after the third open: %+v, want the user's change", st)
		}
		wantNoFile(t, path)
	})

	t.Run("dir", func(t *testing.T) {
		p.reset(t)
		keep := mkFile(t, tempDir(t), "keep.md", "k\n")
		if err := p.OpenFile(testCtx(t), keep); err != nil {
			t.Fatal(err)
		}
		before := p.openState(t)
		dir := tempDir(t)
		path := filepath.Join(dir, "user.lua")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		err := p.OpenUserLua(testCtx(t), path, tmpl)
		if !errors.Is(err, ErrNotRegularFile) {
			t.Fatalf("OpenUserLua(folder) = %v, want ErrNotRegularFile", err)
		}
		if st := p.openState(t); st.Name != before.Name || !slices.Equal(st.Lines, before.Lines) {
			t.Errorf("the current buffer changed: %+v, was %+v", st, before)
		}
		p.notBlocked(t)
		if entries, err := os.ReadDir(path); err != nil || len(entries) != 0 {
			t.Errorf("the folder holds %v, %v; want it empty", entries, err)
		}
		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
			t.Errorf("the parent holds %v, %v; want only the folder", entries, err)
		}
		// One line, which is the sentence of strings.json.
		want := testText(t)["nvimUserLuaNotFile"]
		if msgs := strings.TrimSpace(p.messages(t)); msgs != want || strings.Contains(msgs, "\n") {
			t.Errorf("messages = %q, want the one line %q", msgs, want)
		}
	})

	t.Run("fifo", func(t *testing.T) {
		p.reset(t)
		path := filepath.Join(tempDir(t), "user.lua")
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		err := p.OpenUserLua(testCtx(t), path, tmpl)
		if !errors.Is(err, ErrNotRegularFile) {
			t.Fatalf("OpenUserLua(FIFO) = %v, want ErrNotRegularFile", err)
		}
		if took := time.Since(start); took > testAnswer {
			t.Errorf("took %v: it waited for the FIFO", took)
		}
		p.answers(t)
		p.notBlocked(t)
	})

	t.Run("links", func(t *testing.T) {
		p.reset(t)
		dir := tempDir(t)
		target := mkFile(t, dir, "real.lua", "-- real\n")
		link := filepath.Join(dir, "link.lua")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := p.OpenUserLua(testCtx(t), link, tmpl); err != nil {
			t.Fatal(err)
		}
		if st := p.openState(t); !slices.Equal(st.Lines, []string{"-- real"}) || st.Modified {
			t.Errorf("link to a file: %+v, want the lines of the file, unmodified", st)
		}

		p.reset(t)
		gone := filepath.Join(dir, "gone.lua")
		dangling := filepath.Join(dir, "dangling.lua")
		if err := os.Symlink(gone, dangling); err != nil {
			t.Fatal(err)
		}
		if err := p.OpenUserLua(testCtx(t), dangling, tmpl); err != nil {
			t.Fatal(err)
		}
		if st := p.openState(t); !slices.Equal(st.Lines, tmplLines) || !st.Modified {
			t.Errorf("dangling link: %+v, want the template, modified", st)
		}
		wantNoFile(t, gone)
	})

	t.Run("prompt", func(t *testing.T) {
		p.reset(t)
		keep := mkFile(t, tempDir(t), "keep.md", "k\n")
		if err := p.OpenFile(testCtx(t), keep); err != nil {
			t.Fatal(err)
		}
		before := p.openState(t)
		path := filepath.Join(tempDir(t), "user.lua")
		p.write(t, ":echo \"one\\ntwo\"\r")
		eventually(t, "a hit-enter prompt", func() bool { return p.blocking(t) })

		start := time.Now()
		err := p.OpenUserLua(testCtx(t), path, tmpl)
		if !errors.Is(err, ErrPromptOpen) {
			t.Fatalf("OpenUserLua at a prompt = %v, want ErrPromptOpen", err)
		}
		if took := time.Since(start); took > testWait/4 {
			t.Errorf("the refusal took %v: it waited for the deadline", took)
		}
		p.write(t, "\r")
		eventually(t, "the prompt to close", func() bool { return !p.blocking(t) })
		if st := p.openState(t); st.Name != before.Name || !slices.Equal(st.Lines, before.Lines) {
			t.Errorf("the buffer changed: %+v, was %+v", st, before)
		}
		wantNoFile(t, path)
	})

	t.Run("chars", func(t *testing.T) {
		dir := tempDir(t)
		for _, name := range []string{"a b.lua", "50%.lua", "x#y.lua", "p|q.lua", "%#|.lua"} {
			p.reset(t)
			path := filepath.Join(dir, name)
			if err := p.OpenUserLua(testCtx(t), path, tmpl); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if st := p.openState(t); st.Name != path || !slices.Equal(st.Lines, tmplLines) {
				t.Errorf("%s: buffer %q with %d lines", name, st.Name, len(st.Lines))
			}
			wantNoFile(t, path)

			p.reset(t)
			mkFile(t, dir, name, "-- there\n")
			if err := p.OpenUserLua(testCtx(t), path, tmpl); err != nil {
				t.Fatalf("%s (existing): %v", name, err)
			}
			if st := p.openState(t); st.Name != path || !slices.Equal(st.Lines, []string{"-- there"}) {
				t.Errorf("%s (existing): buffer %q, lines %q", name, st.Name, st.Lines)
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 5 {
			t.Errorf("the folder holds %d entries (%v), want the 5 files made here", len(entries), err)
		}
	})

	t.Run("empty", func(t *testing.T) {
		p.reset(t)
		path := filepath.Join(tempDir(t), "user.lua")
		if err := p.OpenUserLua(testCtx(t), path, ""); err != nil {
			t.Fatal(err)
		}
		if st := p.openState(t); st.Name != path || !slices.Equal(st.Lines, []string{""}) || st.Modified {
			t.Errorf("without a template: %+v, want an empty, unmodified buffer", st)
		}
	})
}

// The question about a swap file is a prompt in which Lua is still answered,
// with a buffer that is only half loaded; OpenUserLua must refuse there too.
func TestUserLuaSwap(t *testing.T) {
	share, file := swapGone(t)
	p, out := newProc(t, func(o *Options) { share(o); o.Rows = 60 })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.OpenFile(ctx, file); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("OpenFile = %v, want it to wrap context.DeadlineExceeded", err)
	}
	eventually(t, "the swap file question", func() bool { return out.has("Found a swap file") })
	path := filepath.Join(tempDir(t), "user.lua")
	cctx, ccancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer ccancel()
	if err := p.OpenUserLua(cctx, path, "-- t"); !errors.Is(err, ErrPromptOpen) {
		t.Errorf("OpenUserLua while asked = %v, want ErrPromptOpen", err)
	}
	wantNoFile(t, path)
	answerSwap(t, p, out)
	eventually(t, "the question to be over", func() bool { return !p.blocking(t) })
}

// A relative path is refused before Neovim is asked: on a Process that is not
// running, a call would fail with ErrNotRunning instead.
func TestUserLuaRel(t *testing.T) {
	err := new(Process).OpenUserLua(context.Background(), "user.lua", "x")
	if err == nil || errors.Is(err, ErrNotRunning) {
		t.Errorf("OpenUserLua(relative) = %v, want an error that is not ErrNotRunning", err)
	}
}

// uncomment turns the examples of the template into code: the lines that start
// with "-- vim.", "-- local", "--   " (the body of the function), "-- end" and
// "-- moje_boje" lose their "-- ". The sentences of the comments keep theirs.
func uncomment(tmpl string) string {
	var out []string
	for _, line := range strings.Split(tmpl, "\n") {
		for _, prefix := range []string{"-- vim.", "-- local", "--   ", "-- end", "-- moje_boje"} {
			if strings.HasPrefix(line, prefix) {
				line = strings.TrimPrefix(line, "-- ")
				break
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n") + "\n"
}

// The template is a valid user.lua as it is (it changes nothing), and with its
// examples switched on it also loads and does what it says.
func TestUserLuaTmpl(t *testing.T) {
	tmpl := testTexts(t)["userLuaTemplate"]

	t.Run("asis", func(t *testing.T) {
		p := startUser(t, tmpl+"\n")
		if u := p.UserConfig(); u != (UserConfig{}) {
			t.Errorf("UserConfig = %+v, want it empty", u)
		}
		var mapped string
		p.lua(t, &mapped, `return vim.fn.maparg("<Space>w", "n")`)
		if mapped != "" {
			t.Errorf("<Space>w is mapped to %q by the template as it is", mapped)
		}
	})

	t.Run("open", func(t *testing.T) {
		code := uncomment(tmpl)
		if bytes.Equal([]byte(code), []byte(tmpl+"\n")) {
			t.Fatal("uncomment changed nothing")
		}
		p := startUser(t, code)
		if u := p.UserConfig(); u != (UserConfig{}) {
			t.Errorf("UserConfig = %+v, want it empty", u)
		}
		var st struct {
			Group       string
			Desc        string
			Timeoutlen  int
			WhichKeyOff bool
		}
		p.lua(t, &st, `
			local map = vim.fn.maparg("<Space>w", "n", false, true)
			return {
				Group = (vim.g.pisar_groups or {}).g or "",
				Desc = map.desc or "",
				Timeoutlen = vim.o.timeoutlen,
				WhichKeyOff = vim.g.pisar_whichkey == false,
			}`)
		if st.Group != "Git" || st.Desc != "Spremi" || st.Timeoutlen != 500 || !st.WhichKeyOff {
			t.Errorf("after start: %+v, want group Git, mapping Spremi, timeoutlen 500, which-key off", st)
		}
	})
}
