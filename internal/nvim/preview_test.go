package nvim

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// quiet is how long the tests wait before they look for events that must NOT
// come: the autocmds fire within a few milliseconds of a change, so this is
// generous. The sentinel in settled then proves nothing is still queued.
const quiet = 200 * time.Millisecond

// settled waits until Neovim has had time to send everything caused by what the
// test did, then sends a sentinel through the same channel (notifications on
// one channel keep their order) and returns the events that came before it.
func settled(t *testing.T, p *Process) []Event {
	t.Helper()
	time.Sleep(quiet)
	err := p.execLua(testCtx(t), "sentinel", nil, `vim.rpcnotify(vim.g.pisar_channel, "pisar_focus")`)
	if err != nil {
		t.Fatal(err)
	}
	evs := collectUntil(t, p, func(evs []Event) bool {
		return len(evs) > 0 && evs[len(evs)-1].Kind == EventFocusTree
	})
	return evs[:len(evs)-1]
}

// only keeps the events of the given kinds, in order.
func only(evs []Event, kinds ...EventKind) []Event {
	var out []Event
	for _, ev := range evs {
		for _, k := range kinds {
			if ev.Kind == k {
				out = append(out, ev)
				break
			}
		}
	}
	return out
}

func changedCount(evs []Event) int { return len(only(evs, EventChanged)) }

func (p *Process) lines(t *testing.T) []string {
	t.Helper()
	var lines []string
	if err := p.execLua(context.Background(), "lines", &lines, `return vim.api.nvim_buf_get_lines(0, 0, -1, false)`); err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestPrevKeys(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "one\ntwo\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a})

	// Normal mode: each mapping notifies once and does nothing else (a space
	// would move the cursor).
	if _, err := p.Write([]byte(" p")); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, Event{Kind: EventTogglePreview})
	if _, err := p.Write([]byte(" f")); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, Event{Kind: EventOpenSearch})
	if evs := settled(t, p); len(evs) != 0 {
		t.Errorf("extra events after the mappings: %+v", evs)
	}
	p.typeKeys(t, "", "n", [2]int{1, 0})

	// Insert mode: a space and the letter are text.
	if _, err := p.Write([]byte("i p f\x1b")); err != nil {
		t.Fatal(err)
	}
	evs := settled(t, p)
	if got := only(evs, EventTogglePreview, EventOpenSearch); len(got) != 0 {
		t.Errorf("events in insert mode: %+v", got)
	}
	if got := p.lines(t); got[0] != " p fone" {
		t.Errorf("first line = %q, want %q", got[0], " p fone")
	}
}

