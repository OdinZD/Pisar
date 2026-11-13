package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"Pisar/internal/config"
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

func TestMenuShortcuts(t *testing.T) {
	var out []menuEmit
	ed := &fakeMenuEditor{}
	a := newActions(ed, &out)
	a.shortcuts()
	if len(out) != 1 || out[0] != (menuEmit{"menu:shortcuts", ""}) {
		t.Errorf("menuEmit %v", out)
	}
	if ed.saves != 0 {
		t.Errorf("Save called %d times", ed.saves)
	}
}

// The item of the overview of shortcuts has a shortcut and is in the list with
// the text of strings.json.
func TestMenuShortcutsItem(t *testing.T) {
	if accelShortcuts == "" {
		t.Fatal("accelShortcuts is empty")
	}
	text := uiTextFor(t)
	if text.MenuShortcuts != "Prečaci tipkovnice" {
		t.Errorf("menuShortcuts = %q", text.MenuShortcuts)
	}
	want := menuShortcut{text.MenuShortcuts, accelShortcuts}
	if !slices.Contains(menuShortcuts(text), want) {
		t.Errorf("menuShortcuts has no %+v", want)
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
	// The four that were there before the Formatting menu, its two, and the
	// overview of shortcuts.
	if len(all) != 7 {
		t.Fatalf("menuShortcuts = %+v, want 7 items", all)
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
		"CmdOrCtrl+n", "CmdOrCtrl+s", "CmdOrCtrl+p", "CmdOrCtrl+c", "CmdOrCtrl+b", "CmdOrCtrl+i", "CmdOrCtrl+/",
	} {
		if _, ok := seen[strings.ToLower(accel)]; !ok {
			t.Errorf("no item has the shortcut %s", accel)
		}
	}
}

func TestMenuThemeItems(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	ids := config.Themes()
	for _, current := range ids {
		t.Run(current, func(t *testing.T) {
			items := themeItems(text, current)
			if len(items) != len(ids) {
				t.Fatalf("%d items, want %d", len(items), len(ids))
			}
			labels := map[string]bool{}
			checked := 0
			for i, it := range items {
				if it.id != ids[i] {
					t.Errorf("item %d is %q, want %q", i, it.id, ids[i])
				}
				if strings.TrimSpace(it.label) == "" || labels[it.label] {
					t.Errorf("item %q has an empty or repeated label %q", it.id, it.label)
				}
				labels[it.label] = true
				if it.checked {
					checked++
					if it.id != current {
						t.Errorf("%q is checked, want %q", it.id, current)
					}
				}
			}
			if checked != 1 {
				t.Errorf("%d items checked, want exactly 1", checked)
			}
		})
	}
}

func TestMenuThemeItemsUnknown(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []string{"", "no-such-theme"} {
		var checked []string
		for _, it := range themeItems(text, current) {
			if it.checked {
				checked = append(checked, it.id)
			}
		}
		if len(checked) != 1 || checked[0] != config.DefaultTheme {
			t.Errorf("current %q: checked %v, want only %q", current, checked, config.DefaultTheme)
		}
	}
}
