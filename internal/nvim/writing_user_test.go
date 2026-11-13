package nvim

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
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
			if m.desc == ... then return true end
		end
		return false`, testTexts(t)["nvimMapSpell"])
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
		first string // what the first line of the error (the reason in the message) starts with
		ran   bool   // the first command ran (the rest of the file does not after an error)
	}{
		{"syntax", "vim.g.user_ran = true\nlocal = = 1\n", "user.lua:2:", false},
		{"run", "vim.g.user_ran = true\nvim.o.number = true\nerror('boom')\nvim.o.wrap = false\n", "user.lua:3: boom", true},
		{"multi", "vim.g.user_ran = true\nerror('first\\nsecond\\nthird')\n", "user.lua:2: first", true},
		{"long", "vim.g.user_ran = true\nerror(string.rep('long ', 200))\n", "user.lua:2: long long", true},
		{"level0", "vim.g.user_ran = true\nerror('no position', 0)\n", "no position", true},
		{"table", "vim.g.user_ran = true\nerror({ code = 1 })\n", "table:", true},
		{"cmd", "vim.g.user_ran = true\nvim.cmd('nonexistentcommand')\n", "Vim:E492", true},
		{"notify", "vim.g.user_ran = true\nvim.notify('an error message', vim.log.levels.ERROR)\n", "an error message", true},
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
			const frame = "user.lua: greška (cijela: :echo g:pisar_user_error): "
			if n := u.notices(p.texts); len(n) != 1 || !strings.HasPrefix(n[0], frame+tc.first) {
				t.Errorf("notices = %q, want one that starts with %q", n, frame+tc.first)
			}
			if u.Error == "" || u.Spell != "" || u.Events != "" || u.Restore != "" {
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
			if len(lines) != 1 || !strings.HasPrefix(lines[0], frame) {
				t.Errorf("messages = %q, want one line of Pisar's text about the error", lines)
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
			if u.Error != "" || u.Spell != tc.note || u.Events != "" || u.Restore != "" {
				t.Errorf("UserConfig = %+v, want the languages %q and nothing else", u, tc.note)
			}
			if m := strings.TrimSpace(p.messages(t)); !strings.HasPrefix(m, "Nema rječnika za: "+tc.note) || strings.Contains(m, "\n") {
				t.Errorf("messages = %q, want one line about the missing dictionary of %q", m, tc.note)
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

// userFlow runs through the events Go listens to, with the preview on for the
// last part, and requires each exactly once. It is what a user.lua that ignores
// events must not be able to silence.
func userFlow(t *testing.T, p *Process) {
	t.Helper()
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "alpha\n")
	b := mkFile(t, dir, "b.md", "beta\n")

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
	// Going to another buffer is announced (and is a change for the preview).
	if err := p.OpenFile(ctx, b); err != nil {
		t.Fatal(err)
	}
	evs := settled(t, p)
	if !hasEvent(evs, Event{Kind: EventBufferEntered, Path: b}) {
		t.Errorf("no buffer entered for %s: %+v", b, evs)
	}
}

// ignoring returns 'eventignore', the global 'eventignorewin' and that of the
// current window.
func (p *Process) ignoring(t *testing.T) (ei, eiwGlobal, eiwWin string) {
	t.Helper()
	var v []string
	p.lua(t, &v, `
		return {
			vim.go.eventignore,
			vim.go.eventignorewin,
			vim.api.nvim_get_option_value("eventignorewin", { scope = "local", win = 0 }),
		}`)
	return v[0], v[1], v[2]
}

// TestUEi: a user.lua that ignores events cannot silence what Go listens to:
// the events are taken out of the options, the file's other entries stay, and
// the user is told.
func TestUEi(t *testing.T) {
	const every = "BufWritePost,BufEnter,BufFilePost,TextChanged,TextChangedI,TextChangedP," +
		"BufReadPost,BufModifiedSet,BufWipeout,VimLeavePre,InsertLeave"
	tests := []struct {
		name string
		src  string
		want [3]string // 'eventignore', 'eventignorewin' global and of the window
		note string    // "" for no note at all
	}{
		{"all", `vim.o.eventignore = "all"`, [3]string{"", "", ""}, "all"},
		{"every", `vim.o.eventignore = "` + every + `,CursorHold"`, [3]string{"CursorHold", "", ""}, "BufEnter"},
		{"case", `vim.o.eventignore = "bufwritepost,CursorHold,BUFENTER"`, [3]string{"CursorHold", "", ""}, "bufwritepost"},
		{"alias", `vim.o.eventignore = "BufRead,CursorHold"`, [3]string{"CursorHold", "", ""}, "BufRead"},
		{"minus", `vim.o.eventignore = "all,-CursorHold,-BufWritePost"`, [3]string{"-CursorHold", "", ""}, "all"},
		{"winall", `vim.o.eventignorewin = "all"`, [3]string{"", "", ""}, "all"},
		{"winone", `vim.o.eventignorewin = "BufWritePost,CursorHold,TextChanged"`, [3]string{"", "CursorHold", "CursorHold"}, "BufWritePost"},
		{"winloc", `vim.api.nvim_set_option_value("eventignorewin", "all,-CursorHold", { scope = "local" })`,
			[3]string{"", "", "-CursorHold"}, "all"},
		{"winglob", `vim.api.nvim_set_option_value("eventignorewin", "BufEnter", { scope = "global" })`,
			[3]string{"", "", ""}, "BufEnter"},
		{"both", "vim.o.eventignore = 'all'\nvim.o.eventignorewin = 'all'", [3]string{"", "", ""}, "all"},
		{"other", `vim.o.eventignore = "CursorHold,CursorMoved"
