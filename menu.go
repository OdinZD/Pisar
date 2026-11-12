package main

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"Pisar/internal/config"
)

// eventMenuCopy asks the frontend to copy: only it knows where the keyboard
// focus is (terminal or an input field). Payload "".
const eventMenuCopy = "menu:copy"

// eventMenuSearch asks the frontend to open the document search (⌘P). Payload "".
const eventMenuSearch = "menu:search"

// eventMenuFormat asks the frontend to format the text (⌘B, ⌘I). Payload: one
// of the format kinds below. Like Copy it is the frontend that decides: the
// shortcut must do nothing while the focus is in an input field.
const eventMenuFormat = "menu:format"

// The formatting actions of the Formatting menu, as EditorService.Format
// names them.
const (
	formatBold   = "bold"
	formatItalic = "italic"
)

// The shortcuts of the own menu items, in one place so that a test can see all
// of them at once: two items with the same one would silently shadow each
// other. Paste and the items of the system roles have theirs from the system.
const (
	accelNewDocument = "CmdOrCtrl+n"
	accelSave        = "CmdOrCtrl+s"
	accelSearch      = "CmdOrCtrl+p"
	accelCopy        = "CmdOrCtrl+c"
)

// formatItem is one item of the Formatting menu: its label, its shortcut and
// the action it asks the frontend for.
type formatItem struct {
	label  string
	accel  string
	action string
}

// formatItems are the items of the Formatting menu, in the order they appear.
// newMenu is built from this list, and the tests read it, because newMenu needs
// a Wails application.
func formatItems(text uiText) []formatItem {
	return []formatItem{
		{label: text.MenuBold, accel: "CmdOrCtrl+b", action: formatBold},
		{label: text.MenuItalic, accel: "CmdOrCtrl+i", action: formatItalic},
	}
}

// themeItem is one radio item of the Theme submenu.
type themeItem struct {
	label, id string
	checked   bool
}

// themeLabel is the name of a theme in the menu.
func themeLabel(text uiText, id string) string {
	switch id {
	case config.ThemePisar:
		return text.ThemePisar
	case config.ThemeCatppuccinMocha:
		return text.ThemeCatppuccinMocha
	case config.ThemeDracula:
		return text.ThemeDracula
	}
	return id // a theme without a text still gets a usable item
}

// themeItems are the items of the Theme submenu, in the order of
// config.Themes. Exactly one is checked: current, or DefaultTheme when current
// is not a known theme. Like formatItems it is built apart from newMenu so that
// tests can read it.
func themeItems(text uiText, current string) []themeItem {
	ids := config.Themes()
	if !slices.Contains(ids, current) {
		current = config.DefaultTheme
	}
	out := make([]themeItem, 0, len(ids))
	for _, id := range ids {
		out = append(out, themeItem{label: themeLabel(text, id), id: id, checked: id == current})
	}
	return out
}

// menuShortcut is an own menu item that has a shortcut.
type menuShortcut struct{ label, accel string }

// menuShortcuts lists every own item that has a shortcut, whichever menu it is
// in.
func menuShortcuts(text uiText) []menuShortcut {
	out := []menuShortcut{
		{text.MenuNewDocument, accelNewDocument},
		{text.MenuSave, accelSave},
		{text.MenuSearch, accelSearch},
		{text.MenuCopy, accelCopy},
	}
	for _, it := range formatItems(text) {
		out = append(out, menuShortcut{it.label, it.accel})
	}
	return out
}

// menuEditor is what the menu items need from the editor.
type menuEditor interface {
	running() bool
	Save(ctx context.Context) error
}

// menuActions holds what the own menu items do. It knows nothing about Wails,
// so tests drive it with fakes.
type menuActions struct {
	editor  menuEditor
	emit    func(name, data string) // to the frontend
	timeout time.Duration           // Limits.CommandTimeout
}

// save writes the current buffer. Without a running Neovim there is nothing to
// write. A failure is only logged: Neovim shows the same message to the user in
// its message line.
func (a *menuActions) save() {
	if !a.editor.running() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	if err := a.editor.Save(ctx); err != nil {
		slog.Error("menu: save", "err", err)
	}
}

// copy hands the decision to the frontend (see eventMenuCopy).
func (a *menuActions) copy() {
	a.emit(eventMenuCopy, "")
}

