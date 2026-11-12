package nvim

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	nvimrpc "github.com/neovim/go-client/nvim"
)

// Tests of the Markdown formatting actions of nvim/init.lua (bold, italic,
// heading, list), through both of the ways in: the mappings (typed keys) and
// Process.Format (RPC). They run the repository's own init.lua in a real
// Neovim. One Neovim serves all cases of a test: the buffer is replaced before
// each case.
//
// Test names are short for the reason given in nvim_test.go.

var formatKeys = map[string]string{"bold": " b", "italic": " i", "heading": " h", "list": " l"}

// fx drives one Neovim for the formatting tests.
type fx struct {
	*Process
}

func newFx(t *testing.T) *fx {
	t.Helper()
	p, _ := newProc(t, nil)
	f := &fx{p}
	// Keys travel through the PTY, asynchronously from the RPC calls. A key that
	// does nothing but count is sent after them: when the count has moved, all
	// the keys before it have been processed (also when they changed nothing,
	// which a test of "does nothing" has to be sure of). CTRL-_ is 0x1f, a
	// single byte, so it cannot be taken for a part of an escape sequence.
	f.lua(t, nil, `
		vim.g.barrier = 0
		vim.keymap.set({ "n", "x", "i" }, "<C-_>", function() vim.g.barrier = vim.g.barrier + 1 end)`)
	return f
}

