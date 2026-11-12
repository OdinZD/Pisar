package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"Pisar/internal/docsafe"
	"Pisar/internal/nvim"
	"Pisar/internal/project"
	"Pisar/internal/term"
)

// This file is the part of the editor service that knows about Word documents:
// Neovim edits the Markdown working copy (doc.md) of a .docx or .doc, and
// everything else in the application, the frontend included, must keep seeing
// the user's file. See worddocs.go for the documents themselves.

// eventDocumentExport reports the progress of writing a Word document's
// Markdown into the user's file. Payload: DocumentExport.
const eventDocumentExport = "document:export"

// The phases of DocumentExport. "converting" is always first; one of the others
// ends the export. "reloaded" ends it too: the working copy was replaced by the
// user's file and the buffer shows it.
const (
	phaseConverting = "converting"
	phaseSaved      = "saved"
	phaseFailed     = "failed"
	phaseCanceled   = "canceled"
	phaseReloaded   = "reloaded"
)

// DocumentExport is a step of an export (see eventDocumentExport). It reaches
// JavaScript as a typed event payload, like DocumentActive.
type DocumentExport struct {
	Path  string `json:"path"`  // the user's file; after the first save of a .doc, the new .docx
	Phase string `json:"phase"` // one of the phase constants
	Code  string `json:"code"`  // the error code when Phase is "failed", otherwise ""
}

var (
	// errSaveDeclined is what saveAll reports when a file that changed outside
	// Pisar was not overwritten: the user chose Cancel, or Reload (which
	// replaces the working copy with the file). Nothing failed, but the
	// document is not saved, so the application must not quit and has no error
	// to show. It is a cancellation, like errRecoverDeclined.
	errSaveDeclined = fmt.Errorf("the export was declined: %w", context.Canceled)
	// errWorkStopped: the editor was shut down before the task ran.
	errWorkStopped = fmt.Errorf("the editor was shut down: %w", term.ErrClosed)
)

// workKind says what a task of the work queue does.
type workKind int

const (
	workExport workKind = iota // write the Markdown of a Word document into the user's file
	workOpen                   // open the Word document named on the command line
	workClose                  // the editor process ended: close the Word documents it had open
)

// workTask is one job of the workQueue. done is closed when it has run; err is
// the result and is only read after that.
type workTask struct {
	kind workKind
	doc  *wordDoc   // workExport
	path string     // workOpen: absolute path
	docs []*wordDoc // workClose
	// explicit: the user asked for the save (the quit dialog's "Save"), so a
	// read-only document is an error instead of being skipped.
	explicit bool

	done chan struct{}
	err  error
}

func newWorkTask(kind workKind) *workTask {
	return &workTask{kind: kind, done: make(chan struct{})}
}

// workQueue runs the slow, interactive jobs of the Word documents on one
// goroutine of its own: the export after a write (pandoc, and a question when
// the file changed outside), the open of the document named on the command
// line, and the closing of documents after Neovim quit. They must not run on
// the goroutine that delivers Neovim's events, which would stop all further
// events while a dialog waits for the user, nor inside Start. One goroutine
// also means that two exports of one document never overlap and that tasks run
// in the order they were queued, which is what lets a write before :wq reach
// the file before the documents are closed.
//
// The goroutine lives from newWorkQueue until stop; its context ends then.
type workQueue struct {
	exec   func(ctx context.Context, t *workTask) error
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	wake   chan struct{} // capacity 1: "the queue changed"

	mu          sync.Mutex
	pending     []*workTask
	current     *workTask   // the task that is running
	interrupted []*workTask // export tasks that failed after stop began
	stopped     bool
}

func newWorkQueue(exec func(ctx context.Context, t *workTask) error) *workQueue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &workQueue{exec: exec, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
	q.wg.Go(q.loop)
	return q
}

func (q *workQueue) loop() {
	for {
		t := q.next()
		if t == nil {
			return
		}
		q.finish(t, q.exec(q.ctx, t))
	}
}

// next blocks until there is a task, and returns nil once the queue is stopped.
func (q *workQueue) next() *workTask {
	for {
		q.mu.Lock()
		if q.ctx.Err() != nil {
			q.mu.Unlock()
			return nil
		}
		if len(q.pending) > 0 {
			t := q.pending[0]
			q.pending[0] = nil
			q.pending = q.pending[1:]
			q.current = t
			q.mu.Unlock()
			return t
		}
		q.mu.Unlock()
		select {
		case <-q.wake:
		case <-q.ctx.Done():
		}
	}
}

