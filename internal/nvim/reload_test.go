package nvim

import (
	"errors"
	"os"
	"testing"
)

// reloadFlag is g:pisar_reloading, which Reload sets around its work.
func (p *Process) reloadFlag(t *testing.T) bool {
	t.Helper()
	var set bool
	if err := p.execLua(testCtx(t), "flag", &set, `return vim.g.pisar_reloading == true`); err != nil {
		t.Fatal(err)
	}
	return set
}

// reloadTrio opens a, b and c (c is current), with the preview on and the
// start-up events consumed.
func reloadTrio(t *testing.T) (p *Process, a, b, c string) {
	t.Helper()
	p, _ = newProc(t, nil)
	ctx := testCtx(t)
	dir := tempDir(t)
	a = mkFile(t, dir, "a.md", "a\n")
	b = mkFile(t, dir, "b.md", "b\n")
	c = mkFile(t, dir, "c.md", "c\n")
	for _, f := range []string{a, b, c} {
		if err := p.OpenFile(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	settled(t, p)
	return p, a, b, c
}

// A reload of a background buffer runs :edit! inside nvim_buf_call, which makes
// that buffer current for a moment. The user did not enter it: no entered
// event, no change event (it is not the buffer on screen), and the next real
// switch, also to the reloaded buffer, is announced exactly once.
func TestReloadBg(t *testing.T) {
	p, a, b, c := reloadTrio(t)
	ctx := testCtx(t)

	if err := os.WriteFile(a, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got := only(settled(t, p), EventBufferEntered, EventChanged); len(got) != 0 {
		t.Errorf("events for a reload of a background buffer: %+v", got)
	}
	if cur, err := p.Current(ctx); err != nil || cur != (Buffer{Path: c}) {
		t.Errorf("Current = %+v, %v; want %s", cur, err, c)
	}
	if p.reloadFlag(t) {
		t.Error("g:pisar_reloading is still set")
	}

	// Entering the reloaded buffer is a real switch, then another one.
	steps := []struct {
		name string
		path string
	}{{"to the reloaded buffer", a}, {"to a third", b}, {"back to c", c}}
	for _, s := range steps {
		if err := p.OpenFile(ctx, s.path); err != nil {
			t.Fatal(err)
		}
		want := []Event{{Kind: EventBufferEntered, Path: s.path}, {Kind: EventChanged}}
		if got := only(settled(t, p), EventBufferEntered, EventChanged); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("%s: events %+v, want %+v", s.name, got, want)
		}
	}
}

// The current buffer: as before, one change event and no entered event.
func TestReloadCur(t *testing.T) {
	p, _, _, c := reloadTrio(t)
	ctx := testCtx(t)
	if err := os.WriteFile(c, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(ctx, c); err != nil {
		t.Fatal(err)
	}
	got := only(settled(t, p), EventBufferEntered, EventChanged)
	if len(got) != 1 || got[0] != (Event{Kind: EventChanged}) {
		t.Errorf("events %+v, want one EventChanged", got)
	}
	if p.reloadFlag(t) {
		t.Error("g:pisar_reloading is still set")
	}
}

// A reload that fails must still clear the flag, or no switch would be
// announced from then on.
func TestReloadFail(t *testing.T) {
	p, a, b, _ := reloadTrio(t)
	ctx := testCtx(t)

	// Neovim itself does not fail for a deleted, unreadable or replaced file
	// (measured; see TestReloadGone), so an autocmd that throws is how the error
	// path of Reload is reached.
	err := p.execLua(ctx, "failing autocmd", nil,
		`vim.cmd("autocmd BufReadPre " .. vim.fn.fnameescape((...)) .. " throw 'boom'")`, a)
	if err != nil {
		t.Fatal(err)
	}
	err = p.Reload(ctx, a)
	if err == nil || errors.Is(err, ErrNoBuffer) {
		t.Fatalf("Reload: %v, want the error of :edit!", err)
	}
	settled(t, p)
	if p.reloadFlag(t) {
		t.Error("g:pisar_reloading is still set after the failure")
	}
	for _, path := range []string{b, a} {
		if err := p.OpenFile(ctx, path); err != nil {
			t.Fatal(err)
		}
		if got := only(settled(t, p), EventBufferEntered); len(got) != 1 || got[0].Path != path {
			t.Errorf("switch to %s after the failed reload: %+v", path, got)
		}
	}
}

// :edit! of a buffer whose file is gone does not fail either, and leaves the
// flag cleared like any other reload.
func TestReloadGone(t *testing.T) {
	p, a, b, _ := reloadTrio(t)
	ctx := testCtx(t)
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(ctx, a); err != nil {
		t.Fatalf("Reload of a deleted file: %v", err)
	}
	if got := only(settled(t, p), EventBufferEntered, EventChanged); len(got) != 0 {
		t.Errorf("events: %+v", got)
	}
	if p.reloadFlag(t) {
		t.Error("g:pisar_reloading is still set")
	}
	if err := p.OpenFile(ctx, b); err != nil {
		t.Fatal(err)
	}
	if got := only(settled(t, p), EventBufferEntered); len(got) != 1 || got[0].Path != b {
		t.Errorf("switch after the reload: %+v", got)
	}
}