vim.o.eventignorewin = "CursorHold"`, [3]string{"CursorHold,CursorMoved", "CursorHold", "CursorHold"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := startUser(t, "vim.g.user_ran = true\n"+tc.src+"\n")
			if !p.ranUser(t) {
				t.Fatal("user.lua did not run")
			}
			p.notBlocked(t)
			ei, eiwG, eiwW := p.ignoring(t)
			if got := [3]string{ei, eiwG, eiwW}; got != tc.want {
				t.Errorf("eventignore, eventignorewin (global, window) = %q, want %q", got, tc.want)
			}
			u := p.UserConfig()
			switch {
			case tc.note == "":
				if u != (UserConfig{}) {
					t.Errorf("UserConfig = %+v, want it empty: nothing of Pisar's was ignored", u)
				}
			case u.Error != "" || u.Spell != "" || u.Restore != "" || !strings.Contains(u.Events, tc.note):
				t.Errorf("UserConfig = %+v, want the events taken out, naming %q, and nothing else", u, tc.note)
			}
			if tc.note != "" {
				if m := strings.TrimSpace(p.messages(t)); !strings.HasPrefix(m, "user.lua je isključio događaje") || strings.Contains(m, "\n") {
					t.Errorf("messages = %q, want one line about the events", m)
				}
			}
			userFlow(t, p)
		})
	}
}

// TestUOne: each event of the two groups, ignored alone, comes back. The events
// are read from the groups themselves, so the test follows init.lua.
func TestUOne(t *testing.T) {
	control, _ := newProse(t, proseOpts{})
	var events []string
	control.lua(t, &events, `
		local seen, list = {}, {}
		for _, group in ipairs({ "PisarNotify", "PisarFormat" }) do
			for _, au in ipairs(vim.api.nvim_get_autocmds({ group = group })) do
				if not seen[au.event] then
					seen[au.event] = true
					table.insert(list, au.event)
				end
			end
		end
		table.sort(list)
		return list`)
	if len(events) < 8 {
		t.Fatalf("events of the groups = %v", events)
	}
	for i, event := range events {
		t.Run("e"+strconv.Itoa(i), func(t *testing.T) {
			p := startUser(t, "vim.o.eventignore = '"+event+",CursorHold'\nvim.o.eventignorewin = '"+event+"'\n")
			ei, eiwG, eiwW := p.ignoring(t)
			if ei != "CursorHold" || eiwG != "" || eiwW != "" {
				t.Errorf("%s ignored alone: eventignore %q, eventignorewin %q and %q", event, ei, eiwG, eiwW)
			}
			if events := p.UserConfig().Events; !strings.Contains(events, event) {
				t.Errorf("events taken out = %q, want it to name %s", events, event)
			}
		})
	}
}

// TestURel: Pisar's own variables are unset after the file, whatever it put in
// them. A true g:pisar_reloading would silence every buffer switch.
func TestURel(t *testing.T) {
	p := startUser(t, `