func (q *workQueue) finish(t *workTask, err error) {
	q.mu.Lock()
	q.current = nil
	if err != nil && q.ctx.Err() != nil && t.kind == workExport {
		// Cut short by the shutdown, possibly before it recorded that the
		// document has text that is only in the working copy.
		q.interrupted = append(q.interrupted, t)
	}
	q.mu.Unlock()
	t.err = err
	close(t.done)
}

// add queues t and returns the task that will do the job. An export of a
// document that already has an export waiting (not yet started: the one running
// may have read doc.md before the latest write) is merged into it, since one
// export after the last write is enough. After stop the task is finished at once.
func (q *workQueue) add(t *workTask) *workTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		t.err = errWorkStopped
		close(t.done)
		return t
	}
	if t.kind == workExport {
		for _, p := range q.pending {
			if p.kind == workExport && p.doc == t.doc {
				p.explicit = p.explicit || t.explicit
				return p
			}
		}
	}
	q.pending = append(q.pending, t)
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return t
}

// busy reports whether an export of d is waiting or running.
func (q *workQueue) busy(d *wordDoc) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.running(d) || q.waits(d)
}

// waiting reports whether an export of d waits and has not started.
func (q *workQueue) waiting(d *wordDoc) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.waits(d)
}

// running and waits need q.mu.
func (q *workQueue) running(d *wordDoc) bool {
	return q.current != nil && q.current.kind == workExport && q.current.doc == d
}

func (q *workQueue) waits(d *wordDoc) bool {
	for _, p := range q.pending {
		if p.kind == workExport && p.doc == d {
			return true
		}
	}
	return false
}

// idle reports whether nothing is waiting or running.
func (q *workQueue) idle() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.current == nil && len(q.pending) == 0
}

// stop ends the queue: the task that runs gets a cancelled context, the ones
// that wait never run. It waits at most wait for the goroutine (a question to
// the user cannot be cancelled). When it ended, ok is true and unfinished holds
// the export tasks that did not complete, which the caller must treat as
// "this document has text that is only in the working copy".
func (q *workQueue) stop(wait time.Duration) (unfinished []*workTask, ok bool) {
	q.mu.Lock()
	q.stopped = true
	q.mu.Unlock()
	q.cancel()

	ended := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(ended)
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ended:
	case <-timer.C:
		return nil, false
	}

	q.mu.Lock()
	waiting := q.pending
	q.pending = nil
	unfinished = append(unfinished, q.interrupted...)
	q.interrupted = nil
	q.mu.Unlock()
	for _, t := range waiting {
		t.err = errWorkStopped
		close(t.done)
		if t.kind == workExport {
			unfinished = append(unfinished, t)
		}
	}
	return unfinished, true
}

// exportMarks remembers, per Word document, the stamp (size and time) doc.md
// had when its text was last known to be in the user's file. A write event
// whose doc.md still has that stamp has nothing new to export: after the
// quit dialog's "Save" the export was queued directly, and the event of the
// write it made arrives later.
type exportMarks struct {
	mu sync.Mutex
	m  map[*wordDoc]fileStamp
}

func (k *exportMarks) set(d *wordDoc, st fileStamp) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = make(map[*wordDoc]fileStamp)
	}
	k.m[d] = st
}

// setIfAbsent is for a document that is opened again: what is known about the
// first open stays.
func (k *exportMarks) setIfAbsent(d *wordDoc, st fileStamp) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = make(map[*wordDoc]fileStamp)
	}
	if _, ok := k.m[d]; !ok {
		k.m[d] = st
	}
}

func (k *exportMarks) matches(d *wordDoc, st fileStamp) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	known, ok := k.m[d]
	return ok && known.same(st)
}

func (k *exportMarks) forget(docs ...*wordDoc) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, d := range docs {
		delete(k.m, d)
	}
}

// isWordName reports whether a file name is a .docx or .doc.
func isWordName(name string) bool {
	kind, _ := project.KindOf(filepath.Base(name))
	return kind == project.KindDOCX || kind == project.KindDOC
}

