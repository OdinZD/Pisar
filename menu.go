package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// newMenu builds the application menu. Own items only start something: the
// click handler runs on the UI thread, so anything that can block (the folder
// dialog) runs in its own goroutine.
//
// EditMenu is there because the interface has plain input fields in this
// phase, which need copy, paste and undo from the system menu.
func newMenu(app *application.App, text uiText, newDocument, addProject func()) *application.Menu {
	menu := app.Menu.New()
	menu.AddRole(application.AppMenu)

	file := menu.AddSubmenu(text.MenuFile)
	file.Add(text.MenuNewDocument).SetAccelerator("CmdOrCtrl+n").OnClick(func(*application.Context) {
		newDocument()
	})
	file.Add(text.MenuAddProject).OnClick(func(*application.Context) {
		go addProject()
	})

	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)
	return menu
}
