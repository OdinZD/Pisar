package nvim

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Tests of the writing defaults in nvim/init.lua: prose options, movement by
// display rows, spelling, and the two switches. They run the repository's own
// init.lua in a real Neovim, with the dictionary the application ships and no
// network.

const proseDictionary = "hr.utf-8.spl"

// Test names are short on purpose, as in nvim_test.go: t.TempDir() embeds the
// name, and the socket path inside it must fit sun_path. The socket name
// carries a counter over all tests of the package, so a test that runs late has
// a longer one. This file is named so that it sorts last: its tests start
// dozens of Neovims, which would otherwise push the counter to three digits
// for the tests of the files after it (readonly_test.go has a name that fits
// only just).

// proseOpts says how newProse starts Neovim.
type proseOpts struct {
	cols, rows  uint16
	noDict      bool   // do not put hr.utf-8.spl where Neovim looks for it
	cutDict     bool   // put only the first half of it there
	words       string // PISAR_SPELLFILE; "" leaves the variable unset
	colorscheme bool   // install the generator's sample colorscheme
	user        string // PISAR_USER_LUA; "" leaves the variable unset
}

// envValue returns the value of key in a list of "K=V" entries.
func envValue(env []string, key string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v
		}
	}
	return ""
}

// newProse starts Neovim like the application does for the writing defaults:
// the dictionary is in the spell folder of the configuration, and the words
// file (if any) is named in PISAR_SPELLFILE.
func newProse(t *testing.T, o proseOpts) (*Process, *output) {
	t.Helper()
	return newProc(t, func(opts *Options) {
		if o.cols > 0 {
			opts.Cols = o.cols
		}
		if o.rows > 0 {
			opts.Rows = o.rows
		}
		appCfg := filepath.Join(envValue(opts.Env, "XDG_CONFIG_HOME"), opts.AppName)
		if !o.noDict {
			dict, err := os.ReadFile(filepath.Join("..", "..", "nvim", "spell", proseDictionary))
			if err != nil {
				t.Fatal(err)
			}
			if o.cutDict {
				dict = dict[:len(dict)/2]
			}
			spellDir := filepath.Join(appCfg, "spell")
			if err := os.MkdirAll(spellDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(spellDir, proseDictionary), dict, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if o.colorscheme {
			cs, err := os.ReadFile(filepath.Join("..", "..", "testdata", "colorscheme", "pisar.lua"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(appCfg, "colors"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(appCfg, "colors", "pisar.lua"), cs, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if o.words != "" {
			opts.Env = append(opts.Env, "PISAR_SPELLFILE="+o.words)
		}
		if o.user != "" {
			opts.Env = append(opts.Env, "PISAR_USER_LUA="+o.user)
		}
	})
}

// wordsFile is the path of a user's words file in a folder of its own, outside
// anything Neovim's configuration holds. The folder exists; the file does not.
func wordsFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(tempDir(t), "spell", "pisar.utf-8.add")
}

func mkWordsDir(t *testing.T, words string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(words), 0o700); err != nil {
		t.Fatal(err)
	}
}

// lua runs a chunk in Neovim and decodes its result.
func (p *Process) lua(t *testing.T, result interface{}, code string, args ...interface{}) {
	t.Helper()
	if err := p.execLua(testCtx(t), "test", result, code, args...); err != nil {
		t.Fatal(err)
	}
}

// proseState is what the tests read about the editing window.
type proseState struct {
	Mode          string
	Row, Col      int // cursor: row 1-based, byte column
	Winline       int // screen row of the cursor inside the window, 1-based
	Pum           bool
	Wrap          bool
	Linebreak     bool
	Breakindent   bool
	Number        bool
	Relativenumb  bool
	Spell         bool
	Spelllang     string
	Spellfile     string
	Spellcapcheck string
	Laststatus    int
	Cmdheight     int
	Hidden        bool
	Shortmess     string
	Blocking      bool
}

func (p *Process) state(t *testing.T) proseState {
	t.Helper()
	var st proseState
	p.lua(t, &st, `
		local pos = vim.api.nvim_win_get_cursor(0)
		local m = vim.api.nvim_get_mode()
		return {
			Mode = m.mode, Blocking = m.blocking,
			Row = pos[1], Col = pos[2], Winline = vim.fn.winline(),
			Pum = vim.fn.pumvisible() == 1,
			Wrap = vim.wo.wrap, Linebreak = vim.wo.linebreak, Breakindent = vim.wo.breakindent,
			Number = vim.wo.number, Relativenumb = vim.wo.relativenumber,
			Spell = vim.wo.spell, Spelllang = vim.bo.spelllang, Spellfile = vim.bo.spellfile,
			Spellcapcheck = vim.bo.spellcapcheck,
			Laststatus = vim.o.laststatus, Cmdheight = vim.o.cmdheight,
			Hidden = vim.o.hidden, Shortmess = vim.o.shortmess,
		}`)
	return st
}

// press sends keys and waits until the cursor or the mode differs from before:
// keys travel through the PTY asynchronously, so a state read right after the
// write could still be the old one. It returns the state after the move.
func (p *Process) press(t *testing.T, keys string) proseState {
	t.Helper()
	before := p.state(t)
	p.write(t, keys)
	var st proseState
	eventually(t, "the keys "+strings.ReplaceAll(keys, "\x1b", "<Esc>")+" to be processed", func() bool {
		st = p.state(t)
		return st.Mode != before.Mode || st.Row != before.Row || st.Col != before.Col || st.Winline != before.Winline
	})
	return st
}

func checkProseOptions(t *testing.T, label string, st proseState, words string) {
	t.Helper()
	checks := []struct {
		name string
		ok   bool
	}{
		{"wrap", st.Wrap},
		{"linebreak", st.Linebreak},
		{"breakindent", st.Breakindent},
		{"number off", !st.Number},
		{"relativenumber off", !st.Relativenumb},
		{"spell", st.Spell},
		{"spelllang hr,en", st.Spelllang == "hr,en"},
		{"spellfile from the environment", st.Spellfile == words},
		{"spellcapcheck empty", st.Spellcapcheck == ""},
		{"laststatus 2", st.Laststatus == 2},
		{"cmdheight 1", st.Cmdheight == 1},
		{"hidden", st.Hidden},
		{"shortmess t", strings.Contains(st.Shortmess, "t")},
		{"shortmess T", strings.Contains(st.Shortmess, "T")},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s: %s is not as init.lua sets it: %+v", label, c.name, st)
		}
	}
}

func TestProseOptions(t *testing.T) {
	words := wordsFile(t)
	mkWordsDir(t, words)
	p, _ := newProse(t, proseOpts{words: words})
	ctx := testCtx(t)
	dir := tempDir(t)

	checkProseOptions(t, "first buffer", p.state(t), words)

	// A buffer opened later: window-local options come from the global values,
	// which init.lua sets too.
	a := mkFile(t, dir, "a.md", "one\n")
	b := mkFile(t, dir, "b.txt", "two\n")
	for _, f := range []string{a, b} {
		if err := p.OpenFile(ctx, f); err != nil {
			t.Fatal(err)
		}
		checkProseOptions(t, filepath.Base(f), p.state(t), words)
	}
}

func TestProseNoSpf(t *testing.T) {
	p, _ := newProse(t, proseOpts{})
	st := p.state(t)
	if !st.Spell || st.Spelllang != "hr,en" || st.Spellfile != "" {
		t.Errorf("without PISAR_SPELLFILE: %+v", st)
	}
}

// wrapped is a paragraph far wider than the 40 columns the movement tests use,
// followed by a second one and a short line.
func wrapped() []string {
	return []string{
		strings.TrimSpace(strings.Repeat("alpha beta gamma ", 12)),
		strings.TrimSpace(strings.Repeat("delta epsilon ", 8)),
		"end",
	}
}

func TestRows(t *testing.T) {
	const down, up = "\x1b[B", "\x1b[A"
	tests := []struct {
		name  string
		start string // keys that put the cursor on the starting place, normal mode
		keys  string
		check func(t *testing.T, from, to proseState)
	}{
		{"j", "", "j", func(t *testing.T, from, to proseState) {
			if to.Row != 1 || to.Winline != from.Winline+1 || to.Col <= from.Col {
				t.Errorf("j: %+v -> %+v, want the same line one screen row lower", from, to)
			}
		}},
		{"down", "", down, func(t *testing.T, from, to proseState) {
			if to.Row != 1 || to.Winline != from.Winline+1 {
				t.Errorf("<Down>: %+v -> %+v, want the same line one screen row lower", from, to)
			}
		}},
		{"2j", "", "2j", func(t *testing.T, from, to proseState) {
			if to.Row != 3 {
				t.Errorf("2j: %+v -> %+v, want line 3", from, to)
			}
		}},
		{"k", "$", "k", func(t *testing.T, from, to proseState) {
			if to.Row != 1 || to.Winline != from.Winline-1 {
				t.Errorf("k: %+v -> %+v, want the same line one screen row higher", from, to)
			}
		}},
		{"up", "$", up, func(t *testing.T, from, to proseState) {
			if to.Row != 1 || to.Winline != from.Winline-1 {
				t.Errorf("<Up>: %+v -> %+v, want the same line one screen row higher", from, to)
			}
		}},
		{"2k", "G", "2k", func(t *testing.T, from, to proseState) {
			if to.Row != 1 {
				t.Errorf("2k: %+v -> %+v, want line 1", from, to)
			}
		}},
		{"vj", "", "vj", func(t *testing.T, from, to proseState) {
			if to.Mode != "v" || to.Row != 1 || to.Winline != from.Winline+1 {
				t.Errorf("vj: %+v -> %+v, want visual mode, same line, one screen row lower", from, to)
			}
		}},
		{"dj", "", "dj", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProse(t, proseOpts{cols: 40, rows: 12})
			p.setBuffer(t, wrapped()...)
			if tc.start != "" {
				// "$" and "G" both change the position.
				p.press(t, tc.start)
			}
			from := p.state(t)
			if tc.name == "dj" {
				// An operator must keep its meaning: dj deletes two whole lines.
				p.write(t, tc.keys)
				eventually(t, "dj", func() bool { return len(p.lines(t)) == 1 })
				if got := p.lines(t); got[0] != "end" {
					t.Errorf("after dj: %q, want only the last line", got)
				}
				return
			}
			tc.check(t, from, p.press(t, tc.keys))
		})
	}
}

func TestRowsIns(t *testing.T) {
	const down, up = "\x1b[B", "\x1b[A"
	p, _ := newProse(t, proseOpts{cols: 40, rows: 12})
	p.setBuffer(t, wrapped()...)

	from := p.press(t, "i")
	if from.Mode != "i" {
		t.Fatalf("mode after i = %q", from.Mode)
	}
	to := p.press(t, down)
	if to.Mode != "i" || to.Row != 1 || to.Winline != from.Winline+1 {
		t.Errorf("<Down> in insert mode: %+v -> %+v, want insert mode, same line, one screen row lower", from, to)
	}
	back := p.press(t, up)
	if back.Mode != "i" || back.Row != 1 || back.Winline != from.Winline {
		t.Errorf("<Up> in insert mode: %+v -> %+v, want insert mode, same line, back on the first screen row", to, back)
	}
	// What is typed afterwards still lands in insert mode, at the cursor.
	p.write(t, "X")
	eventually(t, "X typed", func() bool { return strings.Contains(p.lines(t)[0], "X") })
}

// A completion menu keeps <Down>: it chooses an item instead of moving the cursor.
func TestRowsPum(t *testing.T) {
	p, _ := newProse(t, proseOpts{cols: 40, rows: 12})
	p.setBuffer(t, "apple", "apricot", "")
	p.lua(t, nil, `vim.api.nvim_win_set_cursor(0, { 3, 0 })`)

	// "A" to append on the empty line, then CTRL-N to open the menu.
	p.write(t, "Aap\x0e")
	eventually(t, "the completion menu", func() bool { return p.state(t).Pum })
	var sel struct{ Selected int }
	p.lua(t, &sel, `return { Selected = vim.fn.complete_info({ "selected" }).selected }`)
	first := sel.Selected

	p.write(t, "\x1b[B")
	eventually(t, "<Down> to choose the next item", func() bool {
		p.lua(t, &sel, `return { Selected = vim.fn.complete_info({ "selected" }).selected }`)
		return sel.Selected != first
	})
	st := p.state(t)
	if !strings.HasPrefix(st.Mode, "i") || st.Row != 3 || !st.Pum {
		t.Errorf("after <Down> in the menu: %+v, want insert mode on line 3 with the menu still open", st)
	}
}

func TestProseSwitches(t *testing.T) {
	p, _ := newProse(t, proseOpts{cols: 40, rows: 12})
	ctx := testCtx(t)
	dir := tempDir(t)
	p.setBuffer(t, "one")

	// Each switch changes its option, a second press restores it.
	for _, tc := range []struct {
		name string
		keys string
		get  func(proseState) bool
	}{
		{"spell", " s", func(s proseState) bool { return s.Spell }},
		{"number", " n", func(s proseState) bool { return s.Number }},
	} {
		start := tc.get(p.state(t))
		for i, want := range []bool{!start, start} {
			p.write(t, tc.keys)
			eventually(t, tc.name+" press "+string(rune('1'+i)), func() bool { return tc.get(p.state(t)) == want })
		}
	}
	if st := p.state(t); !st.Spell || st.Number || st.Mode != "n" {
		t.Fatalf("after two presses each: %+v", st)
	}

	// The last choice is what a document opened later gets.
	p.write(t, " s n")
	eventually(t, "both switched", func() bool { st := p.state(t); return !st.Spell && st.Number })
	b := mkFile(t, dir, "b.md", "two\n")
	if err := p.OpenFile(ctx, b); err != nil {
		t.Fatal(err)
	}
	if st := p.state(t); st.Spell || !st.Number {
		t.Errorf("a document opened after the switches: spell=%v number=%v, want false and true", st.Spell, st.Number)
	}

	// Insert mode: the same keys are text.
	p.setBuffer(t, "")
	p.write(t, "i s n\x1b")
	eventually(t, "text typed", func() bool { return p.lines(t)[0] == " s n" })
	if st := p.state(t); st.Spell || !st.Number {
		t.Errorf("insert mode changed an option: %+v", st)
	}
}

// word is the first misspelled word of text as spellbadword() reports it, and
// its kind; "" when there is none.
func (p *Process) badWord(t *testing.T, text string) (word, kind string) {
	t.Helper()
	var res []string
	p.lua(t, &res, `return vim.fn.spellbadword(...)`, text)
	if len(res) != 2 {
		t.Fatalf("spellbadword(%q) = %v", text, res)
	}
	return res[0], res[1]
}

func TestSpell(t *testing.T) {
	words := wordsFile(t)
	mkWordsDir(t, words)
	for _, tc := range []struct {
		name  string
		words string
	}{
		{"env", words},
		{"noenv", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProse(t, proseOpts{words: tc.words})
			for _, w := range []string{"rijč", "mispeled"} {
				if got, kind := p.badWord(t, w); got != w || kind != "bad" {
					t.Errorf("spellbadword(%q) = %q, %q; want it marked bad", w, got, kind)
				}
			}
			for _, w := range []string{"sjećanja", "računalima", "knjižnici", "document", "colour"} {
				if got, kind := p.badWord(t, w); got != "" {
					t.Errorf("spellbadword(%q) = %q, %q; want it accepted", w, got, kind)
				}
			}
			// Croatian and English in one sentence, and the abbreviations that the
			// default capital-letter check would flag.
			for _, s := range []string{
				"Računalo je document.",
				"Kupili smo, npr. jabuke, itd. sutra, tj. danas.",
				"1. kupiti mlijeko",
			} {
				if got, kind := p.badWord(t, s); got != "" {
					t.Errorf("spellbadword(%q) = %q, %q; want nothing", s, got, kind)
				}
			}
		})
	}
}

// A start with spelllang=hr,en must neither wait for an answer nor try to
// download the dictionary, whether the file is there or not.
func TestSpellBoot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		o      proseOpts
		lang   string // 'spelllang' after the start
		hrWord bool   // the dictionary is loaded, so a Croatian word is known
	}{
		{"dict", proseOpts{}, "hr,en", true},
		// A missing file must not be asked for at all: see the comment in init.lua.
		{"none", proseOpts{noDict: true}, "en", false},
		// A damaged file is there, so it is loaded, and Neovim reports it by itself.
		{"cut", proseOpts{cutDict: true}, "hr,en", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, out := newProse(t, tc.o)
			// Start returned, so the RPC answers; the mode proves nothing waits.
			st := p.state(t)
			if st.Mode != "n" || st.Blocking {
				t.Errorf("mode %q, blocking %v after the start; want normal mode, not blocking", st.Mode, st.Blocking)
			}
			if st.Spelllang != tc.lang || !st.Spell {
				t.Errorf("spell=%v spelllang=%q, want true and %q", st.Spell, st.Spelllang, tc.lang)
			}
			if got, _ := p.badWord(t, "mispeled"); got != "mispeled" {
				t.Errorf("English is not checked: %q", got)
			}
			if got, _ := p.badWord(t, "document"); got != "" {
				t.Errorf("English word marked: %q", got)
			}
			// A word that only Croatian knows tells which case this is.
			got, _ := p.badWord(t, "sjećanja")
			if !tc.hrWord && got == "" {
				t.Error("sjećanja accepted without the Croatian dictionary: the test does not test the missing file")
			}
			if tc.hrWord && got != "" {
				t.Errorf("sjećanja marked with the Croatian dictionary: %q", got)
			}
			// Neovim's download question (runtime/lua/nvim/spellfile.lua) or any
			// hit-enter prompt would be on the screen by now.
			screen := string(out.bytes())
			for _, bad := range []string{"(Y)es", "Download", "download", "Press ENTER", "Hit-enter"} {
				if strings.Contains(screen, bad) {
					t.Errorf("the screen contains %q after the start", bad)
				}
			}
			// A key reaches Neovim at once (a prompt would eat or answer it).
			p.typeKeys(t, "ix", "i", [2]int{1, 1})
		})
	}
}

