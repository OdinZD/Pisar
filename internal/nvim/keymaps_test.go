package nvim

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	nvimrpc "github.com/neovim/go-client/nvim"
)

// Tests of the list of shortcuts (_G.pisar_keys in nvim/init.lua and
// Process.Keymaps), in a real Neovim with the repository's init.lua. Names are
// short for the reason in writing_test.go: the test and its subtest together
// stay within 17 characters.

const (
	kmWide = 200 // plenty: nothing is cut
	kmMany = 500
)

// pisarKeymaps is the list init.lua should give without a user.lua: the mappings
// of Pisar's that have a description, in the order the list promises (mode n, x,
// i; in a mode by the keys without regard to case). It is derived from pisarMaps
// and the texts, not written down a second time.
func pisarKeymaps(t *testing.T) []Keymap {
	t.Helper()
	texts := testTexts(t)
	var out []Keymap
	for _, m := range pisarMaps {
		desc := texts[m.key]
		if desc == "" {
			t.Fatalf("no text %q", m.key)
		}
		out = append(out, Keymap{Mode: m.mode, Keys: strings.ReplaceAll(m.lhs, "<leader>", "<Space>"), Desc: desc})
	}
	sortKeymaps(out)
	return out
}

func sortKeymaps(maps []Keymap) {
	rank := map[string]int{"n": 0, "x": 1, "i": 2}
	slices.SortStableFunc(maps, func(a, b Keymap) int {
		if a.Mode != b.Mode {
			return rank[a.Mode] - rank[b.Mode]
		}
		if c := strings.Compare(strings.ToLower(a.Keys), strings.ToLower(b.Keys)); c != 0 {
			return c
		}
		return strings.Compare(a.Keys, b.Keys)
	})
}

func (p *Process) keymaps(t *testing.T, maxMaps, maxText int) Keymaps {
	t.Helper()
	got, err := p.Keymaps(testCtx(t), maxMaps, maxText)
	if err != nil {
		t.Fatalf("Keymaps(%d, %d): %v", maxMaps, maxText, err)
	}
	if got.Maps == nil || got.Groups == nil {
		t.Fatalf("Keymaps(%d, %d) has a nil part: %+v", maxMaps, maxText, got)
	}
	return got
}

