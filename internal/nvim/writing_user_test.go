package nvim

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Tests of the "user.lua" part of nvim/init.lua, in a real Neovim with the
// repository's own init.lua. The user's file lives in t.TempDir(); nothing of
// the real home, ~/.config or ~/.local is read or written. This file is named
// so that it sorts after writing_test.go: see the note there about the length of
// the socket path, which is why the names of tests and subtests are short: the
// name of a test and its subtest together stay within 17 characters ("TestU" for
// all of them), which leaves the longest socket path at 100 of 103 bytes.

// userLua writes a user.lua with this content in a folder of its own and returns
// its path.
func userLua(t *testing.T, content string) string {
	t.Helper()
	return mkFile(t, tempDir(t), "user.lua", content)
}

// startUser starts Neovim with a user.lua that has this content.
func startUser(t *testing.T, content string) *Process {
	t.Helper()
	p, _ := newProse(t, proseOpts{user: userLua(t, content)})
	return p
}

// global is the value of g:name; nil when it is not set.
func (p *Process) global(t *testing.T, name string) interface{} {
	t.Helper()
	var v interface{}
	p.lua(t, &v, `return vim.g[...]`, name)
	return v
}

// ranUser reports whether user.lua ran: the files of these tests start with
// vim.g.user_ran = true.
func (p *Process) ranUser(t *testing.T) bool {
	t.Helper()
	return p.global(t, "user_ran") == true
}

// messagesopt returns 'messagesopt' now and Neovim's own default for it.
func (p *Process) messagesopt(t *testing.T) (now, def string) {
	t.Helper()
	p.lua(t, &now, `return vim.o.messagesopt`)
	p.lua(t, &def, `return vim.api.nvim_get_option_info2("messagesopt", {}).default`)
	return now, def
}

// notBlocked fails the test when Neovim waits for a key (a hit-enter prompt, a
// question): a Lua call is then not answered, but nvim_get_mode is.
func (p *Process) notBlocked(t *testing.T) {
	t.Helper()
	if st := p.state(t); st.Blocking || st.Mode != "n" {
		t.Errorf("Neovim is not in plain normal mode: mode %q, blocking %v", st.Mode, st.Blocking)
	}
}

// TestUFile: what counts as "there is a user.lua".
func TestUFile(t *testing.T) {
	const src = "vim.g.user_ran = true\n"
	tests := []struct {
		name string
		// make returns PISAR_USER_LUA; "" leaves the variable unset.
		make func(t *testing.T) string
		ran  bool
	}{
		{"novar", func(t *testing.T) string { return "" }, false},
		{"nofile", func(t *testing.T) string { return filepath.Join(tempDir(t), "user.lua") }, false},
		{"file", func(t *testing.T) string { return userLua(t, src) }, true},
		{"dir", func(t *testing.T) string {
			dir := filepath.Join(tempDir(t), "user.lua")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			mkFile(t, dir, "init.lua", src)
			return dir
		}, false},
		{"dangling", func(t *testing.T) string {
			link := filepath.Join(tempDir(t), "user.lua")
			if err := os.Symlink(filepath.Join(tempDir(t), "gone.lua"), link); err != nil {
				t.Fatal(err)
			}
			return link
		}, false},
		{"tofile", func(t *testing.T) string {
			link := filepath.Join(tempDir(t), "user.lua")
			if err := os.Symlink(mkFile(t, tempDir(t), "mine.lua", src), link); err != nil {
				t.Fatal(err)
			}
			return link
		}, true},
		{"todir", func(t *testing.T) string {
			dir := tempDir(t)
			mkFile(t, dir, "user.lua", src)
			link := filepath.Join(tempDir(t), "user.lua")
			if err := os.Symlink(dir, link); err != nil {
				t.Fatal(err)
			}
			return link
		}, false},
		{"fifo", func(t *testing.T) string {
			// Reading one would block for ever: it must not be opened.
			fifo := filepath.Join(tempDir(t), "user.lua")
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			return fifo
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProse(t, proseOpts{user: tc.make(t)})
			if got := p.ranUser(t); got != tc.ran {
				t.Errorf("user.lua ran = %v, want %v", got, tc.ran)
			}
			if u := p.UserConfig(); u != (UserConfig{}) {
				t.Errorf("UserConfig = %+v, want it empty", u)
			}
			p.notBlocked(t)
			checkProseOptions(t, tc.name, p.state(t), "")
			if m := p.messages(t); m != "" {
				t.Errorf("messages = %q, want none: a missing file is not worth one", m)
			}
			if now, def := p.messagesopt(t); now != def {
				t.Errorf("messagesopt = %q, want Neovim's own %q", now, def)
			}
		})
	}
}

