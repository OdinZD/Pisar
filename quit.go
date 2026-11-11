package main

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"Pisar/internal/nvim"
)

// quitEditor is what the quit logic needs from the editor.
type quitEditor interface {
	running() bool
	modified(ctx context.Context) ([]nvim.Buffer, error)
	saveAll(ctx context.Context) error
}

// quitChoice is the answer to the "unsaved changes" dialog.
type quitChoice int

const (
	quitChoiceSave quitChoice = iota
	quitChoiceDiscard
	quitChoiceCancel
)

// quitDialogs are the questions the quit logic asks. Every method blocks until
// the user has answered; they are only called from the gate's own goroutine.
type quitDialogs interface {
	AskUnsaved(message string) quitChoice
	AskUnresponsive() (closeAnyway bool)
	ShowSaveError(message string)
}

// quitGate decides whether the application may quit. Cmd+Q and the red close
// button both end up in request.
//
// Wails calls ShouldQuit and the WindowClosing hook on the UI thread, and the
// editor's answer (an RPC call that can hang while Neovim shows a prompt) must
// not be waited for there. So the first request is refused, the question is
// asked in a goroutine, and when the answer is "quit" the gate sets approved
// and calls quit, which makes Wails ask again: that request goes through.
type quitGate struct {
	editor  quitEditor
	dialogs quitDialogs
	text    uiText
	timeout time.Duration // Limits.CommandTimeout, for each editor call
	names   int           // Limits.QuitDialogNames: names listed before "…and N more"
	quit    func()        // really quits (App.Quit); runs in the gate's goroutine

	mu       sync.Mutex
	approved bool // the decision to quit has been made; never asked again
	asking   bool // a check or a dialog is in progress

	wg sync.WaitGroup
}

// request reports whether the application may quit right now. It never blocks.
func (g *quitGate) request() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.approved || !g.editor.running() {
		return true
	}
	if g.asking {
		return false // the dialog is already open; do not stack another
	}
	g.asking = true
	g.wg.Go(g.ask)
	return false
}

// wait blocks until no question is in progress. For tests.
func (g *quitGate) wait() { g.wg.Wait() }

func (g *quitGate) approve() {
	g.mu.Lock()
	g.approved = true
	g.mu.Unlock()
	g.quit()
}

func (g *quitGate) ask() {
	defer func() {
		g.mu.Lock()
		g.asking = false
		g.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	bufs, err := g.editor.modified(ctx)
	cancel()
	if err != nil {
		slog.Warn("quit: cannot ask the editor for unsaved changes", "err", err)
		if g.dialogs.AskUnresponsive() {
			g.approve()
		}
		return
	}
	if len(bufs) == 0 {
		g.approve()
		return
	}

	switch g.dialogs.AskUnsaved(g.unsavedMessage(bufs)) {
	case quitChoiceSave:
		ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
		err := g.editor.saveAll(ctx)
		cancel()
		if errors.Is(err, errSaveDeclined) {
			// The user answered a question about a document with Cancel or
			// Reload: nothing failed, so no error dialog, but nothing is saved
			// either, so the application stays open.
			slog.Info("quit: a document was not saved because the user declined")
			return
		}
		if err != nil {
			slog.Warn("quit: saving all documents failed", "err", err)
			g.dialogs.ShowSaveError(fill(g.text.QuitSaveFailed, "reason", g.saveFailure(err)))
			return
		}
		g.approve()
	case quitChoiceDiscard:
		g.approve()
	case quitChoiceCancel:
	}
}

// unsavedMessage lists the documents: file names, at most g.names of them.
func (g *quitGate) unsavedMessage(bufs []nvim.Buffer) string {
	var lines []string
	for i, b := range bufs {
		if i == g.names {
			lines = append(lines, fill(g.text.QuitMore, "count", strconv.Itoa(len(bufs)-g.names)))
			break
		}
		if b.Path == "" {
			lines = append(lines, g.text.QuitUnnamed)
		} else {
			lines = append(lines, filepath.Base(b.Path))
		}
	}
	return fill(g.text.QuitMessage, "names", strings.Join(lines, "\n"))
}

// saveFailure is the reason shown to the user. A buffer without a name and the
// failures of saving a Word document get the explanation the rest of the
// interface uses; anything else shows what Neovim reported (the service error
// hides it behind a code, so look at its cause).
func (g *quitGate) saveFailure(err error) string {
	if errors.Is(err, nvim.ErrUnnamedBuffer) {
		return g.text.ErrorUnnamedBuffer
	}
	var ae *apiError
	if !errors.As(err, &ae) {
		return err.Error()
	}
	switch ae.code {
	case codeConflict:
		return g.text.ErrorConflict
	case codeConvertFailed:
		return g.text.ErrorConvertFailed
	case codePandocMissing:
		return g.text.ErrorPandocMissing
	case codeReadOnly:
		return g.text.ErrorReadOnly
	}
	if ae.cause != nil {
		return ae.cause.Error()
	}
	return err.Error()
}
