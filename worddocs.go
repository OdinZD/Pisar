package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"Pisar/internal/convert"
	"Pisar/internal/docsafe"
)

// Names inside a working folder besides the ones the convert package owns.
const (
	wordReferenceName = "reference.docx"
	wordImportedName  = "imported.docx" // .doc converted by textutil; the source of the Markdown
	wordExportName    = "export.docx"   // the conversion output that Replace/CreateNew copy from
	wordPrevMarkdown  = "doc.prev.md"
	wordPrevMedia     = "media.prev"
	wordPrevReference = "reference.prev.docx"
)

// maxExportAttempts bounds how often a conversion is repeated because the
// editor wrote doc.md while pandoc was reading it. A real save takes
// milliseconds, so more than one repeat means something keeps writing. It is a
// constant here because the limits in internal/config are not part of this step.
const maxExportAttempts = 4

var (
	// errRecoverDeclined is the user's "Cancel" in the recovery question. It is
	// a cancellation (code "canceled"), not a failure.
	errRecoverDeclined = fmt.Errorf("recovery declined: %w", context.Canceled)
	// errMarkdownUnstable: doc.md changed during every attempt to convert it.
	errMarkdownUnstable = errors.New("the Markdown kept changing while it was converted")
)

type recoverChoice int

const (
	recoverUse recoverChoice = iota
	recoverDiscard
	recoverCancel
)

type conflictChoice int

const (
	conflictOverwrite conflictChoice = iota
	conflictReload
	conflictCancel
)

// wordDialogs are the questions asked about a Word document. Each blocks until answered.
type wordDialogs interface {
	AskRecover(name string) recoverChoice
	AskConflict(name string) conflictChoice
	AskSavePath(title, dir, name string) (path string, err error) // "" when cancelled
}

// wordConverter is what is needed of *convert.Converter.
type wordConverter interface {
	DocxToMarkdown(ctx context.Context, src, dir string) ([]convert.Loss, error)
	MarkdownToDocx(ctx context.Context, dir, reference, dst string) error
	DocToDocx(ctx context.Context, src, dst string) error
	MakeReference(ctx context.Context, src, dst string) error
}

// wordFiles is what is needed of *docsafe.Ops.
type wordFiles interface {
	CreateWork(ctx context.Context, meta docsafe.WorkMeta) (docsafe.WorkCopy, error)
	FindWork(ctx context.Context, path string) (docsafe.WorkCopy, bool, error)
	SaveWorkMeta(ctx context.Context, w docsafe.WorkCopy) error
	DiscardWork(ctx context.Context, w docsafe.WorkCopy) error
	SweepWork(ctx context.Context) ([]docsafe.WorkCopy, error)
	Replace(ctx context.Context, src, dst string, expect docsafe.Snapshot) (docsafe.ReplaceResult, error)
	CreateNew(ctx context.Context, src, dst string) (docsafe.Snapshot, error)
}

type wordDocsDeps struct {
	Convert wordConverter
	Inspect func(ctx context.Context, path string) ([]convert.Loss, error)
	Files   wordFiles
	Dialogs wordDialogs
	Text    uiText
}

// wordView is what the rest of the application may know about a document.
type wordView struct {
	Path     string   // the user's file this document is shown as: the target once there is one, else the origin
	Markdown string   // absolute path of doc.md
	ReadOnly []string // reason codes, empty when the document may be saved
	Imported bool     // came from a .doc
	Dirty    bool     // the Markdown holds text that is not in the user's file
}

type exportOutcome int

const (
	exportSaved exportOutcome = iota
	exportReloaded
	exportCanceled
)

type exportResult struct {
	Outcome exportOutcome
	View    wordView // the document after the export
	Backup  string   // backup made by this export, "" if none
	Warning error    // the file is in place but something after the rename failed
}

