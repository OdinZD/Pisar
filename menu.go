package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// eventMenuCopy asks the frontend to copy: only it knows where the keyboard
// focus is (terminal or an input field). Payload "".
const eventMenuCopy = "menu:copy"

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

// newMenu builds the application menu. Own items only start something and run
// in their own goroutine, so that a blocked Neovim call (up to CommandTimeout)
// never holds up the UI. Wails v3.1.0 already calls a click handler from a
// goroutine of its own (MenuItem.handleClick), but that is not in its contract.
//
// The Edit menu is our own and has only Copy and Paste: Cmd+Z, Cmd+X and
// Cmd+A have no item on purpose, so they do nothing while the focus is in the
// terminal (Neovim has its own undo). Copy is our own item because the answer
// depends on Neovim's mode and on the focus. Paste is the system role
// (selector paste:): the webview gets a real paste event, which xterm.js turns
// into (bracketed) input and the input fields handle themselves.
func newMenu(app *application.App, text uiText, a *menuActions, newDocument, addProject func()) *application.Menu {
	menu := app.Menu.New()
	menu.AddRole(application.AppMenu)

	file := menu.AddSubmenu(text.MenuFile)
	file.Add(text.MenuNewDocument).SetAccelerator("CmdOrCtrl+n").OnClick(func(*application.Context) {
		newDocument()
	})
	file.Add(text.MenuAddProject).OnClick(func(*application.Context) {
		go addProject()
	})
	file.Add(text.MenuSave).SetAccelerator("CmdOrCtrl+s").OnClick(func(*application.Context) {
		go a.save()
	})

	edit := menu.AddSubmenu(text.MenuEdit)
	edit.Add(text.MenuCopy).SetAccelerator("CmdOrCtrl+c").OnClick(func(*application.Context) {
		go a.copy()
	})
	edit.AddRole(application.Paste)
	// The role item carries an English label; nil would mean the role vanished.
	if paste := edit.FindByRole(application.Paste); paste != nil {
		paste.SetLabel(text.MenuPaste)
	}

	menu.AddRole(application.WindowMenu)
	return menu
}