// press sends keys and waits until Neovim has processed them. Keys must not end
// in <Esc>: the barrier byte right after it would be read as Alt-CTRL-_.
func (f *fx) press(t *testing.T, keys string) {
	t.Helper()
	var before int
	f.lua(t, &before, `return vim.g.barrier`)
	f.write(t, keys+"\x1f")
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		var now int
		f.lua(t, &now, `return vim.g.barrier`)
		if now > before {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("keys %q were not processed", keys)
}

// escape leaves insert, visual, operator-pending and command-line mode, and
// dismisses a prompt. It goes through nvim_input, which is "api-fast": Lua is
// not run while a prompt is up.
func (f *fx) escape(t *testing.T) {
	t.Helper()
	err := f.call(testCtx(t), "input", func(nv *nvimrpc.Nvim) error {
		_, err := nv.Input("<Esc>")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// The barrier key is a mapping, which Lua runs: it is only answered once
	// Neovim is out of the prompt.
	f.press(t, "")
}

// reset puts lines into the buffer with no undo history and no 'modified', the
// cursor at the start, normal mode.
func (f *fx) reset(t *testing.T, lines []string) {
	t.Helper()
	f.escape(t)
	f.lua(t, nil, `
		vim.o.undolevels = -1
		vim.api.nvim_buf_set_lines(0, 0, -1, false, ...)
		vim.bo.modified = false
		vim.o.undolevels = 1000
		vim.api.nvim_win_set_cursor(0, { 1, 0 })`, lines)
}

func (f *fx) setCursor(t *testing.T, pos [2]int) {
	t.Helper()
	f.lua(t, nil, `vim.api.nvim_win_set_cursor(0, ...)`, pos)
}

type fstate struct {
	Lines    []string
	Row, Col int // row 1-based, byte column
	Mode     string
	Modified bool
}

func (f *fx) get(t *testing.T) fstate {
	t.Helper()
	var st fstate
	f.lua(t, &st, `
		local pos = vim.api.nvim_win_get_cursor(0)
		return {
			Lines = vim.api.nvim_buf_get_lines(0, 0, -1, false),
			Row = pos[1], Col = pos[2],
			Mode = vim.api.nvim_get_mode().mode, Modified = vim.bo.modified,
		}`)
	return st
}

func (f *fx) format(t *testing.T, kind string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return f.Format(ctx, kind)
}

// fcase is one formatting case.
type fcase struct {
	name  string
	lines []string
	mode  string // "n", "i", "v", "V" or "b" (block)
	at    [2]int // cursor (row 1-based, byte column); the start of a selection
	to    [2]int // the end of a selection, set through the API
	keys  string // moves the end of a selection by typed keys instead of `to`
	kind  string
	want  []string // nil: the text does not change
	cur   [2]int   // cursor afterwards
	after string   // mode afterwards; default "n", "i" for insert mode
}

// place puts Neovim in the mode and position of the case.
func (f *fx) place(t *testing.T, c fcase) {
	t.Helper()
	f.reset(t, c.lines)
	f.setCursor(t, c.at)
	switch c.mode {
	case "n":
		return
	case "i":
		f.press(t, "i")
	case "v", "V":
		f.press(t, c.mode)
	case "b":
		f.press(t, "\x16")
	default:
		t.Fatalf("mode %q", c.mode)
	}
	f.setCursor(t, c.at)
	switch {
	case c.keys != "":
		f.press(t, c.keys)
	case c.mode != "i" && c.to != [2]int{}:
		f.setCursor(t, c.to)
	}
}

// runCases runs each case through the mapping (not in insert mode, where there
// is none) and through Format. After each one that changed the text it checks
// that one "u" gives the old text back and CTRL-R the new one.
func (f *fx) runCases(t *testing.T, cases []fcase) {
	t.Helper()
	for _, c := range cases {
		for _, way := range []string{"map", "rpc"} {
			if way == "map" && c.mode == "i" {
				continue
			}
			t.Run(c.name+"/"+way, func(t *testing.T) {
				f.place(t, c)
				if way == "map" {
					f.press(t, formatKeys[c.kind])
				} else if err := f.format(t, c.kind); err != nil {
					t.Fatalf("Format: %v", err)
				}
				want := c.want
				if want == nil {
					want = c.lines
				}
				wantMode := c.after
				if wantMode == "" {
					wantMode = "n"
					if c.mode == "i" {
						wantMode = "i"
					}
				}
				st := f.get(t)
				if !slices.Equal(st.Lines, want) {
					t.Errorf("text = %q, want %q", st.Lines, want)
				}
				if got := [2]int{st.Row, st.Col}; got != c.cur {
					t.Errorf("cursor = %v, want %v", got, c.cur)
				}
				if st.Mode != wantMode {
					t.Errorf("mode = %q, want %q", st.Mode, wantMode)
				}
				changed := !slices.Equal(want, c.lines)
				if st.Modified != changed {
					t.Errorf("modified = %v, want %v", st.Modified, changed)
				}
				if !changed {
					return
				}
				if st.Mode == "i" {
					f.escape(t)
				}
				f.press(t, "u")
				if got := f.get(t).Lines; !slices.Equal(got, c.lines) {
					t.Errorf("after one u: %q, want the old text %q", got, c.lines)
				}
				f.press(t, "\x12")
				if got := f.get(t).Lines; !slices.Equal(got, want) {
					t.Errorf("after CTRL-R: %q, want %q", got, want)
				}
			})
		}
	}
}

func one(s string) []string { return []string{s} }

func TestFmtBold(t *testing.T) {
	f := newFx(t)
	// Byte offsets: "riječ" is r=0 i=1 j=2 e=3 č=4..5, so six bytes.
	f.runCases(t, []fcase{
		{name: "middle", lines: one("jedna riječ dva"), mode: "n", at: [2]int{1, 8}, kind: "bold",
			want: one("jedna **riječ** dva"), cur: [2]int{1, 10}},
		{name: "first char", lines: one("jedna riječ dva"), mode: "n", at: [2]int{1, 6}, kind: "bold",
			want: one("jedna **riječ** dva"), cur: [2]int{1, 8}},
		{name: "on the 2-byte char", lines: one("jedna riječ dva"), mode: "n", at: [2]int{1, 10}, kind: "bold",
			want: one("jedna **riječ** dva"), cur: [2]int{1, 12}},
		{name: "line start", lines: one("riječ dva"), mode: "n", at: [2]int{1, 0}, kind: "bold",
			want: one("**riječ** dva"), cur: [2]int{1, 2}},
		{name: "line end", lines: one("dva riječ"), mode: "n", at: [2]int{1, 8}, kind: "bold",
			want: one("dva **riječ**"), cur: [2]int{1, 10}},
		{name: "punctuation", lines: one("Pa, riječ, dva."), mode: "n", at: [2]int{1, 8}, kind: "bold",
			want: one("Pa, **riječ**, dva."), cur: [2]int{1, 10}},
		{name: "unwrap", lines: one("jedna **riječ** dva"), mode: "n", at: [2]int{1, 10}, kind: "bold",
			want: one("jedna riječ dva"), cur: [2]int{1, 8}},
		{name: "unwrap on the mark", lines: one("jedna **riječ** dva"), mode: "n", at: [2]int{1, 6}, kind: "bold",
			// The cursor is on a "*", which is not a word character: nothing to do.
			cur: [2]int{1, 6}},
		{name: "blank", lines: one("a  b"), mode: "n", at: [2]int{1, 2}, kind: "bold", cur: [2]int{1, 2}},
		{name: "empty line", lines: []string{"a", "", "b"}, mode: "n", at: [2]int{2, 0}, kind: "bold", cur: [2]int{2, 0}},
		{name: "on punctuation", lines: one("riječ, dva"), mode: "n", at: [2]int{1, 6}, kind: "bold", cur: [2]int{1, 6}},
		{name: "digits", lines: one("abc123 x"), mode: "n", at: [2]int{1, 4}, kind: "bold",
			want: one("**abc123** x"), cur: [2]int{1, 6}},
		{name: "underscore ends a word", lines: one("snake_case"), mode: "n", at: [2]int{1, 0}, kind: "bold",
			want: one("**snake**_case"), cur: [2]int{1, 2}},
		{name: "combining mark", lines: one("re\u0301s x"), mode: "n", at: [2]int{1, 0}, kind: "bold",
			want: one("**re\u0301s** x"), cur: [2]int{1, 2}},
		{name: "next to emoji", lines: one("🙂riječ🙂"), mode: "n", at: [2]int{1, 4}, kind: "bold",
			want: one("🙂**riječ**🙂"), cur: [2]int{1, 6}},
		{name: "on emoji", lines: one("a 🙂 b"), mode: "n", at: [2]int{1, 2}, kind: "bold", cur: [2]int{1, 2}},
		{name: "italic over bold", lines: one("**riječ**"), mode: "n", at: [2]int{1, 3}, kind: "italic",
			want: one("***riječ***"), cur: [2]int{1, 4}},
		{name: "bold over italic", lines: one("*riječ*"), mode: "n", at: [2]int{1, 2}, kind: "bold",
			want: one("***riječ***"), cur: [2]int{1, 4}},
		{name: "unwrap italic of both", lines: one("***riječ***"), mode: "n", at: [2]int{1, 4}, kind: "italic",
			want: one("**riječ**"), cur: [2]int{1, 3}},
		{name: "unwrap bold of both", lines: one("***riječ***"), mode: "n", at: [2]int{1, 4}, kind: "bold",
			want: one("*riječ*"), cur: [2]int{1, 2}},
		{name: "not the next word", lines: one("**a** b"), mode: "n", at: [2]int{1, 6}, kind: "bold",
			want: one("**a** **b**"), cur: [2]int{1, 8}},

		// Visual mode, characterwise.
		{name: "v part", lines: one("jedna riječ dva"), mode: "v", at: [2]int{1, 6}, to: [2]int{1, 10}, kind: "bold",
			want: one("jedna **riječ** dva"), cur: [2]int{1, 15}},
		{name: "v whole line", lines: one("jedna riječ"), mode: "v", at: [2]int{1, 0}, keys: "$", kind: "bold",
			want: one("**jedna riječ**"), cur: [2]int{1, 15}},
		{name: "v two lines", lines: []string{"prvi redak", "drugi redak"}, mode: "v", at: [2]int{1, 5}, to: [2]int{2, 4},
			kind: "bold", want: []string{"prvi **redak", "drugi** redak"}, cur: [2]int{2, 6}},
		{name: "v backwards", lines: one("jedna riječ dva"), mode: "v", at: [2]int{1, 10}, to: [2]int{1, 6}, kind: "bold",
			want: one("jedna **riječ** dva"), cur: [2]int{1, 15}},
		{name: "v with marks", lines: one("jedna **riječ** dva"), mode: "v", at: [2]int{1, 6}, to: [2]int{1, 14}, kind: "bold",
			want: one("jedna riječ dva"), cur: [2]int{1, 10}},
		{name: "v just inside marks", lines: one("jedna **riječ** dva"), mode: "v", at: [2]int{1, 8}, to: [2]int{1, 12}, kind: "bold",
			want: one("jedna riječ dva"), cur: [2]int{1, 10}},
		{name: "v trailing blank", lines: one("jedna riječ dva"), mode: "v", at: [2]int{1, 6}, to: [2]int{1, 12}, kind: "bold",
			want: one("jedna **riječ** dva"), cur: [2]int{1, 15}},
		{name: "v leading blank", lines: one("jedna riječ dva"), mode: "v", at: [2]int{1, 5}, to: [2]int{1, 10}, kind: "bold",
			want: one("jedna **riječ** dva"), cur: [2]int{1, 15}},
		{name: "v only blank", lines: one("a  b"), mode: "v", at: [2]int{1, 1}, to: [2]int{1, 2}, kind: "bold", cur: [2]int{1, 2}},
		{name: "v one emoji", lines: one("a 🙂 b"), mode: "v", at: [2]int{1, 2}, kind: "bold",
			want: one("a **🙂** b"), cur: [2]int{1, 9}},
		{name: "v combining", lines: one("xre\u0301 y"), mode: "v", at: [2]int{1, 1}, to: [2]int{1, 2}, kind: "bold",
			want: one("x**re\u0301** y"), cur: [2]int{1, 8}},
		{name: "v empty line", lines: []string{"a", "", "b"}, mode: "v", at: [2]int{2, 0}, kind: "bold", cur: [2]int{2, 0}},
		{name: "v italic over bold", lines: one("**riječ**"), mode: "v", at: [2]int{1, 2}, to: [2]int{1, 6}, kind: "italic",
			want: one("***riječ***"), cur: [2]int{1, 11}},
		{name: "v italic with both", lines: one("***riječ***"), mode: "v", at: [2]int{1, 3}, to: [2]int{1, 7}, kind: "italic",
			want: one("**riječ**"), cur: [2]int{1, 9}},
		{name: "v whole bold, italic", lines: one("**riječ**"), mode: "v", at: [2]int{1, 0}, keys: "$", kind: "italic",
			want: one("***riječ***"), cur: [2]int{1, 11}},

		// Visual mode, linewise: each line that has text, apart.
		{name: "V empty between", lines: []string{"prvi", "", "treći"}, mode: "V", at: [2]int{1, 0}, to: [2]int{3, 0}, kind: "bold",
			want: []string{"**prvi**", "", "**treći**"}, cur: [2]int{3, 9}},
		{name: "V indent", lines: []string{"  tekst  ", "x"}, mode: "V", at: [2]int{1, 0}, kind: "bold",
			want: []string{"  **tekst**  ", "x"}, cur: [2]int{1, 10}},
		{name: "V all wrapped", lines: []string{"**a**", "**b**"}, mode: "V", at: [2]int{1, 0}, to: [2]int{2, 0}, kind: "bold",
			want: []string{"a", "b"}, cur: [2]int{2, 0}},
		{name: "V some wrapped", lines: []string{"**a**", "b"}, mode: "V", at: [2]int{1, 0}, to: [2]int{2, 0}, kind: "bold",
			want: []string{"**a**", "**b**"}, cur: [2]int{2, 4}},
		{name: "V italic over bold", lines: []string{"**a**", "**b**"}, mode: "V", at: [2]int{1, 0}, to: [2]int{2, 0}, kind: "italic",
			want: []string{"***a***", "***b***"}, cur: [2]int{2, 6}},
		{name: "V only empty", lines: []string{"", ""}, mode: "V", at: [2]int{1, 0}, to: [2]int{2, 0}, kind: "bold", cur: [2]int{2, 0}},

		// Blockwise is refused: nothing changes and visual mode stays.
		{name: "block", lines: []string{"abc def", "abc def"}, mode: "b", at: [2]int{1, 0}, to: [2]int{2, 2}, kind: "bold",
			cur: [2]int{2, 2}, after: "\x16"},
	})
}

func TestFmtItalic(t *testing.T) {
	f := newFx(t)
	f.runCases(t, []fcase{
		{name: "word", lines: one("jedna riječ dva"), mode: "n", at: [2]int{1, 8}, kind: "italic",
			want: one("jedna *riječ* dva"), cur: [2]int{1, 9}},
		{name: "unwrap", lines: one("jedna *riječ* dva"), mode: "n", at: [2]int{1, 9}, kind: "italic",
			want: one("jedna riječ dva"), cur: [2]int{1, 8}},
		{name: "punctuation", lines: one("(riječ)"), mode: "n", at: [2]int{1, 3}, kind: "italic",
			want: one("(*riječ*)"), cur: [2]int{1, 4}},
		{name: "v", lines: one("jedna riječ dva"), mode: "v", at: [2]int{1, 6}, to: [2]int{1, 10}, kind: "italic",
			want: one("jedna *riječ* dva"), cur: [2]int{1, 13}},
		{name: "V", lines: []string{"a", "", "b"}, mode: "V", at: [2]int{1, 0}, to: [2]int{3, 0}, kind: "italic",
			want: []string{"*a*", "", "*b*"}, cur: [2]int{3, 2}},
	})
}

// TestFmtSpan: a word inside a bold or italic span, with the marks not at the
// word, takes the marks of the whole span off. Only the line of the cursor is
// looked at, and a line whose marks do not pair up is treated as before.
func TestFmtSpan(t *testing.T) {
	f := newFx(t)
	f.runCases(t, []fcase{
		{name: "bold last word", lines: one("**jedna riječ**"), mode: "n", at: [2]int{1, 9}, kind: "bold",
			want: one("jedna riječ"), cur: [2]int{1, 7}},
		{name: "bold first word", lines: one("**jedna riječ**"), mode: "n", at: [2]int{1, 3}, kind: "bold",
			want: one("jedna riječ"), cur: [2]int{1, 1}},
		{name: "italic", lines: one("*jedna riječ*"), mode: "n", at: [2]int{1, 8}, kind: "italic",
			want: one("jedna riječ"), cur: [2]int{1, 7}},
		{name: "both, bold off", lines: one("***a b***"), mode: "n", at: [2]int{1, 3}, kind: "bold",
			want: one("*a b*"), cur: [2]int{1, 1}},
		{name: "both, bold off at b", lines: one("***a b***"), mode: "n", at: [2]int{1, 5}, kind: "bold",
			want: one("*a b*"), cur: [2]int{1, 3}},
		{name: "both, italic off", lines: one("***a b***"), mode: "n", at: [2]int{1, 3}, kind: "italic",
			want: one("**a b**"), cur: [2]int{1, 2}},
		{name: "nested italic", lines: one("**a *b* c**"), mode: "n", at: [2]int{1, 8}, kind: "bold",
			want: one("a *b* c"), cur: [2]int{1, 6}},
		{name: "second span", lines: one("**a** b **c d**"), mode: "n", at: [2]int{1, 12}, kind: "bold",
			want: one("**a** b c d"), cur: [2]int{1, 10}},
		{name: "after a bullet", lines: one("* **a b**"), mode: "n", at: [2]int{1, 6}, kind: "bold",
			want: one("* a b"), cur: [2]int{1, 4}},
		{name: "italic in bold wraps", lines: one("**jedna riječ**"), mode: "n", at: [2]int{1, 9}, kind: "italic",
			want: one("**jedna *riječ***"), cur: [2]int{1, 10}},
		{name: "bold in italic wraps", lines: one("*jedna riječ*"), mode: "n", at: [2]int{1, 8}, kind: "bold",
			want: one("*jedna **riječ***"), cur: [2]int{1, 10}},

		// Marks that do not pair up: the word is wrapped, as before.
		{name: "unpaired", lines: one("**jedna riječ"), mode: "n", at: [2]int{1, 9}, kind: "bold",
			want: one("**jedna **riječ**"), cur: [2]int{1, 11}},
		{name: "unpaired later", lines: one("**a** x **b"), mode: "n", at: [2]int{1, 6}, kind: "bold",
			want: one("**a** **x** **b"), cur: [2]int{1, 8}},
		{name: "unequal", lines: one("**jedna riječ*"), mode: "n", at: [2]int{1, 9}, kind: "bold",
			want: one("**jedna **riječ***"), cur: [2]int{1, 11}},
		{name: "times", lines: one("2 * 3 * 4"), mode: "n", at: [2]int{1, 4}, kind: "bold",
			want: one("2 * **3** * 4"), cur: [2]int{1, 6}},
		// The span is on another line: only the line of the cursor is looked at.
		{name: "other line", lines: []string{"**jedna", "riječ**"}, mode: "n", at: [2]int{1, 3}, kind: "italic",
			want: []string{"***jedna*", "riječ**"}, cur: [2]int{1, 4}},
	})
}

// TestFmtApos: an apostrophe between two letters is part of the word, one at its
// edge is not; a "_" at the edge is not either. A script without spaces is no
// word at all.
func TestFmtApos(t *testing.T) {
	f := newFx(t)
	f.runCases(t, []fcase{
		{name: "dont", lines: one("don't stop"), mode: "n", at: [2]int{1, 1}, kind: "bold",
			want: one("**don't** stop"), cur: [2]int{1, 3}},
		{name: "dont, after it", lines: one("don't stop"), mode: "n", at: [2]int{1, 4}, kind: "bold",
			want: one("**don't** stop"), cur: [2]int{1, 6}},
		{name: "on the apostrophe", lines: one("don't stop"), mode: "n", at: [2]int{1, 3}, kind: "bold",
			want: one("**don't** stop"), cur: [2]int{1, 5}},
		{name: "typographic", lines: one("don’t stop"), mode: "n", at: [2]int{1, 0}, kind: "bold",
			want: one("**don’t** stop"), cur: [2]int{1, 2}},
		{name: "italic", lines: one("don't stop"), mode: "n", at: [2]int{1, 1}, kind: "italic",
			want: one("*don't* stop"), cur: [2]int{1, 2}},
		{name: "unwrap", lines: one("**don't** stop"), mode: "n", at: [2]int{1, 4}, kind: "bold",
			want: one("don't stop"), cur: [2]int{1, 2}},
		{name: "two", lines: one("rock'n'roll"), mode: "n", at: [2]int{1, 0}, kind: "bold",
			want: one("**rock'n'roll**"), cur: [2]int{1, 2}},
		{name: "edge", lines: one("'riječ' x"), mode: "n", at: [2]int{1, 3}, kind: "bold",
			want: one("'**riječ**' x"), cur: [2]int{1, 5}},
		{name: "edge typographic", lines: one("’riječ’ x"), mode: "n", at: [2]int{1, 5}, kind: "bold",
			want: one("’**riječ**’ x"), cur: [2]int{1, 7}},
		{name: "on the edge one", lines: one("'riječ' x"), mode: "n", at: [2]int{1, 0}, kind: "bold",
			cur: [2]int{1, 0}},
		{name: "alone", lines: one("a ' b"), mode: "n", at: [2]int{1, 2}, kind: "bold", cur: [2]int{1, 2}},
		{name: "underscore at the edge", lines: one("_riječ_"), mode: "n", at: [2]int{1, 3}, kind: "bold",
			want: one("_**riječ**_"), cur: [2]int{1, 5}},
		{name: "no spaces in the script", lines: one("x 漢字 y"), mode: "n", at: [2]int{1, 2}, kind: "bold",
			cur: [2]int{1, 2}},
	})
}

// TestFmtSel: the end of a characterwise selection follows 'selection'.
// "exclusive" leaves the character at the later end out, "inclusive" and "old"
// take it in; one character selected counts as inclusive in every case.
func TestFmtSel(t *testing.T) {
	f := newFx(t)
	text := one("jedna riječ dva")
	for _, sel := range []string{"inclusive", "old", "exclusive"} {
		t.Run(sel, func(t *testing.T) {
			f.lua(t, nil, `vim.o.selection = ...`, sel)
			t.Cleanup(func() { f.lua(t, nil, `vim.o.selection = "inclusive"`) })
			whole, wholeCur := one("jedna **riječ** dva"), [2]int{1, 15}
			part, partCur := whole, wholeCur
			if sel == "exclusive" {
				part, partCur = one("jedna **rije**č dva"), [2]int{1, 13}
			}
			f.runCases(t, []fcase{
				{name: "v4l", lines: text, mode: "v", at: [2]int{1, 6}, keys: "4l", kind: "bold", want: part, cur: partCur},
				{name: "backwards", lines: text, mode: "v", at: [2]int{1, 10}, keys: "4h", kind: "bold", want: part, cur: partCur},
				{name: "one char", lines: text, mode: "v", at: [2]int{1, 6}, kind: "bold",
					want: one("jedna **r**iječ dva"), cur: [2]int{1, 10}},
				{name: "two lines", lines: []string{"prvi redak", "drugi redak"}, mode: "v", at: [2]int{1, 5}, to: [2]int{2, 4},
					kind: "bold", want: map[bool][]string{
						false: {"prvi **redak", "drugi** redak"},
						true:  {"prvi **redak", "drug**i redak"},
					}[sel == "exclusive"], cur: map[bool][2]int{false: {2, 6}, true: {2, 5}}[sel == "exclusive"]},
				{name: "to the end", lines: one("jedna riječ"), mode: "v", at: [2]int{1, 0}, keys: "$", kind: "bold",
					want: one("**jedna riječ**"), cur: [2]int{1, 15}},
				{name: "V", lines: []string{"a", "b"}, mode: "V", at: [2]int{1, 0}, to: [2]int{2, 0}, kind: "bold",
					want: []string{"**a**", "**b**"}, cur: [2]int{2, 4}},
			})
		})
	}
}

func TestFmtBoldIns(t *testing.T) {
	f := newFx(t)
	f.runCases(t, []fcase{
		{name: "empty buffer", lines: one(""), mode: "i", at: [2]int{1, 0}, kind: "bold",
			want: one("****"), cur: [2]int{1, 2}},
		{name: "middle", lines: one("ab cd"), mode: "i", at: [2]int{1, 3}, kind: "bold",
			want: one("ab ****cd"), cur: [2]int{1, 5}},
		{name: "line end", lines: one("ab"), mode: "i", at: [2]int{1, 2}, kind: "bold",
			want: one("ab****"), cur: [2]int{1, 4}},
		{name: "line start", lines: one("ab"), mode: "i", at: [2]int{1, 0}, kind: "bold",
			want: one("****ab"), cur: [2]int{1, 2}},
		{name: "after 2-byte chars", lines: one("šč"), mode: "i", at: [2]int{1, 4}, kind: "bold",
			want: one("šč****"), cur: [2]int{1, 6}},
		{name: "after emoji", lines: one("🙂"), mode: "i", at: [2]int{1, 4}, kind: "bold",
			want: one("🙂****"), cur: [2]int{1, 6}},
		{name: "italic", lines: one("ab"), mode: "i", at: [2]int{1, 2}, kind: "italic",
			want: one("ab**"), cur: [2]int{1, 3}},
		// Marks that were typed by hand are not "just inserted": a pair goes in.
		{name: "not stepped over", lines: one("a**b"), mode: "i", at: [2]int{1, 1}, kind: "bold",
			want: one("a****" + "**b"), cur: [2]int{1, 3}},
	})
}

// typed sends text in insert mode and waits for it.
func (f *fx) typed(t *testing.T, text string) {
	t.Helper()
	f.press(t, text)
}

func TestFmtInsSeq(t *testing.T) {
	f := newFx(t)
	f.reset(t, one(""))
	f.press(t, "i")

	// bold, text, bold: bold text, and the typing goes on after it.
	if err := f.format(t, "bold"); err != nil {
		t.Fatal(err)
	}
	f.typed(t, "tekst")
	if err := f.format(t, "bold"); err != nil {
		t.Fatal(err)
	}
	f.typed(t, " dalje")
	st := f.get(t)
	if want := one("**tekst** dalje"); !slices.Equal(st.Lines, want) || st.Mode != "i" || st.Col != 15 {
		t.Fatalf("after bold, text, bold, text: %+v, want %q at col 15 in insert mode", st, want)
	}

	// What one "u" does after that: the whole stay in insert mode is one change
	// for Neovim, and the formatting is part of it. Recorded here so that a change
	// of that (an undo step of its own for each action) is noticed.
	f.escape(t)
	f.press(t, "u")
	if got := f.get(t).Lines; !slices.Equal(got, one("")) {
		t.Errorf("one u after bold, text, bold, text gives %q; this documents that it takes the whole stay in insert mode", got)
	}
	f.press(t, "\x12")
	if got := f.get(t).Lines; !slices.Equal(got, one("**tekst** dalje")) {
		t.Errorf("CTRL-R gives %q", got)
	}

	// Nested: italic inside bold, closed in turn.
	f.reset(t, one(""))
	f.press(t, "i")
	for _, step := range []struct{ kind, text string }{
		{"bold", "a "}, {"italic", "b"}, {"italic", " c"}, {"bold", " d"},
	} {
		if err := f.format(t, step.kind); err != nil {
			t.Fatal(err)
		}
		f.typed(t, step.text)
	}
	if got := f.get(t).Lines; !slices.Equal(got, one("**a *b* c** d")) {
		t.Errorf("nested = %q, want %q", got, "**a *b* c** d")
	}

	// Leaving insert mode forgets the open pair: in a new stay a mark in front of
	// the cursor is not stepped over.
	f.reset(t, one(""))
	f.press(t, "i")
	if err := f.format(t, "bold"); err != nil {
		t.Fatal(err)
	}
	f.escape(t)
	f.press(t, "i")
	f.setCursor(t, [2]int{1, 2})
	if err := f.format(t, "bold"); err != nil {
		t.Fatal(err)
	}
	if got := f.get(t); !slices.Equal(got.Lines, one("********")) || got.Col != 4 {
		t.Errorf("a pair left in an earlier stay was stepped over: %+v", got)
	}

	// Deleting the closing mark takes the pair away: nothing to step over.
	f.reset(t, one(""))
	f.press(t, "i")
	if err := f.format(t, "bold"); err != nil {
		t.Fatal(err)
	}
	f.lua(t, nil, `vim.api.nvim_buf_set_text(0, 0, 2, 0, 4, { "" })`)
	if err := f.format(t, "bold"); err != nil {
		t.Fatal(err)
	}
	if got := f.get(t); !slices.Equal(got.Lines, one("******")) || got.Col != 4 {
		t.Errorf("after deleting the closing mark: %+v, want a new pair", got)
	}
}

// TestFmtUndo: each action is a step of its own for u, one by one.
func TestFmtUndo(t *testing.T) {
	f := newFx(t)
	f.reset(t, []string{"jedan dva"})
	f.press(t, formatKeys["bold"])
	f.setCursor(t, [2]int{1, 11})
	f.press(t, formatKeys["italic"])
	f.press(t, formatKeys["heading"])
	if got := f.get(t).Lines; !slices.Equal(got, one("# **jedan** *dva*")) {
		t.Fatalf("after three actions: %q", got)
	}
	for _, want := range []string{"**jedan** *dva*", "**jedan** dva", "jedan dva"} {
		f.press(t, "u")
		if got := f.get(t).Lines; !slices.Equal(got, one(want)) {
			t.Fatalf("after u: %q, want %q", got, want)
		}
	}
}

func TestFmtHead(t *testing.T) {
	f := newFx(t)
	f.runCases(t, []fcase{
		{name: "none to 1", lines: one("tekst"), mode: "n", at: [2]int{1, 2}, kind: "heading",
			want: one("# tekst"), cur: [2]int{1, 4}},
		{name: "1 to 2", lines: one("# tekst"), mode: "n", at: [2]int{1, 4}, kind: "heading",
			want: one("## tekst"), cur: [2]int{1, 5}},
		{name: "2 to 3", lines: one("## tekst"), mode: "n", at: [2]int{1, 4}, kind: "heading",
			want: one("### tekst"), cur: [2]int{1, 5}},
		{name: "3 to none", lines: one("### tekst"), mode: "n", at: [2]int{1, 6}, kind: "heading",
			want: one("tekst"), cur: [2]int{1, 2}},
		{name: "4 to none", lines: one("#### tekst"), mode: "n", at: [2]int{1, 5}, kind: "heading",
			want: one("tekst"), cur: [2]int{1, 0}},
		{name: "6 to none", lines: one("###### tekst"), mode: "n", at: [2]int{1, 8}, kind: "heading",
			want: one("tekst"), cur: [2]int{1, 1}},
		{name: "7 is no heading", lines: one("####### x"), mode: "n", at: [2]int{1, 8}, kind: "heading",
			want: one("# ####### x"), cur: [2]int{1, 10}},
		{name: "no space is no heading", lines: one("#oznaka"), mode: "n", at: [2]int{1, 3}, kind: "heading",
			want: one("# #oznaka"), cur: [2]int{1, 5}},
		{name: "cursor on the mark", lines: one("## tekst"), mode: "n", at: [2]int{1, 0}, kind: "heading",
			want: one("### tekst"), cur: [2]int{1, 4}},
		{name: "diacritics", lines: one("čšž đ"), mode: "n", at: [2]int{1, 6}, kind: "heading",
			want: one("# čšž đ"), cur: [2]int{1, 8}},
		{name: "indent", lines: one("  tekst"), mode: "n", at: [2]int{1, 4}, kind: "heading",
			want: one("  # tekst"), cur: [2]int{1, 6}},
		{name: "indent, 1 to 2", lines: one("  # tekst"), mode: "n", at: [2]int{1, 6}, kind: "heading",
			want: one("  ## tekst"), cur: [2]int{1, 7}},
		{name: "cursor in the indent", lines: one("  tekst"), mode: "n", at: [2]int{1, 1}, kind: "heading",
			want: one("  # tekst"), cur: [2]int{1, 1}},
		{name: "bullet", lines: one("- tekst"), mode: "n", at: [2]int{1, 4}, kind: "heading",
			want: one("# tekst"), cur: [2]int{1, 4}},
		{name: "star bullet", lines: one("* tekst"), mode: "n", at: [2]int{1, 2}, kind: "heading",
			want: one("# tekst"), cur: [2]int{1, 2}},
		{name: "indented bullet", lines: one("  - tekst"), mode: "n", at: [2]int{1, 6}, kind: "heading",
			want: one("  # tekst"), cur: [2]int{1, 6}},
		{name: "numbered keeps its number", lines: one("1. uvod"), mode: "n", at: [2]int{1, 3}, kind: "heading",
			want: one("# 1. uvod"), cur: [2]int{1, 5}},
		{name: "emphasis is no bullet", lines: one("**a** b"), mode: "n", at: [2]int{1, 3}, kind: "heading",
			want: one("# **a** b"), cur: [2]int{1, 5}},
		{name: "empty line", lines: []string{"a", "", "b"}, mode: "n", at: [2]int{2, 0}, kind: "heading",
			want: []string{"a", "# ", "b"}, cur: [2]int{2, 1}},
		{name: "only the mark", lines: one("#"), mode: "n", at: [2]int{1, 0}, kind: "heading",
			want: one("## "), cur: [2]int{1, 2}},
		{name: "3 with empty text", lines: one("### "), mode: "n", at: [2]int{1, 0}, kind: "heading",
			want: one(""), cur: [2]int{1, 0}},

		{name: "insert", lines: one("tekst"), mode: "i", at: [2]int{1, 2}, kind: "heading",
			want: one("# tekst"), cur: [2]int{1, 4}},
		{name: "insert, empty line", lines: one(""), mode: "i", at: [2]int{1, 0}, kind: "heading",
			want: one("# "), cur: [2]int{1, 2}},
		{name: "insert, line end", lines: one("ab"), mode: "i", at: [2]int{1, 2}, kind: "heading",
			want: one("# ab"), cur: [2]int{1, 4}},
		{name: "insert, 3 to none", lines: one("### ab"), mode: "i", at: [2]int{1, 6}, kind: "heading",
			want: one("ab"), cur: [2]int{1, 2}},

		{name: "v, levels differ", lines: []string{"a", "# b", "## c", "### d", "#### e"}, mode: "v", at: [2]int{1, 0}, to: [2]int{5, 0},
			kind: "heading", want: []string{"# a", "## b", "### c", "d", "e"}, cur: [2]int{5, 0}},
		{name: "V, empty between", lines: []string{"a", "", "b"}, mode: "V", at: [2]int{1, 0}, to: [2]int{3, 0}, kind: "heading",
			want: []string{"# a", "", "# b"}, cur: [2]int{3, 2}},
		{name: "V, one empty line", lines: []string{"a", ""}, mode: "V", at: [2]int{2, 0}, kind: "heading",
			want: []string{"a", "# "}, cur: [2]int{2, 1}},
		{name: "v, one line", lines: one("tekst"), mode: "v", at: [2]int{1, 1}, to: [2]int{1, 3}, kind: "heading",
			want: one("# tekst"), cur: [2]int{1, 5}},
		{name: "b", lines: []string{"a", "b"}, mode: "b", at: [2]int{1, 0}, to: [2]int{2, 0}, kind: "heading",
			want: []string{"# a", "# b"}, cur: [2]int{2, 2}},
	})
}

func TestFmtList(t *testing.T) {
	f := newFx(t)
	f.runCases(t, []fcase{
		{name: "add", lines: one("tekst"), mode: "n", at: [2]int{1, 2}, kind: "list",
			want: one("- tekst"), cur: [2]int{1, 4}},
		{name: "remove", lines: one("- tekst"), mode: "n", at: [2]int{1, 4}, kind: "list",
			want: one("tekst"), cur: [2]int{1, 2}},
		{name: "remove star", lines: one("* tekst"), mode: "n", at: [2]int{1, 4}, kind: "list",
			want: one("tekst"), cur: [2]int{1, 2}},
		{name: "remove plus", lines: one("+ tekst"), mode: "n", at: [2]int{1, 4}, kind: "list",
			want: one("tekst"), cur: [2]int{1, 2}},
		{name: "cursor on the mark", lines: one("- tekst"), mode: "n", at: [2]int{1, 0}, kind: "list",
			want: one("tekst"), cur: [2]int{1, 0}},
		{name: "numbered stays", lines: one("1. tekst"), mode: "n", at: [2]int{1, 4}, kind: "list", cur: [2]int{1, 4}},
		{name: "numbered paren stays", lines: one("2) tekst"), mode: "n", at: [2]int{1, 4}, kind: "list", cur: [2]int{1, 4}},
		{name: "heading stays", lines: one("# Naslov"), mode: "n", at: [2]int{1, 4}, kind: "list", cur: [2]int{1, 4}},
		{name: "indented add", lines: one("  tekst"), mode: "n", at: [2]int{1, 4}, kind: "list",
			want: one("  - tekst"), cur: [2]int{1, 6}},
		{name: "indented remove", lines: one("    - tekst"), mode: "n", at: [2]int{1, 8}, kind: "list",
			want: one("    tekst"), cur: [2]int{1, 6}},
		{name: "empty line", lines: []string{"a", "", "b"}, mode: "n", at: [2]int{2, 0}, kind: "list", cur: [2]int{2, 0}},
		{name: "emphasis is no bullet", lines: one("*a* b"), mode: "n", at: [2]int{1, 1}, kind: "list",
			want: one("- *a* b"), cur: [2]int{1, 3}},
		{name: "diacritics", lines: one("čšž"), mode: "n", at: [2]int{1, 2}, kind: "list",
			want: one("- čšž"), cur: [2]int{1, 4}},
		{name: "insert", lines: one("tekst"), mode: "i", at: [2]int{1, 5}, kind: "list",
			want: one("- tekst"), cur: [2]int{1, 7}},
		{name: "insert, remove", lines: one("- tekst"), mode: "i", at: [2]int{1, 7}, kind: "list",
			want: one("tekst"), cur: [2]int{1, 5}},
		{name: "insert, empty line", lines: one(""), mode: "i", at: [2]int{1, 0}, kind: "list",
			cur: [2]int{1, 0}},

		{name: "v, empty between", lines: []string{"a", "", "b"}, mode: "V", at: [2]int{1, 0}, to: [2]int{3, 0}, kind: "list",
			want: []string{"- a", "", "- b"}, cur: [2]int{3, 2}},
		{name: "v, all items", lines: []string{"- a", "", "* b"}, mode: "V", at: [2]int{1, 0}, to: [2]int{3, 0}, kind: "list",
			want: []string{"a", "", "b"}, cur: [2]int{3, 0}},
		{name: "v, mixed", lines: []string{"- a", "b", "1. c", "# d"}, mode: "v", at: [2]int{1, 0}, to: [2]int{4, 0}, kind: "list",
			want: []string{"- a", "- b", "1. c", "# d"}, cur: [2]int{4, 0}},
		{name: "v, only skipped", lines: []string{"1. a", "# b", ""}, mode: "V", at: [2]int{1, 0}, to: [2]int{3, 0}, kind: "list",
			cur: [2]int{3, 0}},
		{name: "b", lines: []string{"a", "b"}, mode: "b", at: [2]int{1, 0}, to: [2]int{2, 0}, kind: "list",
			want: []string{"- a", "- b"}, cur: [2]int{2, 2}},
	})
}

// TestFmtReg: no register, mark, search pattern or message changes.
func TestFmtReg(t *testing.T) {
	cp := newClipProc(t)
	f := &fx{cp.Process}
	f.lua(t, nil, `
		vim.g.barrier = 0
		vim.keymap.set({ "n", "x", "i" }, "<C-_>", function() vim.g.barrier = vim.g.barrier + 1 end)`)

	const snapshot = `
		local out = {}
		for _, r in ipairs({ '"', "0", "1", "-", "a", "+", "/", ":" }) do
			out[#out + 1] = r .. "=" .. vim.fn.getreg(r) .. "|" .. vim.fn.getregtype(r)
		end
		out[#out + 1] = "lt=" .. vim.inspect(vim.api.nvim_buf_get_mark(0, "<"))
		out[#out + 1] = "gt=" .. vim.inspect(vim.api.nvim_buf_get_mark(0, ">"))
		out[#out + 1] = "errmsg=" .. vim.v.errmsg
		out[#out + 1] = "msgs=" .. vim.fn.execute("messages")
		out[#out + 1] = "hlsearch=" .. vim.v.hlsearch
		return out`
	prepare := `
		vim.fn.setreg('"', "quote"); vim.fn.setreg("0", "zero"); vim.fn.setreg("a", "aaa")
		vim.fn.setreg("+", "clip"); vim.fn.setreg("/", "pat")
		vim.api.nvim_buf_set_mark(0, "<", 3, 1, {}); vim.api.nvim_buf_set_mark(0, ">", 3, 2, {})
		vim.v.errmsg = ""`

	for _, kind := range []string{"bold", "italic", "heading", "list"} {
		for _, mode := range []string{"n", "i", "v", "V"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				f.reset(t, []string{"jedan dva", "tri", "", "četiri pet"})
				f.lua(t, nil, prepare)
				f.setCursor(t, [2]int{1, 2})
				if mode != "n" {
					f.press(t, mode)
					if mode == "v" || mode == "V" {
						f.setCursor(t, [2]int{2, 1})
					}
				}
				// In visual mode '< and '> are set by leaving it; take them as that
				// leaves them, and compare the rest.
				var before, after []string
				f.lua(t, &before, snapshot)
				if err := f.format(t, kind); err != nil {
					t.Fatal(err)
				}
				f.lua(t, &after, snapshot)
				for i := range before {
					if strings.HasPrefix(before[i], "lt=") || strings.HasPrefix(before[i], "gt=") {
						if mode == "v" || mode == "V" || mode == "i" {
							continue
						}
					}
					if before[i] != after[i] {
						t.Errorf("%s changed:\n before %q\n after  %q", strings.SplitN(before[i], "=", 2)[0], before[i], after[i])
					}
				}
				if got := cp.clip(t); got == nil || strings.Join(got.Lines, "") != "clip" {
					t.Errorf("clipboard = %+v", got)
				}
			})
		}
	}
}

// TestFmtRO: a buffer that cannot be changed is left alone, silently.
func TestFmtRO(t *testing.T) {
	p, _ := newProc(t, nil)
	f := &fx{p}
	f.lua(t, nil, `
		vim.g.barrier = 0
		vim.keymap.set({ "n", "x", "i" }, "<C-_>", function() vim.g.barrier = vim.g.barrier + 1 end)`)
	ctx := testCtx(t)
	path := mkFile(t, tempDir(t), "a.md", "jedan dva\ntri\n")
	if err := p.OpenFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := p.SetReadOnly(ctx, path, true); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"bold", "italic", "heading", "list"} {
		for _, mode := range []string{"n", "i", "v", "V", "b"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				f.escape(t)
				f.setCursor(t, [2]int{1, 2})
				switch mode {
				case "i", "v", "V":
					f.press(t, mode)
				case "b":
					f.press(t, "\x16")
				}
				want := f.get(t)
				if err := f.format(t, kind); err != nil {
					t.Errorf("Format: %v, want nil", err)
				}
				if got := f.get(t); got.Mode != want.Mode || !slices.Equal(got.Lines, []string{"jedan dva", "tri"}) ||
					got.Modified || got.Row != want.Row || got.Col != want.Col {
					t.Errorf("after Format: %+v, before %+v", got, want)
				}
				if mode != "i" {
					f.press(t, formatKeys[kind])
					if got := f.get(t); got.Mode != want.Mode || !slices.Equal(got.Lines, []string{"jedan dva", "tri"}) || got.Modified {
						t.Errorf("after the mapping: %+v, before %+v", got, want)
					}
				}
			})
		}
	}
}