// wordDoc is one open Word document: its working folder and what is known about it.
type wordDoc struct {
	dir string // the working folder; never changes

	// opMu serializes the operations on this document (open, export, saveCopy,
	// markSaved, ...). It is held across conversions and dialogs.
	opMu sync.Mutex
	// mu guards meta and readOnly. It is held only for a copy, never over I/O or
	// a dialog, so view stays answerable while an export waits for the user.
	mu       sync.Mutex
	meta     docsafe.WorkMeta
	readOnly []string
}

func (d *wordDoc) metaCopy() docsafe.WorkMeta {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.meta
}

func (d *wordDoc) reasons() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.readOnly)
}

func (d *wordDoc) set(meta docsafe.WorkMeta, readOnly []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.meta, d.readOnly = meta, readOnly
}

func (d *wordDoc) workCopy() docsafe.WorkCopy {
	return docsafe.WorkCopy{Dir: d.dir, Meta: d.metaCopy()}
}

func (d *wordDoc) markdownPath() string { return filepath.Join(d.dir, convert.MarkdownName) }

// view is what the rest of the application may know about the document.
func (d *wordDoc) view() wordView {
	d.mu.Lock()
	defer d.mu.Unlock()
	path := d.meta.Target
	if path == "" {
		path = d.meta.Origin
	}
	return wordView{
		Path:     path,
		Markdown: d.markdownPath(),
		ReadOnly: slices.Clone(d.readOnly),
		Imported: isDocPath(d.meta.Origin),
		Dirty:    d.meta.Dirty,
	}
}

func isDocPath(p string) bool  { return strings.EqualFold(filepath.Ext(p), ".doc") }
func isDocxPath(p string) bool { return strings.EqualFold(filepath.Ext(p), ".docx") }

// stem is the file name without its extension.
func stem(p string) string {
	b := filepath.Base(p)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// openCall lets a second open of the same file wait for the first instead of
// making a second working copy.
type openCall struct {
	done chan struct{}
	doc  *wordDoc
	err  error
}

// wordDocs manages the open Word documents. Its methods may be called from
// several goroutines.
type wordDocs struct {
	conv    wordConverter
	inspect func(ctx context.Context, path string) ([]convert.Loss, error)
	files   wordFiles
	dialogs wordDialogs
	text    uiText

	mu      sync.Mutex // guards docs and opening; never held over I/O or a dialog
	docs    []*wordDoc
	opening map[string]*openCall
}

func newWordDocs(d wordDocsDeps) (*wordDocs, error) {
	switch {
	case d.Convert == nil:
		return nil, errors.New("word documents: a converter is required")
	case d.Inspect == nil:
		return nil, errors.New("word documents: Inspect is required")
	case d.Files == nil:
		return nil, errors.New("word documents: the file operations are required")
	case d.Dialogs == nil:
		return nil, errors.New("word documents: the dialogs are required")
	}
	return &wordDocs{
		conv: d.Convert, inspect: d.Inspect, files: d.Files, dialogs: d.Dialogs, text: d.Text,
		opening: make(map[string]*openCall),
	}, nil
}

// snapshotDocs returns the open documents without holding the lock afterwards.
func (w *wordDocs) snapshotDocs() []*wordDoc {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.docs)
}

func (w *wordDocs) all() []*wordDoc { return w.snapshotDocs() }

// samePath compares two paths by name first and by what they resolve to
// second: Neovim may report /private/var/... for what the application knows as /var/....
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// byMarkdown finds the document by the path of its doc.md.
func (w *wordDocs) byMarkdown(md string) (*wordDoc, bool) {
	for _, d := range w.snapshotDocs() {
		if samePath(d.markdownPath(), md) {
			return d, true
		}
	}
	return nil, false
}

// byPath finds the document by the user's file: its origin or its target.
func (w *wordDocs) byPath(path string) (*wordDoc, bool) {
	for _, d := range w.snapshotDocs() {
		m := d.metaCopy()
		if samePath(m.Origin, path) || (m.Target != "" && samePath(m.Target, path)) {
			return d, true
		}
	}
	return nil, false
}

