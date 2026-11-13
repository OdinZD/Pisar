package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"Pisar/internal/nvim"
	"Pisar/internal/preview"
	"Pisar/internal/project"
	"Pisar/internal/term"
)

// This file is the part of the editor service that feeds the preview pane: it
// turns the text of Neovim's current buffer into HTML after a quiet moment in
// the typing, and tells the frontend. The pane itself is the frontend's.

// Events of the preview and the search, sent to the frontend by the editor.
const (
	eventPreviewContent = "preview:content"   // payload: PreviewContent
	eventTogglePreview  = "ui:toggle-preview" // the user pressed <leader>p in Neovim; payload ""
	eventOpenSearch     = "ui:open-search"    // the user pressed <leader>f in Neovim; payload ""
)

// The states of PreviewContent.
const (
	previewStateHTML        = "html"
	previewStateUnsupported = "unsupported"
	previewStateError       = "error"
)

// PreviewContent is what the preview pane shows (see eventPreviewContent). It
// reaches JavaScript as a typed event payload, like DocumentActive.
type PreviewContent struct {
	// Path is the user's file: for a Word document the .docx or .doc, never the
	// Markdown working copy. "" for a buffer without a file, and when the buffer
	// was too large to even be asked which file it holds.
	Path  string `json:"path"`
	State string `json:"state"` // previewStateHTML, previewStateUnsupported or previewStateError
	HTML  string `json:"html"`  // the page body when State is "html", otherwise ""
	Code  string `json:"code"`  // the error code when State is "error", otherwise ""
}

// previewRenderer is what the loop needs of internal/preview. A nil one in
// editorServiceDeps means the service has no preview.
type previewRenderer interface {
	Render(ctx context.Context, markdown []byte, resourceDir string) ([]byte, error)
}

var _ previewRenderer = (*preview.Renderer)(nil)

// previewSource is what the loop needs of the Neovim session.
type previewSource interface {
	BufferText(ctx context.Context, maxBytes int) ([]byte, nvim.Buffer, error)
}

var _ previewSource = (*term.Session)(nil)

// previewTimerFunc starts a timer: fired receives once after d, and stop
// releases it. It is a parameter so that tests drive time by hand.
type previewTimerFunc func(d time.Duration) (fired <-chan time.Time, stop func())

func realPreviewTimer(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// previewTarget says what a buffer is in the preview's eyes.
type previewTarget struct {
	path   string // the user's file; "" for a buffer without one
	dir    string // the folder pictures are read from; "" when there is none
	render bool   // false: the pane is told that this kind of document has no preview
}

// previewLoopDeps are the parts a previewLoop is built from.
type previewLoopDeps struct {
	Source   previewSource
	Renderer previewRenderer
	// Emit hands the result to the frontend. It is called with the loop's lock
	// held, which is what makes "nothing is sent after enable(false) returned"
	// true; it must not call back into the loop.
	Emit   func(PreviewContent)
	Target func(buf nvim.Buffer) previewTarget

	Debounce    time.Duration // quiet time after the last notification
	ReadTimeout time.Duration // one BufferText call
	MaxSource   int           // bytes of text asked for from Neovim
	NewTimer    previewTimerFunc
}

// previewLoop is the one goroutine that renders the preview. It lives from
// newPreviewLoop until stop, and everything it starts ends with its context.
//
// Notifications (changed, enable) only mark that something happened: they bump
// gen, cancel the conversion in progress and wake the loop. The loop waits
// until no notification has come for Debounce, then reads the text, converts
// it and sends the result, unless gen moved meanwhile: a result computed from
// text that is already out of date is never sent, whether or not the
// cancellation reached the conversion in time.
type previewLoop struct {
	d      previewLoopDeps
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	signal chan struct{} // capacity 1: "something changed"

	mu           sync.Mutex
	enabled      bool
	gen          uint64             // bumped by every notification
	cancelRender context.CancelFunc // the conversion in progress, or nil
	last         string             // key of what was sent last; "" after a reset
}

func newPreviewLoop(d previewLoopDeps) *previewLoop {
	if d.NewTimer == nil {
		d.NewTimer = realPreviewTimer
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &previewLoop{d: d, ctx: ctx, cancel: cancel, signal: make(chan struct{}, 1)}
	l.wg.Go(l.run)
	return l
}

// stop ends the loop and waits for its goroutine. Idempotent.
func (l *previewLoop) stop() {
	l.cancel()
	l.wg.Wait()
}

// wake asks the loop to run; a request that is already waiting is enough.
func (l *previewLoop) wake() {
	select {
	case l.signal <- struct{}{}:
	default:
	}
}

// invalidate marks everything computed so far as out of date. l.mu is held.
func (l *previewLoop) invalidate() {
	l.gen++
	if l.cancelRender != nil {
		l.cancelRender()
	}
}

// changed is Neovim's "the text may have changed". It is called on the
// goroutine that delivers Neovim's events, so it does no work.
func (l *previewLoop) changed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.enabled {
		return
	}
	l.invalidate()
	l.wake()
}

// enable switches the preview on or off. Switching on always gives a refresh
// that is sent even if the content has not changed since the last one: the
// page that asks may have just been loaded. Switching off drops the conversion
// in progress, and nothing is sent after it returns.
func (l *previewLoop) enable(on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = on
	l.last = ""
	l.invalidate()
	if on {
		l.wake()
	}
}

// reset is for the end of the Neovim process: the conversion in progress is
// dropped and the next refresh is sent whatever it holds, because the frontend
// has cleared the pane. The preview stays on for the next process.
func (l *previewLoop) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.last = ""
	l.invalidate()
}

