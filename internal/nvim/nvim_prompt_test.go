package nvim

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

// Everything here guards one property: Neovim must never be left on a prompt
// inside an RPC call, because it then answers nothing until a key arrives.

const testAnswer = 2 * time.Second // how long a healthy Neovim may take for one call

// longName returns a file name of about n runes, ending in .md, mixing ASCII
// with č ć š ž đ. Names stay below the 255 bytes a file name may have.
func longName(n int) string {
	var b strings.Builder
	for utf8.RuneCountInString(b.String()) < n-3 {
		b.WriteString("tekst-dokumenta-čćšžđ_")
	}
	name := []rune(b.String())[:n-3]
	return string(name) + ".md"
}

// answers fails the test unless Neovim answers a call promptly, which a
// Neovim stuck on a prompt does not.
func (p *Process) answers(t *testing.T) Buffer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testAnswer)
	defer cancel()
	b, err := p.Current(ctx)
	if err != nil {
		t.Fatalf("Neovim does not answer (stuck on a prompt?): %v", err)
	}
	return b
}

// resizeTo resizes the terminal and waits until Neovim has taken it over.
func (p *Process) resizeTo(t *testing.T, cols uint16) {
	t.Helper()
	if err := p.Resize(cols, 24); err != nil {
		t.Fatal(err)
	}
	eventually(t, fmt.Sprintf("&columns=%d", cols), func() bool {
		var got int
		err := p.execLua(context.Background(), "columns", &got, `return vim.o.columns`)
		return err == nil && got == int(cols)
	})
}

func (p *Process) messages(t *testing.T) string {
	t.Helper()
	var msgs string
	if err := p.execLua(context.Background(), "messages", &msgs, `
		return vim.api.nvim_exec2("messages", { output = true }).output`); err != nil {
		t.Fatal(err)
	}
	return msgs
}

// noSwap stops Neovim from creating swap files. Their name holds the whole
// path of the file, so a very long file name cannot get one (E303); that
// failure is not what these tests are about.
func (p *Process) noSwap(t *testing.T) {
	t.Helper()
	if err := p.execLua(context.Background(), "updatecount", nil, `vim.o.updatecount = 0`); err != nil {
		t.Fatal(err)
	}
}

// openModified opens a new file called name with the text "old" and replaces
// the buffer text, so the buffer is modified.
func (p *Process) openModified(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.OpenFile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	p.setBuffer(t, "nová linija")
	return path
}

// modes are the modes a message must not freeze: in insert mode Neovim also
// blocks its main loop to keep a message on screen (see messageLua).
var modes = []struct{ name, keys, mode string }{
	{"norm", "", "n"},
	{"ins", "i", "i"},
}

func (p *Process) enter(t *testing.T, keys, mode string) {
	t.Helper()
	if keys != "" {
		p.typeKeys(t, keys, mode, [2]int{1, 0})
	}
}

func TestLongErr(t *testing.T) {
	for _, cols := range []uint16{20, 80, 200} {
		for _, m := range modes {
			t.Run(fmt.Sprintf("%d%s", cols, m.name), func(t *testing.T) {
				p, _ := newProc(t, nil)
				p.noSwap(t)
				path := p.openModified(t, t.TempDir(), longName(150))
				// The buffer is not 'readonly', the file is: that gives E505,
				// whose text holds the whole path.
				if err := os.Chmod(path, 0o400); err != nil {
					t.Fatal(err)
				}
				p.resizeTo(t, cols)
				p.enter(t, m.keys, m.mode)

				ctx, cancel := context.WithTimeout(context.Background(), testAnswer)
				defer cancel()
				err := p.Save(ctx)
				if err == nil {
					t.Fatal("Save succeeded on a read-only file")
				}
				// Go gets the full text, however narrow the terminal.
				for _, want := range []string{"E505", filepath.Base(path), "read-only"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error lacks %q: %v", want, err)
					}
				}
				p.answers(t)
				if got := p.mode(t); got != m.mode {
					t.Errorf("mode = %q, want %q", got, m.mode)
				}
				msgs := p.messages(t)
				if !strings.Contains(msgs, "E505") {
					t.Errorf(":messages = %q, want the start of the message", msgs)
				}
				// 150 name cells never fit 80 columns or fewer.
				if cols <= 80 && !strings.Contains(msgs, "…") {
					t.Errorf(":messages = %q, want a cut message", msgs)
				}
			})
		}
	}
}