// lookupExact is the cheap lookup open does under the lock: names only.
func (w *wordDocs) lookupExact(path string) *wordDoc {
	for _, d := range w.docs {
		m := d.metaCopy()
		if m.Origin == path || m.Target == path {
			return d
		}
	}
	return nil
}

// open returns the open document for path (absolute; a .docx or .doc), preparing
// its working copy if needed. Concurrent opens of one file share one result,
// the error included.
func (w *wordDocs) open(ctx context.Context, path string) (*wordDoc, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("open %q: not an absolute path", path)
	}
	path = filepath.Clean(path)
	if !isDocxPath(path) && !isDocPath(path) {
		return nil, fmt.Errorf("open %q: %w", path, docsafe.ErrUnsupportedKind)
	}

	w.mu.Lock()
	if d := w.lookupExact(path); d != nil {
		w.mu.Unlock()
		return d, nil
	}
	if c, ok := w.opening[path]; ok {
		w.mu.Unlock()
		select {
		case <-c.done:
			return c.doc, c.err
		case <-ctx.Done():
			return nil, fmt.Errorf("open %q: %w", path, ctx.Err())
		}
	}
	c := &openCall{done: make(chan struct{})}
	w.opening[path] = c
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		delete(w.opening, path)
		// Another name for a file that is already open (a different spelling of
		// the path) resolves to the same working folder: not a second entry.
		if c.err == nil && c.doc != nil && !slices.Contains(w.docs, c.doc) {
			w.docs = append(w.docs, c.doc)
		}
		w.mu.Unlock()
		close(c.done) // c.doc and c.err are visible to the waiters through this
	}()
	c.doc, c.err = w.openNew(ctx, path)
	return c.doc, c.err
}

