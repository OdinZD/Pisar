package nvim

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// Tests of the which-key window in nvim/init.lua, in a real Neovim with the
// repository's own init.lua. The window is opened with real keys through the
// PTY. Names are short for the reason in writing_test.go: the name of a test and
// its subtest together stay within 17 characters, and each test starts as few
// Neovims as it can, because the counter in the socket name is shared by every
// test of the package.

const (
	keyEsc   = "\x1b"
	keyDown  = "\x1b[B"
	keySpace = " "
)

// floatInfo is a floating window as the tests see it.
type floatInfo struct {
	Win       int
	Buf       int
	Current   bool
	Focusable bool
	Anchor    string
	Relative  string
	Title     string
	Height    int
	Width     int
	// Screen position of the inner window, 1-based, and the size of the screen.
	Row, Col int
	Lines    int // of the editor
	Cmd      int // 'cmdheight'
	Text     []string
	Marks    []mark
}

// mark is an extmark of the window's buffer in the namespace of the window.
type mark struct {
	Row, Col, EndCol int
	Hl               string
}

// floats lists the floating windows.
func (p *Process) floats(t *testing.T) []floatInfo {
	t.Helper()
	var raw string
	p.lua(t, &raw, `
		local res = {}
		local ns = vim.api.nvim_get_namespaces()["pisar_whichkey_hl"]
		for _, w in ipairs(vim.api.nvim_list_wins()) do
			local c = vim.api.nvim_win_get_config(w)
			if c.relative ~= "" then
				local b = vim.api.nvim_win_get_buf(w)
				local pos = vim.fn.win_screenpos(w)
				local title = c.title
				if type(title) == "table" then
					title = title[1] and title[1][1] or ""
				end
				local marks = {}
				if ns then
					for _, m in ipairs(vim.api.nvim_buf_get_extmarks(b, ns, 0, -1, { details = true })) do
						table.insert(marks, { Row = m[2], Col = m[3], EndCol = m[4].end_col, Hl = m[4].hl_group })
					end
				end
				table.insert(res, {
					Win = w, Buf = b, Current = w == vim.api.nvim_get_current_win(),
					Focusable = c.focusable, Anchor = c.anchor, Relative = c.relative,
					Title = title or "", Height = c.height, Width = c.width,
					Row = pos[1], Col = pos[2], Lines = vim.o.lines, Cmd = vim.o.cmdheight,
					Text = vim.api.nvim_buf_get_lines(b, 0, -1, false), Marks = marks,
				})
			end
		end
		return vim.json.encode(res)`)
	var out []floatInfo
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("floats: %v: %s", err, raw)
	}
	return out
}

// waitFloat waits until a float whose text satisfies ok exists, and returns it.
// There is never more than one.
func (p *Process) waitFloat(t *testing.T, what string, ok func(floatInfo) bool) floatInfo {
	t.Helper()
	var f floatInfo
	eventually(t, what, func() bool {
		all := p.floats(t)
		if len(all) > 1 {
			t.Fatalf("%d floating windows, want at most one: %+v", len(all), all)
		}
		if len(all) == 1 && ok(all[0]) {
			f = all[0]
			return true
		}
		return false
	})
	return f
}

// open types the leader and waits for the window.
func (p *Process) open(t *testing.T) floatInfo {
	t.Helper()
	p.write(t, keySpace)
	return p.waitFloat(t, "the which-key window", func(floatInfo) bool { return true })
}

// shut waits until there is no floating window.
func (p *Process) shut(t *testing.T) {
	t.Helper()
	eventually(t, "the window to close", func() bool { return len(p.floats(t)) == 0 })
}

// hooks is the number of vim.on_key() callbacks there are.
func (p *Process) hooks(t *testing.T) int {
	t.Helper()
	var n int
	p.lua(t, &n, `return vim.on_key()`)
	return n
}

// cursor is the cursor: row 1-based, byte column.
func (p *Process) cursor(t *testing.T) [2]int {
	t.Helper()
	st := p.state(t)
	return [2]int{st.Row, st.Col}
}