func TestLongAll(t *testing.T) {
	for _, cols := range []uint16{20, 80} {
		t.Run(fmt.Sprint(cols), func(t *testing.T) {
			p, _ := newProc(t, nil)
			p.noSwap(t)
			dir := t.TempDir()
			a := p.openModified(t, dir, "a-"+longName(150))
			b := p.openModified(t, dir, "b-"+longName(150))
			for _, f := range []string{a, b} {
				if err := os.Chmod(f, 0o400); err != nil {
					t.Fatal(err)
				}
			}
			p.resizeTo(t, cols)

			ctx, cancel := context.WithTimeout(context.Background(), testAnswer)
			defer cancel()
			err := p.SaveAll(ctx)
			if err == nil {
				t.Fatal("SaveAll succeeded on read-only files")
			}
			for _, f := range []string{a, b} {
				if !strings.Contains(err.Error(), filepath.Base(f)) {
					t.Errorf("error lacks %q: %v", filepath.Base(f), err)
				}
			}
			p.answers(t)
			if got := p.mode(t); got != "n" {
				t.Errorf("mode = %q, want n", got)
			}
		})
	}
}

func TestLongOK(t *testing.T) {
	// The success messages ("name" 1L, 12B written; "name" 1L, 4B) are cut by
	// Neovim itself; this proves it, and that init.lua keeps the flags.
	for _, cols := range []uint16{20, 80} {
		for _, m := range modes {
			t.Run(fmt.Sprintf("%d%s", cols, m.name), func(t *testing.T) {
				p, _ := newProc(t, nil)
				p.noSwap(t)
				var shm string
				if err := p.execLua(context.Background(), "shm", &shm, `return vim.o.shortmess`); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(shm, "t") || !strings.Contains(shm, "T") {
					t.Errorf("shortmess = %q, want it to hold t and T", shm)
				}
				p.resizeTo(t, cols)

				path := filepath.Join(t.TempDir(), longName(200))
				if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), testAnswer)
				defer cancel()
				if err := p.OpenFile(ctx, path); err != nil {
					t.Fatalf("OpenFile: %v", err)
				}
				if got := p.answers(t); realPath(t, got.Path) != realPath(t, path) {
					t.Errorf("Current = %q, want %q", got.Path, path)
				}
				p.setBuffer(t, "nová linija")
				p.enter(t, m.keys, m.mode)
				if err := p.Save(ctx); err != nil {
					t.Fatalf("Save: %v", err)
				}
				p.answers(t)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != "nová linija\n" {
					t.Errorf("file = %q", data)
				}
			})
		}
	}
}

func TestOpenSwapDir(t *testing.T) {
	// A swap directory Neovim cannot create makes :edit fail with E303, and the
	// message it leaves behind opens a hit-enter prompt right after the call
	// returns, at any width (measured with Neovim 0.12). OpenFile redraws first.
	p, _ := newProc(t, nil)
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.execLua(context.Background(), "directory", nil,
		`vim.o.directory = (...)`, filepath.Join(notDir, "swap")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := p.OpenFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "E303") {
		t.Fatalf("OpenFile = %v, want an E303 error", err)
	}
	p.answers(t)
}

func TestYank20(t *testing.T) {
	// "4 lines yanked into "+" is wider than the message line of a 20 column
	// terminal; Neovim cuts it ('shortmess' T), so CopyVisual must not freeze.
	p := newClipProc(t)
	p.resizeTo(t, 20)
	p.setBuffer(t, "a", "b", "c", "d")
	p.typeKeys(t, "VjjG", "V", [2]int{4, 0})
	ctx, cancel := context.WithTimeout(context.Background(), testAnswer)
	defer cancel()
	if ok, err := p.CopyVisual(ctx); err != nil || !ok {
		t.Fatalf("CopyVisual = %v, %v; want true, nil", ok, err)
	}
	p.answers(t)
	if err := p.Redraw(ctx); err != nil {
		t.Errorf("Redraw: %v", err)
	}
}