func (w *wordDocs) openNew(ctx context.Context, path string) (*wordDoc, error) {
	wc, found, err := w.files.FindWork(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	if found {
		// Open under another spelling of the path: never discard a folder in use.
		for _, d := range w.snapshotDocs() {
			if d.dir == wc.Dir {
				return d, nil
			}
		}
		if wc.Meta.Dirty {
			switch w.dialogs.AskRecover(filepath.Base(path)) {
			case recoverUse:
				d, err := w.recover(wc)
				if err != nil {
					return nil, fmt.Errorf("open %q: %w", path, err)
				}
				return d, nil
			case recoverDiscard:
			default:
				return nil, fmt.Errorf("open %q: %w", path, errRecoverDeclined)
			}
		}
		// A clean copy is a leftover; a dirty one the user chose to throw away.
		if err := w.files.DiscardWork(ctx, wc); err != nil {
			return nil, fmt.Errorf("open %q: discard the old working copy: %w", path, err)
		}
	}
	return w.prepare(ctx, path)
}

// recover takes a working copy as it is. Nothing in the folder is changed or
// deleted: it may hold the only copy of the user's text.
func (w *wordDocs) recover(wc docsafe.WorkCopy) (*wordDoc, error) {
	for _, name := range []string{convert.MarkdownName, wordReferenceName} {
		fi, err := os.Lstat(filepath.Join(wc.Dir, name))
		switch {
		case err != nil:
			return nil, fmt.Errorf("%w: %s of %s: %w", docsafe.ErrDamagedWork, name, filepath.Base(wc.Dir), err)
		case !fi.Mode().IsRegular():
			return nil, fmt.Errorf("%w: %s of %s is not a regular file", docsafe.ErrDamagedWork, name, filepath.Base(wc.Dir))
		}
	}
	var reasons []string
	if wc.Meta.Target != "" {
		r, err := writableReasons(wc.Meta.Target)
		if err != nil {
			return nil, err
		}
		reasons = r
	}
	return &wordDoc{dir: wc.Dir, meta: wc.Meta, readOnly: reasons}, nil
}

// writableReasons is the read-only reason of docsafe.CheckWritable as a list.
// A file that is gone counts as "not-regular": the document stays open, so its
// text can still be saved as a copy.
func writableReasons(path string) ([]string, error) {
	r, err := docsafe.CheckWritable(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{string(docsafe.ReadOnlyNotRegular)}, nil
	}
	if err != nil {
		return nil, err
	}
	if r == docsafe.ReadOnlyNone {
		return nil, nil
	}
	return []string{string(r)}, nil
}

// prepare makes a new working copy of path.
func (w *wordDocs) prepare(ctx context.Context, path string) (*wordDoc, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("open %q: %w: not a regular file", path, docsafe.ErrNotDocument)
	}
	// Before the conversion, so a change made while it runs shows up as a conflict.
	snap, err := docsafe.TakeSnapshot(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	meta := docsafe.WorkMeta{Origin: path, Snapshot: snap}
	if !isDocPath(path) {
		meta.Target = path // a .doc gets its target at the first export
	}
	wc, err := w.files.CreateWork(ctx, meta)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	reasons, err := w.fill(ctx, wc, path)
	if err != nil {
		// The folder is brand new: nothing of the user's text is in it. The
		// context may be the reason for the failure, and DiscardWork refuses a
		// cancelled one.
		if derr := w.files.DiscardWork(context.WithoutCancel(ctx), wc); derr != nil {
			err = errors.Join(err, derr)
		}
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	return &wordDoc{dir: wc.Dir, meta: wc.Meta, readOnly: reasons}, nil
}

// fill puts the converted document into a new working folder and returns the
// reasons for which it must stay read-only.
func (w *wordDocs) fill(ctx context.Context, wc docsafe.WorkCopy, path string) ([]string, error) {
	src := path
	if isDocPath(path) {
		src = filepath.Join(wc.Dir, wordImportedName)
		if err := w.conv.DocToDocx(ctx, path, src); err != nil {
			return nil, err
		}
	}
	reasons, err := w.convertDocx(ctx, wc.Dir, src)
	if err != nil {
		return nil, err
	}
	if wc.Meta.Target != "" {
		more, err := writableReasons(wc.Meta.Target)
		if err != nil {
			return nil, err
		}
		reasons = append(reasons, more...)
	}
	return reasons, nil
}

// convertDocx checks src, writes the reference document and the Markdown into
// dir, and returns the losses as reason codes.
func (w *wordDocs) convertDocx(ctx context.Context, dir, src string) ([]string, error) {
	losses, err := w.inspect(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("inspect: %w", err)
	}
	if err := w.conv.MakeReference(ctx, src, filepath.Join(dir, wordReferenceName)); err != nil {
		return nil, fmt.Errorf("reference document: %w", err)
	}
	more, err := w.conv.DocxToMarkdown(ctx, src, dir)
	if err != nil {
		return nil, fmt.Errorf("convert to Markdown: %w", err)
	}
	var reasons []string
	for _, l := range append(losses, more...) {
		reasons = append(reasons, string(l))
	}
	return reasons, nil
}

// markSaved records that the editor wrote doc.md.
func (w *wordDocs) markSaved(ctx context.Context, d *wordDoc) error {
	// Waits for an export in progress on purpose: a :w that lands during an
	// export must set Dirty after the export cleared it, not before.
	d.opMu.Lock()
	defer d.opMu.Unlock()
	meta, ro := d.metaCopy(), d.reasons()
	// A read-only buffer cannot change; with :w! the content is the same.
	if len(ro) > 0 || meta.Dirty {
		return nil
	}
	meta.Dirty = true
	if err := w.saveMeta(ctx, d, meta); err != nil {
		return fmt.Errorf("mark %q as saved: %w", filepath.Base(meta.Origin), err)
	}
	d.set(meta, ro)
	return nil
}

func (w *wordDocs) saveMeta(ctx context.Context, d *wordDoc, meta docsafe.WorkMeta) error {
	return w.files.SaveWorkMeta(ctx, docsafe.WorkCopy{Dir: d.dir, Meta: meta})
}

// fileStamp is what is compared to see whether the editor wrote a file.
type fileStamp struct {
	size int64
	mod  time.Time
}

func stampOf(path string) (fileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{fi.Size(), fi.ModTime()}, nil
}

func (a fileStamp) same(b fileStamp) bool { return a.size == b.size && a.mod.Equal(b.mod) }

// removeOwn removes a file of Pisar's own working folder. A failure only
// leaves a stray file that the next conversion removes first.
func removeOwn(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("word documents: cannot remove a working file", "file", filepath.Base(path), "err", err)
	}
}

// convertMarkdown writes out from the Markdown of d. The editor may write
// doc.md while pandoc reads it, so the file's size and time are compared
// before and after, and the conversion is repeated when they differ.
func (w *wordDocs) convertMarkdown(ctx context.Context, d *wordDoc, out string) error {
	md := d.markdownPath()
	ref := filepath.Join(d.dir, wordReferenceName)
	for range maxExportAttempts {
		if err := os.Remove(out); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove the previous output: %w", err)
		}
		before, err := stampOf(md)
		if err != nil {
			return fmt.Errorf("read the Markdown: %w", err)
		}
		if err := w.conv.MarkdownToDocx(ctx, d.dir, ref, out); err != nil {
			return fmt.Errorf("convert to .docx: %w", err)
		}
		after, err := stampOf(md)
		if err != nil {
			return fmt.Errorf("read the Markdown: %w", err)
		}
		if before.same(after) {
			return nil
		}
	}
	return errMarkdownUnstable
}