// wordDoc finds the Word document whose working copy is the file Neovim reports.
func (e *EditorService) wordDoc(markdown string) (*wordDoc, bool) {
	if e.words == nil || markdown == "" {
		return nil, false
	}
	return e.words.byMarkdown(markdown)
}

// unexported reports whether the document has text that is not in the user's
// file: it is Dirty, or an export is on its way (the write is known, the
// working copy not yet marked), which is what busy tells.
func (e *EditorService) unexported(d *wordDoc) bool {
	return d.view().Dirty || e.queue.busy(d)
}

// describeWord is the document:active payload for a Word document: the user's
// file, never doc.md.
func (e *EditorService) describeWord(d *wordDoc) DocumentActive {
	v := d.view()
	a := e.describePath(v.Path)
	a.ReadOnly = v.ReadOnly
	if a.ReadOnly == nil {
		a.ReadOnly = []string{} // an empty array in JSON, never null
	}
	a.Imported = v.Imported
	if v.Simplified != nil {
		a.Simplified = v.Simplified // else the empty array describePath made: never null in JSON
	}
	// Not busy: an announcement made by the export itself would always see its
	// own task. What the export leaves behind is in Dirty.
	a.Unexported = d.view().Dirty || e.queue.waiting(d)
	return a
}

// openWord opens the Word document at abs in Neovim: the working copy is made
// (or recovered) first and the buffer shows doc.md, read-only when the
// document must not be saved. The conversion and the recovery question run
// outside the time limit of editor calls, which only covers the calls to Neovim.
func (e *EditorService) openWord(ctx context.Context, abs string) error {
	// Checked before the conversion, which can take long: it would be for nothing.
	if !e.session.Running() {
		return fmt.Errorf("open %q: %w", filepath.Base(abs), nvim.ErrNotRunning)
	}
	d, err := e.words.open(ctx, abs)
	if err != nil {
		return err
	}
	if st, err := stampOf(d.markdownPath()); err == nil {
		e.marks.setIfAbsent(d, st)
	}
	v := d.view()
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	if err := e.session.OpenFile(ctx, v.Markdown); err != nil {
		return err
	}
	if len(v.ReadOnly) > 0 {
		if err := e.session.SetReadOnly(ctx, v.Markdown, true); err != nil {
			return err
		}
	}
	return nil
}

// SaveCopy saves the active Word document as a new .docx the user names, and
// opens that file in its place. Closing the dialog is not an error. The
// original, and the document's working copy, stay as they are.
func (e *EditorService) SaveCopy(ctx context.Context) error {
	slog.Debug("service call", "method", "SaveCopy")
	if e.words == nil {
		return failEditor("save copy", fmt.Errorf("%w: Word documents are not supported", docsafe.ErrNotDocument))
	}
	bctx, cancel := e.bounded(ctx)
	cur, err := e.session.Current(bctx)
	cancel()
	if err != nil {
		return failEditor("save copy", err)
	}
	d, ok := e.wordDoc(cur.Path)
	if !ok {
		return failEditor("save copy", fmt.Errorf("%w: the current buffer is not a Word document", docsafe.ErrNotDocument))
	}
	// The dialog and the conversion are not limited in time: the user may think.
	path, err := e.words.saveCopy(ctx, d)
	if err != nil {
		return failEditor("save copy", err)
	}
	if path == "" {
		return nil
	}
	if err := e.openWord(ctx, path); err != nil {
		return failEditor("save copy", err)
	}
	return nil
}

// refuseReadOnlySave fails when the current buffer is a Word document that
// must not be saved, so that Neovim is not asked to write it.
func (e *EditorService) refuseReadOnlySave(ctx context.Context) error {
	if e.words == nil {
		return nil
	}
	cur, err := e.session.Current(ctx)
	if err != nil {
		return err
	}
	d, ok := e.wordDoc(cur.Path)
	if !ok {
		return nil
	}
	if ro := d.reasons(); len(ro) > 0 {
		v := d.view()
		return fmt.Errorf("save %q: %w", filepath.Base(v.Path), &docsafe.ReadOnlyError{Path: v.Path, Reason: docsafe.ReadOnlyReason(ro[0])})
	}
	return nil
}