// hookGone proves that the catcher of the window is off, in three ways: no
// window, no callback beyond base, and the keys of :normal reach the buffer (in
// the drain they would be swallowed: they are not typed). It replaces the text of
// the buffer.
func (p *Process) hookGone(t *testing.T, base int) {
	t.Helper()
	p.shut(t)
	eventually(t, "the catcher to be taken off", func() bool { return p.hooks(t) == base })
	var col int
	p.lua(t, &col, `
		vim.api.nvim_buf_set_lines(0, 0, -1, false, { "ab" })
		vim.api.nvim_win_set_cursor(0, { 1, 0 })
		vim.cmd("normal! l")
		return vim.api.nvim_win_get_cursor(0)[2]`)
	if col != 1 {
		t.Errorf(":normal! l left the cursor in column %d, want 1: something swallows keys", col)
	}
}

// fresh puts the editor in a known state: three lines, the cursor at the start,
// normal mode, nothing open.
func (p *Process) fresh(t *testing.T, base int, lines ...string) {
	t.Helper()
	p.shut(t)
	eventually(t, "the catcher to be taken off", func() bool { return p.hooks(t) == base })
	if len(lines) == 0 {
		lines = []string{"one", "two", "three"}
	}
	p.setBuffer(t, lines...)
	p.notBlocked(t)
}

// wantRows is what the window shows for the mappings of Pisar's in normal mode:
// "key  description", by key, upper and lower case together.
func wantRows(t *testing.T) []string {
	t.Helper()
	texts := testTexts(t)
	var rows []string
	for _, m := range pisarMaps {
		if key, ok := strings.CutPrefix(m.lhs, "<leader>"); ok && m.mode == "n" {
			rows = append(rows, key+"  "+texts[m.key])
		}
	}
	slices.SortFunc(rows, func(a, b string) int { return strings.Compare(strings.ToLower(a[:1]), strings.ToLower(b[:1])) })
	return rows
}