func TestFit(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		max  int
		want string
	}{
		{"fits", "hello", 5, "hello"},
		{"cut", "hello world", 8, "hello w…"},
		{"empty", "", 5, ""},
		// č ć š ž đ are two bytes but one cell: counting bytes would cut early.
		{"dia", "čćšžđ", 5, "čćšžđ"},
		{"diacut", "čćšžđčćšžđ", 6, "čćšžđ…"},
		// CJK characters are two cells: three of them (6) do not leave room for "…".
		{"wide", "日本語", 6, "日本語"},
		{"widecut", "日本語日本語", 6, "日本…"},
		{"emoji", "ab😀cd", 4, "ab…"},
		{"lines", "a\n  b\tc", 20, "a b c"},
		{"tiny", "abc", 1, "…"},
	}
	p, _ := newProc(t, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got struct {
				Text  string
				Width int
			}
			err := p.execLua(context.Background(), "fit", &got, messageLua+`
				local text = fit(...)
				return { Text = text, Width = vim.fn.strdisplaywidth(text) }`, tc.msg, tc.max)
			if err != nil {
				t.Fatal(err)
			}
			if got.Text != tc.want {
				t.Errorf("fit(%q, %d) = %q, want %q", tc.msg, tc.max, got.Text, tc.want)
			}
			if got.Width > tc.max {
				t.Errorf("fit(%q, %d) is %d cells wide", tc.msg, tc.max, got.Width)
			}
		})
	}
}

// hangs reports whether lua failed to return within d, which is how a hit-enter
// prompt inside a call looks. It then dismisses the prompt.
func (p *Process) hangs(t *testing.T, d time.Duration, lua string, args ...interface{}) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	err := p.execLua(ctx, "probe", nil, lua, args...)
	if err == nil {
		return false
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, werr := p.Write([]byte("\r")); werr != nil {
		t.Fatal(werr)
	}
	eventually(t, "Neovim to answer after the prompt", func() bool {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Second)
		defer ccancel()
		_, cerr := p.Current(cctx)
		return cerr == nil
	})
	return true
}

func TestWidth(t *testing.T) {
	// msg_width must be exact for every layout that changes the room on the
	// message line: a message of that many cells fits, one cell more opens a
	// hit-enter prompt. This is what the 12 reserved cells (default) rest on.
	tests := []struct {
		name string
		set  string
		want int // at 80 columns; measured, not derived from the rule
	}{
		{"default", "", 68},
		{"nosc", "noshowcmd", 79},
		{"noru", "noruler", 68},
		{"nonenone", "noshowcmd noruler", 79},
		{"ls0", "laststatus=0", 51},
		{"ls0nosc", "laststatus=0 noshowcmd", 61},
		{"ls1", "laststatus=1", 51},
		{"ls3", "laststatus=3", 68},
		{"sloc", "showcmdloc=statusline", 79},
		{"ruf", "laststatus=0 rulerformat=%50(%l%)", 18},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProc(t, nil)
			if tc.set != "" {
				if err := p.execLua(context.Background(), "set", nil,
					`vim.cmd("set " .. (...)); vim.cmd.redraw()`, tc.set); err != nil {
					t.Fatal(err)
				}
			}
			var got int
			if err := p.execLua(context.Background(), "width", &got, messageLua+`
			return msg_width()`); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("msg_width() = %d, want %d", got, tc.want)
			}
			echo := `vim.api.nvim_echo({ { string.rep("x", ...), "ErrorMsg" } }, true, {}); vim.cmd.redraw()`
			if p.hangs(t, testAnswer, echo, got) {
				t.Errorf("an echo of %d cells opened a prompt", got)
			}
			if !p.hangs(t, 500*time.Millisecond, echo, got+1) {
				t.Errorf("an echo of %d cells did not open a prompt: the rule is too strict", got+1)
			}
		})
	}
}