// format hands the decision to the frontend (see eventMenuFormat). Go does not
// call Neovim here: the keys the user typed just before are still on their way
// through the frontend's ordered queue, and the format must follow them.
func (a *menuActions) format(kind string) {
	a.emit(eventMenuFormat, kind)
}

// search hands the decision to the frontend (see eventMenuSearch): the search
// panel and the focus are its business.
func (a *menuActions) search() {
	a.emit(eventMenuSearch, "")
}

// newMenu builds the application menu. Own items only start something and run
// in their own goroutine, so that a blocked Neovim call (up to CommandTimeout)
// never holds up the UI. Wails v3.1.0 already calls a click handler from a
// goroutine of its own (MenuItem.handleClick), but that is not in its contract.
//
// The Formatting menu sits between Edit and View; its items only emit
// eventMenuFormat. The View menu sits between Formatting and Window and holds
// the Theme submenu: three radio items without shortcuts. theme is the one
// checked at the start; chooseTheme applies a choice (SettingsService.setTheme).
//
// The Edit menu is our own and has only Copy and Paste: Cmd+Z, Cmd+X and
// Cmd+A have no item on purpose, so they do nothing while the focus is in the
// terminal (Neovim has its own undo). Copy is our own item because the answer
// depends on Neovim's mode and on the focus. Paste is the system role
// (selector paste:): the webview gets a real paste event, which xterm.js turns
// into (bracketed) input and the input fields handle themselves.
func newMenu(app *application.App, text uiText, a *menuActions, theme string, chooseTheme func(id string) error, newDocument, addProject func()) *application.Menu {
	menu := app.Menu.New()
	menu.AddRole(application.AppMenu)

	file := menu.AddSubmenu(text.MenuFile)
	file.Add(text.MenuNewDocument).SetAccelerator(accelNewDocument).OnClick(func(*application.Context) {
		newDocument()
	})
	file.Add(text.MenuAddProject).OnClick(func(*application.Context) {
		go addProject()
	})
	file.Add(text.MenuSave).SetAccelerator(accelSave).OnClick(func(*application.Context) {
		go a.save()
	})
	file.Add(text.MenuSearch).SetAccelerator(accelSearch).OnClick(func(*application.Context) {
		go a.search()
	})

	edit := menu.AddSubmenu(text.MenuEdit)
	edit.Add(text.MenuCopy).SetAccelerator(accelCopy).OnClick(func(*application.Context) {
		go a.copy()
	})
	edit.AddRole(application.Paste)
	// The role item carries an English label; nil would mean the role vanished.
	if paste := edit.FindByRole(application.Paste); paste != nil {
		paste.SetLabel(text.MenuPaste)
	}

	// Not started with "go": emitting never blocks, and one goroutine per
	// click would let two quick shortcuts reach the frontend in either order.
	format := menu.AddSubmenu(text.MenuFormat)
	for _, it := range formatItems(text) {
		format.Add(it.label).SetAccelerator(it.accel).OnClick(func(*application.Context) {
			a.format(it.action)
		})
	}

	view := menu.AddSubmenu(text.MenuView)
	addThemeMenu(view.AddSubmenu(text.MenuTheme), themeItems(text, theme), chooseTheme)

	menu.AddRole(application.WindowMenu)
	return menu
}

// addThemeMenu fills the Theme submenu with radio items. Wails has already moved
// the check to the clicked item when the handler runs, so the handler only
// has to put it right again when the choice failed.
//
// The handler is not started with "go": setTheme writes one small local file and
// never calls Neovim, so it is quick, and Wails runs every handler in a goroutine
// of its own anyway. mu is held for the whole choice, so two quick clicks are
// applied one after the other and current is always the theme that was applied
// last. The check is then set on that theme, whichever click finished last.
func addThemeMenu(sub *application.Menu, items []themeItem, chooseTheme func(id string) error) {
	var (
		mu      sync.Mutex
		current string
		radios  = map[string]*application.MenuItem{}
	)
	for _, it := range items {
		radios[it.id] = sub.AddRadio(it.label, it.checked)
		if it.checked {
			current = it.id
		}
	}
	for _, it := range items {
		radios[it.id].OnClick(func(*application.Context) {
			mu.Lock()
			defer mu.Unlock()
			if err := chooseTheme(it.id); err != nil {
				slog.Error("menu: choose theme", "theme", it.id, "err", err)
			} else {
				current = it.id
			}
			// SetChecked on a radio item unchecks the others of its group.
			radios[current].SetChecked(true)
		})
	}
}