vim.g.user_ran = true
vim.g.pisar_reloading = true
vim.g.pisar_preview = true
vim.g.pisar_channel = "not a channel"
`)
	if !p.ranUser(t) {
		t.Fatal("user.lua did not run")
	}
	p.notBlocked(t)
	for _, name := range []string{"pisar_reloading", "pisar_preview"} {
		if v := p.global(t, name); v != nil && v != false {
			t.Errorf("g:%s = %#v after the file, want it unset", name, v)
		}
	}
	// With the preview off typing sends no change event, buffer switches are
	// announced, and Go's own channel is in place (userFlow depends on all three).
	userFlow(t, p)
	if u := p.UserConfig(); u != (UserConfig{}) {
		t.Errorf("UserConfig = %+v, want it empty: nothing was undone that needs a note", u)
	}
}

// TestUVar: the variables init.lua reports in cannot be made to fail the start
// by a file that puts something other than text in them.
func TestUVar(t *testing.T) {
	tests := []struct{ name, value string }{
		{"table", "{ 1, 2 }"},
		{"dict", "{ code = 1 }"},
		{"number", "5"},
		{"float", "1.5"},
		{"bool", "true"},
		{"nil", "vim.NIL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := startUser(t, "vim.g.user_ran = true\nvim.g.pisar_user_error = "+tc.value+"\nvim.g.pisar_user_note = "+tc.value+"\n")
			if !p.ranUser(t) {
				t.Fatal("user.lua did not run")
			}
			p.notBlocked(t)
			if u := p.UserConfig(); u != (UserConfig{}) {
				t.Errorf("UserConfig = %+v, want it empty: a value that is no text is no report", u)
			}
			if m := p.messages(t); m != "" {
				t.Errorf("messages = %q, want none", m)
			}
		})
	}
}

// TestUMsg: everything Pisar tells about user.lua is Croatian, one line each,
// cut to the screen without cutting a character, and the most important one
// (the error) is the one left on the screen. Neovim has all three to say: a
// language without a dictionary, an ignored event and an error.
func TestUMsg(t *testing.T) {
	const src = `vim.g.user_ran = true