func (l *previewLoop) run() {
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-l.signal:
		}
		if !l.settle() {
			return
		}
		l.refresh()
	}
}

// settle waits until no notification has come for Debounce. It reports false
// when the loop was stopped.
func (l *previewLoop) settle() bool {
	for {
		fired, stop := l.d.NewTimer(l.d.Debounce)
		select {
		case <-l.ctx.Done():
			stop()
			return false
		case <-l.signal:
			stop() // typing goes on: wait again
		case <-fired:
			stop()
			return true
		}
	}
}

// refresh does one round: text, conversion, send.
func (l *previewLoop) refresh() {
	l.mu.Lock()
	if !l.enabled {
		l.mu.Unlock()
		return
	}
	gen := l.gen
	ctx, cancel := context.WithCancel(l.ctx)
	l.cancelRender = cancel
	l.mu.Unlock()

	content, key, ok := l.build(ctx)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.cancelRender = nil
	// ctx.Err() is the cancellation made by a notification or by stop; an
	// error the conversion itself ended with (its own time limit) is content.
	// Read before cancel(), which would make it always set.
	superseded := ctx.Err() != nil
	cancel()
	if !ok || !l.enabled || l.gen != gen || superseded || l.ctx.Err() != nil {
		return
	}
	l.last = key
	l.d.Emit(content)
}

// build produces what to send and the key that identifies it. ok is false when
// there is nothing to send: the work was cancelled, Neovim is gone, or the
// content is the one that was sent last. An empty key means "do not remember
// this": a failure that may pass is tried again at the next notification.
func (l *previewLoop) build(ctx context.Context) (content PreviewContent, key string, ok bool) {
	l.mu.Lock()
	last := l.last
	l.mu.Unlock()

	readCtx, cancel := context.WithTimeout(ctx, l.d.ReadTimeout)
	text, buf, err := l.d.Source.BufferText(readCtx, l.d.MaxSource)
	cancel()
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, nvim.ErrNotRunning) || errors.Is(err, term.ErrClosed) {
			// The frontend clears the pane when Neovim ends (nvim:exit).
			return PreviewContent{}, "", false
		}
		return l.failed(PreviewContent{}, err, "", "", "", last)
	}

	target := l.d.Target(buf)
	if !target.render {
		key = previewKey(previewStateUnsupported, target.path, "", "")
		if key == last {
			return PreviewContent{}, "", false
		}
		return PreviewContent{Path: target.path, State: previewStateUnsupported}, key, true
	}

	sum := sha256.Sum256(text)
	digest := hex.EncodeToString(sum[:])
	// Before the conversion, so that an unchanged text costs nothing (pandoc
	// takes about a tenth of a second).
	if key = previewKey(previewStateHTML, target.path, target.dir, digest); key == last {
		return PreviewContent{}, "", false
	}
	// The same for a page that was too large: the same text is again.
	if last == previewKey(previewStateError, target.path, target.dir, codePreviewTooLarge+digest) {
		return PreviewContent{}, "", false
	}
	html, err := l.d.Renderer.Render(ctx, text, target.dir)
	if err != nil {
		if ctx.Err() != nil {
			return PreviewContent{}, "", false
		}
		return l.failed(PreviewContent{Path: target.path}, err, target.path, target.dir, digest, last)
	}
	return PreviewContent{Path: target.path, State: previewStateHTML, HTML: string(html)}, key, true
}