// TestWhichKey: the window of the shipped mappings, in one Neovim.
func TestWhichKey(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	base := p.hooks(t)
	texts := testTexts(t)

	t.Run("opt", func(t *testing.T) {
		var tl int
		p.lua(t, &tl, `return vim.o.timeoutlen`)
		if tl != 500 {
			t.Errorf("timeoutlen = %d, want 500", tl)
		}
	})

	t.Run("open", func(t *testing.T) {
		p.fresh(t, base)
		dir := tempDir(t)
		a := mkFile(t, dir, "a.md", "alpha\n")
		if err := p.OpenFile(ctx, a); err != nil {
			t.Fatal(err)
		}
		p.setBuffer(t, "alpha")
		curBefore, err := p.Current(ctx)
		if err != nil {
			t.Fatal(err)
		}
		bufsBefore, err := p.Buffers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		modBefore, err := p.Modified(ctx)
		if err != nil {
			t.Fatal(err)
		}
		f := p.open(t)
		p.notBlocked(t) // the mode is "n" and Neovim is not waiting
		if f.Current || f.Focusable || f.Relative != "editor" || f.Anchor != "SE" {
			t.Errorf("float = %+v, want an unfocused, unfocusable one, relative to the editor, anchored SE", f)
		}
		// The status line is on the row above the command line; the window ends
		// above it (with a row of margin: the corner of a border is kept clear).
		statusRow := f.Lines - f.Cmd
		if bottom := f.Row + f.Height; bottom >= statusRow || bottom < statusRow-2 {
			t.Errorf("the bottom border is on row %d, want it just above the status line on %d", bottom, statusRow)
		}
		if got := f.Title; got != texts["nvimKeysTitle"] {
			t.Errorf("title = %q, want %q", got, texts["nvimKeysTitle"])
		}
		if want := wantRows(t); !slices.Equal(f.Text, want) {
			t.Errorf("rows =\n%q\nwant\n%q", f.Text, want)
		}
		if f.Height != len(f.Text) {
			t.Errorf("height %d for %d rows", f.Height, len(f.Text))
		}
		// The keys are marked, the groups are none here.
		for i := range f.Text {
			if !slices.Contains(f.Marks, mark{Row: i, Col: 0, EndCol: 1, Hl: "PisarKey"}) {
				t.Errorf("row %d has no PisarKey mark: %+v", i, f.Marks)
			}
		}
		if cur, err := p.Current(ctx); err != nil || cur != curBefore {
			t.Errorf("Current = %+v, %v; want %+v", cur, err, curBefore)
		}
		if bufs, err := p.Buffers(ctx); err != nil || !slices.Equal(bufs, bufsBefore) {
			t.Errorf("Buffers = %+v, %v; want %+v", bufs, err, bufsBefore)
		}
		if mod, err := p.Modified(ctx); err != nil || !slices.Equal(mod, modBefore) {
			t.Errorf("Modified = %+v, %v; want %+v", mod, err, modBefore)
		}
		p.write(t, keyEsc)
		p.hookGone(t, base)
	})

	t.Run("go", func(t *testing.T) {
		p.fresh(t, base)
		settled(t, p)
		p.open(t)
		p.write(t, "e")
		waitKind(t, p, EventFocusTree)
		p.hookGone(t, base)
		if extra := settled(t, p); len(only(extra, EventFocusTree)) != 0 {
			t.Errorf("the mapping ran twice: %+v", extra)
		}
	})

	t.Run("fast", func(t *testing.T) {
		p.fresh(t, base)
		settled(t, p)
		p.write(t, " e")
		waitKind(t, p, EventFocusTree)
		if f := p.floats(t); len(f) != 0 {
			t.Errorf("a window for a mapping typed fast: %+v", f)
		}
		p.hookGone(t, base)
	})

	t.Run("esc", func(t *testing.T) {
		p.fresh(t, base)
		p.open(t)
		before := p.cursor(t)
		p.write(t, keyEsc)
		p.shut(t)
		// The next key works as usual, and it is the first thing that moved.
		st := p.press(t, "l")
		if st.Row != before[0] || st.Col != before[1]+1 {
			t.Errorf("cursor %v -> %d,%d after <Esc> and l, want one column on", before, st.Row, st.Col)
		}
		if got := p.lines(t); !slices.Equal(got, []string{"one", "two", "three"}) {
			t.Errorf("text = %q", got)
		}
		p.hookGone(t, base)
	})

	t.Run("z", func(t *testing.T) {
		// z is a prefix of its own commands: let by, it would take the next key.
		p.fresh(t, base)
		p.open(t)
		p.write(t, "z")
		p.shut(t)
		if st := p.press(t, "l"); st.Col != 1 {
			t.Errorf("column %d after z and l, want 1: z was let through", st.Col)
		}
		p.hookGone(t, base)
	})

	t.Run("j", func(t *testing.T) {
		// j is mapped to gj: two keys, the second not typed. The window must
		// swallow both.
		p.fresh(t, base)
		p.open(t)
		p.write(t, "j")
		p.shut(t)
		p.write(t, "j")
		eventually(t, "the second j", func() bool { return p.cursor(t)[0] == 2 })
		// Keys are read in order: when l has run, an earlier j would have too.
		p.write(t, "l")
		eventually(t, "l", func() bool { return p.cursor(t)[1] == 1 })
		if got := p.cursor(t); got[0] != 2 {
			t.Errorf("cursor = %v, want row 2: the first j moved it", got)
		}
		p.hookGone(t, base)
	})

	t.Run("space", func(t *testing.T) {
		p.fresh(t, base)
		p.open(t)
		p.write(t, keySpace)
		p.shut(t)
		st := p.press(t, "l")
		if st.Col != 1 {
			t.Errorf("column %d, want 1", st.Col)
		}
		if f := p.floats(t); len(f) != 0 {
			t.Errorf("a second window: %+v", f)
		}
		p.hookGone(t, base)
	})

	t.Run("vis", func(t *testing.T) {
		p.fresh(t, base, "alpha beta")
		p.write(t, "ve")
		eventually(t, "visual mode", func() bool { return p.state(t).Mode == "v" && p.state(t).Col == 4 })
		p.open(t)
		if m := p.state(t).Mode; m != "v" {
			t.Errorf("mode = %q with the window open, want v", m)
		}
		p.write(t, "b")
		eventually(t, "bold", func() bool { return p.lines(t)[0] == "**alpha** beta" })
		p.hookGone(t, base)
	})

	t.Run("vesc", func(t *testing.T) {
		// <Esc> is swallowed: visual mode, which it would end, stays.
		p.fresh(t, base, "alpha beta")
		p.write(t, "v")
		eventually(t, "visual mode", func() bool { return p.state(t).Mode == "v" })
		p.open(t)
		p.write(t, keyEsc)
		p.shut(t)
		p.write(t, "l")
		eventually(t, "l", func() bool { return p.cursor(t)[1] == 1 })
		if m := p.state(t).Mode; m != "v" {
			t.Errorf("mode = %q, want v: <Esc> was let through", m)
		}
		p.write(t, keyEsc)
		eventually(t, "normal mode", func() bool { return p.state(t).Mode == "n" })
		p.hookGone(t, base)
	})

	t.Run("calls", func(t *testing.T) {
		// What Go does while the window is up works, and does not close it.
		p.fresh(t, base)
		a := mkFile(t, tempDir(t), "c.md", "word\n")
		if err := p.OpenFile(ctx, a); err != nil {
			t.Fatal(err)
		}
		p.setBuffer(t, "word again")
		p.open(t)
		if err := p.Save(ctx); err != nil {
			t.Fatalf("Save with the window open: %v", err)
		}
		if got, err := os.ReadFile(a); err != nil || string(got) != "word again\n" {
			t.Errorf("file = %q, %v", got, err)
		}
		if err := p.Format(ctx, "bold"); err != nil {
			t.Fatalf("Format with the window open: %v", err)
		}
		if got := p.lines(t); got[0] != "**word** again" {
			t.Errorf("after Format: %q", got)
		}
		p.notBlocked(t)
		if f := p.floats(t); len(f) != 1 {
			t.Errorf("%d windows after Save and Format, want the one that was open", len(f))
		}
		p.write(t, keyEsc)
		p.hookGone(t, base)
	})

	t.Run("size", func(t *testing.T) {
		p.fresh(t, base)
		p.open(t)
		if err := p.Resize(60, 20); err != nil {
			t.Fatal(err)
		}
		p.shut(t)
		p.hookGone(t, base)
		if err := p.Resize(80, 24); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the size", func() bool {
			var cols int
			p.lua(t, &cols, `return vim.o.columns`)
			return cols == 80
		})
	})

	t.Run("file", func(t *testing.T) {
		p.fresh(t, base)
		b := mkFile(t, tempDir(t), "b.md", "beta\n")
		p.open(t)
		if err := p.OpenFile(ctx, b); err != nil {
			t.Fatal(err)
		}
		p.shut(t)
		p.hookGone(t, base)
		if cur, err := p.Current(ctx); err != nil || cur.Path != b {
			t.Errorf("Current = %+v, %v; want %s", cur, err, b)
		}
	})

	t.Run("ins", func(t *testing.T) {
		// Leaving normal mode without a key of ours closes the window.
		p.fresh(t, base)
		p.open(t)
		// Not a typed key (the window would swallow that): a command.
		p.lua(t, nil, `vim.cmd.startinsert()`)
		p.shut(t)
		eventually(t, "insert mode", func() bool { return p.state(t).Mode == "i" })
		p.write(t, keyEsc)
		eventually(t, "normal mode", func() bool { return p.state(t).Mode == "n" })
		p.hookGone(t, base)
	})
}

