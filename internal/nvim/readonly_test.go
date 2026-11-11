package nvim

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// bufferLines reads the text of the buffer that holds path, current or not.
func (p *Process) bufferLines(t *testing.T, path string) []string {
	t.Helper()
	var lines []string
	err := p.execLua(testCtx(t), "buffer lines", &lines, bufferLua+findLua+`
		local hits = buffersOf((...))
		if #hits == 0 then error("no buffer", 0) end
		return vim.api.nvim_buf_get_lines(hits[1], 0, -1, false)`, path)
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

// bufferFlags returns 'modifiable', 'readonly' and 'modified' of the buffer of path.
func (p *Process) bufferFlags(t *testing.T, path string) (modifiable, readonly, modified bool) {
	t.Helper()
	var res struct{ Modifiable, Readonly, Modified bool }
	err := p.execLua(testCtx(t), "buffer flags", &res, bufferLua+findLua+`
		local b = buffersOf((...))[1]
		return { Modifiable = vim.bo[b].modifiable, Readonly = vim.bo[b].readonly, Modified = vim.bo[b].modified }`, path)
	if err != nil {
		t.Fatal(err)
	}
	return res.Modifiable, res.Readonly, res.Modified
}

// setLines is what a change through the RPC interface looks like.
func (p *Process) setLines(path string, lines ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	return p.execLua(ctx, "set lines", nil, bufferLua+findLua+`
		local b = buffersOf((...))[1]
		vim.api.nvim_buf_set_lines(b, 0, -1, false, { select(2, ...) })`, append([]any{path}, anySlice(lines)...)...)
}

func anySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func TestSetReadOnly(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "one\ntwo\n")
	b := mkFile(t, dir, "b.md", "b\n")
	for _, f := range []string{a, b} { // a ends up in the background
		if err := p.OpenFile(ctx, f); err != nil {
			t.Fatal(err)
		}
	}

	for _, target := range []struct {
		name string
		path string
	}{{"background buffer", a}, {"current buffer", b}} {
		t.Run(target.name, func(t *testing.T) {
			if err := p.SetReadOnly(ctx, target.path, true); err != nil {
				t.Fatal(err)
			}
			if mod, ro, _ := p.bufferFlags(t, target.path); mod || !ro {
				t.Errorf("modifiable=%v readonly=%v, want false and true", mod, ro)
			}
			// A change through the RPC interface fails with E21 and leaves the text alone.
			before := p.bufferLines(t, target.path)
			err := p.setLines(target.path, "changed")
			if err == nil {
				t.Fatal("the text of a read-only buffer was changed through the RPC interface")
			}
			if !strings.Contains(err.Error(), "not 'modifiable'") {
				t.Errorf("err = %v, want the 'modifiable' refusal", err)
			}
			if !slices.Equal(p.bufferLines(t, target.path), before) {
				t.Errorf("the text changed: %v", p.bufferLines(t, target.path))
			}
			if _, _, modified := p.bufferFlags(t, target.path); modified {
				t.Error("a read-only buffer is modified")
			}

			if err := p.SetReadOnly(ctx, target.path, false); err != nil {
				t.Fatal(err)
			}
			if mod, ro, _ := p.bufferFlags(t, target.path); !mod || ro {
				t.Errorf("modifiable=%v readonly=%v, want true and false", mod, ro)
			}
			if err := p.setLines(target.path, "changed"); err != nil {
				t.Fatalf("a writable buffer refuses a change: %v", err)
			}
			if _, _, modified := p.bufferFlags(t, target.path); !modified {
				t.Error("the change did not mark the buffer modified")
			}
		})
	}

	t.Run("no buffer", func(t *testing.T) {
		err := p.SetReadOnly(ctx, mkFile(t, dir, "none.md", "x\n"), true)
		if !errors.Is(err, ErrNoBuffer) {
			t.Errorf("err = %v, want ErrNoBuffer", err)
		}
	})
}

// Typing in a read-only buffer changes nothing either (measured: dd in normal
// mode gives E21 and the line stays).
func TestSetReadOnlyTyping(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "one\ntwo\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := p.SetReadOnly(ctx, a, true); err != nil {
		t.Fatal(err)
	}
	// "j" is the barrier: when the cursor has moved, the keys before it were handled.
	p.typeKeys(t, "ddjix\x1b", "n", [2]int{2, 0})
	if got := p.bufferLines(t, a); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("text = %v", got)
	}
	if _, _, modified := p.bufferFlags(t, a); modified {
		t.Error("typing marked a read-only buffer modified")
	}
}

// :w refuses a read-only buffer; :w! writes its unchanged text, which only
// touches the working copy, never the user's file.
func TestSetReadOnlyWrite(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "one\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := p.SetReadOnly(ctx, a, true); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(ctx); err == nil {
		t.Error("Save succeeded on a read-only buffer")
	}
	if got, _ := os.ReadFile(a); string(got) != "one\n" {
		t.Errorf("the file changed: %q", got)
	}
}

func TestReload(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "one\ntwo\n")
	b := mkFile(t, dir, "b.md", "b\n")
	for _, f := range []string{a, b} { // a ends up in the background
		if err := p.OpenFile(ctx, f); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("background buffer with changes", func(t *testing.T) {
		if err := p.setLines(a, "typed"); err != nil {
			t.Fatal(err)
		}
		mkFile(t, dir, "a.md", "new one\nnew two\nnew three\n")
		if err := p.Reload(ctx, a); err != nil {
			t.Fatal(err)
		}
		if got := p.bufferLines(t, a); !slices.Equal(got, []string{"new one", "new two", "new three"}) {
			t.Errorf("text = %v", got)
		}
		if _, _, modified := p.bufferFlags(t, a); modified {
			t.Error("a reloaded buffer is still modified")
		}
		if cur, err := p.Current(ctx); err != nil || cur.Path != b {
			t.Errorf("the current buffer = %+v, %v; the reload must not switch buffers", cur, err)
		}
	})

	t.Run("read-only stays read-only", func(t *testing.T) {
		if err := p.SetReadOnly(ctx, a, true); err != nil {
			t.Fatal(err)
		}
		mkFile(t, dir, "a.md", "from outside\n")
		if err := p.Reload(ctx, a); err != nil {
			t.Fatal(err)
		}
		if got := p.bufferLines(t, a); !slices.Equal(got, []string{"from outside"}) {
			t.Errorf("text = %v", got)
		}
		if mod, ro, _ := p.bufferFlags(t, a); mod || !ro {
			t.Errorf("modifiable=%v readonly=%v after the reload, want false and true", mod, ro)
		}
		// ... and the caller may then make it writable.
		if err := p.SetReadOnly(ctx, a, false); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("current buffer", func(t *testing.T) {
		if err := p.setLines(b, "typed"); err != nil {
			t.Fatal(err)
		}
		mkFile(t, dir, "b.md", "outside b\n")
		if err := p.Reload(ctx, b); err != nil {
			t.Fatal(err)
		}
		if got := p.bufferLines(t, b); !slices.Equal(got, []string{"outside b"}) {
			t.Errorf("text = %v", got)
		}
	})

	t.Run("no buffer", func(t *testing.T) {
		err := p.Reload(ctx, mkFile(t, dir, "none.md", "x\n"))
		if !errors.Is(err, ErrNoBuffer) {
			t.Errorf("err = %v, want ErrNoBuffer", err)
		}
	})
}
