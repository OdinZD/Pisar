package nvim

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"
)

// settle makes the current buffer modified and returns every event that
// arrived up to the resulting modified-state event. Notifications keep their
// order on one channel, so this is a barrier: whatever Neovim sent before the
// call is in the result, and nothing sent later can be mistaken for it.
func settle(t *testing.T, p *Process) []Event {
	t.Helper()
	p.setBuffer(t, "settle")
	return collectUntil(t, p, func(got []Event) bool {
		return len(got) > 0 && got[len(got)-1].Kind == EventModifiedChanged
	})
}

func kinds(evs []Event, kind EventKind) []Event {
	var out []Event
	for _, ev := range evs {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func TestBuffers(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "a\n")
	b := mkFile(t, dir, "b.md", "b\n")

	bufs, err := p.Buffers(ctx)
	if err != nil || len(bufs) != 0 {
		t.Fatalf("Buffers at start = %+v, %v; want none (the unnamed buffer is not counted)", bufs, err)
	}
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := p.OpenFile(ctx, b); err != nil {
		t.Fatal(err)
	}
	p.setBuffer(t, "changed")
	// A help buffer is listed by Neovim as a buffer too, but has no ordinary file.
	if _, err := p.Write([]byte(":help\r")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the help buffer", func() bool {
		var bt string
		return p.execLua(ctx, "buftype", &bt, `return vim.bo.buftype`) == nil && bt == "help"
	})
	want := []Buffer{{Path: a}, {Path: b, Modified: true}}
	bufs, err = p.Buffers(ctx)
	if err != nil || !slices.Equal(bufs, want) {
		t.Fatalf("Buffers = %+v, %v; want %+v", bufs, err, want)
	}
}

func TestWipeBackground(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "a\n")
	b := mkFile(t, dir, "b.md", "b\n")
	for _, f := range []string{a, b} {
		if err := p.OpenFile(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a}, Event{Kind: EventBufferEntered, Path: b})

	was, err := p.Wipe(ctx, a)
	if err != nil || was {
		t.Fatalf("Wipe(a) = %v, %v; want false, nil", was, err)
	}
	bufs, err := p.Buffers(ctx)
	if err != nil || !slices.Equal(bufs, []Buffer{{Path: b}}) {
		t.Fatalf("Buffers = %+v, %v; want only b", bufs, err)
	}
	if cur, err := p.Current(ctx); err != nil || cur != (Buffer{Path: b}) {
		t.Fatalf("Current = %+v, %v; want b", cur, err)
	}
	if got := kinds(settle(t, p), EventBufferEntered); len(got) != 0 {
		t.Fatalf("wiping a background buffer sent %+v", got)
	}
}

func TestWipeCurrent(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "a\n")
	b := mkFile(t, dir, "b.md", "b\n")
	for _, f := range []string{a, b} {
		if err := p.OpenFile(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a}, Event{Kind: EventBufferEntered, Path: b})

	was, err := p.Wipe(ctx, b)
	if err != nil || !was {
		t.Fatalf("Wipe(b) = %v, %v; want true, nil", was, err)
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a})
	if cur, err := p.Current(ctx); err != nil || cur != (Buffer{Path: a}) {
		t.Fatalf("Current = %+v, %v; want a", cur, err)
	}
	bufs, err := p.Buffers(ctx)
	if err != nil || !slices.Equal(bufs, []Buffer{{Path: a}}) {
		t.Fatalf("Buffers = %+v, %v; want only a", bufs, err)
	}
	select {
	case <-p.Done():
		t.Fatal("Neovim quit")
	default:
	}
}

