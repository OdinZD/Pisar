package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"Pisar/internal/config"
	"Pisar/internal/convert"
	"Pisar/internal/docsafe"
)

// The tests here run the editor service with a real Neovim, a real pandoc and
// the real docsafe on the sample documents of testdata/docx. Everything lives
// in the t.TempDir() of newEd; only the dialogs are fake. Test names are short
// because the temp folder name carries them (see the note at the top of
// editorservice_test.go).

// wdParts are the pieces of the Word documents behind one service, built from
// the deps newEd is going to use (so they sit in the test's own folders).
type wdParts struct {
	dlg   *fakeWordDialogs
	conv  *hookConverter
	ops   *docsafe.Ops
	words *wordDocs

	// failMd makes the next conversions Markdown -> .docx fail like pandoc does.
	failMd atomic.Bool
	// openGate, when set before the service is built, holds every .docx -> Markdown
	// conversion until it is closed.
	openGate chan struct{}
	// wrap puts another converter in front of the hooks (set before apply).
	wrap func(wordConverter) wordConverter
}

func (p *wdParts) apply(t *testing.T) func(*editorServiceDeps) {
	t.Helper()
	return func(d *editorServiceDeps) {
		if _, err := exec.LookPath(config.PandocBin); err != nil {
			t.Skip("pandoc is not installed")
		}
		l := d.Limits
		real, err := convert.New(convert.Options{
			PandocBin: d.Store.PandocBin(), TextutilBin: d.Store.TextutilBin(), Timeout: l.ConvertTimeout,
			Limits: convert.InspectLimits{MaxEntries: l.MaxDocxEntries, MaxPartBytes: l.MaxDocxPartBytes},
		})
		if err != nil {
			t.Fatal(err)
		}
		p.dlg = &fakeWordDialogs{}
		p.conv = &hookConverter{wordConverter: real}
		p.conv.beforeMdToDocx = func(int, string) error {
			if p.failMd.Load() {
				return &convert.ToolError{Tool: "pandoc", Err: errors.New("simulated failure")}
			}
			return nil
		}
		p.conv.beforeDocxToMd = func(int, string) error {
			if p.openGate != nil {
				<-p.openGate
			}
			return nil
		}
		p.ops, err = docsafe.New(docsafe.Options{
			TrashBin: config.DefaultTrashBin, Timeout: l.CommandTimeout,
			BackupsDir: d.Store.BackupsDir(), MaxBackups: l.MaxBackups, WorkDir: d.Store.WorkDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		var conv wordConverter = p.conv
		if p.wrap != nil {
			conv = p.wrap(conv)
		}
		p.words, err = newWordDocs(wordDocsDeps{
			Convert: conv,
			Examine: func(ctx context.Context, path string) (convert.Report, error) {
				return convert.Examine(ctx, path, wordLimits())
			},
			Files: p.ops, Dialogs: p.dlg, Text: uiTextFor(t), ExportAttempts: l.ExportAttempts,
		})
		if err != nil {
			t.Fatal(err)
		}
		d.Words = p.words
	}
}

func uiTextFor(t *testing.T) uiText {
	t.Helper()
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// wdEnv is a started service with Word documents and a project to hold them.
type wdEnv struct {
	*edEnv
	*wdParts
	proj config.Project
	dir  string // the project folder: the user's folder
}

func startWd(t *testing.T) *wdEnv {
	t.Helper()
	parts := &wdParts{}
	e := startEd(t, parts.apply(t))
	p, dir := e.addProject("proj")
	return &wdEnv{edEnv: e, wdParts: parts, proj: p, dir: dir}
}

func (w *wdEnv) path(name string) string { return filepath.Join(w.dir, name) }

// open copies a sample document into the project and opens it in Neovim.
func (w *wdEnv) open(name string) DocumentActive {
	w.t.Helper()
	fixture(w.t, name, w.dir)
	return w.openExisting(name)
}

func (w *wdEnv) openExisting(name string) DocumentActive {
	w.t.Helper()
	from := w.rec.mark()
	if err := w.svc.OpenDocument(context.Background(), w.proj.ID, name); err != nil {
		w.t.Fatalf("OpenDocument(%s): %v", name, err)
	}
	return w.rec.waitActive(w.t, from, name, func(d DocumentActive) bool { return d.Name == name })
}

// typeText appends a paragraph by typing, and waits until the buffer is modified.
func (w *wdEnv) typeText(name, text string) {
	w.t.Helper()
	from := w.rec.mark()
	if err := w.svc.Write("Go\x1bo" + text + "\x1b"); err != nil {
		w.t.Fatal(err)
	}
	w.rec.waitState(w.t, from, w.path(name), true)
}

func (w *wdEnv) save() {
	w.t.Helper()
	if err := w.svc.Save(context.Background()); err != nil {
		w.t.Fatalf("Save: %v", err)
	}
}

func (r *recorder) waitExport(t *testing.T, from int, phase string) DocumentExport {
	t.Helper()
	ev := r.wait(t, from, "document:export "+phase, func(ev recorded) bool {
		d, ok := ev.data.(DocumentExport)
		return ev.name == eventDocumentExport && ok && d.Phase == phase
	})
	return ev.data.(DocumentExport)
}

// exports lists the document:export events since from, in order.
func (r *recorder) exports(from int) []DocumentExport {
	var out []DocumentExport
	for _, ev := range r.since(from) {
		if d, ok := ev.data.(DocumentExport); ev.name == eventDocumentExport && ok {
			out = append(out, d)
		}
	}
	return out
}

func phases(exports []DocumentExport) []string {
	var out []string
	for _, x := range exports {
		out = append(out, x.Phase)
	}
	return out
}

// waitCond polls until cond holds.
func waitCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(edWait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// docxHas reports whether the .docx at path has text in it.
func docxHas(t *testing.T, path, text string) bool {
	t.Helper()
	return strings.Contains(docxText(t, path), text)
}

// bufferText is what Neovim's current buffer holds.
func (e *edEnv) bufferText() string { return strings.Join(e.eval(`getline(1, '$')`), "\n") }

func (w *wdEnv) workFolders() []string {
	w.t.Helper()
	entries, err := os.ReadDir(w.store.WorkDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		w.t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		out = append(out, filepath.Join(w.store.WorkDir(), en.Name()))
	}
	return out
}

func (w *wdEnv) backupCount() int {
	w.t.Helper()
	n := 0
	err := filepath.WalkDir(w.store.BackupsDir(), func(_ string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

// An .docx opens through its working copy: the frontend learns the user's file,
// Neovim holds doc.md.
func TestWdOpen(t *testing.T) {
	w := startWd(t)
	a := w.open("basic.docx")
	if !sameFile(a.Path, w.path("basic.docx")) || a.Name != "basic.docx" || a.Rel != "basic.docx" || a.ProjectID != w.proj.ID {
		t.Errorf("document:active = %+v", a)
	}
	if a.ReadOnly == nil || len(a.ReadOnly) != 0 || a.Imported || a.Unexported {
		t.Errorf("flags = %+v, want a writable document that is not imported and not unexported", a)
	}
	// An empty array in the JSON, not null: the frontend reads .length.
	if b, err := json.Marshal(a); err != nil || !bytes.Contains(b, []byte(`"readOnly":[]`)) {
		t.Errorf("JSON = %s, %v", b, err)
	}
	cur, err := w.svc.session.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cur.Path) != convert.MarkdownName || !strings.HasPrefix(realPath(cur.Path), realPath(w.store.WorkDir())+string(filepath.Separator)) {
		t.Errorf("the buffer is %q, want doc.md in the working folder", cur.Path)
	}
	// The user's file is not touched by opening.
	sameBytes(t, w.path("basic.docx"), readFile(t, filepath.Join("testdata", "docx", "basic.docx")))
}

// The formatting a save would simplify travels in document:active as an array of
// codes, empty (never null) when there is none, and is empty again once the
// document has been saved.
func TestWdSimplified(t *testing.T) {
	w := startWd(t)

	a := w.open("basic.docx")
	if a.Simplified == nil || len(a.Simplified) != 0 {
		t.Errorf("basic.docx: simplified = %#v, want an empty array", a.Simplified)
	}
	if b, err := json.Marshal(a); err != nil || !bytes.Contains(b, []byte(`"simplified":[]`)) {
		t.Errorf("basic.docx: JSON = %s, %v", b, err)
	}

	a = w.open("chart.docx")
	if !slices.Equal(a.ReadOnly, []string{"embedded-objects"}) || a.Simplified == nil || len(a.Simplified) != 0 {
		t.Errorf("chart.docx: readOnly = %v, simplified = %#v", a.ReadOnly, a.Simplified)
	}

	a = w.open("formatting.docx")
	if len(a.ReadOnly) != 0 || !slices.Equal(a.Simplified, allSimplified) {
		t.Fatalf("formatting.docx: readOnly = %v, simplified = %v", a.ReadOnly, a.Simplified)
	}
	want := `"simplified":["alignment","text-color","font","spacing","indentation","page-breaks","tabs","fields","table-formatting","drawings"]`
	if b, err := json.Marshal(a); err != nil || !bytes.Contains(b, []byte(want)) {
		t.Errorf("formatting.docx: JSON = %s, %v", b, err)
	}

	path := w.path("formatting.docx")
	d, ok := w.svc.words.byPath(path)
	if !ok {
		t.Fatal("formatting.docx is not open")
	}
	w.typeText("formatting.docx", newText)
	// The edit alone changes nothing: the file still has the formatting.
	if got := w.svc.describe(d.markdownPath()).Simplified; !slices.Equal(got, allSimplified) {
		t.Errorf("simplified = %v after an edit that was not saved", got)
	}
	from := w.rec.mark()
	w.save()
	w.rec.waitExport(t, from, phaseSaved)
	got := w.svc.describe(d.markdownPath()).Simplified
	if got == nil || len(got) != 0 {
		t.Errorf("simplified = %#v after the save, want an empty array", got)
	}
}

// A write of doc.md is followed by an export: converting, then saved, and the
// user's .docx has the new text and the picture it had.
func TestWdSave(t *testing.T) {
	w := startWd(t)
	pictures := mediaHashes(t, filepath.Join("testdata", "docx", "basic.docx"))
	w.open("basic.docx")
	path := w.path("basic.docx")

	w.typeText("basic.docx", newText)
	from := w.rec.mark()
	w.save()
	w.rec.waitExport(t, from, phaseSaved)
	got := w.rec.exports(from)
	if len(got) != 2 || got[0].Phase != phaseConverting || got[1].Phase != phaseSaved {
		t.Fatalf("phases = %v, want converting, saved", phases(got))
	}
	for _, x := range got {
		if !sameFile(x.Path, path) || x.Code != "" {
			t.Errorf("event %+v", x)
		}
	}
	if !docxHas(t, path, newText) {
		t.Error("the .docx lacks the new text")
	}
	if !slices.Equal(mediaHashes(t, path), pictures) {
		t.Error("the pictures of the document changed")
	}
	if w.backupCount() != 1 {
		t.Errorf("%d backups, want 1", w.backupCount())
	}
	// The state events carry the user's file, also the one that follows the export.
	savedAt := -1
	for i, ev := range w.rec.since(from) {
		if d, ok := ev.data.(DocumentExport); ev.name == eventDocumentExport && ok && d.Phase == phaseSaved {
			savedAt = i
		}
	}
	waitCond(t, "document:state after the export", func() bool {
		evs := w.rec.since(from)
		for _, ev := range evs[savedAt+1:] {
			if d, ok := ev.data.(DocumentState); ev.name == eventDocumentState && ok && !d.Modified && sameFile(d.Path, path) {
				return true
			}
		}
		return false
	})
	for _, ev := range w.rec.since(0) {
		if d, ok := ev.data.(DocumentState); ev.name == eventDocumentState && ok && filepath.Base(d.Path) == convert.MarkdownName {
			t.Errorf("a state event carries the working copy: %+v", d)
		}
	}
	if unsaved, err := w.svc.unsaved(context.Background(), path); err != nil || unsaved {
		t.Errorf("unsaved after the export = %v, %v", unsaved, err)
	}
}

// A document with comments is read-only: nothing in the buffer changes, Save
// is refused without asking Neovim, :w! does nothing to the user's file.
func TestWdReadOnly(t *testing.T) {
	w := startWd(t)
	a := w.open("comments.docx")
	if !slices.Equal(a.ReadOnly, []string{"comments"}) {
		t.Fatalf("readOnly = %v", a.ReadOnly)
	}
	original := readFile(t, w.path("comments.docx"))

	// Typing changes nothing, and the buffer says it cannot be changed.
	// (Keys that are commands in normal mode, each of them refused.)
	before := w.eval(`[join(getline(1, '$'), ' ')]`)[0]
	if err := w.svc.Write("Gox\x1b"); err != nil {
		t.Fatal(err)
	}
	got := w.eval(`[&modifiable, &readonly, &modified, join(getline(1, '$'), ' ')]`)
	if got[0] != "0" || got[1] != "1" || got[2] != "0" || got[3] != before {
		t.Errorf("modifiable/readonly/modified/text = %v", got)
	}

	from := w.rec.mark()
	err := w.svc.Save(context.Background())
	wantCode(t, err, codeReadOnly)
	// :w! writes the unchanged working copy; the export must not follow.
	if err := w.svc.Write(":w!\r"); err != nil {
		t.Fatal(err)
	}
	w.eval(`[1]`) // the keys before it were handled
	time.Sleep(300 * time.Millisecond)
	if x := w.rec.exports(from); len(x) != 0 {
		t.Errorf("a read-only document was exported: %v", x)
	}
	sameBytes(t, w.path("comments.docx"), original)
	if w.backupCount() != 0 {
		t.Error("a backup was made for a document that was not written")
	}
}

// An imported .doc: the first save makes Name.docx next to it, the .doc stays,
// and the frontend gets the document:active of the new file.
func TestWdLegacy(t *testing.T) {
	w := startWd(t)
	if _, err := os.Stat(config.DefaultTextutilBin); err != nil {
		t.Skip("textutil is not available")
	}
	a := w.open("legacy.doc")
	if !a.Imported || !sameFile(a.Path, w.path("legacy.doc")) || a.Name != "legacy.doc" {
		t.Fatalf("document:active = %+v", a)
	}
	original := readFile(t, w.path("legacy.doc"))
	w.typeText("legacy.doc", newText)

	from := w.rec.mark()
	w.save()
	saved := w.rec.waitExport(t, from, phaseSaved)
	if !sameFile(saved.Path, w.path("legacy.docx")) {
		t.Errorf("saved event path = %q, want legacy.docx", saved.Path)
	}
	b := w.rec.waitActive(t, from, "legacy.docx", func(d DocumentActive) bool { return d.Name == "legacy.docx" })
	if !sameFile(b.Path, w.path("legacy.docx")) || !b.Imported || b.Rel != "legacy.docx" {
		t.Errorf("document:active after the first save = %+v", b)
	}
	if !docxHas(t, w.path("legacy.docx"), newText) {
		t.Error("the new .docx lacks the text")
	}
	sameBytes(t, w.path("legacy.doc"), original)
	if w.backupCount() != 0 {
		t.Error("a backup of a file that was never overwritten")
	}
	// The next save goes to the new file.
	w.typeText("legacy.docx", newText+"2")
	from = w.rec.mark()
	w.save()
	w.rec.waitExport(t, from, phaseSaved)
	if !docxHas(t, w.path("legacy.docx"), newText+"2") {
		t.Error("the second save did not reach legacy.docx")
	}
	sameBytes(t, w.path("legacy.doc"), original)
}

// The file changed outside Pisar: Cancel leaves it alone and the document stays
// unsaved; Reload puts the outside text into the buffer.
func TestWdConflict(t *testing.T) {
	w := startWd(t)
	w.open("basic.docx")
	path := w.path("basic.docx")
	outside := readFile(t, filepath.Join("testdata", "docx", "header-image.docx"))
	if err := os.WriteFile(path, outside, 0o644); err != nil {
		t.Fatal(err)
	}
	w.typeText("basic.docx", newText)

	w.dlg.conflicts = []conflictChoice{conflictCancel}
	from := w.rec.mark()
	w.save()
	w.rec.waitExport(t, from, phaseCanceled)
	if got := phases(w.rec.exports(from)); !slices.Equal(got, []string{phaseConverting, phaseCanceled}) {
		t.Errorf("phases = %v", got)
	}
	sameBytes(t, path, outside)
	if unsaved, err := w.svc.unsaved(context.Background(), path); err != nil || !unsaved {
		t.Errorf("unsaved after Cancel = %v, %v; the text is only in the working copy", unsaved, err)
	}

	w.dlg.conflicts = []conflictChoice{conflictReload}
	from = w.rec.mark()
	w.save()
	w.rec.waitExport(t, from, phaseReloaded)
	sameBytes(t, path, outside)
	if text := w.bufferText(); !strings.Contains(text, "Dokument sa slikom u zaglavlju") || strings.Contains(text, newText) {
		t.Errorf("the buffer after Reload:\n%s", text)
	}
	// The events: active again, then reloaded.
	w.rec.waitActive(t, from, "after the reload", func(d DocumentActive) bool { return sameFile(d.Path, path) && !d.Unexported })
	if unsaved, err := w.svc.unsaved(context.Background(), path); err != nil || unsaved {
		t.Errorf("unsaved after Reload = %v, %v", unsaved, err)
	}
	// Nothing was overwritten, so the file is not backed up; the only copy is
	// the Markdown of the user's text, which has to outlive the working folder.
	copies, err := filepath.Glob(filepath.Join(w.store.BackupsDir(), "*", "*-basic.docx.md"))
	if err != nil {
		t.Fatal(err)
	}
	if n := w.backupCount(); n != 1 || len(copies) != 1 {
		t.Errorf("backups = %d, of them Markdown copies of the text = %d; want just that one copy", n, len(copies))
	}
}

// The quit dialog: the Word document is listed under the user's file, and
// saveAll returns when the .docx has been written.
func TestWdQuitSave(t *testing.T) {
	w := startWd(t)
	w.open("basic.docx")
	path := w.path("basic.docx")
	w.typeText("basic.docx", newText)

	bufs, err := w.svc.modified(context.Background())
	if err != nil || len(bufs) != 1 || !sameFile(bufs[0].Path, path) || !bufs[0].Modified {
		t.Fatalf("modified = %+v, %v; want the .docx", bufs, err)
	}
	from := w.rec.mark()
	if err := w.svc.saveAll(context.Background()); err != nil {
		t.Fatalf("saveAll: %v", err)
	}
	if !docxHas(t, path, newText) {
		t.Fatal("saveAll returned before the .docx was written")
	}
	// The notification of the write SaveAll made arrives later and must not
	// start a second export.
	time.Sleep(500 * time.Millisecond)
	if n := slices.Index(phases(w.rec.exports(from)), phaseConverting); n < 0 || slices.Index(phases(w.rec.exports(from))[n+1:], phaseConverting) >= 0 {
		t.Errorf("phases = %v, want a single export", phases(w.rec.exports(from)))
	}
	if bufs, err := w.svc.modified(context.Background()); err != nil || len(bufs) != 0 {
		t.Errorf("modified after saveAll = %+v, %v", bufs, err)
	}
}

// A conversion that fails: saveAll fails with the code, the file is untouched,
// and the document stays unsaved even though its buffer is no longer modified.
func TestWdQuitFail(t *testing.T) {
	w := startWd(t)
	w.open("basic.docx")
	path := w.path("basic.docx")
	original := readFile(t, path)

	w.failMd.Store(true)
	w.typeText("basic.docx", newText)
	from := w.rec.mark()
	w.save() // writes doc.md; the export that follows fails
	failed := w.rec.waitExport(t, from, phaseFailed)
	if failed.Code != codeConvertFailed || !sameFile(failed.Path, path) {
		t.Errorf("failed event = %+v", failed)
	}
	sameBytes(t, path, original)

	// The buffer is saved, the text is not exported: still unsaved.
	bufs, err := w.svc.modified(context.Background())
	if err != nil || len(bufs) != 1 || !sameFile(bufs[0].Path, path) {
		t.Fatalf("modified = %+v, %v; want the .docx although its buffer is not modified", bufs, err)
	}
	if unsaved, err := w.svc.unsaved(context.Background(), path); err != nil || !unsaved {
		t.Errorf("unsaved = %v, %v", unsaved, err)
	}
	err = w.svc.saveAll(context.Background())
	wantCode(t, err, codeConvertFailed)
	sameBytes(t, path, original)

	// Once the conversion works again the same state exports.
	w.failMd.Store(false)
	if err := w.svc.saveAll(context.Background()); err != nil {
		t.Fatalf("saveAll: %v", err)
	}
	if !docxHas(t, path, newText) {
		t.Error("the .docx lacks the text after the second saveAll")
	}
	if unsaved, err := w.svc.unsaved(context.Background(), path); err != nil || unsaved {
		t.Errorf("unsaved after the export = %v, %v", unsaved, err)
	}
}

// Cancel in the conflict question: saveAll reports errSaveDeclined, which the
// quit gate does not show as an error.
func TestWdQuitDeclined(t *testing.T) {
	for _, c := range []struct {
		name   string
		choice conflictChoice
	}{{"cancel", conflictCancel}, {"reload", conflictReload}} {
		t.Run(c.name, func(t *testing.T) {
			w := startWd(t)
			w.open("basic.docx")
			path := w.path("basic.docx")
			outside := readFile(t, filepath.Join("testdata", "docx", "header-image.docx"))
			if err := os.WriteFile(path, outside, 0o644); err != nil {
				t.Fatal(err)
			}
			w.typeText("basic.docx", newText)
			w.dlg.conflicts = []conflictChoice{c.choice}
			err := w.svc.saveAll(context.Background())
			if !errors.Is(err, errSaveDeclined) {
				t.Fatalf("saveAll = %v, want errSaveDeclined", err)
			}
			wantCode(t, err, codeCanceled)
			sameBytes(t, path, outside)
		})
	}
}

// The wait for the exports is not limited by the caller's context: a question
// to the user may take longer than the 10 seconds of the quit gate.
func TestWdQuitWaits(t *testing.T) {
	w := startWd(t)
	w.open("basic.docx")
	path := w.path("basic.docx")
	if err := os.WriteFile(path, readFile(t, filepath.Join("testdata", "docx", "header-image.docx")), 0o644); err != nil {
		t.Fatal(err)
	}
	w.typeText("basic.docx", newText)
	w.dlg.conflicts = []conflictChoice{conflictOverwrite}
	w.dlg.during = func() { time.Sleep(600 * time.Millisecond) }

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := w.svc.saveAll(ctx); err != nil {
		t.Fatalf("saveAll: %v", err)
	}
	if !docxHas(t, path, newText) {
		t.Error("saveAll returned before the user's answer was acted on")
	}
}

// unsaved, renamed and removed are what ProjectService asks.
func TestWdProject(t *testing.T) {
	w := startWd(t)
	ctx := context.Background()
	w.open("basic.docx")
	oldPath := w.path("basic.docx")

	if unsaved, err := w.svc.unsaved(ctx, oldPath); err != nil || unsaved {
		t.Fatalf("unsaved right after the open = %v, %v", unsaved, err)
	}
	w.typeText("basic.docx", newText)
	if unsaved, err := w.svc.unsaved(ctx, oldPath); err != nil || !unsaved {
		t.Errorf("unsaved with a modified buffer = %v, %v", unsaved, err)
	}
	w.save()
	waitCond(t, "the export", func() bool { return docxHas(t, oldPath, newText) })
	waitCond(t, "unsaved to clear", func() bool {
		u, err := w.svc.unsaved(ctx, oldPath)
		return err == nil && !u
	})

	// A rename: the buffer stays doc.md, the frontend learns the new name, and
	// the next export goes to the new file.
	newPath := w.path("renamed.docx")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	from := w.rec.mark()
	if err := w.svc.renamed(ctx, oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	a := w.rec.waitActive(t, from, "renamed.docx", func(d DocumentActive) bool { return d.Name == "renamed.docx" })
	if !sameFile(a.Path, newPath) || a.Rel != "renamed.docx" {
		t.Errorf("document:active after the rename = %+v", a)
	}
	cur, err := w.svc.session.Current(ctx)
	if err != nil || filepath.Base(cur.Path) != convert.MarkdownName {
		t.Errorf("the buffer after the rename = %+v, %v; it must stay doc.md", cur, err)
	}
	w.typeText("renamed.docx", newText+"2")
	from = w.rec.mark()
	w.save()
	w.rec.waitExport(t, from, phaseSaved)
	if !docxHas(t, newPath, newText+"2") {
		t.Error("the export did not go to the new name")
	}
	if exists(oldPath) {
		t.Error("the old file came back")
	}
	if unsaved, err := w.svc.unsaved(ctx, newPath); err != nil || unsaved {
		t.Errorf("unsaved under the new name = %v, %v", unsaved, err)
	}

	// The Trash: the buffer goes, the working copy goes, nothing is left open.
	if err := os.Remove(newPath); err != nil {
		t.Fatal(err)
	}
	from = w.rec.mark()
	if err := w.svc.removed(ctx, newPath); err != nil {
		t.Fatal(err)
	}
	w.rec.waitActive(t, from, "empty", func(d DocumentActive) bool { return d.Path == "" })
	if len(w.words.all()) != 0 || len(w.workFolders()) != 0 {
		t.Errorf("open documents %d, working folders %v", len(w.words.all()), w.workFolders())
	}
}

// :wq writes doc.md and quits: the export that was queued still runs, then the
// working copy is closed.
func TestWdWQ(t *testing.T) {
	w := startWd(t)
	w.open("basic.docx")
	path := w.path("basic.docx")
	w.typeText("basic.docx", newText)

	from := w.rec.mark()
	if err := w.svc.Write(":wq\r"); err != nil {
		t.Fatal(err)
	}
	w.rec.waitName(t, from, eventNvimExit)
	waitCond(t, "the .docx to be written", func() bool { return docxHas(t, path, newText) })
	waitCond(t, "the documents to be closed", func() bool { return len(w.words.all()) == 0 && len(w.workFolders()) == 0 })
	if got := phases(w.rec.exports(from)); !slices.Equal(got, []string{phaseConverting, phaseSaved}) {
		t.Errorf("phases = %v", got)
	}
}

// :q! throws the changes away: the file is untouched and the clean working copy goes.
func TestWdQuit(t *testing.T) {
	w := startWd(t)
	w.open("basic.docx")
	path := w.path("basic.docx")
	original := readFile(t, path)
	w.typeText("basic.docx", newText)

	from := w.rec.mark()
	if err := w.svc.Write(":q!\r"); err != nil {
		t.Fatal(err)
	}
	w.rec.waitName(t, from, eventNvimExit)
	waitCond(t, "the documents to be closed", func() bool { return len(w.words.all()) == 0 && len(w.workFolders()) == 0 })
	sameBytes(t, path, original)
	if x := w.rec.exports(from); len(x) != 0 {
		t.Errorf("exports = %v", x)
	}
	if w.backupCount() != 0 {
		t.Error("a backup of a file that was not written")
	}
}

// The document on the command line: Start returns before the conversion ends,
// the document opens after it.
func TestWdInitial(t *testing.T) {
	needNvim(t)
	parts := &wdParts{openGate: make(chan struct{})}
	var file string
	e := newEd(t, parts.apply(t), func(d *editorServiceDeps) {
		d.InitialFile = filepath.Join(filepath.Dir(d.Store.Home()), "p", "start.docx")
		file = d.InitialFile
		fixture(t, "basic.docx", filepath.Dir(file))
		if err := os.Rename(filepath.Join(filepath.Dir(file), "basic.docx"), file); err != nil {
			t.Fatal(err)
		}
	})
	if !e.svc.HasInitialDocument() {
		t.Fatal("a .docx on the command line was refused")
	}
	started := make(chan error, 1)
	go func() { started <- e.svc.Start(context.Background(), 100, 30, sample(t)) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(5 * time.Second):
		close(parts.openGate) // let the worker go before the test ends
		t.Fatal("Start waited for the conversion")
	}
	if e.svc.HasInitialDocument() {
		t.Error("HasInitialDocument after Start")
	}
	time.Sleep(200 * time.Millisecond)
	for _, ev := range e.rec.since(0) {
		if d, ok := ev.data.(DocumentActive); ev.name == eventDocumentActive && ok && d.Name == "start.docx" {
			t.Errorf("the document opened while its conversion is held: %+v", d)
		}
	}
	close(parts.openGate)
	a := e.rec.waitActive(t, 0, "start.docx", func(d DocumentActive) bool { return d.Name == "start.docx" })
	if !sameFile(a.Path, file) || len(a.ReadOnly) != 0 {
		t.Errorf("document:active = %+v", a)
	}
	waitCond(t, "the open task to end", e.svc.queue.idle)
}

// A document on the command line that cannot be opened is reported with its code.
func TestWdInitialFail(t *testing.T) {
	needNvim(t)
	parts := &wdParts{}
	e := newEd(t, parts.apply(t), func(d *editorServiceDeps) {
		d.InitialFile = filepath.Join(filepath.Dir(d.Store.Home()), "p", "bad.docx")
		write(t, d.InitialFile, "this is not a zip")
	})
	if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	x := e.rec.waitExport(t, 0, phaseFailed)
	if x.Code != codeNotWord || !sameFile(x.Path, filepath.Join(e.projects, "bad.docx")) {
		t.Errorf("event = %+v", x)
	}
}

// What the command line may name: a .docx or .doc that exists as a regular
// file, not a link; without Word support still nothing but .md and .txt.
func TestWdInitialRefusal(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, file string)
		words bool
		want  bool
	}{
		{"docx", func(t *testing.T, f string) { write(t, f, "x") }, true, true},
		{"missing docx", func(*testing.T, string) {}, true, false},
		{"folder named docx", func(t *testing.T, f string) {
			if err := os.Mkdir(f, 0o755); err != nil {
				t.Fatal(err)
			}
		}, true, false},
		{"link to a docx", func(t *testing.T, f string) {
			target := f + ".real"
			write(t, target, "x")
			if err := os.Symlink(target, f); err != nil {
				t.Fatal(err)
			}
		}, true, false},
		{"docx without Word support", func(t *testing.T, f string) { write(t, f, "x") }, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parts := &wdParts{}
			mutate := []func(*editorServiceDeps){func(d *editorServiceDeps) {
				d.InitialFile = filepath.Join(filepath.Dir(d.Store.Home()), "p", "x.docx")
				c.setup(t, d.InitialFile)
			}}
			if c.words {
				mutate = append([]func(*editorServiceDeps){parts.apply(t)}, mutate...)
			}
			e := newEd(t, mutate...)
			if got := e.svc.HasInitialDocument(); got != c.want {
				t.Errorf("HasInitialDocument = %v, want %v", got, c.want)
			}
		})
	}
}

// Save as a copy of a read-only document: a new file next to the original,
// which stays as it was; the copy becomes the active document and may be edited.
func TestWdSaveCopy(t *testing.T) {
	w := startWd(t)
	ctx := context.Background()
	w.open("comments.docx")
	original := readFile(t, w.path("comments.docx"))

	// Closing the dialog is not an error and changes nothing.
	from := w.rec.mark()
	if err := w.svc.SaveCopy(ctx); err != nil {
		t.Fatalf("SaveCopy cancelled: %v", err)
	}
	if got := w.rec.since(from); len(got) != 0 {
		t.Errorf("events after a cancelled SaveCopy: %v", got)
	}
	if _, _, n := w.dlg.counts(); n != 1 {
		t.Fatalf("%d save dialogs, want 1", n)
	}

	copyPath := w.path("kopija.docx")
	w.dlg.paths = []string{copyPath}
	from = w.rec.mark()
	if err := w.svc.SaveCopy(ctx); err != nil {
		t.Fatalf("SaveCopy: %v", err)
	}
	if !docxHas(t, copyPath, "Dokument s komentarom") {
		t.Error("the copy lacks the text")
	}
	sameBytes(t, w.path("comments.docx"), original)
	a := w.rec.waitActive(t, from, "kopija.docx", func(d DocumentActive) bool { return d.Name == "kopija.docx" })
	if !sameFile(a.Path, copyPath) || len(a.ReadOnly) != 0 {
		t.Errorf("document:active = %+v", a)
	}
	// The copy is an ordinary document: it can be edited and saved.
	w.typeText("kopija.docx", newText)
	from = w.rec.mark()
	w.save()
	w.rec.waitExport(t, from, phaseSaved)
	if !docxHas(t, copyPath, newText) {
		t.Error("the copy was not saved")
	}
	sameBytes(t, w.path("comments.docx"), original)

	// Not a Word document: refused.
	p, dir := w.addProject("md")
	write(t, filepath.Join(dir, "a.md"), "# a\n")
	from = w.rec.mark()
	if err := w.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	w.rec.waitActive(t, from, "a.md", func(d DocumentActive) bool { return d.Name == "a.md" })
	wantCode(t, w.svc.SaveCopy(ctx), codeNotDocument)
}

// Without Word support SaveCopy refuses, and .docx stays unsupported.
func TestWdOff(t *testing.T) {
	e := startEd(t)
	p, _ := docFixture(t, e)
	ctx := context.Background()
	wantCode(t, e.svc.OpenDocument(ctx, p.ID, "d.docx"), codeUnsupportedKind)
	wantCode(t, e.svc.SaveCopy(ctx), codeNotDocument)
}

// The shutdown during an export does not hang, emits nothing after it returns,
// stops the worker, and keeps the working copy that holds the unsaved text.
func TestWdShutdown(t *testing.T) {
	stuck := &stuckConv{started: make(chan struct{}, 1)}
	parts := &wdParts{wrap: func(c wordConverter) wordConverter { stuck.wordConverter = c; return stuck }}
	e := startEd(t, parts.apply(t))
	p, dir := e.addProject("proj")
	w := &wdEnv{edEnv: e, wdParts: parts, proj: p, dir: dir}
	w.open("basic.docx")
	path := w.path("basic.docx")
	original := readFile(t, path)
	w.typeText("basic.docx", newText)
	w.save()
	select {
	case <-stuck.started:
	case <-time.After(edWait):
		t.Fatal("the export never reached the converter")
	}

	done := make(chan struct{})
	go func() {
		e.svc.shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(edWait):
		t.Fatal("shutdown hangs during an export")
	}
	at := e.rec.mark()
	time.Sleep(200 * time.Millisecond)
	if got := e.rec.since(at); len(got) != 0 {
		t.Errorf("events after shutdown: %v", got)
	}
	ended := make(chan struct{})
	go func() {
		e.svc.queue.wg.Wait()
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(edWait):
		t.Error("the worker goroutine did not end")
	}
	sameBytes(t, path, original)
	wc, found, err := parts.ops.FindWork(context.Background(), path)
	if err != nil || !found || !wc.Meta.Dirty {
		t.Errorf("working copy = found %v, dirty %v, %v; it must stay, with its text", found, wc.Meta.Dirty, err)
	}
}

// stuckConv holds the export in the converter until its context ends.
type stuckConv struct {
	wordConverter
	started chan struct{}
}

func (s *stuckConv) MarkdownToDocx(ctx context.Context, _, _, _ string) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

// A text that is written and queued for export when the application shuts down
// before the worker got to it is still marked: the next start offers it back.
func TestWdShutdownQueued(t *testing.T) {
	stuck := &stuckConv{started: make(chan struct{}, 1)}
	parts := &wdParts{wrap: func(c wordConverter) wordConverter { stuck.wordConverter = c; return stuck }}
	e := startEd(t, parts.apply(t))
	p, dir := e.addProject("proj")
	w := &wdEnv{edEnv: e, wdParts: parts, proj: p, dir: dir}
	w.open("basic.docx")
	fixture(t, "header-image.docx", w.dir)
	w.openExisting("header-image.docx")
	second := w.path("header-image.docx")
	w.typeText("header-image.docx", newText)
	w.save()
	<-stuck.started // the export of header-image.docx holds the worker
	// A write of the first document: it waits behind the stuck one.
	if err := e.svc.Write(":b#\r"); err != nil {
		t.Fatal(err)
	}
	w.rec.waitActive(t, 0, "basic.docx again", func(d DocumentActive) bool { return d.Name == "basic.docx" })
	w.typeText("basic.docx", newText)
	w.save()
	first := w.path("basic.docx")
	waitCond(t, "the second export to be queued", func() bool {
		d, ok := parts.words.byPath(first)
		return ok && e.svc.queue.busy(d)
	})

	e.svc.shutdown()
	for _, p := range []string{first, second} {
		wc, found, err := parts.ops.FindWork(context.Background(), p)
		if err != nil || !found || !wc.Meta.Dirty {
			t.Errorf("%s: working copy found %v, dirty %v, %v", filepath.Base(p), found, wc.Meta.Dirty, err)
		}
	}
}

// --- the queue on its own ---

type queueProbe struct {
	release chan struct{}
	started chan *workTask
	ran     chan *workTask
}

func newProbe() *queueProbe {
	return &queueProbe{release: make(chan struct{}), started: make(chan *workTask, 16), ran: make(chan *workTask, 16)}
}

func (p *queueProbe) exec(ctx context.Context, t *workTask) error {
	p.started <- t
	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	p.ran <- t
	return nil
}

func exportTask(d *wordDoc, explicit bool) *workTask {
	t := newWorkTask(workExport)
	t.doc, t.explicit = d, explicit
	return t
}

func recv(t *testing.T, ch <-chan *workTask) *workTask {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(edWait):
		t.Fatal("timeout waiting for the queue")
	}
	return nil
}

// Exports of one document that have not started are one export; the one that
// runs is not merged into (it may have read doc.md before the latest write).
func TestWorkQueueCoalesce(t *testing.T) {
	probe := newProbe()
	q := newWorkQueue(probe.exec)
	t.Cleanup(func() { q.stop(edWait) })
	a, b := &wordDoc{}, &wordDoc{}

	first := q.add(exportTask(a, false))
	recv(t, probe.started) // running, so no longer mergeable
	second := q.add(exportTask(a, false))
	third := q.add(exportTask(a, true))
	other := q.add(exportTask(b, false))
	if second != third {
		t.Error("two waiting exports of one document were not merged")
	}
	if first == second || other == second {
		t.Error("a running export or another document's export was merged into")
	}
	if !second.explicit {
		t.Error("the merged task lost the explicit flag")
	}
	if !q.busy(a) || !q.busy(b) || q.idle() {
		t.Error("busy/idle are wrong while tasks wait")
	}

	close(probe.release)
	for _, want := range []*workTask{first, second, other} {
		<-want.done
	}
	if q.busy(a) || q.busy(b) || !q.idle() {
		t.Error("the queue is not idle after all tasks ran")
	}
	if got := len(probe.ran); got != 3 {
		t.Errorf("%d tasks ran, want 3 (a, a, b)", got)
	}
}

// Tasks run one at a time, in the order they were queued.
func TestWorkQueueOrder(t *testing.T) {
	var order []string
	done := make(chan struct{})
	q := newWorkQueue(func(_ context.Context, t *workTask) error {
		order = append(order, t.path)
		if t.path == "last" {
			close(done)
		}
		return nil
	})
	t.Cleanup(func() { q.stop(edWait) })
	for _, p := range []string{"one", "two", "three", "last"} {
		task := newWorkTask(workOpen)
		task.path = p
		q.add(task)
	}
	<-done
	if !slices.Equal(order, []string{"one", "two", "three", "last"}) {
		t.Errorf("order = %v", order)
	}
}

// stop cancels the task that runs, drops the ones that wait and reports the
// exports among them; tasks added afterwards finish at once.
func TestWorkQueueStop(t *testing.T) {
	probe := newProbe()
	q := newWorkQueue(probe.exec)
	a, b := &wordDoc{}, &wordDoc{}
	running := q.add(exportTask(a, false))
	recv(t, probe.started)
	waiting := q.add(exportTask(b, false))
	open := newWorkTask(workOpen)
	q.add(open)

	unfinished, ok := q.stop(edWait)
	if !ok {
		t.Fatal("stop did not wait for the goroutine")
	}
	if len(unfinished) != 2 || !slices.Contains(unfinished, running) || !slices.Contains(unfinished, waiting) {
		t.Errorf("unfinished = %d tasks, want the running and the waiting export", len(unfinished))
	}
	for _, task := range []*workTask{running, waiting, open} {
		select {
		case <-task.done:
		default:
			t.Error("a task is not finished after stop")
		}
	}
	if !errors.Is(running.err, context.Canceled) || !errors.Is(waiting.err, errWorkStopped) {
		t.Errorf("errors: running %v, waiting %v", running.err, waiting.err)
	}
	late := q.add(exportTask(a, false))
	select {
	case <-late.done:
	default:
		t.Error("a task added after stop was not finished")
	}
	if !errors.Is(late.err, errWorkStopped) {
		t.Errorf("late.err = %v", late.err)
	}
	if !q.idle() {
		t.Error("the stopped queue is not idle")
	}
}

// A task that never ends (a dialog) does not hold stop up for ever.
func TestWorkQueueStopStuck(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	q := newWorkQueue(func(context.Context, *workTask) error {
		close(started)
		<-release
		return nil
	})
	q.add(newWorkTask(workOpen))
	<-started
	if _, ok := q.stop(50 * time.Millisecond); ok {
		t.Error("stop reported that a task that is stuck had ended")
	}
	close(release)
	q.wg.Wait()
}

// After :wq the export may still be waiting for the user: the editor counts as
// running (so the quit gate asks), and the document is listed as unsaved.
func TestWdRunning(t *testing.T) {
	w := startWd(t)
	w.open("basic.docx")
	path := w.path("basic.docx")
	if err := os.WriteFile(path, readFile(t, filepath.Join("testdata", "docx", "header-image.docx")), 0o644); err != nil {
		t.Fatal(err)
	}
	w.typeText("basic.docx", newText)

	asked, answer := make(chan struct{}), make(chan struct{})
	w.dlg.conflicts = []conflictChoice{conflictCancel}
	w.dlg.during = func() {
		close(asked)
		<-answer
	}
	from := w.rec.mark()
	if err := w.svc.Write(":wq\r"); err != nil {
		t.Fatal(err)
	}
	w.rec.waitName(t, from, eventNvimExit)
	<-asked
	if w.svc.session.Running() {
		t.Error("Neovim still runs")
	}
	if !w.svc.running() {
		t.Error("running() is false while an export waits for the user")
	}
	bufs, err := w.svc.modified(context.Background())
	if err != nil || len(bufs) != 1 || !sameFile(bufs[0].Path, path) {
		t.Errorf("modified = %+v, %v; want the .docx", bufs, err)
	}
	close(answer)
	waitCond(t, "the queue to end", func() bool { return !w.svc.running() })
	// The text stays in the working copy for the next start; nothing is open.
	if len(w.words.all()) != 0 {
		t.Error("the document is still open")
	}
	wc, found, err := w.ops.FindWork(context.Background(), path)
	if err != nil || !found || !wc.Meta.Dirty {
		t.Errorf("working copy found %v, dirty %v, %v", found, wc.Meta.Dirty, err)
	}
}