func TestSpellZg(t *testing.T) {
	words := wordsFile(t)
	testZg(t, words)
}

// A comma in the path (PISAR_HOME can hold anything) must not split 'spellfile'.
func TestSpellZgComma(t *testing.T) {
	testZg(t, filepath.Join(tempDir(t), "a,b", "pisar.utf-8.add"))
}

func testZg(t *testing.T, words string) {
	t.Helper()
	mkWordsDir(t, words)
	const word = "blorptastic"

	p, _ := newProse(t, proseOpts{words: words})
	p.setBuffer(t, word)
	if got, _ := p.badWord(t, word); got != word {
		t.Fatalf("%q is known before zg", word)
	}
	if _, err := os.Stat(words); !os.IsNotExist(err) {
		t.Fatalf("the words file exists before any zg: %v", err)
	}
	p.write(t, "zg")
	eventually(t, "zg to add the word", func() bool {
		got, _ := p.badWord(t, word)
		return got == ""
	})
	data, err := os.ReadFile(words)
	if err != nil {
		t.Fatalf("zg did not write %s: %v", words, err)
	}
	if !bytes.Contains(data, []byte(word)) {
		t.Errorf("%s = %q, want it to contain %q", words, data, word)
	}
	// Neovim keeps the compiled list beside the text file, still outside its
	// configuration folder.
	if _, err := os.Stat(words + ".spl"); err != nil {
		t.Errorf("no compiled word list beside the words file: %v", err)
	}
	// A new Neovim with the same words file still knows the word.
	cctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	if err := p.Close(cctx); err != nil {
		t.Fatal(err)
	}
	p2, _ := newProse(t, proseOpts{words: words})
	if got, _ := p2.badWord(t, word); got != "" {
		t.Errorf("%q is marked again after a restart", word)
	}
	if got, _ := p2.badWord(t, "rijč"); got != "rijč" {
		t.Errorf("the words file made everything valid: %q", got)
	}
}