vim.o.spelllang = "de"
vim.o.eventignore = "all"
error("šđčćž " .. string.rep("ŽĐČĆŠ", 30))
`
	tests := []struct {
		name  string
		cols  uint16
		texts bool
		want  []string // what the three lines start with
	}{
		{"c80", 80, true, []string{"Nema rječnika za: de.", "user.lua je isključio događaje koje", "user.lua: greška (cijela: :echo g:pisar_user_error): user.lua:4: "}},
		{"c40", 40, true, []string{"Nema rječnika za: de. Pravo…", "user.lua je isključio događ…", "user.lua: greška (cijela: :…"}},
		{"c20", 20, true, []string{"Nema rj…", "user.lu…", "user.lu…"}},
		// No texts: the facts alone, without Pisar's sentences.
		{"raw", 80, false, []string{"de", "all", "user.lua:4: šđčćž"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProse(t, proseOpts{cols: tc.cols, user: userLua(t, src), noTexts: !tc.texts})
			p.notBlocked(t)
			lines := strings.Split(strings.TrimSpace(p.messages(t)), "\n")
			if len(lines) != len(tc.want) {
				t.Fatalf("messages = %q, want %d lines", lines, len(tc.want))
			}
			for i, line := range lines {
				if !utf8.ValidString(line) {
					t.Errorf("line %d is cut inside a character: %q", i, line)
				}
				if w := utf8.RuneCountInString(line); w > int(tc.cols)-12 {
					t.Errorf("line %d is %d cells wide, at most %d fit: %q", i, w, int(tc.cols)-12, line)
				}
				if !strings.HasPrefix(line, tc.want[i]) {
					t.Errorf("line %d = %q, want it to start with %q", i, line, tc.want[i])
				}
			}
			// At 80 columns the hint to the full text is whole and it is the reason
			// that is cut: the hint comes first.
			if tc.name == "c80" && !strings.HasSuffix(lines[2], "…") {
				t.Errorf("line 2 = %q, want the reason cut with an ellipsis", lines[2])
			}
			// The full texts are not lost by the cut.
			if u := p.UserConfig(); u.Spell != "de" || u.Events != "all" || !strings.Contains(u.Error, "ŽĐČĆŠŽĐČĆŠ") {
				t.Errorf("UserConfig = %+v", u)
			}
		})
	}
}

// TestUFail: when init.lua cannot restore what Go relies on after user.lua, the
// start still succeeds and the user is told, in Croatian, with the reason.
func TestUFail(t *testing.T) {
	const src = "vim.g.user_ran = true\nvim.opt = nil\n"
	for _, cols := range []uint16{80, 40} {
		t.Run("c"+strconv.Itoa(int(cols)), func(t *testing.T) {
			p, _ := newProse(t, proseOpts{cols: cols, user: userLua(t, src)})
			p.notBlocked(t)
			u := p.UserConfig()
			if !strings.Contains(u.Restore, "opt") || u.Error != "" || u.Spell != "" || u.Events != "" {
				t.Errorf("UserConfig = %+v, want only the reason of the failed restore", u)
			}
			m := strings.TrimSpace(p.messages(t))
			if strings.Contains(m, "\n") || !strings.HasPrefix(m, "Postavke nakon user.lua nisu"[:20]) ||
				!utf8.ValidString(m) || utf8.RuneCountInString(m) > int(cols)-12 {
				t.Errorf("messages = %q, want one line of Pisar's text within %d cells", m, int(cols)-12)
			}
			if strings.Contains(m, "restore after") {
				t.Errorf("messages = %q holds the old English frame", m)
			}
		})
	}
}

// TestUNoEn guards against the English sentences coming back: what Pisar puts in
// front of the user is Croatian and lives in frontend/src/strings.json, so none
// of the old texts may be in the Lua file or in the Go files that send text to
// Neovim.
func TestUNoEn(t *testing.T) {
	old := []string{
		"Neovim is waiting", "waiting for an answer", "no dictionary for", "taken out",
		"restore after", "No file name for buffer", "E141", "nothing was written", "nothing was reloaded", "no buffer was removed",
		`"Pisar: " ..`, "Formatting failed", "format failed",
		// The descriptions of the mappings.
		"move by display rows", "toggle spell", "toggle line numbers", "focus the project tree",
		"toggle the preview", "open the document search", `"Pisar: format "`,
	}
	for _, file := range []string{
		filepath.Join("..", "..", "nvim", "init.lua"), "nvim.go", "userlua.go", "format.go", "preview.go",
	} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range old {
			if strings.Contains(string(data), text) {
				t.Errorf("%s holds %q: that sentence belongs in strings.json, in Croatian", file, text)
			}
		}
	}
	// Each text the package takes is in the file, and says what it has to.
	texts := testTexts(t)
	for key, placeholder := range map[string]string{
		"nvimFormatFailed": "{reason}", "nvimUserLuaError": "{reason}", "nvimSpellMissing": "{languages}",
		"nvimEventsRestored": "{events}", "nvimRestoreFailed": "{reason}", "nvimPromptOpen": "", "nvimNoFileName": "{buffer}",
	} {
		if texts[key] == "" || !strings.Contains(texts[key], placeholder) {
			t.Errorf("%s = %q, want a text with %q", key, texts[key], placeholder)
		}
	}
}

// pisarMaps are the mappings init.lua makes, with the key of the text that
// describes each (the rows' keys are made for normal and visual mode, and for
// insert mode only the arrows).
var pisarMaps = []struct{ mode, lhs, key string }{
	{"n", "j", "nvimMapRows"}, {"x", "j", "nvimMapRows"}, {"n", "k", "nvimMapRows"}, {"x", "k", "nvimMapRows"},
	{"n", "<Down>", "nvimMapRows"}, {"x", "<Down>", "nvimMapRows"}, {"i", "<Down>", "nvimMapRows"},
	{"n", "<Up>", "nvimMapRows"}, {"x", "<Up>", "nvimMapRows"}, {"i", "<Up>", "nvimMapRows"},
	{"n", "<leader>e", "nvimMapTree"}, {"n", "<leader>p", "nvimMapPreview"}, {"n", "<leader>f", "nvimMapSearch"},
	{"n", "<leader>?", "nvimMapShortcuts"},
	{"n", "<leader>s", "nvimMapSpell"}, {"n", "<leader>n", "nvimMapNumbers"},
	{"n", "<leader>b", "nvimMapBold"}, {"x", "<leader>b", "nvimMapBold"},
	{"n", "<leader>i", "nvimMapItalic"}, {"x", "<leader>i", "nvimMapItalic"},
	{"n", "<leader>h", "nvimMapHeading"}, {"x", "<leader>h", "nvimMapHeading"},
	{"n", "<leader>l", "nvimMapList"}, {"x", "<leader>l", "nvimMapList"},
	{"n", "<Space>", "nvimMapKeys"}, {"x", "<Space>", "nvimMapKeys"},
}

// TestUMap: every mapping of Pisar's has its description in Croatian, from
// PISAR_TEXT. Without the variable or with JSON that does not decode the
// mappings are there and work, with no description and no sentence of a
// fallback; the start does not fail.
func TestUMap(t *testing.T) {
	texts := testTexts(t)
	tests := []struct {
		name string
		env  string // PISAR_TEXT; "" leaves it as Options.Text makes it
		none bool   // no Options.Text
		want bool   // the descriptions are there
	}{
		{"texts", "", false, true},
		{"none", "", true, false},
		{"bad", "{not json", false, false},
		{"array", "[1, 2]", false, false},
		{"null", "null", false, false},
		{"notext", `{"nvimMapSpell": 5, "nvimMapBold": {"a": 1}}`, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProc(t, func(o *Options) {
				if tc.none {
					o.Text = nil
				}
				if tc.env != "" {
					o.Env = append(o.Env, textEnv+"="+tc.env)
				}
			})
			for _, m := range pisarMaps {
				var got struct {
					Exists bool
					Desc   string
				}
				p.lua(t, &got, `
					local m = vim.fn.maparg((...), select(2, ...), false, true)
					return { Exists = m.lhs ~= nil, Desc = m.desc or "" }`, m.lhs, m.mode)
				if !got.Exists {
					t.Errorf("no mapping of %s in mode %s", m.lhs, m.mode)
					continue
				}
				want := ""
				if tc.want {
					want = texts[m.key]
				}
				if got.Desc != want {
					t.Errorf("%s in mode %s has the description %q, want %q", m.lhs, m.mode, got.Desc, want)
				}
			}
			// They work: <leader>s switches spelling, <leader>b makes a word bold.
			p.setBuffer(t, "riječ")
			spell := p.state(t).Spell
			p.write(t, " s")
			eventually(t, "<leader>s", func() bool { return p.state(t).Spell != spell })
			p.write(t, " b")
			eventually(t, "<leader>b", func() bool {
				l := p.lines(t)
				return len(l) == 1 && l[0] == "**riječ**"
			})
		})
	}
}

// TestULate: what user.lua puts off until the end of Neovim's start is restored
// too: a scheduled call, a timer, a VimEnter autocmd of its own, a call that
// that autocmd schedules. The order is measured (see restore_again in
// init.lua): the file's VimEnter autocmds, then Pisar's pass, then the file's
// scheduled calls and its timers of 0 and 20 ms, then what its VimEnter autocmd
// scheduled, then Pisar's scheduled pass, and Go connects after all of them.
func TestULate(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		events string // what is reported as taken out of 'eventignore'
	}{
		{"sched", `vim.schedule(function() vim.o.eventignore = "all" end)`, "all"},
		{"defer0", `vim.defer_fn(function() vim.o.eventignore = "all" end, 0)`, "all"},
		{"defer20", `vim.defer_fn(function() vim.o.eventignore = "BufEnter,TextChanged" end, 20)`, "BufEnter,TextChanged"},
		{"enter", `vim.api.nvim_create_autocmd("VimEnter", { callback = function()
  vim.o.eventignore = "BufWritePost"
end })`, "BufWritePost"},
		{"hidden", `vim.api.nvim_create_autocmd("VimEnter", { callback = function()
  vim.g.pisar_reloading = true
  vim.o.hidden = false
  vim.o.laststatus = 0
end })`, ""},
		{"nested", `vim.api.nvim_create_autocmd("VimEnter", { callback = function()
  vim.schedule(function()
    vim.o.eventignore = "all"
    vim.o.hidden = false
    vim.g.pisar_reloading = true
  end)
end })`, "all"},
		{"chan", `vim.schedule(function() vim.g.pisar_channel = "not a channel" end)`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := startUser(t, "vim.g.user_ran = true\n"+tc.src+"\n")
			p.notBlocked(t)
			if ei, _, _ := p.ignoring(t); ei != "" {
				t.Errorf("eventignore = %q after the start, want it empty", ei)
			}
			if st := p.state(t); !st.Hidden || st.Laststatus != 2 {
				t.Errorf("hidden %v, laststatus %d after the start", st.Hidden, st.Laststatus)
			}
			if v := p.global(t, "pisar_reloading"); v != nil && v != false {
				t.Errorf("g:pisar_reloading = %#v, want it unset", v)
			}
			u := p.UserConfig()
			if tc.events == "" && u != (UserConfig{}) {
				t.Errorf("UserConfig = %+v, want it empty", u)
			}
			if tc.events != "" && u.Events != tc.events {
				t.Errorf("events taken out = %q, want %q, from the end of the start too", u.Events, tc.events)
			}
			if tc.events != "" {
				if m := p.messages(t); !strings.Contains(m, "user.lua je isključio događaje") {
					t.Errorf("messages = %q, want the note about the events", m)
				}
			}
			// The channel is Go's, and every event arrives once.
			userFlow(t, p)
		})
	}
}

// TestULateAnn: a pass that comes after Go has connected tells the user itself.
// The file's VimEnter autocmd waits until Go is connected and only then ignores
// the events: Go has read its notes by then, and Pisar's pass in the same
// VimEnter puts them back, leaves g:pisar_channel as Go set it and shows the
// note from the texts init.lua has.
func TestULateAnn(t *testing.T) {
	p := startUser(t, `vim.g.user_ran = true
