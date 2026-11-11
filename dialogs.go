package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// wailsDialogs is the Wails implementation of dialogs.
//
// Checked in Wails v3.1.0 (pkg/application/dialogs.go, dialogs_darwin.go):
//   - OpenFile().PromptForSingleSelection blocks the calling goroutine until the
//     user answers (the dialog itself runs on the main thread), so it may be
//     called from a service goroutine. Cancelling gives "" and no error.
//   - MessageDialog.Show does not block: it returns at once and the pressed
//     button's callback runs later on the main thread. ConfirmTrash turns that
//     into a blocking call with a channel.
type wailsDialogs struct {
	app  *application.App
	text uiText
}

func (d *wailsDialogs) ChooseFolder() (string, error) {
	return d.app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		SetTitle(d.text.ChooseFolderTitle).
		PromptForSingleSelection()
}

func (d *wailsDialogs) ConfirmTrash(name string) (bool, error) {
	// Buffered, and sent to without blocking: a callback runs on the main
	// thread and must never wait for us.
	answer := make(chan bool, 1)
	send := func(v bool) {
		select {
		case answer <- v:
		default:
		}
	}

	dlg := d.app.Dialog.Question().
		SetTitle(d.text.TrashTitle).
		SetMessage(fill(d.text.TrashMessage, "name", name))
	confirm := dlg.AddButton(d.text.TrashConfirm)
	cancel := dlg.AddButton(d.text.Cancel)
	confirm.OnClick(func() { send(true) })
	cancel.OnClick(func() { send(false) })
	// Cancel is the default (Return) and the Escape button: a stray key press
	// must not delete anything.
	dlg.SetDefaultButton(cancel).SetCancelButton(cancel).Show()
	return <-answer, nil
}