// unsavedWords returns the open Word documents that are not saved: the buffer
// is modified (bufs lists the modified buffers), the working copy is Dirty, or
// an export is waiting or running.
func (e *EditorService) unsavedWords(bufs []nvim.Buffer) []*wordDoc {
	var out []*wordDoc
	for _, d := range e.words.all() {
		unsaved := e.unexported(d)
		for _, b := range bufs {
			if b.Path != "" && samePath(b.Path, d.markdownPath()) {
				unsaved = true
			}
		}
		if unsaved {
			out = append(out, d)
		}
	}
	return out
}

// withWords turns the modified buffers into what the quit dialog lists: a Word
// document under the name of the user's file, once, and also the Word
// documents that are unsaved without a modified buffer.
func (e *EditorService) withWords(bufs []nvim.Buffer) []nvim.Buffer {
	out := make([]nvim.Buffer, 0, len(bufs))
	listed := map[*wordDoc]bool{}
	for _, b := range bufs {
		d, ok := e.wordDoc(b.Path)
		switch {
		case !ok:
			out = append(out, b)
		case !listed[d]:
			listed[d] = true
			out = append(out, nvim.Buffer{Path: d.view().Path, Modified: true})
		}
	}
	for _, d := range e.unsavedWords(bufs) {
		if !listed[d] {
			out = append(out, nvim.Buffer{Path: d.view().Path, Modified: true})
		}
	}
	return out
}

// wordUnsaved reports whether the Word document of the user's file at path is
// unsaved, and whether there is such a document at all.
func (e *EditorService) wordUnsaved(ctx context.Context, path string) (unsaved, isWord bool, err error) {
	if e.words == nil {
		return false, false, nil
	}
	d, ok := e.words.byPath(path)
	if !ok {
		return false, false, nil
	}
	if e.unexported(d) {
		return true, true, nil
	}
	bufs, err := e.session.Buffers(ctx)
	if err != nil {
		return false, true, err
	}
	for _, b := range bufs {
		if b.Modified && samePath(b.Path, d.markdownPath()) {
			return true, true, nil
		}
	}
	return false, true, nil
}

// wordRenamed tells the document that its file got a new name. The buffer is
// left alone: it is doc.md, whose name does not change.
func (e *EditorService) wordRenamed(ctx context.Context, oldPath, newPath string) (handled bool, err error) {
	if e.words == nil {
		return false, nil
	}
	d, ok := e.words.byPath(oldPath)
	if !ok {
		return false, nil
	}
	if err := e.words.renamed(ctx, oldPath, newPath); err != nil {
		return true, err
	}
	e.announce(ctx, d, true)
	return true, nil
}

// wordRemoved closes the buffer of a Word document whose file went to the
// Trash and discards its working copy. The buffer goes first: when it cannot
// be closed, the working copy, which may hold text, stays.
func (e *EditorService) wordRemoved(ctx context.Context, path string) (handled bool, err error) {
	if e.words == nil {
		return false, nil
	}
	d, ok := e.words.byPath(path)
	if !ok {
		return false, nil
	}
	if _, err := e.session.Wipe(ctx, d.markdownPath()); err != nil {
		return true, err
	}
	if err := e.words.removed(ctx, path); err != nil {
		return true, err
	}
	e.marks.forget(d)
	return true, nil
}

// queueExport queues the export that follows a write of doc.md. A document
// that is read-only is not exported: with :w! the text is the one that was
// opened, and the user's file must stay as it is.
func (e *EditorService) queueExport(markdown string) {
	d, ok := e.wordDoc(markdown)
	if !ok || len(d.reasons()) > 0 {
		return
	}
	e.queue.add(&workTask{kind: workExport, doc: d, done: make(chan struct{})})
}

// closeWords queues the closing of the Word documents that are open now, for
// the end of a Neovim process. It goes behind the exports already queued, so a
// write before :wq reaches the user's file first.
func (e *EditorService) closeWords() {
	if e.words == nil {
		return
	}
	docs := e.words.all()
	if len(docs) == 0 {
		return
	}
	t := newWorkTask(workClose)
	t.docs = docs
	e.queue.add(t)
}

// runTask is the workQueue's job runner.
func (e *EditorService) runTask(ctx context.Context, t *workTask) error {
	switch t.kind {
	case workExport:
		return e.runExport(ctx, t)
	case workOpen:
		return e.runOpen(ctx, t)
	case workClose:
		return e.runClose(ctx, t)
	}
	return fmt.Errorf("unknown task kind %d", t.kind)
}