// TestUNoWrite: Neovim reads the file and nothing more: its bytes and its
// modification time stay as they were. (That the Go side that starts Neovim does
// not write it either is tested in the main package.)
func TestUNoWrite(t *testing.T) {
	const src = "vim.g.user_ran = true\nvim.o.number = true\n"
	path := userLua(t, src)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := newProse(t, proseOpts{user: path})
	if err := p.Close(testCtx(t)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != src {
		t.Errorf("user.lua = %q, %v; want it unchanged", got, err)
	}
	after, err := os.Stat(path)
	if err != nil || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		t.Errorf("user.lua stat changed: %v -> %v, %v", before, after, err)
	}
}

// TestUOpts: what the file may change, and does.
func TestUOpts(t *testing.T) {
	p := startUser(t, `
vim.g.user_ran = true
vim.o.number = true
vim.o.wrap = false
vim.o.spell = false
vim.o.spelllang = "en"
vim.o.linebreak = false
vim.cmd.highlight("Normal guifg=#123456")
vim.keymap.set("n", "<leader>x", function() vim.g.mapped = true end)
vim.api.nvim_create_autocmd("InsertEnter", {
  group = vim.api.nvim_create_augroup("mine", { clear = true }),
  callback = function() vim.g.entered_insert = true end,
})
`)
	if !p.ranUser(t) {
		t.Fatal("user.lua did not run")
	}
	st := p.state(t)
	if !st.Number || st.Wrap || st.Spell || st.Spelllang != "en" || st.Linebreak {
		t.Errorf("the file's options do not win over init.lua's: %+v", st)
	}
	var fg float64
	p.lua(t, &fg, `return vim.api.nvim_get_hl(0, { name = "Normal" }).fg`)
	if int(fg) != 0x123456 {
		t.Errorf("Normal fg = %#x, want 0x123456", int(fg))
	}

	p.write(t, " x")
	eventually(t, "the mapping of the file", func() bool { return p.global(t, "mapped") == true })
	p.write(t, "i")
	eventually(t, "the autocmd of the file", func() bool { return p.global(t, "entered_insert") == true })
	p.write(t, "\x1b")
	eventually(t, "normal mode", func() bool { return p.state(t).Mode == "n" })

	// Pisar's own mappings are still there.
	var found bool
	p.lua(t, &found, `
		for _, m in ipairs(vim.api.nvim_get_keymap("n")) do
			if m.desc == "Pisar: toggle spell checking" then return true end
		end
		return false`)
	if !found {
		t.Error("a mapping of Pisar's is gone")
	}
	p.notBlocked(t)
}