// userKeys is the user.lua of the tests with mappings of the user's own.
const userKeys = `
vim.g.user_ran = true
vim.g.pisar_groups = { g = "Git" }
vim.keymap.set("n", "<leader>gs", function() vim.g.hit = "gs" end, { desc = "status" })
vim.keymap.set("n", "<leader>gc", function() vim.g.hit = "gc" end, { desc = "commit" })
vim.keymap.set("n", "<leader>hx", function() vim.g.hit = "hx" end, { desc = "ex" })
vim.keymap.set("n", "<leader>hy", function() vim.g.hit = "hy" end, { desc = "why" })
vim.keymap.set("n", "<leader>k", function() vim.g.count = vim.v.count end, { desc = "count" })
vim.keymap.set("n", "<leader>q", function() vim.g.hit = "q" end)
vim.keymap.set("n", "<leader>ds", function() vim.g.hit = "ds" end)
vim.keymap.set("n", "<leader>dc", function() vim.g.hit = "dc" end)
vim.keymap.set("n", "<leader><Down>", function() vim.g.hit = "down" end, { desc = "arrow" })
vim.keymap.set("n", "<leader>č", function() vim.g.hit = "cc" end, { desc = "letter" })
vim.keymap.set("n", "<leader>m", function() vim.g.hit = "m" end, { desc = "short" })
vim.keymap.set("n", "<leader>mm", function() vim.g.hit = "mm" end, { desc = "long" })
vim.keymap.set("n", "<leader>w", function() vim.g.hit = "w" end, { desc = "a very long description that does not fit in the window at all, not even close to it" })
`