func (e *EditorService) emitExport(path, phase, code string) {
	slog.Debug("editor event", "event", eventDocumentExport, "file", filepath.Base(path), "phase", phase, "code", code)
	e.notify(eventDocumentExport, DocumentExport{Path: path, Phase: phase, Code: code})
}

// runExport records the write, converts, replaces the user's file and reports
// each step. Its error is what a waiting saveAll learns: nil when the file is
// saved, errSaveDeclined when the user declined, otherwise the failure.
func (e *EditorService) runExport(ctx context.Context, t *workTask) error {
	d := t.doc
	if len(d.reasons()) > 0 && !t.explicit {
		slog.Debug("editor: not exporting a read-only document", "file", filepath.Base(d.view().Path))
		return nil
	}
	// Nothing new since the last export: the write this task answers is already in the file.
	if st, err := stampOf(d.markdownPath()); err == nil && !d.view().Dirty && e.marks.matches(d, st) {
		slog.Debug("editor: nothing to export", "file", filepath.Base(d.view().Path))
		return nil
	}

	path := d.view().Path
	if err := e.words.markSaved(ctx, d); err != nil {
		return e.exportFailed(path, err)
	}
	e.emitExport(path, phaseConverting, "")
	// Before the conversion: a write that happens during it must not be taken for exported.
	stamp, stampErr := stampOf(d.markdownPath())
	res, err := e.words.export(ctx, d)
	if err != nil {
		return e.exportFailed(path, err)
	}
	switch res.Outcome {
	case exportSaved:
		if stampErr == nil {
			e.marks.set(d, stamp)
		}
		if res.Warning != nil {
			// The file is in place; the user's work is not at risk.
			slog.Warn("editor: the document is saved, but something after the save failed", "file", filepath.Base(res.View.Path), "err", res.Warning)
		}
		e.emitExport(res.View.Path, phaseSaved, "")
		// The first save of a .doc gives the document a new file, which the
		// toolbar and the tree have to learn.
		e.announce(ctx, d, res.View.Path != path)
		return nil
	case exportCanceled:
		e.emitExport(path, phaseCanceled, "")
		return errSaveDeclined
	case exportReloaded:
		if err := e.reloadBuffer(ctx, d); err != nil {
			return e.exportFailed(path, err)
		}
		if st, err := stampOf(d.markdownPath()); err == nil {
			e.marks.set(d, st)
		}
		e.announce(ctx, d, true)
		e.emitExport(res.View.Path, phaseReloaded, "")
		return errSaveDeclined
	}
	return e.exportFailed(path, fmt.Errorf("unknown export outcome %d", res.Outcome))
}

// exportFailed reports a failed export and returns the error.
func (e *EditorService) exportFailed(path string, err error) error {
	code := codeOf(err)
	if code == codeInternal {
		slog.Error("editor: export failed", "file", filepath.Base(path), "err", err)
	} else {
		slog.Warn("editor: export failed", "file", filepath.Base(path), "code", code, "err", err)
	}
	e.emitExport(path, phaseFailed, code)
	return err
}

// reloadBuffer makes the buffer of d show the working copy again, which the
// "Reload" answer has just replaced with the user's file, and sets its
// read-only state to what the reloaded document needs.
func (e *EditorService) reloadBuffer(ctx context.Context, d *wordDoc) error {
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	md := d.markdownPath()
	// No buffer: Neovim does not have the document open (any more), and the
	// next open reads the new file anyway.
	if err := e.session.Reload(ctx, md); err != nil && !errors.Is(err, nvim.ErrNoBuffer) {
		return err
	}
	if err := e.session.SetReadOnly(ctx, md, len(d.reasons()) > 0); err != nil && !errors.Is(err, nvim.ErrNoBuffer) {
		return err
	}
	return nil
}

// announce tells the frontend what changed about d: its state, and, when
// asked and the document is the active one, the whole document:active again
// (a new name, new read-only reasons).
func (e *EditorService) announce(ctx context.Context, d *wordDoc, active bool) {
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	md := d.markdownPath()
	if active {
		if cur, err := e.session.Current(ctx); err == nil && samePath(cur.Path, md) {
			// Active also sends the state.
			editorEvents{e}.Active(cur.Path, cur.Modified)
			return
		}
	}
	bufs, err := e.session.Buffers(ctx)
	if err != nil {
		slog.Debug("editor: cannot report the state of a document", "err", err)
		return
	}
	for _, b := range bufs {
		if samePath(b.Path, md) {
			e.notify(eventDocumentState, DocumentState{Path: d.view().Path, Modified: b.Modified})
			return
		}
	}
}