// blocking asks for the mode, which Neovim answers also while it waits for a
// key; a Lua call would not be answered then.
func (p *Process) blocking(t *testing.T) bool {
	t.Helper()
	var blocking bool
	err := p.call(testCtx(t), "mode", func(nv *nvimrpc.Nvim) error {
		m, err := nv.Mode()
		if err == nil {
			blocking = m.Blocking
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return blocking
}

func hasKeymap(maps []Keymap, mode, keys string) (Keymap, bool) {
	for _, m := range maps {
		if m.Mode == mode && m.Keys == keys {
			return m, true
		}
	}
	return Keymap{}, false
}

// TestKeymaps: the list without a user.lua, in one Neovim; the cases that need a
// user.lua start their own.
func TestKeymaps(t *testing.T) {
	p, _ := newProc(t, nil)
	want := pisarKeymaps(t)

	t.Run("base", func(t *testing.T) {
		got := p.keymaps(t, kmMany, kmWide)
		if !slices.Equal(got.Maps, want) {
			t.Errorf("maps:\n got %+v\nwant %+v", got.Maps, want)
		}
		// Neovim's own mappings (English descriptions) are not the application's.
		for _, k := range []string{"gcc", "gc", "gx", "grn", "]d", "[d"} {
			if m, ok := hasKeymap(got.Maps, "n", k); ok {
				t.Errorf("Neovim's own %q is in the list: %+v", k, m)
			}
		}
		if len(got.Groups) != 0 {
			t.Errorf("groups = %v, want none", got.Groups)
		}
	})

	t.Run("snap", func(t *testing.T) {
		// The defaults really are there when init.lua starts, which is what the
		// snapshot at its top depends on.
		var has bool
		p.lua(t, &has, `
			for _, m in ipairs(vim.api.nvim_get_keymap("n")) do
				if m.lhsraw == "gcc" and m.desc ~= nil and m.desc ~= "" then
					return true
				end
			end
			return false`)
		if !has {
			t.Fatal("this Neovim has no default gcc with a description: the snapshot is not needed, or wrong")
		}
	})

	t.Run("limit", func(t *testing.T) {
		got := p.keymaps(t, 3, kmWide)
		if !slices.Equal(got.Maps, want[:3]) {
			t.Errorf("3 maps = %+v, want %+v", got.Maps, want[:3])
		}
		got = p.keymaps(t, 1, kmWide)
		if !slices.Equal(got.Maps, want[:1]) {
			t.Errorf("1 map = %+v, want %+v", got.Maps, want[:1])
		}
		for _, tc := range []struct {
			name           string
			maxMaps, maxTx int
		}{{"maps0", 0, 10}, {"text0", 10, 0}, {"both0", 0, 0}, {"neg", -1, 10}} {
			if _, err := p.Keymaps(testCtx(t), tc.maxMaps, tc.maxTx); err == nil {
				t.Errorf("%s: no error", tc.name)
			}
		}
		// The refusal is made before any call: the process still answers.
		p.keymaps(t, 1, 1)
	})

	t.Run("hit", func(t *testing.T) {
		// A message of two lines on the command line opens a hit-enter prompt.
		p.write(t, ":echo \"one\\ntwo\"\r")
		eventually(t, "a hit-enter prompt", func() bool { return p.blocking(t) })

		ctx, cancel := context.WithTimeout(context.Background(), testWait)
		defer cancel()
		start := time.Now()
		_, err := p.Keymaps(ctx, kmMany, kmWide)
		if !errors.Is(err, ErrPromptOpen) {
			t.Fatalf("Keymaps at a prompt = %v, want ErrPromptOpen", err)
		}
		if d := time.Since(start); d > testWait/4 {
			t.Errorf("the refusal took %v: it waited for the deadline", d)
		}

		p.write(t, "\r")
		eventually(t, "the prompt to close", func() bool { return !p.blocking(t) })
		if got := p.keymaps(t, kmMany, kmWide); !slices.Equal(got.Maps, want) {
			t.Errorf("after the prompt: %+v", got.Maps)
		}
	})

	t.Run("wk", func(t *testing.T) {
		p.setBuffer(t, "one")
		base := p.hooks(t)
		p.open(t)
		got := p.keymaps(t, kmMany, kmWide)
		if !slices.Equal(got.Maps, want) {
			t.Errorf("with the window open: %+v", got.Maps)
		}
		if n := len(p.floats(t)); n != 1 {
			t.Errorf("%d floating windows after the call, want the one that was open", n)
		}
		p.write(t, keyEsc)
		p.shut(t)
		p.hookGone(t, base)
	})
}

// userKeymaps is a user.lua that maps, names groups and puts in what must not
// be listed.
const userKeymaps = `
vim.g.user_ran = true
local function noop() end
vim.keymap.set("n", "<leader>u", noop, { desc = "user map" })
vim.keymap.set("n", "<leader>w", noop)
vim.keymap.set("n", "<leader><Down>", noop, { desc = "down" })
vim.keymap.set("n", "<leader>č", noop, { desc = "čćžšđ čćžšđ" })
vim.keymap.set("x", "<leader>u", noop, { desc = "user visual" })
vim.keymap.set("i", "<C-g>y", noop, { desc = "user insert" })
vim.keymap.set("n", "gx", noop, { desc = "my open" })
vim.keymap.set("n", "<leader>z", noop, { buffer = 0, desc = "only here" })
vim.g.pisar_groups = { g = "Git", bad = 5, e = "", h = "čćžšđ čćžšđ" }
`

func TestKeymapsUser(t *testing.T) {
	p := startUser(t, userKeymaps)
	if !p.ranUser(t) {
		t.Fatal("user.lua did not run")
	}
	got := p.keymaps(t, kmMany, kmWide)

	for _, c := range []struct{ mode, keys, desc string }{
		{"n", "<Space>u", "user map"},
		{"n", "<Space><Down>", "down"},
		{"n", "<Space>č", "čćžšđ čćžšđ"},
		{"x", "<Space>u", "user visual"},
		{"i", "<C-G>y", "user insert"},
		// Replaces Neovim's own gx, so it is the user's and is listed.
		{"n", "gx", "my open"},
	} {
		m, ok := hasKeymap(got.Maps, c.mode, c.keys)
		if !ok {
			t.Errorf("%s %s is not in the list: %+v", c.mode, c.keys, got.Maps)
			continue
		}
		if m.Desc != c.desc {
			t.Errorf("%s %s has the description %q, want %q", c.mode, c.keys, m.Desc, c.desc)
		}
	}
	if _, ok := hasKeymap(got.Maps, "n", "<Space>w"); ok {
		t.Error("a mapping without a description is in the list")
	}
	if _, ok := hasKeymap(got.Maps, "n", "<Space>z"); ok {
		t.Error("a mapping of one buffer is in the list")
	}
	for _, k := range []string{"gcc", "gc", "grn"} {
		if _, ok := hasKeymap(got.Maps, "n", k); ok {
			t.Errorf("Neovim's own %q is in the list", k)
		}
	}
	// The whole list, the user's and Pisar's together, is in the promised order.
	sorted := slices.Clone(got.Maps)
	sortKeymaps(sorted)
	if !slices.Equal(sorted, got.Maps) {
		t.Errorf("the list is not in order: %+v", got.Maps)
	}

	wantGroups := map[string]string{"g": "Git", "h": "čćžšđ čćžšđ"}
	if len(got.Groups) != len(wantGroups) || got.Groups["g"] != "Git" || got.Groups["h"] != "čćžšđ čćžšđ" {
		t.Errorf("groups = %v, want %v", got.Groups, wantGroups)
	}

	// The cut of the text: characters, never through a multibyte one.
	cut := p.keymaps(t, kmMany, 5)
	for _, m := range cut.Maps {
		if utf8.RuneCountInString(m.Keys) > 5 || utf8.RuneCountInString(m.Desc) > 5 || !utf8.ValidString(m.Keys+m.Desc) {
			t.Errorf("not cut to 5 characters: %+v", m)
		}
	}
	found := false
	for _, m := range cut.Maps {
		if m.Desc == "čćžšđ" {
			found = true
		}
	}
	if !found {
		t.Errorf("the description of <Space>č is not cut to \"čćžšđ\": %+v", cut.Maps)
	}
	if cut.Groups["h"] != "čćžšđ" || cut.Groups["g"] != "Git" {
		t.Errorf("group names not cut as they should: %v", cut.Groups)
	}

	// At most maxMaps groups too; the ones that stay are the first by key.
	one := p.keymaps(t, 1, kmWide)
	if len(one.Groups) != 1 || one.Groups["g"] != "Git" {
		t.Errorf("one group = %v, want only g", one.Groups)
	}
}

// A user.lua that deletes or replaces the function Go calls does not take the
// list away: restore_required puts it back.
func TestKeymapsGone(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"del", `_G.pisar_keys = nil`},
		{"swap", `_G.pisar_keys = function() return 5 end`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := startUser(t, "vim.g.user_ran = true\n"+tc.src+"\n")
			if !p.ranUser(t) {
				t.Fatal("user.lua did not run")
			}
			got := p.keymaps(t, kmMany, kmWide)
			if want := pisarKeymaps(t); !slices.Equal(got.Maps, want) {
				t.Errorf("maps = %+v, want %+v", got.Maps, want)
			}
		})
	}
}

// What the user puts in vim.g.pisar_groups does not make the call fail.
func TestKeymapsBad(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"num", `vim.g.pisar_groups = 5`},
		{"str", `vim.g.pisar_groups = "g"`},
		{"list", `vim.g.pisar_groups = { "x", "y" }`},
		{"vals", `vim.g.pisar_groups = { a = 5, b = { 1 }, c = true, d = "" }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := startUser(t, "vim.g.user_ran = true\n"+tc.src+"\n")
			if !p.ranUser(t) {
				t.Fatal("user.lua did not run")
			}
			got := p.keymaps(t, kmMany, kmWide)
			if len(got.Groups) != 0 {
				t.Errorf("groups = %v, want none", got.Groups)
			}
			if want := pisarKeymaps(t); !slices.Equal(got.Maps, want) {
				t.Errorf("maps = %+v, want %+v", got.Maps, want)
			}
		})
	}
}