// TestWhichKeyUser: the mappings of a user.lua, in one Neovim.
func TestWhichKeyUser(t *testing.T) {
	p := startUser(t, userKeys)
	base := p.hooks(t)
	hit := func() string {
		s, _ := p.global(t, "hit").(string)
		return s
	}
	clear := func() { p.lua(t, nil, `vim.g.hit = nil`) }

	t.Run("grp", func(t *testing.T) {
		p.fresh(t, base)
		f := p.open(t)
		// The named group first among the g's, and a group without a name is a count.
		if !hasRow(f.Text, "g", "+Git") {
			t.Errorf("rows = %q, want one for the group g", f.Text)
		}
		if !hasRow(f.Text, "h", "+2") {
			t.Errorf("rows = %q, want one for the unnamed group h", f.Text)
		}
		i := slices.IndexFunc(f.Text, func(s string) bool { return strings.HasSuffix(s, "+Git") })
		// The label of a group is marked from where it starts to the end of the row.
		if i < 0 || !slices.Contains(f.Marks, mark{Row: i, Col: len(f.Text[i]) - len("+Git"), EndCol: len(f.Text[i]), Hl: "PisarKeyGroup"}) {
			t.Errorf("no PisarKeyGroup mark for the group: %+v", f.Marks)
		}
		// g is the start of defaults of Neovim (gc, gx...): it arrives after the
		// timeout.
		p.write(t, "g")
		inside := p.waitFloat(t, "the group", func(f floatInfo) bool { return f.Title == "Git" })
		if want := []string{"c  commit", "s  status"}; !slices.Equal(inside.Text, want) {
			t.Errorf("group rows = %q, want %q", inside.Text, want)
		}
		clear()
		p.write(t, "s")
		eventually(t, "the mapping", func() bool { return hit() == "gs" })
		p.hookGone(t, base)
	})

	t.Run("name", func(t *testing.T) {
		p.fresh(t, base)
		p.open(t)
		p.write(t, "h")
		inside := p.waitFloat(t, "the group", func(f floatInfo) bool { return f.Title == "h" })
		if want := []string{"x  ex", "y  why"}; !slices.Equal(inside.Text, want) {
			t.Errorf("group rows = %q, want %q", inside.Text, want)
		}
		clear()
		p.write(t, "y")
		eventually(t, "the mapping", func() bool { return hit() == "hy" })
		p.hookGone(t, base)
	})

	t.Run("count", func(t *testing.T) {
		p.fresh(t, base)
		p.write(t, "3 ")
		p.waitFloat(t, "the window", func(floatInfo) bool { return true })
		p.write(t, "k")
		eventually(t, "the count", func() bool {
			var n int
			p.lua(t, &n, `return vim.g.count or -1`)
			return n == 3
		})
		p.hookGone(t, base)
	})

	t.Run("nodesc", func(t *testing.T) {
		p.fresh(t, base)
		f := p.open(t)
		for _, row := range f.Text {
			if strings.HasPrefix(row, "q ") {
				t.Errorf("a mapping without a description is listed: %q", f.Text)
			}
		}
		clear()
		p.write(t, "q")
		eventually(t, "the mapping without a description", func() bool { return hit() == "q" })
		p.hookGone(t, base)
	})

	t.Run("keys", func(t *testing.T) {
		p.fresh(t, base)
		f := p.open(t)
		if !hasRow(f.Text, "<Down>", "arrow") || !hasRow(f.Text, "č", "letter") {
			t.Errorf("rows = %q, want one for <Down> and one for č", f.Text)
		}
		clear()
		p.write(t, keyDown)
		eventually(t, "<leader><Down>", func() bool { return hit() == "down" })
		p.hookGone(t, base)

		p.fresh(t, base)
		p.open(t)
		clear()
		p.write(t, "č")
		eventually(t, "<leader>č", func() bool { return hit() == "cc" })
		p.hookGone(t, base)
	})

	// A group in which no mapping has a description, and with no name, is not
	// listed, but it can be entered. With nothing to show the catcher stays,
	// without a window; the keys must never be put back, or the bare <Space>
	// fires again and opens the window again for ever.
	t.Run("blind", func(t *testing.T) {
		p.fresh(t, base)
		f := p.open(t)
		for _, row := range f.Text {
			if strings.HasPrefix(row, "d ") {
				t.Errorf("a group without descriptions is listed: %q", f.Text)
			}
		}
		clear()
		p.write(t, "d")
		p.shut(t)
		p.notBlocked(t) // answers, is in normal mode, waits for nothing
		p.write(t, "s")
		eventually(t, "the mapping in the group without descriptions", func() bool { return hit() == "ds" })
		p.hookGone(t, base)
	})

	t.Run("blesc", func(t *testing.T) {
		p.fresh(t, base)
		p.open(t)
		clear()
		p.write(t, "d")
		p.shut(t)
		p.write(t, keyEsc)
		// j moving the cursor shows that the <Esc> and the d before it are done
		// with, and that nothing swallows keys any more.
		p.write(t, "j")
		eventually(t, "j", func() bool { return p.cursor(t)[0] == 2 })
		if got := hit(); got != "" {
			t.Errorf("a mapping ran: %q", got)
		}
		if f := p.floats(t); len(f) != 0 {
			t.Errorf("the window opened again: %+v", f)
		}
		p.notBlocked(t)
		p.hookGone(t, base)
	})

	t.Run("both", func(t *testing.T) {
		// <leader>m and <leader>mm: a group, the short mapping is for typing fast.
		p.fresh(t, base)
		f := p.open(t)
		if !hasRow(f.Text, "m", "+1") {
			t.Errorf("rows = %q, want m as a group of one", f.Text)
		}
		p.write(t, keyEsc)
		p.hookGone(t, base)
	})

	t.Run("cut", func(t *testing.T) {
		p.fresh(t, base)
		f := p.open(t)
		if f.Width > 78 {
			t.Errorf("width %d does not fit the editor", f.Width)
		}
		i := slices.IndexFunc(f.Text, func(s string) bool { return strings.HasPrefix(s, "w ") })
		if i < 0 || !strings.HasSuffix(f.Text[i], "…") {
			t.Errorf("the long row is %q, want it cut with an ellipsis", f.Text)
		}
		p.write(t, keyEsc)
		p.hookGone(t, base)
	})
}

