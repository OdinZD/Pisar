package main

import (
	"context"
	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"Pisar/spike/internal/term"
)

// Menu texts (the interface language is Croatian). The only place they live.
const (
	menuFile        = "Datoteka"
	menuEdit        = "Uređivanje"
	menuView        = "Prikaz"
	itemSave        = "Spremi"
	itemCopy        = "Kopiraj"
	itemPaste       = "Zalijepi"
	itemRendererDOM = "Renderer: DOM"
	itemRendererGL  = "Renderer: WebGL"
)

// eventRenderer asks the frontend to switch its renderer; payload "dom" or "webgl".
const eventRenderer = "ui:renderer"

// terminal is what the menu needs from a term.Session.
type terminal interface {
	Save(ctx context.Context) error
	CopyVisual(ctx context.Context) (bool, error)
}

// menuActions holds what the menu items do. It knows nothing about Wails, so
// tests can drive it with fakes.
type menuActions struct {
	term    terminal
	emit    func(name, data string) // to the frontend
	trace   *tracer                 // may be nil
	timeout time.Duration
}

func newMenuActions(t terminal, emit func(name, data string), tr *tracer) *menuActions {
	return &menuActions{term: t, emit: emit, trace: tr, timeout: callTimeout}
}

// save writes the current buffer. A failure is only recorded: Neovim shows the
// same message to the user (see nvim.Process.Save).
func (a *menuActions) save() {
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	err := a.term.Save(ctx)
	a.trace.logf("menu", "save err=%s", errText(err))
	if err != nil {
		log.Printf("menu save: %v", err)
	}
}

// copy copies Neovim's visual selection, and when there is none asks the
// frontend to copy the terminal's own selection. An error does not fall back
// to the terminal selection: Neovim's state is unknown, and copying a stale
// terminal selection would put wrong text on the clipboard.
func (a *menuActions) copy() {
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	visual, err := a.term.CopyVisual(ctx)
	a.trace.logf("menu", "copy visual=%t err=%s", visual, errText(err))
	if err != nil {
		log.Printf("menu copy: %v", err)
		return
	}
	if !visual {
		a.emit(term.EventCopy, "")
	}
}

// setRenderer asks the frontend to switch to "dom" or "webgl".
func (a *menuActions) setRenderer(kind string) {
	if kind != "dom" && kind != "webgl" {
		a.trace.logf("menu", "renderer=%q ignored", kind)
		return
	}
	a.trace.logf("menu", "renderer=%s", kind)
	a.emit(eventRenderer, kind)
}

// newMenu builds the application menu. Items run in their own goroutine: the
// click handler runs on the UI thread, and a blocked Neovim call (up to
// callTimeout) must not freeze the window.
//
// Cmd+Z, Cmd+X and Cmd+A have no item on purpose, so they do nothing in the
// webview; Neovim has its own undo. Copy is our own item because the answer
// depends on Neovim's mode. Paste is the system role (selector paste:), so the
// webview gets a real paste event, which xterm.js turns into (bracketed) input.
func newMenu(wails *application.App, a *menuActions) *application.Menu {
	menu := wails.Menu.New()
	menu.AddRole(application.AppMenu)

	file := menu.AddSubmenu(menuFile)
	file.Add(itemSave).SetAccelerator("CmdOrCtrl+s").OnClick(func(*application.Context) {
		go a.save()
	})

	edit := menu.AddSubmenu(menuEdit)
	edit.Add(itemCopy).SetAccelerator("CmdOrCtrl+c").OnClick(func(*application.Context) {
		go a.copy()
	})
	edit.AddRole(application.Paste)
	if paste := edit.FindByRole(application.Paste); paste != nil {
		paste.SetLabel(itemPaste)
	}

	// No accelerators: this is for a manual comparison only.
	view := menu.AddSubmenu(menuView)
	view.Add(itemRendererDOM).OnClick(func(*application.Context) { a.setRenderer("dom") })
	view.Add(itemRendererGL).OnClick(func(*application.Context) { a.setRenderer("webgl") })

	menu.AddRole(application.WindowMenu)
	return menu
}
