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

// askButtons shows a question with the given buttons and blocks until one is
// pressed; it returns the index of that button. defaultIdx is the Return
// button, cancelIdx the Escape one (they may be the same button, as for a
// choice where a stray key must do nothing). A callback runs on the main
// thread, so it only sends, without ever waiting for us (MessageDialog.Show
// itself does not block, see above).
func (d *wailsDialogs) askButtons(dlg *application.MessageDialog, labels []string, defaultIdx, cancelIdx int) int {
	answer := make(chan int, 1)
	buttons := make([]*application.Button, len(labels))
	for i, label := range labels {
		buttons[i] = dlg.AddButton(label)
		buttons[i].OnClick(func() {
			select {
			case answer <- i:
			default:
			}
		})
	}
	dlg.SetDefaultButton(buttons[defaultIdx]).SetCancelButton(buttons[cancelIdx]).Show()
	return <-answer
}

// AskUnsaved is the quitDialogs question about unsaved changes. Save is the
// default button; Escape means Cancel.
func (d *wailsDialogs) AskUnsaved(message string) quitChoice {
	dlg := d.app.Dialog.Question().SetTitle(d.text.QuitTitle).SetMessage(message)
	switch d.askButtons(dlg, []string{d.text.QuitSave, d.text.QuitDiscard, d.text.Cancel}, 0, 2) {
	case 0:
		return quitChoiceSave
	case 1:
		return quitChoiceDiscard
	default:
		return quitChoiceCancel
	}
}

// AskUnresponsive asks whether to close although the editor does not answer.
// Cancel is the default and the Escape button: closing loses work.
func (d *wailsDialogs) AskUnresponsive() bool {
	dlg := d.app.Dialog.Warning().SetTitle(d.text.QuitUnresponsiveTitle).SetMessage(d.text.QuitUnresponsive)
	return d.askButtons(dlg, []string{d.text.QuitAnyway, d.text.Cancel}, 1, 1) == 0
}

// ShowSaveError tells the user that saving before the quit failed, and waits
// for the dialog to be dismissed so that nothing else is asked over it.
func (d *wailsDialogs) ShowSaveError(message string) {
	dlg := d.app.Dialog.Error().SetTitle(d.text.QuitSaveFailedTitle).SetMessage(message)
	d.askButtons(dlg, []string{d.text.OK}, 0, 0)
}
