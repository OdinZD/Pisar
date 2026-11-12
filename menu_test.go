package main

import (
	"context"
	"errors"
	"strings"
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

func TestMenuFormat(t *testing.T) {
	for _, c := range []struct{ name, kind, want string }{
		{"bold", formatBold, "bold"},
		{"italic", formatItalic, "italic"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var out []menuEmit
			ed := &fakeMenuEditor{isRunning: true}
			a := newActions(ed, &out)
			a.format(c.kind)
			if len(out) != 1 || out[0] != (menuEmit{"menu:format", c.want}) {
				t.Errorf("menuEmit %v", out)
			}
			// The frontend decides, and the service is reached through its ordered queue:
			// the menu itself calls neither Save nor Neovim.
			if ed.saves != 0 {
				t.Errorf("Save called %d times", ed.saves)
			}
		})
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

// The Formatting menu is built from formatItems: two items, Bold and Italic, in
// that order, with the shortcuts the plan names and the texts of uiText.
func TestMenuFormatItems(t *testing.T) {
	text := uiTextFor(t)
	want := []formatItem{
		{label: text.MenuBold, accel: "CmdOrCtrl+b", action: "bold"},
		{label: text.MenuItalic, accel: "CmdOrCtrl+i", action: "italic"},
	}
	got := formatItems(text)
	if len(got) != len(want) {
		t.Fatalf("formatItems = %+v, want %d items", got, len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("item %d = %+v, want %+v", i, got[i], w)
		}
		if got[i].label == "" {
			t.Errorf("item %d has no label", i)
		}
	}
	// The actions are the kinds EditorService.Format knows.
	if got[0].action != formatBold || got[1].action != formatItalic {
		t.Errorf("actions = %q, %q", got[0].action, got[1].action)
	}
}

// Two own items with the same shortcut would shadow each other. Modifier and key
// are compared in lower case, since the key is not case-sensitive.
func TestMenuShortcutsUnique(t *testing.T) {
	text := uiTextFor(t)
	all := menuShortcuts(text)
	// The four that were there before the Formatting menu, and its two.
	if len(all) != 6 {
		t.Fatalf("menuShortcuts = %+v, want 6 items", all)
	}
	seen := map[string]string{}
	for _, sc := range all {
		if sc.label == "" || sc.accel == "" {
			t.Errorf("shortcut with an empty label or key: %+v", sc)
		}
		key := strings.ToLower(sc.accel)
		if other, dup := seen[key]; dup {
			t.Errorf("%q is the shortcut of both %q and %q", sc.accel, other, sc.label)
		}
		seen[key] = sc.label
	}
	for _, accel := range []string{
		"CmdOrCtrl+n", "CmdOrCtrl+s", "CmdOrCtrl+p", "CmdOrCtrl+c", "CmdOrCtrl+b", "CmdOrCtrl+i",
	} {
		if _, ok := seen[strings.ToLower(accel)]; !ok {
			t.Errorf("no item has the shortcut %s", accel)
		}
	}
}
