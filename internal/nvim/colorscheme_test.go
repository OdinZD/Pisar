package nvim

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	nvimrpc "github.com/neovim/go-client/nvim"
)

// Tests of Process.Colorscheme in a real Neovim. The colorschemes are the
// generator's own output (testdata/colorscheme), not text written here.
//
// Test names are short for the reason given in nvim_test.go.

// scheme reads a file of testdata/colorscheme.
func scheme(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "colorscheme", name+".lua"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// look is what the tests compare: the colors Neovim has in use.
type look struct {
	Background string
	Name       string // g:colors_name
	Fg, Bg     int    // of the Normal group
}

// normalRe finds the Normal group in the generator's output.
var normalRe = regexp.MustCompile(`nvim_set_hl\(0, "Normal", \{ fg = "#([0-9A-Fa-f]{6})", bg = "#([0-9A-Fa-f]{6})"`)

// want is the look the colorscheme text promises: the Normal colors are read
// from its first Normal line, the background from its header.
func want(t *testing.T, text, background string) look {
	t.Helper()
	m := normalRe.FindStringSubmatch(text)
	if m == nil {
		t.Fatal("no Normal group with fg and bg in the colorscheme")
	}
	fg, err := strconv.ParseInt(m[1], 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	bg, err := strconv.ParseInt(m[2], 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	return look{Background: background, Name: "pisar", Fg: int(fg), Bg: int(bg)}
}

// look reads the colors now. It does not return while Neovim waits in a prompt.
func (p *Process) look(t *testing.T) look {
	t.Helper()
	var l look
	p.lua(t, &l, `
		local normal = vim.api.nvim_get_hl(0, { name = "Normal" })
		return { Background = vim.o.background, Name = vim.g.colors_name or "", Fg = normal.fg, Bg = normal.bg }`)
	return l
}

// newColors starts Neovim with colors/pisar.lua holding text, in the folder
// where the application keeps it. It returns the path of the file.
func newColors(t *testing.T, text string) (*Process, string) {
	t.Helper()
	var file string
	p, _ := newProc(t, func(o *Options) {
		dir := filepath.Join(envValue(o.Env, "XDG_CONFIG_HOME"), o.AppName, "colors")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		file = filepath.Join(dir, "pisar.lua")
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	return p, file
}

func put(t *testing.T, file, text string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Light to dark and back: the colors follow the file, and the name stays.
func TestColorschemeSwap(t *testing.T) {
	light, dark := scheme(t, "pisar"), scheme(t, "dracula")
	p, file := newColors(t, light)
	if got, w := p.look(t), want(t, light, "light"); got != w {
		t.Fatalf("at the start: %+v, want %+v", got, w)
	}
	for _, step := range []struct{ name, text, bg string }{
		{"to dark", dark, "dark"},
		{"back to light", light, "light"},
		{"to the other dark", scheme(t, "catppuccin-mocha"), "dark"},
	} {
		put(t, file, step.text)
		if err := p.Colorscheme(testCtx(t)); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got, w := p.look(t), want(t, step.text, step.bg); got != w {
			t.Errorf("%s: %+v, want %+v", step.name, got, w)
		}
	}
}

// A call made while Neovim waits in a hit-enter prompt stays queued: the caller
// gets the error of its context, and the colors change when the prompt closes.
func TestColorschemePrompt(t *testing.T) {
	light, dark := scheme(t, "pisar"), scheme(t, "dracula")
	p, file := newColors(t, light)
	// A message of two lines typed on the command line, as a user would.
	p.write(t, ":echo \"one\\ntwo\"\r")
	eventually(t, "a hit-enter prompt", func() bool {
		blocking := false
		err := p.call(testCtx(t), "mode", func(nv *nvimrpc.Nvim) error {
			m, err := nv.Mode()
			blocking = m.Blocking
			return err
		})
		return err == nil && blocking
	})
	put(t, file, dark)

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	if err := p.Colorscheme(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Colorscheme at a prompt = %v, want DeadlineExceeded", err)
	}

	// Closing the prompt lets the queued call run. Reading the colors blocks
	// until then, so the first read after the key already sees the result; the
	// loop only covers the time the key needs to travel through the PTY.
	p.write(t, "\r")
	w := want(t, dark, "dark")
	eventually(t, "the dark colors after the prompt", func() bool { return p.look(t) == w })
}

// A missing file is an error that is not ErrNotRunning, and Neovim keeps its
// colors and answers the next call.
func TestColorschemeNone(t *testing.T) {
	light, dark := scheme(t, "pisar"), scheme(t, "dracula")
	p, file := newColors(t, light)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	err := p.Colorscheme(testCtx(t))
	if err == nil || errors.Is(err, ErrNotRunning) {
		t.Fatalf("Colorscheme without a file = %v, want an error from Neovim", err)
	}
	if got, w := p.look(t), want(t, light, "light"); got != w {
		t.Errorf("after the failure: %+v, want the old %+v", got, w)
	}
	put(t, file, dark)
	if err := p.Colorscheme(testCtx(t)); err != nil {
		t.Fatalf("Colorscheme after the file came back: %v", err)
	}
	if got, w := p.look(t), want(t, dark, "dark"); got != w {
		t.Errorf("after the file came back: %+v, want %+v", got, w)
	}
}

func TestColorschemeEnd(t *testing.T) {
	p, _ := newColors(t, scheme(t, "pisar"))
	if err := p.Close(testCtx(t)); err != nil {
		t.Fatal(err)
	}
	if err := p.Colorscheme(testCtx(t)); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Colorscheme after Close = %v, want ErrNotRunning", err)
	}
}