func TestChanged(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "one\ntwo\nthree\n")
	b := mkFile(t, dir, "b.md", "b\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a})

	// Preview off: edits and a buffer switch give no change events.
	if _, err := p.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if n := changedCount(settled(t, p)); n != 0 {
		t.Errorf("%d change events with the preview off (normal mode)", n)
	}
	p.setBuffer(t, "off")
	if n := changedCount(settled(t, p)); n != 0 {
		t.Errorf("%d change events with the preview off (RPC edit)", n)
	}

	// Switching it on queues one, so the first render needs no edit.
	if err := p.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	if n := changedCount(settled(t, p)); n != 1 {
		t.Errorf("%d change events after SetPreview(true), want 1", n)
	}

	steps := []struct {
		name string
		do   func()
		want []Event // only EventBufferEntered and EventChanged, in order
	}{
		{"normal x", func() { p.write(t, "x") }, []Event{{Kind: EventChanged}}},
		{"normal dd", func() { p.write(t, "dd") }, []Event{{Kind: EventChanged}}},
		{"RPC edit", func() { p.setBuffer(t, "x", "y") }, []Event{{Kind: EventChanged}}},
		{"undo", func() { p.write(t, "u") }, []Event{{Kind: EventChanged}}},
		{"other buffer", func() {
			if err := p.OpenFile(ctx, b); err != nil {
				t.Fatal(err)
			}
		}, []Event{{Kind: EventBufferEntered, Path: b}, {Kind: EventChanged}}},
		{"back to a buffer", func() { p.write(t, ":bprevious\r") },
			[]Event{{Kind: EventBufferEntered, Path: a, Modified: true}, {Kind: EventChanged}}},
		// BufReadPost: :edit! replaces the text without TextChanged (measured).
		{":edit! after an outside change", func() {
			if err := os.WriteFile(a, []byte("outside\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			p.write(t, ":edit!\r")
		}, []Event{{Kind: EventChanged}}},
		{"Reload", func() {
			if err := os.WriteFile(a, []byte("outside again\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := p.Reload(ctx, a); err != nil {
				t.Fatal(err)
			}
		}, []Event{{Kind: EventChanged}}},
	}
	for _, s := range steps {
		s.do()
		got := only(settled(t, p), EventBufferEntered, EventChanged)
		if fmt.Sprint(got) != fmt.Sprint(s.want) {
			t.Errorf("%s: events %+v, want %+v", s.name, got, s.want)
		}
	}
	if got, _, err := p.BufferText(ctx, 1<<20); err != nil || string(got) != "outside again\n" {
		t.Errorf("BufferText after the reload = %q, %v", got, err)
	}

	// Off again: nothing, although the buffer changes and switches.
	if err := p.SetPreview(ctx, false); err != nil {
		t.Fatal(err)
	}
	p.write(t, "x")
	p.setBuffer(t, "z")
	if err := p.OpenFile(ctx, b); err != nil {
		t.Fatal(err)
	}
	if n := changedCount(settled(t, p)); n != 0 {
		t.Errorf("%d change events after SetPreview(false)", n)
	}
}

func (p *Process) write(t *testing.T, keys string) {
	t.Helper()
	if _, err := p.Write([]byte(keys)); err != nil {
		t.Fatal(err)
	}
}

// TestChangedIns records what Neovim sends while typing in insert mode.
func TestChangedIns(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "alphabet\n\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := p.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	settled(t, p)

	// Typed one key at a time: one event per character (TextChangedI), none
	// for entering or leaving insert mode.
	p.write(t, "Gi")
	if n := changedCount(settled(t, p)); n != 0 {
		t.Errorf("%d change events on entering insert mode", n)
	}
	for _, c := range "hčé" {
		p.write(t, string(c))
		waitKind(t, p, EventChanged)
	}
	p.write(t, "\x1b")
	if n := changedCount(settled(t, p)); n != 0 {
		t.Errorf("%d unexpected change events while typing and leaving insert mode", n)
	}

	// Typed in one go (a paste), Neovim sends fewer: it skips TextChangedI while
	// typeahead is waiting. What matters is that at least one comes after the
	// last change, so the consumer's last render is of the final text.
	const burst = "hello world, and some more"
	p.write(t, "Go"+burst+"\x1b")
	n := changedCount(settled(t, p))
	t.Logf("%d change events for one write of %d characters", n, len(burst))
	if n < 1 {
		t.Fatal("no change event for a burst of typing")
	}
	if text, _, err := p.BufferText(ctx, 1<<20); err != nil || !strings.Contains(string(text), burst) {
		t.Errorf("BufferText = %q, %v; want it to contain the typed text", text, err)
	}

	// With the completion menu open the change is TextChangedP, not TextChangedI.
	p.setBuffer(t, "alphabet alpine", "")
	settled(t, p)
	p.write(t, "ji")
	for _, c := range "alp" {
		p.write(t, string(c))
		waitKind(t, p, EventChanged)
	}
	p.write(t, "\x0e") // CTRL-N: opens the menu and inserts the first match
	waitKind(t, p, EventChanged)
	var shown int
	if err := p.execLua(ctx, "pum", &shown, `return vim.fn.pumvisible()`); err != nil || shown != 1 {
		t.Errorf("pumvisible = %d, %v; want the menu open", shown, err)
	}
	if got := p.lines(t); got[1] != "alphabet" && got[1] != "alpine" {
		t.Errorf("second line = %q, want the completed word", got[1])
	}
	// A second CTRL-N changes the text again, and still with the menu open.
	p.write(t, "\x0e")
	waitKind(t, p, EventChanged)
	p.write(t, "\x1b")
	settled(t, p)
}

func TestPrevDown(t *testing.T) {
	p, _ := newProc(t, nil)
	if err := p.Close(testCtx(t)); err != nil {
		t.Fatal(err)
	}
	ctx := testCtx(t)
	if err := p.SetPreview(ctx, true); !errors.Is(err, ErrNotRunning) {
		t.Errorf("SetPreview: %v, want ErrNotRunning", err)
	}
	if _, _, err := p.BufferText(ctx, 100); !errors.Is(err, ErrNotRunning) {
		t.Errorf("BufferText: %v, want ErrNotRunning", err)
	}
}

func TestText(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)

	open := func(name, content string) string {
		path := mkFile(t, dir, name, content)
		if err := p.OpenFile(ctx, path); err != nil {
			t.Fatal(err)
		}
		return path
	}
	enew := func() {
		if err := p.execLua(ctx, "enew", nil, `vim.cmd.enew({ bang = true })`); err != nil {
			t.Fatal(err)
		}
	}
	setOpt := func(name string, value bool) {
		if err := p.execLua(ctx, "option", nil, `vim.bo[(...)] = select(2, ...)`, name, value); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name  string
		setup func() Buffer // returns the buffer BufferText must describe
		want  string
	}{
		{"unsaved text, not the file's", func() Buffer {
			a := open("a.md", "on disk\n")
			p.setBuffer(t, "on screen", "second")
			return Buffer{Path: a, Modified: true}
		}, "on screen\nsecond\n"},
		{"diacritics and multibyte", func() Buffer {
			a := open("d.md", "x\n")
			p.setBuffer(t, "čćžšđ ✓", "😀 ŠĐ")
			return Buffer{Path: a, Modified: true}
		}, "čćžšđ ✓\n😀 ŠĐ\n"},
		{"empty buffer", func() Buffer { enew(); return Buffer{} }, ""},
		{"empty file", func() Buffer { return Buffer{Path: open("e.md", "")} }, ""},
		{"file of one newline", func() Buffer { return Buffer{Path: open("n.md", "\n")} }, "\n"},
		{"unnamed buffer with text", func() Buffer {
			enew()
			p.setBuffer(t, "scratch")
			return Buffer{Modified: true}
		}, "scratch\n"},
		// :w would add the missing end of line ('fixeol' is on), so the text has it.
		{"no final newline, fixeol", func() Buffer { return Buffer{Path: open("x.md", "x")} }, "x\n"},
		{"no final newline, nofixeol", func() Buffer {
			a := open("y.md", "x")
			setOpt("fixeol", false)
			return Buffer{Path: a}
		}, "x"},
		{"final newline, nofixeol", func() Buffer {
			a := open("z.md", "x\n")
			setOpt("fixeol", false)
			return Buffer{Path: a}
		}, "x\n"},
		{"dos line endings", func() Buffer { return Buffer{Path: open("w.md", "a\r\nb\r\n")} }, "a\nb\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantBuf := tc.setup()
			got, buf, err := p.BufferText(ctx, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if buf != wantBuf {
				t.Errorf("Buffer = %+v, want %+v", buf, wantBuf)
			}
			if string(got) != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

// A buffer that is not an ordinary file has no description, like Current's, but
// its text is still there.
func TestTextHelp(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	if err := p.execLua(ctx, "help", nil, `vim.cmd.help()`); err != nil {
		t.Fatal(err)
	}
	text, buf, err := p.BufferText(ctx, 1<<20)
	if err != nil || buf != (Buffer{}) || len(text) == 0 {
		t.Errorf("BufferText = %d bytes, %+v, %v; want text and an empty description", len(text), buf, err)
	}
}

// countReads makes nvim_buf_get_lines count its calls in _G.reads, which proves
// that a text over the limit is not even read, let alone sent.
func countReads(t *testing.T, p *Process) {
	t.Helper()
	err := p.execLua(context.Background(), "count reads", nil, `
		local orig = vim.api.nvim_buf_get_lines
		_G.reads = 0
		vim.api.nvim_buf_get_lines = function(...)
			_G.reads = _G.reads + 1
			return orig(...)
		end`)
	if err != nil {
		t.Fatal(err)
	}
}

func reads(t *testing.T, p *Process) int {
	t.Helper()
	var n int
	if err := p.execLua(context.Background(), "reads", &n, `return _G.reads`); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTextSize(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	countReads(t, p)

	// "abc\nčd\n" is 3+1 + 3+1 = 8 bytes: č takes two.
	a := mkFile(t, dir, "a.md", "abc\nčd\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	const size = 8
	text, _, err := p.BufferText(ctx, size)
	if err != nil || len(text) != size {
		t.Fatalf("at the limit: %d bytes, %v", len(text), err)
	}
	if n := reads(t, p); n != 1 {
		t.Errorf("%d reads at the limit, want 1", n)
	}
	text, buf, err := p.BufferText(ctx, size-1)
	if !errors.Is(err, ErrBufferTooLarge) {
		t.Fatalf("one byte over: %q, %v; want ErrBufferTooLarge", text, err)
	}
	if text != nil || buf != (Buffer{}) {
		t.Errorf("one byte over returned %q and %+v with the error", text, buf)
	}
	if n := reads(t, p); n != 1 {
		t.Errorf("%d reads after the refusal, want still 1: the text must not be read", n)
	}

	// An empty buffer is 0 bytes, and a buffer without its final newline
	// ('nofixeol') is one byte shorter than with it.
	if err := p.execLua(ctx, "enew", nil, `vim.cmd.enew({ bang = true })`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.BufferText(ctx, 0); err != nil {
		t.Errorf("empty buffer with limit 0: %v", err)
	}
	x := mkFile(t, dir, "x.md", "x")
	if err := p.OpenFile(ctx, x); err != nil {
		t.Fatal(err)
	}
	if err := p.execLua(ctx, "nofixeol", nil, `vim.bo.fixeol = false`); err != nil {
		t.Fatal(err)
	}
	if text, _, err := p.BufferText(ctx, 1); err != nil || string(text) != "x" {
		t.Errorf("nofixeol at the limit: %q, %v", text, err)
	}
	if _, _, err := p.BufferText(ctx, 0); !errors.Is(err, ErrBufferTooLarge) {
		t.Errorf("nofixeol one byte over: %v", err)
	}

	// A real big buffer: refused without a transfer, and read whole when it fits.
	const lineLen, lineCount = 1023, 8192 // 8 MiB with the newlines
	err = p.execLua(ctx, "big", nil, `
		local line = string.rep("x", (...))
		local lines = {}
		for i = 1, select(2, ...) do lines[i] = line end
		vim.cmd.enew({ bang = true })
		vim.api.nvim_buf_set_lines(0, 0, -1, false, lines)`, lineLen, lineCount)
	if err != nil {
		t.Fatal(err)
	}
	before := reads(t, p)
	start := time.Now()
	if _, _, err := p.BufferText(ctx, 1024); !errors.Is(err, ErrBufferTooLarge) {
		t.Fatalf("big buffer, small limit: %v", err)
	}
	refused := time.Since(start)
	if reads(t, p) != before {
		t.Error("the big buffer was read before it was refused")
	}
	start = time.Now()
	text, _, err = p.BufferText(ctx, (lineLen+1)*lineCount)
	if err != nil || len(text) != (lineLen+1)*lineCount {
		t.Fatalf("big buffer at the limit: %d bytes, %v", len(text), err)
	}
	t.Logf("8 MiB buffer: refused in %v, transferred in %v", refused, time.Since(start))
}

// TestTextSwitch switches buffers as fast as it can while BufferText runs: the
// text and the description of one answer must always belong to one buffer.
func TestTextSwitch(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "text of a\n")
	b := mkFile(t, dir, "b.md", "text of b\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := p.OpenFile(ctx, b); err != nil {
		t.Fatal(err)
	}

	swCtx, stop := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; swCtx.Err() == nil; i++ {
			next := a
			if i%2 == 1 {
				next = b
			}
			// An error here ends the test through the reads below, or is the
			// cancellation itself.
			_ = p.OpenFile(swCtx, next)
		}
	}()
	defer func() { stop(); wg.Wait() }()

	seen := map[string]int{}
	for range 300 {
		text, buf, err := p.BufferText(ctx, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"text of a\n": a, "text of b\n": b}[string(text)]
		if want == "" || buf.Path != want {
			t.Fatalf("text %q came with path %q", text, buf.Path)
		}
		seen[string(text)]++
	}
	t.Logf("answers per buffer: %v", seen)
	if len(seen) != 2 {
		t.Errorf("only saw %v: the switching did not overlap the calls", seen)
	}
}

// TestTextState: reading must leave everything the user can see or use alone.
func TestTextState(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "one two\nthree four\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	err := p.execLua(ctx, "state", nil, `
		vim.api.nvim_win_set_cursor(0, { 2, 3 })
		vim.fn.setreg("a", "kept a")
		vim.fn.setreg('"', "kept unnamed")
		vim.fn.setreg("0", "kept yank")
		vim.fn.setreg("/", "kept search")
		vim.v.errmsg = "kept errmsg"`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var s string
		err := p.execLua(ctx, "snapshot", &s, `
			-- vim.inspect, not vim.json.encode: inspect sorts the keys, encode does not,
			-- and two snapshots of the same state must be the same string.
			return vim.inspect({
				cursor = vim.api.nvim_win_get_cursor(0),
				mode = vim.api.nvim_get_mode().mode,
				regs = { vim.fn.getreg("a"), vim.fn.getreg('"'), vim.fn.getreg("0"), vim.fn.getreg("/") },
				regtypes = { vim.fn.getregtype("a"), vim.fn.getregtype('"') },
				errmsg = vim.v.errmsg,
				messages = vim.api.nvim_exec2("messages", { output = true }).output,
				tick = vim.b.changedtick,
				buf = vim.api.nvim_get_current_buf(),
				modified = vim.bo.modified,
				visual = { vim.fn.getpos("'<"), vim.fn.getpos("'>") },
			})`)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	for _, m := range []struct{ name, keys, mode string }{
		{"normal", "", "n"},
		{"visual", "v", "v"},
		{"insert", "\x1bi", "i"},
	} {
		p.typeKeys(t, m.keys, m.mode, [2]int{2, 3})
		before := snapshot()
		if _, _, err := p.BufferText(ctx, 1<<20); err != nil {
			t.Fatal(err)
		}
		if _, _, err := p.BufferText(ctx, 1); !errors.Is(err, ErrBufferTooLarge) {
			t.Fatalf("%s: %v", m.name, err)
		}
		if after := snapshot(); after != before {
			t.Errorf("%s mode: state changed by BufferText\nbefore: %s\nafter:  %s", m.name, before, after)
		}
	}
}