// TestULead: the <leader> mappings may be changed or removed, and a new
// mapleader applies to the file's own mappings only.
func TestULead(t *testing.T) {
	t.Run("remap", func(t *testing.T) {
		p := startUser(t, `
vim.g.user_ran = true
vim.keymap.set("n", "<leader>p", function() vim.g.mine = true end)
vim.keymap.del("n", "<leader>f")
`)
		p.write(t, " p")
		eventually(t, "the file's <leader>p", func() bool { return p.global(t, "mine") == true })
		p.write(t, " e") // untouched
		waitKind(t, p, EventFocusTree)
		p.write(t, " f") // removed: nothing is sent; "f" waits for a character
		p.write(t, "\x1b")
		evs := settled(t, p)
		if n := len(only(evs, EventTogglePreview)); n != 0 {
			t.Errorf("<leader>p still reaches Go: %d events", n)
		}
		if n := len(only(evs, EventOpenSearch)); n != 0 {
			t.Errorf("<leader>f still reaches Go: %d events", n)
		}
	})
	t.Run("leader", func(t *testing.T) {
		p := startUser(t, `
vim.g.user_ran = true
vim.g.mapleader = ","
vim.keymap.set("n", "<leader>y", function() vim.g.comma = true end)
`)
		p.write(t, ",y")
		eventually(t, "the file's mapping on the new leader", func() bool { return p.global(t, "comma") == true })
		// Pisar's own stay on the space: <leader>s toggles spelling.
		if !p.state(t).Spell {
			t.Fatal("spell is not on to begin with")
		}
		p.write(t, " s")
		eventually(t, "<leader>s on the space", func() bool { return !p.state(t).Spell })
		p.write(t, " p")
		if n := len(only(settled(t, p), EventTogglePreview)); n != 1 {
			t.Errorf("<leader>p on the space: %d events, want 1", n)
		}
	})
}

// TestUErr: a mistake in the file does not stop the start, tells the user
// and leaves everything of Pisar's in place.
func TestUErr(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		first string // what the first line of the error starts with
		ran   bool   // the first command ran (the rest of the file does not after an error)
	}{
		{"syntax", "vim.g.user_ran = true\nlocal = = 1\n", "user.lua:2:", false},
		{"run", "vim.g.user_ran = true\nvim.o.number = true\nerror('boom')\nvim.o.wrap = false\n", "user.lua:3: boom", true},
		{"multi", "vim.g.user_ran = true\nerror('first\\nsecond\\nthird')\n", "user.lua:2: first", true},
		{"long", "vim.g.user_ran = true\nerror(string.rep('long ', 200))\n", "user.lua:2: long long", true},
		{"level0", "vim.g.user_ran = true\nerror('no position', 0)\n", "user.lua: no position", true},
		{"table", "vim.g.user_ran = true\nerror({ code = 1 })\n", "user.lua: table:", true},
		{"cmd", "vim.g.user_ran = true\nvim.cmd('nonexistentcommand')\n", "user.lua: Vim:E492", true},
		{"notify", "vim.g.user_ran = true\nvim.notify('an error message', vim.log.levels.ERROR)\n", "user.lua: an error message", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := userLua(t, tc.src)
			p, _ := newProse(t, proseOpts{user: path})
			p.notBlocked(t)
			if got := p.ranUser(t); got != tc.ran {
				t.Errorf("first command ran = %v, want %v", got, tc.ran)
			}
			u := p.UserConfig()
			if !strings.HasPrefix(u.notice(), tc.first) {
				t.Errorf("notice = %q, want it to start with %q", u.notice(), tc.first)
			}
			if u.Error == "" || u.Note != "" {
				t.Errorf("UserConfig = %+v, want an error and no note", u)
			}
			if got, ok := p.global(t, "pisar_user_error").(string); !ok || got != u.Error {
				t.Errorf("g:pisar_user_error = %#v, want the text Go has: %q", got, u.Error)
			}
			if strings.Contains(u.Error, "init.lua") || strings.Contains(u.Error, "...") ||
				strings.Contains(u.Error, filepath.Base(filepath.Dir(path))) {
				t.Errorf("the error shows a path or the frames of init.lua: %q", u.Error)
			}
			if tc.name == "multi" && !strings.Contains(u.Error, "third") {
				t.Errorf("the full text lost lines: %q", u.Error)
			}

			// One line on the message line, within its width (80 columns, minus
			// what the ruler area keeps), kept in :messages.
			lines := strings.Split(strings.TrimSpace(p.messages(t)), "\n")
			if len(lines) != 1 || !strings.HasPrefix(lines[0], tc.first[:min(len(tc.first), 8)]) {
				t.Errorf("messages = %q, want one line of the error", lines)
			}
			if w := len([]rune(lines[0])); w > 68 {
				t.Errorf("the message is %d cells wide, over 68", w)
			}

			// Pisar's side is intact, also after an error.
			st := p.state(t)
			if !st.Hidden || !strings.Contains(st.Shortmess, "t") || st.Laststatus != 2 || st.Cmdheight != 1 {
				t.Errorf("state after an error: %+v", st)
			}
		})
	}
}