// swapGone creates the state E325 asks about: a swap file whose Neovim died.
// A second Process opens the file, changes it and writes the swap file out
// (:preserve; ordinary typing only flushes it after 'updatecount' keys or 4 s
// of idling), then it is killed with SIGKILL, server process included, so
// nothing cleans up. A living owner would not do: Neovim's default SwapExists
// handler skips the question when the swap file belongs to a running process
// (:help W325). Both processes share XDG_STATE_HOME, where Neovim keeps swap
// files ('directory'). It returns that directory and the file.
func swapGone(t *testing.T) (share func(*Options), file string) {
	t.Helper()
	state := t.TempDir()
	share = func(o *Options) { o.Env = append(o.Env, "XDG_STATE_HOME="+state) }
	file = filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	owner, _ := newProc(t, share)
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	if err := owner.OpenFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	owner.setBuffer(t, "unsaved")
	var swap string
	if err := owner.execLua(ctx, "preserve", &swap, `
		vim.cmd.preserve()
		return vim.fn.swapname("%")`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(swap); err != nil {
		t.Fatalf("no swap file after :preserve: %v", err)
	}
	owner.mu.Lock()
	server := owner.serverPid
	owner.mu.Unlock()
	if err := syscall.Kill(server, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-owner.Pid(), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitDone(t, owner)
	if _, err := os.Stat(swap); err != nil {
		t.Fatalf("swap file gone after the kill: %v", err)
	}
	return share, file
}

// swapRows: with 24 rows the question text is longer than the screen and a
// "-- More --" pager comes first, which a user pages with the space bar (an "e"
// typed too early is eaten by it); with 60 rows the question is shown at once.
var swapRows = []struct {
	name string
	rows uint16
}{
	{"more", 24},
	{"fit", 60},
}

// answerSwap answers "edit anyway" like a user would: it pages with the space
// bar until the question is on screen, then types e.
func answerSwap(t *testing.T, p *Process, out *output) {
	t.Helper()
	eventually(t, "the question to be answered", func() bool {
		key := " "
		if out.has("(E)dit anyway") {
			key = "e"
		}
		if _, err := p.Write([]byte(key)); err != nil {
			t.Fatal(err)
		}
		return key == "e"
	})
}

func (o *output) has(s string) bool { return strings.Contains(string(o.bytes()), s) }

func TestSwap(t *testing.T) {
	for _, tc := range swapRows {
		t.Run(tc.name, func(t *testing.T) {
			share, file := swapGone(t)
			p, out := newProc(t, func(o *Options) { share(o); o.Rows = tc.rows })
			// A modified buffer in the background: SaveAll has something to write,
			// which it must not do while the question stands.
			modFile := p.openModified(t, t.TempDir(), "mod.md")

			// The question is not silenced: the call waits for the user's answer.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			start := time.Now()
			err := p.OpenFile(ctx, file)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("OpenFile = %v, want it to wrap context.DeadlineExceeded", err)
			}
			if took := time.Since(start); took > 3*time.Second {
				t.Errorf("OpenFile took %v for a 1 s context", took)
			}
			eventually(t, "the swap file question on the terminal", func() bool { return out.has("Found a swap file") })

			if tc.rows >= 60 && !out.has("(E)dit anyway") {
				t.Error("the question itself was not shown")
			}

			// While the question stands no call may outlive its context. Whether
			// it is answered depends on the screen (a pager does not serve RPC).
			// Save in particular must not write: the buffer is still empty here
			// and used to be written over the file.
			for name, call := range map[string]func(context.Context) error{
				"Current":  func(c context.Context) error { _, err := p.Current(c); return err },
				"Modified": func(c context.Context) error { _, err := p.Modified(c); return err },
				"Save":     p.Save,
				"SaveAll":  p.SaveAll,
			} {
				cctx, ccancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
				start := time.Now()
				err := call(cctx)
				ccancel()
				if (name == "Save" || name == "SaveAll") && tc.rows >= 60 {
					if err == nil || !strings.Contains(err.Error(), "waiting for an answer") {
						t.Errorf("%s while asked = %v, want a refusal", name, err)
					} else {
						t.Logf("%s while asked: %v", name, err)
					}
				}
				if took := time.Since(start); took > time.Second {
					t.Errorf("%s took %v for a 300 ms context", name, took)
				}
			}

			// Nothing was written, byte for byte, whichever the screen.
			if data, err := os.ReadFile(file); err != nil || string(data) != "x\n" {
				t.Errorf("file while asked = %q, %v; want it untouched", data, err)
			}
			if data, err := os.ReadFile(modFile); err != nil || string(data) != "old\n" {
				t.Errorf("modified buffer's file while asked = %q, %v; want it untouched", data, err)
			}

			// The user answers "edit anyway" through the terminal.
			answerSwap(t, p, out)
			eventually(t, "the file to be loaded", func() bool {
				var lines []string
				cctx, ccancel := context.WithTimeout(context.Background(), time.Second)
				defer ccancel()
				err := p.execLua(cctx, "lines", &lines, `return vim.api.nvim_buf_get_lines(0, 0, -1, false)`)
				return err == nil && len(lines) == 1 && lines[0] == "x"
			})
			if got := p.answers(t); realPath(t, got.Path) != realPath(t, file) || got.Modified {
				t.Errorf("Current = %+v, want the unmodified %q", got, file)
			}
			if got := p.mode(t); got != "n" {
				t.Errorf("mode = %q, want n", got)
			}
			if data, err := os.ReadFile(file); err != nil || string(data) != "x\n" {
				t.Errorf("file = %q, %v; want it untouched", data, err)
			}
			// SaveAll works again: it writes the background buffer and leaves the
			// file that was asked about alone (its buffer is not modified).
			saveCtx, saveCancel := context.WithTimeout(context.Background(), testAnswer)
			defer saveCancel()
			if err := p.SaveAll(saveCtx); err != nil {
				t.Errorf("SaveAll after the answer: %v", err)
			}
			if data, err := os.ReadFile(modFile); err != nil || string(data) != "nová linija\n" {
				t.Errorf("modified buffer's file after SaveAll = %q, %v; want its text", data, err)
			}
			if data, err := os.ReadFile(file); err != nil || string(data) != "x\n" {
				t.Errorf("file after SaveAll = %q, %v; want it untouched", data, err)
			}
			// Later calls work. (Not the same file again: Neovim keeps the old swap
			// file, so it would ask again.)
			other := filepath.Join(filepath.Dir(file), "other.md")
			if err := os.WriteFile(other, []byte("y\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx2, cancel2 := context.WithTimeout(context.Background(), testAnswer)
			defer cancel2()
			if err := p.OpenFile(ctx2, other); err != nil {
				t.Errorf("OpenFile after the answer: %v", err)
			}
		})
	}
}

func TestSwapClose(t *testing.T) {
	for _, tc := range swapRows {
		t.Run(tc.name, func(t *testing.T) {
			share, file := swapGone(t)
			p, out := newProc(t, func(o *Options) { share(o); o.Rows = tc.rows; o.StopTimeout = testStop })
			pid, sock := p.Pid(), p.sock

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := p.OpenFile(ctx, file); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("OpenFile = %v, want it to wrap context.DeadlineExceeded", err)
			}
			eventually(t, "the swap file question on the terminal", func() bool { return out.has("Found a swap file") })

			start := time.Now()
			if err := p.Close(context.Background()); err != nil {
				t.Fatalf("Close: %v", err)
			}
			took := time.Since(start)
			t.Logf("Close took %v, stopped by stage %d", took, p.stoppedBy.Load())
			if took > 6*testStop {
				t.Errorf("Close took %v with the question standing, want a few stages at most", took)
			}
			if processExists(pid) {
				t.Errorf("pid %d still exists", pid)
			}
			if nvimAlive(t, sock) {
				t.Error("a process with the socket path on its command line is still alive")
			}
		})
	}
}

func TestSaveAsk(t *testing.T) {
	// A file changed on disk since Neovim read it makes :write ask y/n inside
	// the call. Like the swap question, it is Neovim's own protection and it is
	// left alone (conflict handling is docsafe's job); Save only has to return
	// when its context ends, and Neovim must be usable after the answer.
	p, out := newProc(t, nil)
	path := p.openModified(t, t.TempDir(), "ask.md")
	if err := os.WriteFile(path, []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An mtime in the future: no need to wait for the clock to tick.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Save(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Save = %v, want it to wrap context.DeadlineExceeded", err)
	}
	eventually(t, "the question on the terminal", func() bool { return out.has("Do you really want to write to it") })
	// SaveAll has to come back within its context too, and is refused like Save
	// is: the question is Neovim's own protection of a file that changed on disk.
	allCtx, allCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	allStart := time.Now()
	err := p.SaveAll(allCtx)
	allCancel()
	if err == nil || !strings.Contains(err.Error(), "waiting for an answer") {
		t.Errorf("SaveAll while the question stands = %v, want a refusal", err)
	}
	if took := time.Since(allStart); took > time.Second {
		t.Errorf("SaveAll took %v for a 500 ms context", took)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "other\n" {
		t.Errorf("file while asked = %q, %v; want the other writer's text, untouched", data, err)
	}
	if _, err := p.Write([]byte("n")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Neovim to answer", func() bool {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Second)
		defer ccancel()
		b, err := p.Current(cctx)
		return err == nil && b.Modified
	})
	if data, err := os.ReadFile(path); err != nil || string(data) != "other\n" {
		t.Errorf("file = %q, %v; want the other writer's text, untouched", data, err)
	}
}