vim.api.nvim_create_autocmd("VimEnter", { callback = function()
  vim.wait(5000, function() return vim.g.pisar_channel ~= nil end, 5)
  vim.o.eventignore = "all"
end })
`)
	var ch interface{}
	eventually(t, "the second pass", func() bool {
		ei, _, _ := p.ignoring(t)
		return ei == ""
	})
	if ch = p.global(t, "pisar_channel"); ch == nil || ch == false {
		t.Errorf("g:pisar_channel = %#v, want Go's channel", ch)
	}
	if u := p.UserConfig(); u != (UserConfig{}) {
		t.Errorf("UserConfig = %+v: Go had read it before the file ignored anything", u)
	}
	var note struct{ Events string }
	p.lua(t, &note, `return { Events = (vim.g.pisar_user_note or {}).events }`)
	if note.Events != "all" {
		t.Errorf("g:pisar_user_note = %+v, want the events of the late pass in it", note)
	}
	if m := strings.TrimSpace(p.messages(t)); strings.Count(m, "user.lua je isključio događaje") != 1 || strings.Contains(m, "\n") {
		t.Errorf("messages = %q, want the note once, from init.lua", m)
	}
	userFlow(t, p)
}

// TestULimit documents the limit of the protection: it covers the loading of
// user.lua and the end of Neovim's start, not later work. An autocmd of the
// user's that ignores an event on a later event still silences Pisar's. That is
// the user's own code and the limit is known (init.lua says so next to the
// list of what comes back); this test states today's behaviour so that a change
// of it is a decision.
func TestULimit(t *testing.T) {
	p := startUser(t, `vim.g.user_ran = true