// TestUOut: what the file prints while it loads, or later in the start,
// does not freeze it.
func TestUOut(t *testing.T) {
	var many strings.Builder
	many.WriteString("vim.g.user_ran = true\n")
	many.WriteString("for i = 1, 30 do print('line ' .. i) end\n")
	many.WriteString("print(string.rep('long ', 100))\n")
	many.WriteString("vim.notify('a notice')\nvim.cmd([[echo 'echoed']])\nvim.cmd([[echomsg 'kept']])\n")
	many.WriteString("vim.cmd([[echo join(range(100), \"\\n\")]])\n")
	tests := []struct {
		name string
		src  string
		out  string // g:pisar_user_output has this
		msg  string // :messages has this
	}{
		{"print", many.String(), "line 30", ""},
		{"later", "vim.g.user_ran = true\nvim.schedule(function() print('sched1\\nsched2\\nsched3') end)\n", "", "sched3"},
		{"enter", "vim.g.user_ran = true\nvim.api.nvim_create_autocmd('VimEnter', { callback = function() print('e1\\ne2\\ne3') end })\n", "", "e3"},
		{"warn", "vim.g.user_ran = true\nvim.notify('warn\\nline2', vim.log.levels.WARN)\n", "line2", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			p := startUser(t, tc.src)
			p.notBlocked(t)
			t.Logf("started in %v", time.Since(start))
			if !p.ranUser(t) {
				t.Error("user.lua did not run")
			}
			if u := p.UserConfig(); u != (UserConfig{}) {
				t.Errorf("UserConfig = %+v, want it empty: printing is not a failure", u)
			}
			if tc.out != "" {
				if got, _ := p.global(t, "pisar_user_output").(string); !strings.Contains(got, tc.out) {
					t.Errorf("g:pisar_user_output = %q, want it to hold %q", got, tc.out)
				}
			}
			if tc.msg != "" {
				eventually(t, "the message in :messages", func() bool { return strings.Contains(p.messages(t), tc.msg) })
			}
			// The prompts are Neovim's own again.
			eventually(t, "'messagesopt' back", func() bool {
				now, def := p.messagesopt(t)
				return now == def
			})
		})
	}

	t.Run("mopt", func(t *testing.T) {
		// A 'messagesopt' of the user's own is what is there after the start.
		p := startUser(t, "vim.g.user_ran = true\nvim.o.messagesopt = 'hit-enter,history:77'\n")
		eventually(t, "the file's 'messagesopt'", func() bool {
			now, _ := p.messagesopt(t)
			return now == "hit-enter,history:77"
		})
	})
}