// export writes the Markdown of d into the user's file.
func (w *wordDocs) export(ctx context.Context, d *wordDoc) (exportResult, error) {
	d.opMu.Lock()
	defer d.opMu.Unlock()

	meta := d.metaCopy()
	name := filepath.Base(d.view().Path)
	if ro := d.reasons(); len(ro) > 0 {
		return exportResult{}, fmt.Errorf("export %q: %w", name,
			&docsafe.ReadOnlyError{Path: d.view().Path, Reason: docsafe.ReadOnlyReason(ro[0])})
	}

	out := filepath.Join(d.dir, wordExportName)
	defer removeOwn(out)
	if err := w.convertMarkdown(ctx, d, out); err != nil {
		return exportResult{}, fmt.Errorf("export %q: %w", name, err)
	}
	if meta.Target == "" {
		return w.exportFirst(ctx, d, out)
	}
	return w.exportReplace(ctx, d, out)
}

// exportReplace puts out in place of the target.
func (w *wordDocs) exportReplace(ctx context.Context, d *wordDoc, out string) (exportResult, error) {
	meta := d.metaCopy()
	name := filepath.Base(meta.Target)
	for {
		res, err := w.files.Replace(ctx, out, meta.Target, meta.Snapshot)
		if !errors.Is(err, docsafe.ErrConflict) {
			return w.finishReplace(ctx, d, meta, out, res, err)
		}
		// The list lock is not held: only this document's opMu, which view does not take.
		switch w.dialogs.AskConflict(name) {
		case conflictOverwrite:
			// Replace backs up the other program's content by itself.
			snap, err := docsafe.TakeSnapshot(ctx, meta.Target)
			if err != nil {
				return exportResult{}, fmt.Errorf("export %q: %w", name, err)
			}
			meta.Snapshot = snap
		case conflictReload:
			return w.reload(ctx, d)
		default:
			return exportResult{Outcome: exportCanceled, View: d.view()}, nil
		}
	}
}