// hasRow reports whether a row is this key and this label, with the white space
// between them (the keys are padded to the widest one).
func hasRow(rows []string, key, label string) bool {
	return slices.ContainsFunc(rows, func(s string) bool {
		rest, ok := strings.CutPrefix(s, key)
		return ok && strings.HasPrefix(rest, "  ") && strings.TrimSpace(rest) == label
	})
}

// TestWhichKeyOff: vim.g.pisar_whichkey = false.
func TestWhichKeyOff(t *testing.T) {
	p := startUser(t, "vim.g.user_ran = true\nvim.g.pisar_whichkey = false\n")
	for _, mode := range []string{"n", "x"} {
		var has bool
		p.lua(t, &has, `return next(vim.fn.maparg("<Space>", (...), false, true)) ~= nil`, mode)
		if has {
			t.Errorf("a mapping of <Space> in mode %s", mode)
		}
	}
	p.write(t, " ")
	// <leader>e works at once, and there is still no window when the timeout of
	// the bare <Space> would be over.
	p.write(t, "e")
	waitKind(t, p, EventFocusTree)
	if f := p.floats(t); len(f) != 0 {
		t.Errorf("a window with which-key off: %+v", f)
	}
	p.notBlocked(t)
}

// TestWhichKeyOwn: a <Space> of the user's own stays theirs, in the mode it is for.
func TestWhichKeyOwn(t *testing.T) {
	p := startUser(t, `
vim.g.user_ran = true
vim.o.timeoutlen = 300
vim.keymap.set("n", "<Space>", function() vim.g.own = (vim.g.own or 0) + 1 end)
`)
	var tl int
	p.lua(t, &tl, `return vim.o.timeoutlen`)
	if tl != 300 {
		t.Errorf("timeoutlen = %d, want the user's 300", tl)
	}
	base := p.hooks(t)
	var desc map[string]string
	p.lua(t, &desc, `
		return {
			n = vim.fn.maparg("<Space>", "n", false, true).desc or "",
			x = vim.fn.maparg("<Space>", "x", false, true).desc or "",
		}`)
	if desc["n"] != "" || desc["x"] != testTexts(t)["nvimMapKeys"] {
		t.Errorf("descriptions of <Space> = %q, want the user's (none) in n and Pisar's in x", desc)
	}
	p.write(t, " ")
	eventually(t, "the user's mapping", func() bool { return p.global(t, "own") != nil })
	if f := p.floats(t); len(f) != 0 {
		t.Errorf("a window over the user's <Space>: %+v", f)
	}
	p.hookGone(t, base)

	// Visual mode has the window.
	p.setBuffer(t, "alpha beta")
	p.write(t, "v")
	eventually(t, "visual mode", func() bool { return p.state(t).Mode == "v" })
	p.open(t)
	p.write(t, keyEsc)
	p.shut(t)
	p.write(t, keyEsc)
	eventually(t, "normal mode", func() bool { return p.state(t).Mode == "n" })
	p.hookGone(t, base)
}