vim.api.nvim_create_autocmd("BufWinEnter", { callback = function() vim.o.eventignore = "BufWritePost" end })
`)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "alpha\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	p.write(t, "ix\x1b:w\r")
	evs := settled(t, p)
	if ei, _, _ := p.ignoring(t); ei != "BufWritePost" {
		t.Fatalf("eventignore = %q, want the user's later setting to stand", ei)
	}
	if n := len(only(evs, EventSaved)); n != 0 {
		t.Errorf("%d saved events: the known limit no longer holds, update the comment in init.lua", n)
	}
}

// TestUFrame: the texts are filled in the same way in Lua (init.lua) and in Go
// (frame): every placeholder, in one pass, so a value that holds the placeholder
// itself is not replaced again, and "%" is just a character.
func TestUFrame(t *testing.T) {
	p, _ := newProc(t, nil)
	tests := []struct{ name, template, value, want string }{
		{"one", "a {x} b", "V", "a V b"},
		{"two", "{x} and {x}", "V", "V and V"},
		{"again", "[{x}]", "{x}", "[{x}]"},
		{"again two", "{x}-{x}", "q{x}r", "q{x}r-q{x}r"},
		{"percent", "{x} {x}", "100%1%%", "100%1%% 100%1%%"},
		{"other", "{y} {x}", "V", "{y} V"},
		{"none", "no placeholder", "V", "no placeholder"},
		{"empty", "", "V", "V"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			p.lua(t, &got, `return pisar_msg.framed(select(1, ...), "x", select(2, ...))`, tc.template, tc.value)
			if got != tc.want {
				t.Errorf("Lua: framed(%q, x, %q) = %q, want %q", tc.template, tc.value, got, tc.want)
			}
			if got := frame(tc.template, "x", tc.value); got != tc.want {
				t.Errorf("Go: frame(%q, x, %q) = %q, want %q", tc.template, tc.value, got, tc.want)
			}
		})
	}
}