// runOpen opens the Word document named on the command line. The conversion
// and the recovery question are why this is not done inside Start.
func (e *EditorService) runOpen(ctx context.Context, t *workTask) error {
	err := e.openWord(ctx, t.path)
	switch {
	case err == nil, ctx.Err() != nil:
		// Done, or cut short by the shutdown, which reports nothing.
	case errors.Is(err, context.Canceled):
		// The user declined the recovery question.
		slog.Info("editor: the document on the command line was not opened", "file", filepath.Base(t.path))
		e.emitExport(t.path, phaseCanceled, "")
	default:
		slog.Error("editor: cannot open the document on the command line", "file", filepath.Base(t.path), "err", err)
		e.emitExport(t.path, phaseFailed, codeOf(err))
	}
	return err
}

// runClose closes the documents of a Neovim process that ended.
func (e *EditorService) runClose(ctx context.Context, t *workTask) error {
	e.marks.forget(t.docs...)
	if err := e.words.release(ctx, t.docs); err != nil {
		slog.Warn("editor: cannot close the Word documents", "err", err)
		return err
	}
	return nil
}

// stopWords is the Word documents' part of shutdown: the queue stops, the
// documents whose export did not complete are marked as holding unsaved text
// (so the next start offers to recover it), and the working copies with
// nothing unsaved are removed.
func (e *EditorService) stopWords() {
	unfinished, ok := e.queue.stop(e.limits.CommandTimeout)
	if !ok {
		// A question to the user is still open. Closing now would wait for it
		// forever, and every working copy is safe where it is.
		slog.Error("editor: an export did not stop; the working copies are kept")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), e.limits.CommandTimeout)
	defer cancel()
	for _, t := range unfinished {
		if err := e.words.markSaved(ctx, t.doc); err != nil {
			slog.Error("editor: cannot record that a document has unsaved text", "file", filepath.Base(t.doc.view().Path), "err", err)
		}
	}
	if err := e.words.close(ctx); err != nil {
		slog.Warn("editor: cannot close the Word documents", "err", err)
	}
}

// saveAllWords is the part of saveAll for the Word documents. Writing their
// working copies is not enough: the user's files must be written too, and the
// application must not quit before that, or while the text exists only in a
// working copy. Waiting is not bounded by ctx (the caller's ctx is the limit
// for the Neovim calls): the conversion has limits of its own, and a question
// to the user takes as long as the user does.
func (e *EditorService) saveAllWords(ctx context.Context) error {
	bctx, cancel := e.bounded(ctx)
	defer cancel()

	// Before SaveAll: afterwards the buffers are no longer modified.
	bufs, err := e.session.Modified(bctx)
	if err != nil {
		return err
	}
	docs := e.unsavedWords(bufs)

	saveErr := e.session.SaveAll(bctx)
	if errors.Is(saveErr, nvim.ErrNotRunning) && len(docs) > 0 {
		// Neovim ended; what is left to write are the exports (:wq or :q
		// leave text in a working copy until its export is done).
		saveErr = nil
	}

	// Queued directly: the notification of the write SaveAll has just made
	// reaches us later, and must not be waited for.
	tasks := make([]*workTask, 0, len(docs))
	for _, d := range docs {
		t := newWorkTask(workExport)
		t.doc, t.explicit = d, true
		tasks = append(tasks, e.queue.add(t))
	}

	var failures []error
	declined := false
	for _, t := range tasks {
		select {
		case <-t.done:
		case <-e.queue.ctx.Done():
			failures = append(failures, errWorkStopped)
			continue
		}
		switch {
		case t.err == nil:
		case errors.Is(t.err, errSaveDeclined):
			declined = true
		default:
			failures = append(failures, t.err)
		}
	}
	if err := errors.Join(append([]error{saveErr}, failures...)...); err != nil {
		return err
	}
	if declined {
		return errSaveDeclined
	}
	return nil
}