// finishReplace records the outcome of a Replace. A Replace that fails after
// the rename returns the new state together with the error: the file is in
// place, so the document counts as saved and the error becomes a warning.
func (w *wordDocs) finishReplace(ctx context.Context, d *wordDoc, meta docsafe.WorkMeta, out string,
	res docsafe.ReplaceResult, err error) (exportResult, error) {
	name := filepath.Base(meta.Target)
	// Nothing below may be cut short: the user's file already has the new content.
	done := context.WithoutCancel(ctx)
	var warning error
	if err != nil {
		snap, placed := w.placed(done, res, out, meta.Target)
		if !placed {
			return exportResult{}, fmt.Errorf("export %q: %w", name, err)
		}
		res.Snapshot, warning = snap, fmt.Errorf("export %q: %w", name, err)
	}
	meta.Snapshot, meta.Dirty = res.Snapshot, false
	if serr := w.saveMeta(done, d, meta); serr != nil {
		warning = errors.Join(warning, fmt.Errorf("export %q: save the working copy state: %w", name, serr))
	}
	d.set(meta, d.reasons())
	return exportResult{Outcome: exportSaved, View: d.view(), Backup: res.Backup, Warning: warning}, nil
}

// placed tells whether a failed Replace had already put the new content in
// place. Replace says so by returning the new snapshot; when it could not read
// it, the target is compared with the output.
func (w *wordDocs) placed(ctx context.Context, res docsafe.ReplaceResult, out, target string) (docsafe.Snapshot, bool) {
	if res.Snapshot.SHA256 != "" {
		return res.Snapshot, true
	}
	want, err := docsafe.TakeSnapshot(ctx, out)
	if err != nil {
		return docsafe.Snapshot{}, false
	}
	got, err := docsafe.TakeSnapshot(ctx, target)
	if err != nil || !got.Same(want) {
		return docsafe.Snapshot{}, false
	}
	return got, true
}

// exportFirst is the first save of an imported .doc: a new .docx next to it.
func (w *wordDocs) exportFirst(ctx context.Context, d *wordDoc, out string) (exportResult, error) {
	meta := d.metaCopy()
	dir := filepath.Dir(meta.Origin)
	name := stem(meta.Origin) + ".docx"
	nd, err := w.createDocx(ctx, out, filepath.Join(dir, name), w.text.ImportSaveTitle, dir, name)
	if err != nil {
		return exportResult{}, fmt.Errorf("export %q: %w", filepath.Base(meta.Origin), err)
	}
	if nd.path == "" {
		return exportResult{Outcome: exportCanceled, View: d.view()}, nil
	}
	done := context.WithoutCancel(ctx) // the new file exists
	meta.Target, meta.Snapshot, meta.Dirty = nd.path, nd.snap, false
	warning := nd.warn
	if serr := w.saveMeta(done, d, meta); serr != nil {
		warning = errors.Join(warning, fmt.Errorf("export %q: save the working copy state: %w", filepath.Base(nd.path), serr))
	}
	d.set(meta, d.reasons())
	return exportResult{Outcome: exportSaved, View: d.view(), Warning: warning}, nil
}

// newDocx is the result of createDocx; path is "" when the user cancelled.
type newDocx struct {
	path string
	snap docsafe.Snapshot
	warn error // the file is in place but something after it was created failed
}

// createDocx writes out as a new file at path, never over an existing one: when
// the name is taken, the user is asked for another until one is free or they
// cancel.
func (w *wordDocs) createDocx(ctx context.Context, out, path, title, dir, suggested string) (newDocx, error) {
	for {
		if path == "" {
			p, err := w.askDocxPath(title, dir, suggested)
			if err != nil {
				return newDocx{}, err
			}
			if p == "" {
				return newDocx{}, nil
			}
			path = p
		}
		snap, err := w.files.CreateNew(ctx, out, path)
		switch {
		case err == nil:
			return newDocx{path: path, snap: snap}, nil
		case errors.Is(err, docsafe.ErrExists):
			path = ""
		case snap.SHA256 != "":
			// CreateNew returns the snapshot with the error only once the file is complete.
			return newDocx{path: path, snap: snap, warn: err}, nil
		default:
			return newDocx{}, err
		}
	}
}