// TestFmtErr: errors of Format.
func TestFmtErr(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)

	for _, kind := range []string{"", "Bold", "bold ", "underline", "bold\n"} {
		if err := p.Format(ctx, kind); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("Format(%q) = %v, want ErrUnknownFormat", kind, err)
		}
	}

	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	// An unknown kind is refused before anything else, a known one needs Neovim.
	if err := p.Format(ctx, "bold"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Format after Close = %v, want ErrNotRunning", err)
	}
	if err := p.Format(ctx, "underline"); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("Format(underline) after Close = %v, want ErrUnknownFormat", err)
	}
}

// fastMode is the mode as nvim_get_mode answers it, which it does also while
// Neovim waits at a prompt and cannot run Lua.
func (f *fx) fastMode(t *testing.T) string {
	t.Helper()
	var mode string
	err := f.call(testCtx(t), "mode", func(nv *nvimrpc.Nvim) error {
		m, err := nv.Mode()
		if err == nil {
			mode = m.Mode
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return mode
}

// TestFmtWait: Neovim in a mode that waits for input. Format returns in time,
// the text and the mode are the same, and nothing is asked on the screen.
func TestFmtWait(t *testing.T) {
	f := newFx(t)
	lines := []string{"jedan dva", "tri"}
	tests := []struct {
		name string
		keys string
		mode string // what mode() starts with while waiting
	}{
		{"cmdline", ":", "c"},
		{"operator", "d", "no"},
		{"prompt", ":echo repeat('x', 3000)\r", "r"},
		{"replace", "R", "R"},
		{"select", "gh", "s"},
	}
	for _, tc := range tests {
		for _, kind := range []string{"bold", "italic", "heading", "list"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				f.reset(t, lines)
				f.setCursor(t, [2]int{1, 2})
				f.write(t, tc.keys)
				eventually(t, "mode "+tc.mode, func() bool { return strings.HasPrefix(f.fastMode(t), tc.mode) })
				waiting := f.fastMode(t)
				start := time.Now()
				if err := f.format(t, kind); err != nil {
					t.Errorf("Format: %v, want nil", err)
				}
				if d := time.Since(start); d > 2*time.Second {
					t.Errorf("Format took %v", d)
				}
				if got := f.fastMode(t); got != waiting {
					t.Errorf("mode after Format = %q, want %q", got, waiting)
				}
				// Away from the state again, for the next case. A prompt is left with
				// <Esc>, and Neovim answers Lua calls again only after that.
				f.escape(t)
				if st := f.get(t); !slices.Equal(st.Lines, lines) || st.Modified {
					t.Errorf("after Format: %+v, want the same text", st)
				}
			})
		}
	}
}

// TestFmtEvt: the formatting is a change like typing: the preview is told and
// the buffer is modified.
func TestFmtEvt(t *testing.T) {
	p, _ := newProc(t, nil)
	f := &fx{p}
	ctx := testCtx(t)
	p.setBuffer(t, "jedan dva")
	p.lua(t, nil, `vim.bo.modified = false`)
	if err := p.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	settled(t, p)

	for _, mode := range []string{"n", "i"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "i" {
				p.write(t, "i")
				eventually(t, "insert mode", func() bool { return p.mode(t) == "i" })
			}
			if err := f.format(t, "bold"); err != nil {
				t.Fatal(err)
			}
			if n := changedCount(settled(t, p)); n == 0 {
				t.Errorf("no EventChanged after Format in mode %q", mode)
			}
			if !f.get(t).Modified {
				t.Errorf("the buffer is not modified after Format in mode %q", mode)
			}
		})
	}
}