// TestUIn: a file that asks for a key while it loads still stops the
// start. Nothing in Neovim can answer for the user at that point; what is
// guaranteed is that Start ends with an error at its time limit and leaves no
// process behind.
func TestUIn(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not found in PATH")
	}
	initLua, err := filepath.Abs(filepath.Join("..", "..", "nvim", "init.lua"))
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"input":   "vim.fn.input('Name: ')\n",
		"confirm": "vim.fn.confirm('Sure?', '&Yes\\n&No')\n",
		"getchar": "vim.fn.getchar()\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			o := Options{
				Bin: "nvim", InitLua: initLua, AppName: testAppTag, SocketDir: dir,
				Cols: 80, Rows: 24, StartTimeout: time.Second, StopTimeout: testWait,
				Env: []string{
					"XDG_CONFIG_HOME=" + dir, "XDG_DATA_HOME=" + dir,
					"XDG_STATE_HOME=" + dir, "XDG_CACHE_HOME=" + dir, "TMPDIR=" + dir,
					"PISAR_USER_LUA=" + userLua(t, src),
				},
			}
			begin := time.Now()
			p, err := Start(context.Background(), o)
			if err == nil {
				_ = p.Close(context.Background())
				t.Fatal("Start succeeded, want it to time out")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("err = %v, want it to wrap %v", err, context.DeadlineExceeded)
			}
			if took := time.Since(begin); took > 5*time.Second {
				t.Errorf("Start took %v", took)
			}
			if nvimAlive(t, dir) {
				t.Error("a Neovim process started by this test is still alive")
			}
		})
	}
}

// TestUKeep: what Go relies on is back after a file that undoes it.
func TestUKeep(t *testing.T) {
	groups := func(p *Process) (n [2]int) {
		p.lua(t, &n, `
			return {
				#vim.api.nvim_get_autocmds({ group = "PisarNotify" }),
				#vim.api.nvim_get_autocmds({ group = "PisarFormat" }),
			}`)
		return n
	}
	control, _ := newProse(t, proseOpts{})
	wantGroups := groups(control)
	if wantGroups[0] == 0 || wantGroups[1] == 0 {
		t.Fatalf("the control has no autocmds: %v", wantGroups)
	}

	tests := []struct {
		name string
		src  string
	}{
		{"all", `
vim.g.user_ran = true
vim.o.hidden = false
vim.o.shortmess = ""
vim.o.laststatus = 0
vim.o.cmdheight = 3
vim.api.nvim_del_augroup_by_name("PisarNotify")
vim.api.nvim_del_augroup_by_name("PisarFormat")
_G.pisar_format = nil
`},
		{"some", `
vim.g.user_ran = true
vim.opt.shortmess:remove({ "t", "T" })
vim.api.nvim_clear_autocmds({ group = "PisarNotify", event = { "BufWritePost", "VimLeavePre", "TextChanged" } })
_G.pisar_format = function() return "not Pisar's" end
`},
		{"none", "vim.g.user_ran = true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := startUser(t, tc.src)
			if !p.ranUser(t) {
				t.Fatal("user.lua did not run")
			}
			p.notBlocked(t)
			st := p.state(t)
			if !st.Hidden || st.Laststatus != 2 || st.Cmdheight != 1 ||
				!strings.Contains(st.Shortmess, "t") || !strings.Contains(st.Shortmess, "T") {
				t.Errorf("not set again: hidden %v, laststatus %d, cmdheight %d, shortmess %q",
					st.Hidden, st.Laststatus, st.Cmdheight, st.Shortmess)
			}
			if got := groups(p); got != wantGroups {
				t.Errorf("autocmds in PisarNotify and PisarFormat = %v, want %v: lost or doubled", got, wantGroups)
			}

			ctx := testCtx(t)
			dir := tempDir(t)
			a := mkFile(t, dir, "a.md", "alpha\n")
			b := mkFile(t, dir, "b.md", "beta\n")

			// The events Go listens to arrive, and once each.
			if err := p.OpenFile(ctx, a); err != nil {
				t.Fatal(err)
			}
			wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a})
			p.write(t, "ix\x1b")
			wantEvents(t, p, Event{Kind: EventModifiedChanged, Path: a, Modified: true})
			p.write(t, ":w\r")
			wantEvents(t, p, Event{Kind: EventSaved, Path: a}, Event{Kind: EventModifiedChanged, Path: a})
			if extra := settled(t, p); len(extra) != 0 {
				t.Errorf("more events than expected: %+v", extra)
			}

			// The preview's change events: one per typed character.
			if err := p.SetPreview(ctx, true); err != nil {
				t.Fatal(err)
			}
			settled(t, p)
			p.write(t, "iy")
			waitKind(t, p, EventChanged)
			p.write(t, "\x1b")
			if n := changedCount(settled(t, p)); n != 0 {
				t.Errorf("%d change events too many for one character", n)
			}

			// Opening a document while another has unsaved changes: 'hidden'.
			if err := p.OpenFile(ctx, b); err != nil {
				t.Fatalf("opening a document with unsaved changes in another: %v", err)
			}
			if cur, err := p.Current(ctx); err != nil || cur.Path != b {
				t.Errorf("Current = %+v, %v; want %s", cur, err, b)
			}
			if mod, err := p.Modified(ctx); err != nil || len(mod) != 1 || mod[0].Path != a {
				t.Errorf("Modified = %+v, %v; want only %s", mod, err, a)
			}

			// Format is Pisar's, whatever the file put there.
			p.setBuffer(t, "word")
			if err := p.Format(ctx, "bold"); err != nil {
				t.Fatal(err)
			}
			if got := p.lines(t); len(got) != 1 || got[0] != "**word**" {
				t.Errorf("after Format bold: %q", got)
			}
		})
	}
}