// spyLua notes every <Space> that comes out of the mappings: the key of the
// mapping arrives with typed = " ". That is the sign that the callback of the
// window has been called (it is called after the key), without the window
// showing anything to wait for.
const spyLua = `
vim.g.user_ran = true
vim.on_key(function(_, typed)
  if typed == " " then vim.g.spied = (vim.g.spied or 0) + 1 end
end, vim.api.nvim_create_namespace("spy"))
`

// idle checks that a <Space> in this Neovim opened nothing, failed in no way,
// and left Neovim free.
func idle(t *testing.T, p *Process, base int) {
	t.Helper()
	p.write(t, " ")
	eventually(t, "the <Space> to be read", func() bool { return p.global(t, "spied") != nil })
	// A call that is answered after the mapping has run.
	var errmsg string
	p.lua(t, &errmsg, `return vim.v.errmsg`)
	if errmsg != "" {
		t.Errorf("v:errmsg = %q", errmsg)
	}
	if m := p.messages(t); strings.TrimSpace(m) != "" {
		t.Errorf("messages = %q, want none", m)
	}
	if f := p.floats(t); len(f) != 0 {
		t.Errorf("a window: %+v", f)
	}
	p.notBlocked(t)
	p.hookGone(t, base)
}

// TestWhichKeySmall: a terminal with no room for the window.
func TestWhichKeySmall(t *testing.T) {
	p, _ := newProse(t, proseOpts{cols: 20, rows: 5, user: userLua(t, spyLua)})
	idle(t, p, p.hooks(t))
}

// TestWhichKeyMany: more rows than the screen has room for.
func TestWhichKeyMany(t *testing.T) {
	p, _ := newProse(t, proseOpts{rows: 12})
	base := p.hooks(t)
	f := p.open(t)
	// 12 rows: the command line, the status line, a row of margin and the two
	// borders leave 7 for text.
	if f.Height != 7 || len(f.Text) != 7 || f.Text[6] != "…" || f.Row < 2 {
		t.Errorf("height %d, top row %d, rows %q; want 7 rows ending in an ellipsis, in view", f.Height, f.Row, f.Text)
	}
	want := wantRows(t)[:6]
	if !slices.Equal(f.Text[:6], want) {
		t.Errorf("rows = %q, want the first six of %q", f.Text[:6], want)
	}
	p.write(t, keyEsc)
	p.hookGone(t, base)
}

// TestWhichKeyNone: without texts Pisar's mappings have no description, and
// there is nothing to show.
func TestWhichKeyNone(t *testing.T) {
	p, _ := newProse(t, proseOpts{noTexts: true, user: userLua(t, spyLua)})
	idle(t, p, p.hooks(t))
}