// askDocxPath asks where to save and makes sure the name ends in .docx.
func (w *wordDocs) askDocxPath(title, dir, name string) (string, error) {
	p, err := w.dialogs.AskSavePath(title, dir, name)
	if err != nil {
		return "", fmt.Errorf("ask for a file name: %w", err)
	}
	if p == "" {
		return "", nil
	}
	if !isDocxPath(p) {
		p += ".docx"
	}
	return p, nil
}

// saveCopy writes the Markdown of d into a new .docx the user names, and returns its path ("" when cancelled).
func (w *wordDocs) saveCopy(ctx context.Context, d *wordDoc) (string, error) {
	d.opMu.Lock()
	defer d.opMu.Unlock()

	v := d.view()
	dir := filepath.Dir(v.Path)
	suggested := stem(v.Path) + " " + w.text.SaveCopySuffix + ".docx"
	path, err := w.askDocxPath(w.text.SaveCopyTitle, dir, suggested)
	if err != nil {
		return "", fmt.Errorf("save a copy of %q: %w", filepath.Base(v.Path), err)
	}
	if path == "" {
		return "", nil
	}
	out := filepath.Join(d.dir, wordExportName)
	defer removeOwn(out)
	if err := w.convertMarkdown(ctx, d, out); err != nil {
		return "", fmt.Errorf("save a copy of %q: %w", filepath.Base(v.Path), err)
	}
	nd, err := w.createDocx(ctx, out, path, w.text.SaveCopyTitle, dir, suggested)
	if err != nil {
		return "", fmt.Errorf("save a copy of %q: %w", filepath.Base(v.Path), err)
	}
	if nd.warn != nil {
		slog.Warn("word documents: the copy is in place, but", "file", filepath.Base(nd.path), "err", nd.warn)
	}
	return nd.path, nil
}

// aside is one file or folder of a working folder that reload moves out of the
// way and can put back.
type aside struct {
	name, prev string
	done       bool // the original is out of the way (or there was none)
	moved      bool // ... and it is now under prev
}

// reload replaces the working copy with a fresh conversion of the target. The
// old Markdown stays as doc.prev.md; if the new conversion fails, everything
// goes back as it was, so the user's text never ends up under a name nobody opens.
func (w *wordDocs) reload(ctx context.Context, d *wordDoc) (exportResult, error) {
	meta := d.metaCopy()
	name := filepath.Base(meta.Target)
	asides := []*aside{
		{name: convert.MarkdownName, prev: wordPrevMarkdown},
		{name: convert.MediaDirName, prev: wordPrevMedia},
		{name: wordReferenceName, prev: wordPrevReference},
	}
	fail := func(err error) (exportResult, error) {
		err = fmt.Errorf("reload %q: %w", name, err)
		if rerr := w.restore(d.dir, asides); rerr != nil {
			err = errors.Join(err, fmt.Errorf("restore the working copy: %w", rerr))
		}
		return exportResult{}, err
	}

	for _, a := range asides {
		from, prev := filepath.Join(d.dir, a.name), filepath.Join(d.dir, a.prev)
		// A leftover of an earlier reload; for doc.md the rename replaces it.
		if a.name != convert.MarkdownName {
			if err := os.RemoveAll(prev); err != nil {
				return fail(err)
			}
		}
		err := os.Rename(from, prev)
		switch {
		case err == nil:
			a.done, a.moved = true, true
		case errors.Is(err, fs.ErrNotExist) && a.name == convert.MediaDirName:
			a.done = true // a document without pictures has no media/
		default:
			return fail(err)
		}
	}

	// Before the conversion, as in prepare: a later change is a conflict.
	snap, err := docsafe.TakeSnapshot(ctx, meta.Target)
	if err != nil {
		return fail(err)
	}
	reasons, err := w.convertDocx(ctx, d.dir, meta.Target)
	if err != nil {
		return fail(err)
	}
	more, err := writableReasons(meta.Target)
	if err != nil {
		return fail(err)
	}
	reasons = append(reasons, more...)

	meta.Snapshot, meta.Dirty = snap, false
	if err := w.saveMeta(ctx, d, meta); err != nil {
		return fail(fmt.Errorf("save the working copy state: %w", err))
	}
	d.set(meta, reasons)
	// The old pictures and reference are not the user's text; doc.prev.md stays.
	for _, a := range asides[1:] {
		if err := os.RemoveAll(filepath.Join(d.dir, a.prev)); err != nil {
			slog.Warn("word documents: cannot remove a working file", "file", a.prev, "err", err)
		}
	}
	return exportResult{Outcome: exportReloaded, View: d.view()}, nil
}