// TestUSpell: a 'spelllang' with a language that has no dictionary is
// corrected, with a note, instead of asking to download it.
func TestUSpell(t *testing.T) {
	tests := []struct {
		name   string
		set    string // 'spelllang' of the file
		noDict bool   // Pisar's Croatian dictionary is missing
		want   string
		note   string
	}{
		{"de", "hr,en,de", false, "hr,en", "de"},
		{"only", "de", false, "hr,en", "de"},
		{"two", "fr,de,en", false, "en", "fr,de"},
		{"region", "en_us,pt_br", false, "en_us", "pt_br"},
		{"ok", "en_us,hr", false, "en_us,hr", ""},
		{"nohr", "hr,en", true, "en", "hr"},
		{"odd", "en,a-b", false, "en", "a-b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := userLua(t, "vim.g.user_ran = true\nvim.o.spelllang = '"+tc.set+"'\n")
			p, _ := newProse(t, proseOpts{user: path, noDict: tc.noDict})
			p.notBlocked(t)
			if got := p.state(t).Spelllang; got != tc.want {
				t.Errorf("spelllang = %q, want %q", got, tc.want)
			}
			u := p.UserConfig()
			if tc.note == "" {
				if u != (UserConfig{}) {
					t.Errorf("UserConfig = %+v, want it empty", u)
				}
				return
			}
			if u.Error != "" || !strings.Contains(u.Note, tc.note) {
				t.Errorf("UserConfig = %+v, want a note naming %q and no error", u, tc.note)
			}
			if m := p.messages(t); !strings.Contains(m, "spelllang") || strings.Count(strings.TrimSpace(m), "\n") != 0 {
				t.Errorf("messages = %q, want one line about spelllang", m)
			}
		})
	}
}

// TestUSpellOff: the core's own VimEnter autocmd for a missing language ends
// in "set spell"; a file that switched spelling off keeps it off.
func TestUSpellOff(t *testing.T) {
	p := startUser(t, "vim.g.user_ran = true\nvim.o.spelllang = 'de'\nvim.o.spell = false\n")
	p.notBlocked(t)
	if st := p.state(t); st.Spell || st.Spelllang != "hr,en" {
		t.Errorf("spell %v, spelllang %q; want spelling off and the languages Pisar chose", st.Spell, st.Spelllang)
	}
	if m := p.messages(t); strings.Contains(m, "Download") || strings.Contains(m, "Created") {
		t.Errorf("messages = %q: the download question was asked", m)
	}
}