// TestUndercurl checks what Neovim sends for a misspelled word in the terminal
// the application gives it (TERM=xterm-256color, COLORTERM=truecolor). The
// colorscheme draws SpellBad as an undercurl in the "danger" colour (#B42318,
// 180;35;24). Measured with Neovim 0.12:
//
// At startup Neovim sends "CSI 4:3 m" followed by the query DECRQSS "m"
// (DCS $ q m ST) and decides from the answer whether the terminal can draw a
// curly underline. Only an answer that contains "4:3" turns it on:
//   - no answer, and the answer of xterm.js 6.0.0 ("DCS 1 $ r 0 m ST": its
//     requestStatusString always reports "0m", see InputHandler.ts), give a
//     plain underline "CSI 0;4 m" in the text colour and no underline colour;
//   - an answer such as "DCS 1 $ r 0;4:3 m ST" gives "CSI 4:3 m" and the
//     underline colour "CSI 58:2::180:35:24 m". When both answers arrive, in
//     either order, the one with "4:3" wins.
//
// xterm.js's DOM renderer, the only renderer the frontend has, draws style 3
// as "text-decoration: wavy underline" (class xterm-underline-3, xterm.css) and
// takes the colour of SGR 58 for text-decoration-color (DomRendererRowFactory.ts).
// So the pump answers the query with "4:3" itself (termAnswers in nvim.go), as it
// does for the other start-up queries. The cases: the pump alone, and with the
// answer of xterm.js arriving afterwards, as it does in the application.
func TestUndercurl(t *testing.T) {
	const (
		query = "\x1bP$qm\x1b\\"
		plain = "\x1b[0;4m"
		curly = "\x1b[4:3m\x1b[58:2::180:35:24m"
	)
	tests := []struct {
		name  string
		reply string // what xterm.js would send after the pump
	}{
		{"pump", ""},
		{"xterm", "\x1bP1$r0m\x1b\\"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			words := wordsFile(t)
			mkWordsDir(t, words)
			p, out := newProse(t, proseOpts{words: words, colorscheme: true})
			eventually(t, "the style query", func() bool { return bytes.Contains(out.bytes(), []byte(query)) })
			if tc.reply != "" {
				p.write(t, tc.reply)
			}
			a := mkFile(t, tempDir(t), "a.md", "rijč\n")
			if err := p.OpenFile(testCtx(t), a); err != nil {
				t.Fatal(err)
			}
			eventually(t, "the word on the screen", func() bool { return bytes.Contains(out.bytes(), []byte("rijč")) })
			_ = settled(t, p)

			// What Neovim sent right before the word, the last time it drew it.
			got := out.bytes()
			i := bytes.LastIndex(got, []byte("rijč"))
			before := string(got[max(0, i-80):i])
			if !strings.Contains(before, curly) {
				t.Errorf("before the word: %q, want %q", before, curly)
			}
			if strings.Contains(before, plain) {
				t.Errorf("before the word: %q, must not contain the plain underline %q", before, plain)
			}
		})
	}
}