func TestWipeOnly(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	a := mkFile(t, tempDir(t), "a.md", "a\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: a})

	was, err := p.Wipe(ctx, a)
	if err != nil || !was {
		t.Fatalf("Wipe = %v, %v; want true, nil", was, err)
	}
	wantEvents(t, p, Event{Kind: EventBufferEntered, Path: ""})
	select {
	case <-p.Done():
		t.Fatal("Neovim quit after its last buffer was wiped")
	default:
	}
	if cur, err := p.Current(ctx); err != nil || cur != (Buffer{}) {
		t.Fatalf("Current = %+v, %v; want an unnamed buffer", cur, err)
	}
	if bufs, err := p.Buffers(ctx); err != nil || len(bufs) != 0 {
		t.Fatalf("Buffers = %+v, %v; want none", bufs, err)
	}
}

func TestWipeModified(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "a\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	p.setBuffer(t, "unsaved")

	was, err := p.Wipe(ctx, a)
	if !errors.Is(err, ErrBufferModified) || was {
		t.Fatalf("Wipe = %v, %v; want false, ErrBufferModified", was, err)
	}
	bufs, err := p.Buffers(ctx)
	if err != nil || !slices.Equal(bufs, []Buffer{{Path: a, Modified: true}}) {
		t.Fatalf("Buffers = %+v, %v; want a, still modified", bufs, err)
	}
	var text string
	if err := p.execLua(ctx, "text", &text, `return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), "\n")`); err != nil || text != "unsaved" {
		t.Fatalf("text = %q, %v; the changes must stay", text, err)
	}
}

func TestWipeNone(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "a\n")
	other := mkFile(t, dir, "other.md", "o\n")
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{other, filepath.Join(dir, "nope.md"), filepath.Join(dir, "gone", "a.md")} {
		was, err := p.Wipe(ctx, path)
		if err != nil || was {
			t.Errorf("Wipe(%s) = %v, %v; want false, nil", path, was, err)
		}
	}
	if bufs, err := p.Buffers(ctx); err != nil || !slices.Equal(bufs, []Buffer{{Path: a}}) {
		t.Fatalf("Buffers = %+v, %v; want a untouched", bufs, err)
	}
}

// After a rename or a move to the Trash the old file is gone, yet its buffer
// must still be found. A link to the folder must find it as well.
func TestWipeGone(t *testing.T) {
	cases := []struct {
		name string
		// gone makes the file at path disappear; path is the real path.
		gone func(t *testing.T, dir, path string)
		// ask turns the real path into the path handed to Wipe.
		ask func(t *testing.T, dir string) string
	}{
		{
			"rm",
			func(t *testing.T, _, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			},
			func(_ *testing.T, dir string) string { return filepath.Join(dir, "a.md") },
		},
		{
			"mv",
			func(t *testing.T, dir, path string) {
				if err := os.Rename(path, filepath.Join(dir, "z.md")); err != nil {
					t.Fatal(err)
				}
			},
			func(_ *testing.T, dir string) string { return filepath.Join(dir, "a.md") },
		},
		{
			"link",
			func(*testing.T, string, string) {},
			func(t *testing.T, dir string) string {
				link := filepath.Join(tempDir(t), "ln")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(link, "a.md")
			},
		},
		{
			"rm link",
			func(t *testing.T, _, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			},
			func(t *testing.T, dir string) string {
				link := filepath.Join(tempDir(t), "ln")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(link, "a.md")
			},
		},
		{
			"dir",
			func(t *testing.T, dir, _ string) {
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
			},
			func(_ *testing.T, dir string) string { return filepath.Join(dir, "a.md") },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, _ := newProc(t, nil)
			ctx := testCtx(t)
			parent := tempDir(t)
			dir := filepath.Join(parent, "dir")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			a := mkFile(t, dir, "a.md", "a\n")
			b := mkFile(t, parent, "b.md", "b\n")
			if err := p.OpenFile(ctx, b); err != nil {
				t.Fatal(err)
			}
			if err := p.OpenFile(ctx, a); err != nil {
				t.Fatal(err)
			}
			ask := c.ask(t, dir)
			c.gone(t, dir, a)

			was, err := p.Wipe(ctx, ask)
			if err != nil || !was {
				t.Fatalf("Wipe(%s) = %v, %v; want true, nil", ask, was, err)
			}
			if bufs, err := p.Buffers(ctx); err != nil || !slices.Equal(bufs, []Buffer{{Path: b}}) {
				t.Fatalf("Buffers = %+v, %v; want only b", bufs, err)
			}
		})
	}
}

func TestWipeAlive(t *testing.T) {
	p, _ := newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a := mkFile(t, dir, "a.md", "a\n")
	b := mkFile(t, dir, "b.md", "b\n")
	for _, f := range []string{a, b} {
		if err := p.OpenFile(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{b, a} {
		if _, err := p.Wipe(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	// Answered within 2 s: no prompt or message froze Neovim.
	start := time.Now()
	if _, err := p.Current(ctx); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Current took %v", d)
	}
	if msgs := p.messages(t); regexp.MustCompile(`\bE\d+:`).MatchString(msgs) {
		t.Fatalf(":messages has an error: %q", msgs)
	}
	var errmsg string
	if err := p.execLua(ctx, "errmsg", &errmsg, `return vim.v.errmsg`); err != nil || errmsg != "" {
		t.Fatalf("v:errmsg = %q, %v", errmsg, err)
	}
	// Neovim still reacts to the buffers: a new file can be opened.
	if err := p.OpenFile(ctx, a); err != nil {
		t.Fatalf("OpenFile after Wipe: %v", err)
	}
}