// restore undoes reload's moves: what the failed conversion left under the
// original names is removed and the moved files come back.
func (w *wordDocs) restore(dir string, asides []*aside) error {
	var errs []error
	for _, a := range asides {
		if !a.done {
			continue // never touched: the original is still where it was
		}
		from, prev := filepath.Join(dir, a.name), filepath.Join(dir, a.prev)
		if err := os.RemoveAll(from); err != nil {
			errs = append(errs, err)
			continue // renaming over it would fail or mix the two
		}
		if a.moved {
			if err := os.Rename(prev, from); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// renamed records that the user's file got a new name.
func (w *wordDocs) renamed(ctx context.Context, oldPath, newPath string) error {
	d, ok := w.byPath(oldPath)
	if !ok {
		return nil
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	oldPath, newPath = filepath.Clean(oldPath), filepath.Clean(newPath)
	meta := d.metaCopy()
	changed := false
	if samePath(meta.Origin, oldPath) {
		meta.Origin, changed = newPath, true
	}
	if meta.Target != "" && samePath(meta.Target, oldPath) {
		meta.Target, changed = newPath, true
	}
	if !changed {
		return nil
	}
	if err := w.saveMeta(ctx, d, meta); err != nil {
		return fmt.Errorf("rename %q: %w", filepath.Base(oldPath), err)
	}
	d.set(meta, d.reasons())
	return nil
}

// removed forgets a document whose file went to the Trash and discards its working copy.
func (w *wordDocs) removed(ctx context.Context, path string) error {
	d, ok := w.byPath(path)
	if !ok {
		return nil
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	w.mu.Lock()
	w.docs = slices.DeleteFunc(w.docs, func(x *wordDoc) bool { return x == d })
	w.mu.Unlock()
	if err := w.files.DiscardWork(ctx, d.workCopy()); err != nil {
		return fmt.Errorf("remove %q: %w", filepath.Base(path), err)
	}
	return nil
}

// sweep is for the start of the application: it removes the working copies
// that hold nothing unsaved and notes the others.
func (w *wordDocs) sweep(ctx context.Context) error {
	kept, err := w.files.SweepWork(ctx)
	for _, wc := range kept {
		// The name only, never the content.
		slog.Info("word documents: an unsaved working copy is kept for recovery", "document", filepath.Base(wc.Meta.Origin))
	}
	if err != nil {
		return fmt.Errorf("sweep working copies: %w", err)
	}
	return nil
}

// close is for the shutdown: working copies with nothing unsaved go, the
// others stay for the recovery question at the next start.
func (w *wordDocs) close(ctx context.Context) error {
	var errs []error
	for _, d := range w.snapshotDocs() {
		d.opMu.Lock()
		if !d.metaCopy().Dirty {
			w.mu.Lock()
			w.docs = slices.DeleteFunc(w.docs, func(x *wordDoc) bool { return x == d })
			w.mu.Unlock()
			if err := w.files.DiscardWork(ctx, d.workCopy()); err != nil {
				errs = append(errs, fmt.Errorf("close %q: %w", filepath.Base(d.view().Path), err))
			}
		}
		d.opMu.Unlock()
	}
	return errors.Join(errs...)
}