// The answers of the pump must not slow the start or leave anything on the
// screen: Start returns as before, and Neovim is in normal mode with no message.
func TestStartAnswers(t *testing.T) {
	began := time.Now()
	p, out := newProse(t, proseOpts{})
	took := time.Since(began)
	t.Logf("Start took %v", took)
	if took > 3*time.Second { // about 0.1 s without load
		t.Errorf("Start took %v", took)
	}
	if st := p.state(t); st.Mode != "n" || st.Blocking {
		t.Errorf("mode %q, blocking %v after the start", st.Mode, st.Blocking)
	}
	eventually(t, "the query", func() bool { return bytes.Contains(out.bytes(), []byte("\x1bP$qm\x1b\\")) })
	var msg struct{ Err, Messages string }
	p.lua(t, &msg, `return { Err = vim.v.errmsg, Messages = vim.api.nvim_exec2("messages", { output = true }).output }`)
	if msg.Err != "" || msg.Messages != "" {
		t.Errorf("v:errmsg %q, :messages %q", msg.Err, msg.Messages)
	}
}

// Redraw must send the screen again even when nothing on it changed: a page that
// was reloaded has an empty terminal and waits for it. ":redraw!" sent nothing
// in that case (Neovim 0.12, a file open, nothing changed).
func TestRedrawSends(t *testing.T) {
	p, out := newProse(t, proseOpts{cols: 100, rows: 30})
	f := mkFile(t, tempDir(t), "b.txt", "hello world\n")
	if err := p.OpenFile(testCtx(t), f); err != nil {
		t.Fatal(err)
	}
	_ = settled(t, p)
	for i := range 5 {
		before := len(out.bytes())
		if err := p.Redraw(testCtx(t)); err != nil {
			t.Fatal(err)
		}
		eventually(t, "bytes after Redraw", func() bool { return len(out.bytes()) > before })
		got := string(out.bytes()[before:])
		if !strings.Contains(got, "hello world") {
			t.Errorf("redraw %d sent %q, want the text of the file", i, got)
		}
		time.Sleep(quiet)
	}
}