// failed turns an error into an error event. Only "too large" is remembered
// by its key: the same text gives the same answer. Anything else may be a
// passing trouble (a timeout, a busy disk), so it is tried again at the next
// notification.
func (l *previewLoop) failed(c PreviewContent, err error, path, dir, digest, last string) (PreviewContent, string, bool) {
	code := codeOf(err)
	switch code {
	case codePreviewTooLarge, codePandocMissing, codePandocTooOld:
		slog.Debug("editor: no preview", "code", code)
	default:
		// The error text of a failed pandoc quotes the document; only its kind
		// and, for a tool, the way it ended are logged.
		var te *preview.ToolError
		if errors.As(err, &te) {
			slog.Warn("editor: preview failed", "code", code, "tool", te.Tool, "cause", te.Err)
		} else {
			slog.Warn("editor: preview failed", "code", code, "type", fmt.Sprintf("%T", err))
		}
	}
	c.State, c.Code = previewStateError, code
	if code != codePreviewTooLarge {
		return c, "", true
	}
	key := previewKey(previewStateError, path, dir, code+digest)
	if key == last {
		return PreviewContent{}, "", false
	}
	return c, key, true
}

// previewKey identifies a result: two refreshes with the same key show the same.
func previewKey(state, path, dir, detail string) string {
	return state + "\x00" + path + "\x00" + dir + "\x00" + detail
}

// SetPreview tells the editor whether the preview pane is open. While it is,
// the editor sends preview:content after the first change and after every
// pause in typing; the call itself gives one refresh. It may be repeated (a
// page that was loaded again asks again), and every call with true gives a
// refresh. When Neovim is not running the state is only remembered: Start
// applies it, so the preview stays on across a restart of Neovim. Without a
// preview in the application it does nothing.
func (e *EditorService) SetPreview(ctx context.Context, enabled bool) error {
	slog.Debug("service call", "method", "SetPreview", "enabled", enabled)
	if e.preview == nil {
		return nil
	}
	if e.isClosed() {
		return failEditor("set preview", term.ErrClosed)
	}
	ctx, cancel := e.bounded(ctx)
	defer cancel()

	e.previewMu.Lock()
	defer e.previewMu.Unlock()
	e.previewWanted = enabled
	// Our side first: switching off must stop what is on its way before Neovim
	// is told, and switching on must not lose the first notification.
	e.preview.enable(enabled)
	if err := e.session.SetPreview(ctx, enabled); err != nil && !errors.Is(err, nvim.ErrNotRunning) {
		if enabled {
			e.previewWanted = false
			e.preview.enable(false)
		}
		return failEditor("set preview", err)
	}
	return nil
}

// applyPreview is Start's part: a new Neovim process does not know that the
// preview is on.
func (e *EditorService) applyPreview(ctx context.Context) {
	if e.preview == nil {
		return
	}
	e.previewMu.Lock()
	defer e.previewMu.Unlock()
	if !e.previewWanted {
		return
	}
	e.preview.enable(true)
	if err := e.session.SetPreview(ctx, true); err != nil {
		slog.Warn("editor: cannot switch the preview on in Neovim", "err", err)
	}
}

// previewTargetOf decides what the preview does with a buffer. A Markdown
// document is converted with its own folder as the place pictures come from;
// a Word document is its Markdown working copy, shown under the user's file
// with the working folder, where the converter put the pictures. Anything else
// (.txt, an unnamed buffer, a file with an extension Pisar does not know) has
// no preview.
func (e *EditorService) previewTargetOf(buf nvim.Buffer) previewTarget {
	file := buf.Path
	if file == "" {
		return previewTarget{}
	}
	if d, ok := e.wordDoc(file); ok {
		v := d.view()
		return previewTarget{path: v.Path, dir: existingDir(filepath.Dir(v.Markdown)), render: true}
	}
	if kind, _ := project.KindOf(filepath.Base(file)); kind == project.KindMD {
		return previewTarget{path: file, dir: existingDir(filepath.Dir(file)), render: true}
	}
	return previewTarget{path: file}
}

// existingDir returns dir when it is an existing absolute folder, otherwise "":
// the renderer then makes a preview without pictures.
func existingDir(dir string) string {
	if filepath.IsAbs(dir) && isDir(dir) {
		return dir
	}
	return ""
}
