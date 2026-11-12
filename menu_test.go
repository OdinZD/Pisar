package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeMenuEditor struct {
	isRunning bool
	saveErr   error
	saves     int
	gotDL     bool
}

func (f *fakeMenuEditor) running() bool { return f.isRunning }

func (f *fakeMenuEditor) Save(ctx context.Context) error {
	f.saves++
	_, f.gotDL = ctx.Deadline()
	return f.saveErr
}

type menuEmit struct{ name, data string }

func newActions(ed *fakeMenuEditor, out *[]menuEmit) *menuActions {
	return &menuActions{
		editor:  ed,
		emit:    func(name, data string) { *out = append(*out, menuEmit{name, data}) },
		timeout: time.Second,
	}
}

func TestMenuSave(t *testing.T) {
	ed := &fakeMenuEditor{isRunning: true}
	a := newActions(ed, new([]menuEmit))
	a.save()
	if ed.saves != 1 {
		t.Errorf("Save called %d times, want 1", ed.saves)
	}
	if !ed.gotDL {
		t.Error("Save got a context without a deadline")
	}
}

func TestMenuSaveError(t *testing.T) {
	ed := &fakeMenuEditor{isRunning: true, saveErr: errors.New("E45: readonly")}
	a := newActions(ed, new([]menuEmit))
	a.save() // must neither panic nor retry
	if ed.saves != 1 {
		t.Errorf("Save called %d times, want 1", ed.saves)
	}
}

func TestMenuSaveNotRunning(t *testing.T) {
	ed := &fakeMenuEditor{}
	a := newActions(ed, new([]menuEmit))
	a.save()
	if ed.saves != 0 {
		t.Errorf("Save called %d times without a running editor", ed.saves)
	}
}

func TestMenuCopy(t *testing.T) {
	var out []menuEmit
	a := newActions(&fakeMenuEditor{}, &out)
	a.copy()
	if len(out) != 1 || out[0] != (menuEmit{"menu:copy", ""}) {
		t.Errorf("menuEmit %v", out)
	}
}

func TestMenuSearch(t *testing.T) {
	var out []menuEmit
	ed := &fakeMenuEditor{}
	a := newActions(ed, &out)
	a.search()
	if len(out) != 1 || out[0] != (menuEmit{"menu:search", ""}) {
		t.Errorf("menuEmit %v", out)
	}
	// The search does not need Neovim: it is the frontend's panel.
	if ed.saves != 0 {
		t.Errorf("Save called %d times", ed.saves)
	}
}