// Neovim's own download of dictionaries is off: nothing listens for a missing
// file, so a language set later cannot start a question either.
func TestSpellNoDl(t *testing.T) {
	p, _ := newProse(t, proseOpts{})
	var res struct {
		Missing int
		Loaded  int
	}
	p.lua(t, &res, `
		return {
			Missing = #vim.api.nvim_get_autocmds({ event = "SpellFileMissing" }),
			Loaded = vim.g.loaded_spellfile_plugin or 0,
		}`)
	if res.Missing != 0 || res.Loaded == 0 {
		t.Errorf("SpellFileMissing autocmds: %d, g:loaded_spellfile_plugin: %d; want none and set", res.Missing, res.Loaded)
	}
	// A language without a file only gives a warning.
	p.lua(t, nil, `vim.cmd("silent! set spelllang=hr,en,xx")`)
	if st := p.state(t); st.Mode != "n" || st.Blocking {
		t.Errorf("after setting a missing language: mode %q, blocking %v", st.Mode, st.Blocking)
	}
}

// A query may be cut anywhere between two reads of the PTY; it is answered
// exactly once, and a query seen whole is not answered again by the next read.
func TestQueryScanner(t *testing.T) {
	dsr, da1, sgr := "\x1b[5n", "\x1b[c", "\x1bP$qm\x1b\\"
	reply := map[string]string{dsr: "\x1b[0n", da1: "\x1b[?1;2c", sgr: "\x1bP1$r0;4:3m\x1b\\"}
	for _, q := range []string{dsr, da1, sgr} {
		for _, noise := range []string{"", "abc\x1b[m", "\x1b[4:3m"} {
			stream := noise + q + "tail text"
			for cut := 0; cut <= len(stream); cut++ {
				t.Run("", func(t *testing.T) {
					var sc queryScanner
					got := append(sc.feed([]byte(stream[:cut])), sc.feed([]byte(stream[cut:]))...)
					if len(got) != 1 || got[0] != reply[q] {
						t.Errorf("%q cut at %d: answers %q, want one %q", stream, cut, got, reply[q])
					}
				})
			}
			// One byte at a time.
			var sc queryScanner
			var got []string
			for i := 0; i < len(stream); i++ {
				got = append(got, sc.feed([]byte{stream[i]})...)
			}
			if len(got) != 1 || got[0] != reply[q] {
				t.Errorf("%q byte by byte: answers %q, want one %q", stream, got, reply[q])
			}
		}
	}
	// Two queries in one read, and the same one twice.
	var sc queryScanner
	got := sc.feed([]byte(dsr + sgr + dsr))
	want := []string{reply[dsr], reply[dsr], reply[sgr]}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("answers %q, want %q", got, want)
	}
}
