package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"Pisar/internal/config"
	"Pisar/internal/convert"
	"Pisar/internal/docsafe"
)

// The tests here run the real pandoc and textutil on the sample documents in
// testdata/docx. The documents are copied into a "user folder" in t.TempDir();
// the working folder and the backups are other folders of the same temp dir.

const newText = "IZMJENAXYZ" // a token that cannot be in a sample document

type saveCall struct{ title, dir, name string }

// fakeWordDialogs records the questions and answers from queues. A queue that
// has run out answers "cancel", so an unexpected extra question shows up in the
// call counts instead of hanging.
type fakeWordDialogs struct {
	mu        sync.Mutex
	recovers  []recoverChoice
	conflicts []conflictChoice
	paths     []string
	pathErr   error
	during    func() // runs inside every question, like the user thinking

	recoverCalls  []string
	conflictCalls []string
	saveCalls     []saveCall
}

func (f *fakeWordDialogs) AskRecover(name string) recoverChoice {
	f.mu.Lock()
	f.recoverCalls = append(f.recoverCalls, name)
	c := recoverCancel
	if len(f.recovers) > 0 {
		c, f.recovers = f.recovers[0], f.recovers[1:]
	}
	during := f.during
	f.mu.Unlock()
	if during != nil {
		during()
	}
	return c
}

func (f *fakeWordDialogs) AskConflict(name string) conflictChoice {
	f.mu.Lock()
	f.conflictCalls = append(f.conflictCalls, name)
	c := conflictCancel
	if len(f.conflicts) > 0 {
		c, f.conflicts = f.conflicts[0], f.conflicts[1:]
	}
	during := f.during
	f.mu.Unlock()
	if during != nil {
		during()
	}
	return c
}

func (f *fakeWordDialogs) AskSavePath(title, dir, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveCalls = append(f.saveCalls, saveCall{title, dir, name})
	if f.pathErr != nil {
		return "", f.pathErr
	}
	p := ""
	if len(f.paths) > 0 {
		p, f.paths = f.paths[0], f.paths[1:]
	}
	return p, nil
}

func (f *fakeWordDialogs) counts() (recoverN, conflictN, saveN int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recoverCalls), len(f.conflictCalls), len(f.saveCalls)
}

// hookConverter runs the real converter, after an optional hook that may fail
// the call or touch the working folder first.
type hookConverter struct {
	wordConverter
	mdToDocxCalls  atomic.Int32
	docxToMdCalls  atomic.Int32
	beforeMdToDocx func(call int, dir string) error
	beforeDocxToMd func(call int, src string) error
}

func (h *hookConverter) MarkdownToDocx(ctx context.Context, dir, reference, dst string) error {
	n := int(h.mdToDocxCalls.Add(1))
	if h.beforeMdToDocx != nil {
		if err := h.beforeMdToDocx(n, dir); err != nil {
			return err
		}
	}
	return h.wordConverter.MarkdownToDocx(ctx, dir, reference, dst)
}

func (h *hookConverter) DocxToMarkdown(ctx context.Context, src, dir string) ([]convert.Loss, error) {
	n := int(h.docxToMdCalls.Add(1))
	if h.beforeDocxToMd != nil {
		if err := h.beforeDocxToMd(n, src); err != nil {
			return nil, err
		}
	}
	return h.wordConverter.DocxToMarkdown(ctx, src, dir)
}

// hookFiles runs the real file operations around Replace.
type hookFiles struct {
	wordFiles
	replaceCalls  atomic.Int32
	beforeReplace func(call int) // may change the target, as another program would
	afterReplace  func(res docsafe.ReplaceResult, err error) (docsafe.ReplaceResult, error)
}

func (h *hookFiles) Replace(ctx context.Context, src, dst string, expect docsafe.Snapshot) (docsafe.ReplaceResult, error) {
	n := int(h.replaceCalls.Add(1))
	if h.beforeReplace != nil {
		h.beforeReplace(n)
	}
	res, err := h.wordFiles.Replace(ctx, src, dst, expect)
	if h.afterReplace != nil {
		return h.afterReplace(res, err)
	}
	return res, err
}

// failBackupFiles makes the saving of the user's text fail, as a full disk would.
type failBackupFiles struct {
	wordFiles
	calls atomic.Int32
}

func (f *failBackupFiles) BackupText(context.Context, string, string) (string, error) {
	f.calls.Add(1)
	return "", errors.New("disk full")
}

type wordEnv struct {
	t       *testing.T
	user    string
	work    string
	backups string
	conv    *convert.Converter
	dlg     *fakeWordDialogs
	text    uiText
	ops     *docsafe.Ops
	docs    *wordDocs
}

func wordLimits() convert.InspectLimits {
	return convert.InspectLimits{MaxEntries: config.DefaultMaxDocxEntries, MaxPartBytes: config.DefaultMaxDocxPartBytes}
}

func newWordEnv(t *testing.T) *wordEnv {
	t.Helper()
	if _, err := exec.LookPath(config.PandocBin); err != nil {
		t.Skip("pandoc is not installed")
	}
	root := t.TempDir()
	e := &wordEnv{t: t, user: filepath.Join(root, "user"), work: filepath.Join(root, "work"), backups: filepath.Join(root, "backups"), dlg: &fakeWordDialogs{}}
	if err := os.Mkdir(e.user, 0o755); err != nil {
		t.Fatal(err)
	}
	conv, err := convert.New(convert.Options{PandocBin: config.PandocBin, TextutilBin: config.DefaultTextutilBin, Timeout: config.DefaultConvertTimeout, Limits: wordLimits()})
	if err != nil {
		t.Fatal(err)
	}
	e.conv = conv
	if e.text, err = loadUIText(); err != nil {
		t.Fatal(err)
	}
	e.docs = e.manager(nil, nil)
	return e
}

// manager builds a manager over the env's folders with a new docsafe.Ops, which
// is what a restarted application has: the same folders, nothing remembered.
func (e *wordEnv) manager(conv wordConverter, wrapFiles func(wordFiles) wordFiles) *wordDocs {
	e.t.Helper()
	ops, err := docsafe.New(docsafe.Options{
		TrashBin: config.DefaultTrashBin, Timeout: 10 * time.Second,
		BackupsDir: e.backups, MaxBackups: config.DefaultMaxBackups, WorkDir: e.work,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	e.ops = ops
	if conv == nil {
		conv = e.conv
	}
	var files wordFiles = ops
	if wrapFiles != nil {
		files = wrapFiles(ops)
	}
	m, err := newWordDocs(wordDocsDeps{
		Convert: conv,
		Examine: func(ctx context.Context, p string) (convert.Report, error) {
			return convert.Examine(ctx, p, wordLimits())
		},
		Files: files, Dialogs: e.dlg, Text: e.text, ExportAttempts: config.DefaultExportAttempts,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return m
}

func (e *wordEnv) needTextutil() {
	e.t.Helper()
	if _, err := os.Stat(config.DefaultTextutilBin); err != nil {
		e.t.Skip("textutil is not available")
	}
}

// fixture copies a sample document into dir and returns the copy's path.
func fixture(t *testing.T, name, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "docx", name))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustOpen(t *testing.T, m *wordDocs, p string) *wordDoc {
	t.Helper()
	d, err := m.open(t.Context(), p)
	if err != nil {
		t.Fatalf("open %s: %v", p, err)
	}
	return d
}

// edit appends text to the Markdown of d, as the editor's :w would, and tells
// the manager.
func edit(t *testing.T, m *wordDocs, d *wordDoc, text string) {
	t.Helper()
	f, err := os.OpenFile(d.markdownPath(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n\n" + text + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.markSaved(t.Context(), d); err != nil {
		t.Fatal(err)
	}
}

func exportSavedOK(t *testing.T, m *wordDocs, d *wordDoc) exportResult {
	t.Helper()
	res, err := m.export(t.Context(), d)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.Outcome != exportSaved || res.Warning != nil {
		t.Fatalf("export = %+v, want saved without a warning", res)
	}
	return res
}

func zipPart(t *testing.T, path, name string) []byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("%s is not a zip: %v", path, err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rc.Close() }()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatalf("%s has no %s", path, name)
	return nil
}

// docxText is word/document.xml; the sample text is in w:t runs as it is.
func docxText(t *testing.T, path string) string { return string(zipPart(t, path, "word/document.xml")) }

// mediaHashes is the sorted hashes of the pictures in word/media/.
func mediaHashes(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	var out []string
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "word/media/") || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		if cerr := rc.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		out = append(out, hex.EncodeToString(sum[:]))
	}
	slices.Sort(out)
	return out
}

func workDirs(t *testing.T, e *wordEnv) []string {
	t.Helper()
	entries, err := os.ReadDir(e.work)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		out = append(out, filepath.Join(e.work, en.Name()))
	}
	return out
}

func backupFiles(t *testing.T, e *wordEnv) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(e.backups, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func userFiles(t *testing.T, e *wordEnv) []string {
	t.Helper()
	entries, err := os.ReadDir(e.user)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		out = append(out, en.Name())
	}
	return out
}

func mustExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); err != nil {
		t.Errorf("%s should exist: %v", p, err)
	}
}

func mustNotExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s should not exist (err %v)", p, err)
	}
}

func sameBytes(t *testing.T, p string, want []byte) {
	t.Helper()
	if !bytes.Equal(readFile(t, p), want) {
		t.Errorf("%s was changed", p)
	}
}

func TestWordDocsOpenBasic(t *testing.T) {
	e := newWordEnv(t)
	p := fixture(t, "basic.docx", e.user)
	orig := readFile(t, p)

	d := mustOpen(t, e.docs, p)
	v := d.view()
	if v.Path != p || v.Imported || v.Dirty || len(v.ReadOnly) != 0 {
		t.Fatalf("view = %+v", v)
	}
	if !strings.HasPrefix(v.Markdown, e.work) || filepath.Base(v.Markdown) != "doc.md" {
		t.Errorf("Markdown path %q is not a doc.md in the work folder", v.Markdown)
	}
	for _, name := range []string{"doc.md", "reference.docx", "meta.json"} {
		fi, err := os.Lstat(filepath.Join(d.dir, name))
		if err != nil || !fi.Mode().IsRegular() {
			t.Errorf("%s: %v", name, err)
		}
	}
	if media, err := os.ReadDir(filepath.Join(d.dir, "media")); err != nil || len(media) == 0 {
		t.Errorf("media/: %v, %d entries", err, len(media))
	}
	sameBytes(t, p, orig)

	wc, found, err := e.ops.FindWork(t.Context(), p)
	if err != nil || !found {
		t.Fatalf("FindWork: %v %v", found, err)
	}
	snap, err := docsafe.TakeSnapshot(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if wc.Meta.Dirty || wc.Meta.Origin != p || wc.Meta.Target != p || !wc.Meta.Snapshot.Same(snap) {
		t.Errorf("meta = %+v", wc.Meta)
	}

	if again := mustOpen(t, e.docs, p); again != d {
		t.Error("the second open returned another document")
	}
	if got, ok := e.docs.byPath(p); !ok || got != d {
		t.Error("byPath")
	}
	if got, ok := e.docs.byMarkdown(v.Markdown); !ok || got != d {
		t.Error("byMarkdown")
	}
	if got := e.docs.all(); len(got) != 1 || got[0] != d {
		t.Errorf("all = %v", got)
	}
	if n := len(workDirs(t, e)); n != 1 {
		t.Errorf("%d working folders, want 1", n)
	}
}

func TestWordDocsOpenReadOnlyReasons(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		prepare func(t *testing.T, path string)
		want    []string
	}{
		{name: "comments", fixture: "comments.docx", want: []string{"comments"}},
		{name: "tracked changes", fixture: "tracked-changes.docx", want: []string{"tracked-changes"}},
		{name: "text box", fixture: "textbox.docx", want: []string{"text-boxes"}},
		{name: "chart", fixture: "chart.docx", want: []string{"embedded-objects"}},
		{name: "not writable", fixture: "basic.docx", want: []string{"no-permission"},
			prepare: func(t *testing.T, p string) {
				if err := os.Chmod(p, 0o444); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "hard link", fixture: "basic.docx", want: []string{"hard-links"},
			prepare: func(t *testing.T, p string) {
				if err := os.Link(p, p+".link"); err != nil {
					t.Fatal(err)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newWordEnv(t)
			p := fixture(t, c.fixture, e.user)
			if c.prepare != nil {
				c.prepare(t, p)
			}
			orig := readFile(t, p)
			d := mustOpen(t, e.docs, p)
			if got := d.view().ReadOnly; !slices.Equal(got, c.want) {
				t.Errorf("ReadOnly = %v, want %v", got, c.want)
			}
			sameBytes(t, p, orig)
			mustExist(t, d.markdownPath())
		})
	}
}

func TestWordDocsOpenLegacyDoc(t *testing.T) {
	e := newWordEnv(t)
	e.needTextutil()
	p := fixture(t, "legacy.doc", e.user)
	orig := readFile(t, p)

	d := mustOpen(t, e.docs, p)
	v := d.view()
	if !v.Imported || v.Path != p || v.Dirty || len(v.ReadOnly) != 0 {
		t.Fatalf("view = %+v", v)
	}
	if m := d.metaCopy(); m.Target != "" || m.Origin != p {
		t.Errorf("meta = %+v", m)
	}
	mustExist(t, v.Markdown)
	sameBytes(t, p, orig)
}

// allSimplified is what formatting.docx has: one of everything, in the order of
// the list in internal/convert.
var allSimplified = []string{
	"alignment", "text-color", "font", "spacing", "indentation",
	"page-breaks", "tabs", "fields", "table-formatting", "drawings",
}

// The formatting a save would simplify is known from the opening, does not make
// the document read-only, and is gone once the document has been saved.
func TestWordDocsSimplified(t *testing.T) {
	t.Run("a document with direct formatting", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "formatting.docx", e.user)
		orig := readFile(t, p)
		d := mustOpen(t, e.docs, p)
		v := d.view()
		if !slices.Equal(v.Simplified, allSimplified) || len(v.ReadOnly) != 0 {
			t.Fatalf("view = %+v, want every simplification and a writable document", v)
		}
		sameBytes(t, p, orig)

		edit(t, e.docs, d, newText)
		if got := d.view().Simplified; !slices.Equal(got, allSimplified) {
			t.Errorf("Simplified = %v after an edit that was not exported", got)
		}
		res := exportSavedOK(t, e.docs, d)
		if len(res.View.Simplified) != 0 || len(d.view().Simplified) != 0 {
			t.Errorf("Simplified = %v / %v after the export, want none", res.View.Simplified, d.view().Simplified)
		}
	})
	t.Run("a document pandoc wrote itself has none", func(t *testing.T) {
		e := newWordEnv(t)
		d := mustOpen(t, e.docs, fixture(t, "basic.docx", e.user))
		if got := d.view().Simplified; len(got) != 0 {
			t.Errorf("Simplified = %v", got)
		}
	})
	t.Run("a chart opens the document read-only", func(t *testing.T) {
		e := newWordEnv(t)
		d := mustOpen(t, e.docs, fixture(t, "chart.docx", e.user))
		if got := d.view().ReadOnly; !slices.Equal(got, []string{"embedded-objects"}) {
			t.Errorf("ReadOnly = %v", got)
		}
	})
	t.Run("a read-only document still has its list", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "formatting.docx", e.user)
		if err := os.Chmod(p, 0o444); err != nil {
			t.Fatal(err)
		}
		d := mustOpen(t, e.docs, p)
		v := d.view()
		if !slices.Equal(v.ReadOnly, []string{"no-permission"}) || !slices.Equal(v.Simplified, allSimplified) {
			t.Errorf("view = %+v", v)
		}
	})
	t.Run("a reload fills it from the new version", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		d := mustOpen(t, e.docs, p)
		edit(t, e.docs, d, newText)
		replaceWith(t, p, "formatting.docx")
		e.dlg.conflicts = []conflictChoice{conflictReload}
		res, err := e.docs.export(t.Context(), d)
		if err != nil || res.Outcome != exportReloaded {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		if !slices.Equal(res.View.Simplified, allSimplified) || !slices.Equal(d.view().Simplified, allSimplified) {
			t.Errorf("Simplified = %v / %v after the reload", res.View.Simplified, d.view().Simplified)
		}
	})
	t.Run("a reload to a version without formatting empties it", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "formatting.docx", e.user)
		d := mustOpen(t, e.docs, p)
		edit(t, e.docs, d, newText)
		replaceWith(t, p, "basic.docx")
		e.dlg.conflicts = []conflictChoice{conflictReload}
		if res, err := e.docs.export(t.Context(), d); err != nil || res.Outcome != exportReloaded {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		if got := d.view().Simplified; len(got) != 0 {
			t.Errorf("Simplified = %v after the reload", got)
		}
	})
	t.Run("the first save of an imported .doc empties it", func(t *testing.T) {
		e := newWordEnv(t)
		e.needTextutil()
		d := mustOpen(t, e.docs, fixture(t, "legacy.doc", e.user))
		// What textutil's output has depends on the macOS version, so the list
		// is put there by hand: the point is what the export does to it.
		d.reset(d.metaCopy(), nil, []string{"font"})
		edit(t, e.docs, d, newText)
		res := exportSavedOK(t, e.docs, d)
		if len(res.View.Simplified) != 0 || len(d.view().Simplified) != 0 {
			t.Errorf("Simplified = %v / %v after the first save", res.View.Simplified, d.view().Simplified)
		}
	})
}

func TestWordDocsOpenFailures(t *testing.T) {
	t.Run("not a Word document", func(t *testing.T) {
		e := newWordEnv(t)
		p := filepath.Join(e.user, "bad.docx")
		if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := e.docs.open(t.Context(), p)
		if !errors.Is(err, convert.ErrNotDocx) || codeOf(err) != codeNotWord {
			t.Fatalf("err = %v (code %s)", err, codeOf(err))
		}
		if n := len(workDirs(t, e)); n != 0 {
			t.Errorf("%d working folders left", n)
		}
		sameBytes(t, p, []byte("hello"))
		if len(e.docs.all()) != 0 {
			t.Error("a failed open must not leave a document")
		}
	})
	t.Run("pandoc missing", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		orig := readFile(t, p)
		hc := &hookConverter{wordConverter: e.conv, beforeDocxToMd: func(int, string) error {
			return fmt.Errorf("%w: pandoc: %w", convert.ErrToolMissing, exec.ErrNotFound)
		}}
		m := e.manager(hc, nil)
		_, err := m.open(t.Context(), p)
		if !errors.Is(err, convert.ErrToolMissing) || codeOf(err) != codePandocMissing {
			t.Fatalf("err = %v (code %s)", err, codeOf(err))
		}
		if n := len(workDirs(t, e)); n != 0 {
			t.Errorf("%d working folders left", n)
		}
		sameBytes(t, p, orig)
	})
	t.Run("not a regular file", func(t *testing.T) {
		e := newWordEnv(t)
		target := fixture(t, "basic.docx", e.user)
		link := filepath.Join(e.user, "link.docx")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := e.docs.open(t.Context(), link); !errors.Is(err, docsafe.ErrNotDocument) {
			t.Fatalf("err = %v", err)
		}
		if n := len(workDirs(t, e)); n != 0 {
			t.Errorf("%d working folders left", n)
		}
	})
	t.Run("other kinds and relative paths", func(t *testing.T) {
		e := newWordEnv(t)
		if _, err := e.docs.open(t.Context(), filepath.Join(e.user, "a.md")); !errors.Is(err, docsafe.ErrUnsupportedKind) {
			t.Errorf("err = %v", err)
		}
		if _, err := e.docs.open(t.Context(), "basic.docx"); err == nil {
			t.Error("a relative path must be refused")
		}
	})
}

func TestWordDocsMarkSaved(t *testing.T) {
	e := newWordEnv(t)
	p := fixture(t, "basic.docx", e.user)
	d := mustOpen(t, e.docs, p)
	if err := e.docs.markSaved(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	if !d.view().Dirty {
		t.Error("Dirty must be set")
	}
	wc, _, err := e.ops.FindWork(t.Context(), p)
	if err != nil || !wc.Meta.Dirty {
		t.Errorf("meta.json: %+v, %v", wc.Meta, err)
	}

	t.Run("read-only documents stay clean", func(t *testing.T) {
		e := newWordEnv(t)
		d := mustOpen(t, e.docs, fixture(t, "comments.docx", e.user))
		if err := e.docs.markSaved(t.Context(), d); err != nil {
			t.Fatal(err)
		}
		if d.view().Dirty {
			t.Error("a read-only document must not become dirty")
		}
	})
}

func TestWordDocsExport(t *testing.T) {
	e := newWordEnv(t)
	p := fixture(t, "basic.docx", e.user)
	orig := readFile(t, p)
	origMedia := mediaHashes(t, p)
	d := mustOpen(t, e.docs, p)

	edit(t, e.docs, d, newText)
	if !d.view().Dirty {
		t.Fatal("Dirty after the edit")
	}
	res := exportSavedOK(t, e.docs, d)
	if res.Backup == "" {
		t.Fatal("the first export must back up the original")
	}
	sameBytes(t, res.Backup, orig)
	if !strings.Contains(docxText(t, p), newText) || !strings.Contains(docxText(t, p), "Naslov prve razine") {
		t.Error("the file lacks the new or the old text")
	}
	if got := mediaHashes(t, p); !slices.Equal(got, origMedia) {
		t.Errorf("pictures = %v, want %v", got, origMedia)
	}
	if res.View.Dirty || d.view().Dirty || res.View.Path != p {
		t.Errorf("view after export = %+v", res.View)
	}
	wc, _, err := e.ops.FindWork(t.Context(), p)
	if err != nil || wc.Meta.Dirty {
		t.Errorf("meta.json after export: %+v, %v", wc.Meta, err)
	}
	snap, err := docsafe.TakeSnapshot(t.Context(), p)
	if err != nil || !wc.Meta.Snapshot.Same(snap) {
		t.Errorf("the snapshot in meta.json is not the file's: %v", err)
	}
	mustNotExist(t, filepath.Join(d.dir, "export.docx"))
	if got := userFiles(t, e); !slices.Equal(got, []string{"basic.docx"}) {
		t.Errorf("the user folder holds %v", got)
	}

	// A second export: no new backup, the new text arrives.
	edit(t, e.docs, d, newText+"2")
	res = exportSavedOK(t, e.docs, d)
	if res.Backup != "" {
		t.Errorf("second export made the backup %s", res.Backup)
	}
	if !strings.Contains(docxText(t, p), newText+"2") {
		t.Error("the second text is missing")
	}
	if n := len(backupFiles(t, e)); n != 1 {
		t.Errorf("%d backups, want 1", n)
	}
}

func TestWordDocsExportRefusals(t *testing.T) {
	t.Run("read-only document", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "comments.docx", e.user)
		orig := readFile(t, p)
		d := mustOpen(t, e.docs, p)
		_, err := e.docs.export(t.Context(), d)
		if !errors.Is(err, docsafe.ErrReadOnly) || codeOf(err) != codeReadOnly {
			t.Fatalf("err = %v", err)
		}
		sameBytes(t, p, orig)
		if len(backupFiles(t, e)) != 0 {
			t.Error("a refused export must not back anything up")
		}
	})
	t.Run("conversion fails", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		orig := readFile(t, p)
		hc := &hookConverter{wordConverter: e.conv}
		m := e.manager(hc, nil)
		d := mustOpen(t, m, p)
		edit(t, m, d, newText)
		md := readFile(t, d.markdownPath())
		hc.beforeMdToDocx = func(int, string) error {
			return &convert.ToolError{Tool: "pandoc", Err: errors.New("exit status 1")}
		}
		_, err := m.export(t.Context(), d)
		if codeOf(err) != codeConvertFailed {
			t.Fatalf("err = %v (code %s)", err, codeOf(err))
		}
		sameBytes(t, p, orig)
		sameBytes(t, d.markdownPath(), md)
		if !d.view().Dirty {
			t.Error("Dirty must stay set")
		}
		mustNotExist(t, filepath.Join(d.dir, "export.docx"))
	})
	t.Run("Markdown changes during the conversion", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		hc := &hookConverter{wordConverter: e.conv}
		m := e.manager(hc, nil)
		d := mustOpen(t, m, p)
		edit(t, m, d, newText)
		hc.beforeMdToDocx = func(call int, dir string) error {
			if call != 1 {
				return nil
			}
			// The editor writes while pandoc is about to read.
			return os.WriteFile(filepath.Join(dir, "doc.md"), append(readFile(t, filepath.Join(dir, "doc.md")), []byte("\nWHILEWRITING\n")...), 0o644)
		}
		exportSavedOK(t, m, d)
		if n := hc.mdToDocxCalls.Load(); n != 2 {
			t.Errorf("%d conversions, want 2", n)
		}
		if !strings.Contains(docxText(t, p), "WHILEWRITING") {
			t.Error("the repeated conversion must carry the text written meanwhile")
		}
	})
	t.Run("Markdown never settles", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		orig := readFile(t, p)
		hc := &hookConverter{wordConverter: e.conv}
		m := e.manager(hc, nil)
		d := mustOpen(t, m, p)
		edit(t, m, d, newText)
		hc.beforeMdToDocx = func(call int, dir string) error {
			f, err := os.OpenFile(filepath.Join(dir, "doc.md"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			_, werr := fmt.Fprintf(f, "\nx%d\n", call)
			return errors.Join(werr, f.Close())
		}
		_, err := m.export(t.Context(), d)
		if !errors.Is(err, errMarkdownUnstable) {
			t.Fatalf("err = %v", err)
		}
		if n := hc.mdToDocxCalls.Load(); n != config.DefaultExportAttempts {
			t.Errorf("%d conversions, want %d", n, config.DefaultExportAttempts)
		}
		sameBytes(t, p, orig)
		if !d.view().Dirty {
			t.Error("Dirty must stay set")
		}
	})
	t.Run("Replace reports a failure after the rename", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		hf := &hookFiles{afterReplace: func(res docsafe.ReplaceResult, err error) (docsafe.ReplaceResult, error) {
			return res, errors.Join(err, errors.New("cannot sync the folder"))
		}}
		m := e.manager(nil, func(f wordFiles) wordFiles { hf.wordFiles = f; return hf })
		d := mustOpen(t, m, p)
		edit(t, m, d, newText)
		res, err := m.export(t.Context(), d)
		if err != nil || res.Outcome != exportSaved || res.Warning == nil {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		if d.view().Dirty || !strings.Contains(docxText(t, p), newText) {
			t.Error("the file is in place, so the document counts as saved")
		}
		if wc, _, _ := e.ops.FindWork(t.Context(), p); wc.Meta.Dirty {
			t.Error("meta.json still says dirty")
		}
	})
}

// replaceWith overwrites the user's file with another sample, as another program would.
func replaceWith(t *testing.T, p, sample string) []byte {
	t.Helper()
	data := readFile(t, filepath.Join("testdata", "docx", sample))
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWordDocsExportConflict(t *testing.T) {
	setup := func(t *testing.T) (*wordEnv, string, *wordDoc, []byte) {
		t.Helper()
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		d := mustOpen(t, e.docs, p)
		edit(t, e.docs, d, newText)
		return e, p, d, replaceWith(t, p, "comments.docx")
	}

	t.Run("cancel", func(t *testing.T) {
		e, p, d, external := setup(t)
		md := readFile(t, d.markdownPath())
		res, err := e.docs.export(t.Context(), d)
		if err != nil || res.Outcome != exportCanceled {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		if _, n, _ := e.dlg.counts(); n != 1 {
			t.Errorf("AskConflict called %d times", n)
		}
		if e.dlg.conflictCalls[0] != "basic.docx" {
			t.Errorf("asked about %q", e.dlg.conflictCalls[0])
		}
		sameBytes(t, p, external)
		sameBytes(t, d.markdownPath(), md)
		if !d.view().Dirty || !res.View.Dirty {
			t.Error("Dirty must stay set")
		}
		if len(backupFiles(t, e)) != 0 {
			t.Error("cancelling must not back anything up")
		}
	})
	t.Run("overwrite", func(t *testing.T) {
		e, p, d, external := setup(t)
		e.dlg.conflicts = []conflictChoice{conflictOverwrite}
		res := exportSavedOK(t, e.docs, d)
		if _, n, _ := e.dlg.counts(); n != 1 {
			t.Errorf("AskConflict called %d times", n)
		}
		if !strings.Contains(docxText(t, p), newText) {
			t.Error("the new text is not in the file")
		}
		if res.Backup == "" {
			t.Fatal("no backup of the other program's version")
		}
		sameBytes(t, res.Backup, external)
		if d.view().Dirty {
			t.Error("Dirty after a saved export")
		}
	})
	t.Run("overwrite, then the file changes again", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		var external2 []byte
		hf := &hookFiles{beforeReplace: func(call int) {
			if call == 2 { // the retry after "overwrite"
				external2 = replaceWith(t, p, "tracked-changes.docx")
			}
		}}
		m := e.manager(nil, func(f wordFiles) wordFiles { hf.wordFiles = f; return hf })
		d := mustOpen(t, m, p)
		edit(t, m, d, newText)
		replaceWith(t, p, "comments.docx")
		e.dlg.conflicts = []conflictChoice{conflictOverwrite, conflictCancel}
		res, err := m.export(t.Context(), d)
		if err != nil || res.Outcome != exportCanceled {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		if _, n, _ := e.dlg.counts(); n != 2 {
			t.Errorf("AskConflict called %d times, want 2", n)
		}
		sameBytes(t, p, external2)
	})
	t.Run("reload", func(t *testing.T) {
		e, p, d, external := setup(t)
		mine := readFile(t, d.markdownPath())
		e.dlg.conflicts = []conflictChoice{conflictReload}
		res, err := e.docs.export(t.Context(), d)
		if err != nil || res.Outcome != exportReloaded {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		sameBytes(t, p, external)
		md := string(readFile(t, d.markdownPath()))
		if !strings.Contains(md, "Dokument s komentarom") || strings.Contains(md, newText) {
			t.Errorf("doc.md is not the other version:\n%s", md)
		}
		sameBytes(t, filepath.Join(d.dir, "doc.prev.md"), mine)
		if res.View.Dirty || d.view().Dirty {
			t.Error("Dirty after a reload")
		}
		// The version now on disk has comments, so it can only be read.
		if got := res.View.ReadOnly; !slices.Equal(got, []string{"comments"}) {
			t.Errorf("ReadOnly = %v", got)
		}
		mustExist(t, filepath.Join(d.dir, "reference.docx"))
		mustNotExist(t, filepath.Join(d.dir, "media.prev"))
		mustNotExist(t, filepath.Join(d.dir, "reference.prev.docx"))
		wc, _, err := e.ops.FindWork(t.Context(), p)
		snap, serr := docsafe.TakeSnapshot(t.Context(), p)
		if err != nil || serr != nil || wc.Meta.Dirty || !wc.Meta.Snapshot.Same(snap) {
			t.Errorf("meta.json = %+v (%v, %v)", wc.Meta, err, serr)
		}
		// The text survives the closing of the document: it is a Markdown copy in
		// the backup folder, and nothing else is backed up (the file on disk is
		// not touched by a reload).
		copies := backupFiles(t, e)
		if len(copies) != 1 || !strings.HasSuffix(copies[0], "-basic.docx.md") {
			t.Fatalf("backups after a reload = %q, want one Markdown copy", copies)
		}
		sameBytes(t, copies[0], mine)
		if err := e.docs.close(t.Context()); err != nil {
			t.Fatal(err)
		}
		mustNotExist(t, d.dir)        // nothing unsaved: the working folder is gone ...
		sameBytes(t, copies[0], mine) // ... the copy is not
		m2 := e.manager(nil, nil)
		if err := m2.sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
		sameBytes(t, copies[0], mine)
		// A second reload replaces doc.prev.md.
		if _, err := e.docs.export(t.Context(), d); !errors.Is(err, docsafe.ErrReadOnly) {
			t.Errorf("export of the read-only reload: %v", err)
		}
	})
	t.Run("reload without a saved copy of the text does not happen", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		hc := &hookConverter{wordConverter: e.conv}
		bf := &failBackupFiles{}
		m := e.manager(hc, func(f wordFiles) wordFiles { bf.wordFiles = f; return bf })
		d := mustOpen(t, m, p)
		edit(t, m, d, newText)
		external := replaceWith(t, p, "comments.docx")
		mine := readFile(t, d.markdownPath())
		convCalls := hc.docxToMdCalls.Load()
		e.dlg.conflicts = []conflictChoice{conflictReload}
		if _, err := m.export(t.Context(), d); err == nil {
			t.Fatal("expected an error")
		}
		if bf.calls.Load() != 1 {
			t.Errorf("BackupText called %d times", bf.calls.Load())
		}
		if hc.docxToMdCalls.Load() != convCalls {
			t.Error("the file was loaded although the text was not saved")
		}
		sameBytes(t, d.markdownPath(), mine)
		mustNotExist(t, filepath.Join(d.dir, "doc.prev.md"))
		mustExist(t, filepath.Join(d.dir, "reference.docx"))
		if !d.view().Dirty {
			t.Error("Dirty was cleared")
		}
		sameBytes(t, p, external)
		if got := backupFiles(t, e); len(got) != 0 {
			t.Errorf("backups = %q", got)
		}
	})
	t.Run("reload fails", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		hc := &hookConverter{wordConverter: e.conv}
		m := e.manager(hc, nil)
		d := mustOpen(t, m, p)
		edit(t, m, d, newText)
		external := replaceWith(t, p, "comments.docx")
		mine := readFile(t, d.markdownPath())
		mediaBefore, err := os.ReadDir(filepath.Join(d.dir, "media"))
		if err != nil {
			t.Fatal(err)
		}
		hc.beforeDocxToMd = func(int, string) error { return errors.New("pandoc crashed") }
		e.dlg.conflicts = []conflictChoice{conflictReload}
		_, err = m.export(t.Context(), d)
		if err == nil {
			t.Fatal("expected an error")
		}
		sameBytes(t, d.markdownPath(), mine)
		mustNotExist(t, filepath.Join(d.dir, "doc.prev.md"))
		mustExist(t, filepath.Join(d.dir, "reference.docx"))
		mustNotExist(t, filepath.Join(d.dir, "reference.prev.docx"))
		mustNotExist(t, filepath.Join(d.dir, "media.prev"))
		if after, err := os.ReadDir(filepath.Join(d.dir, "media")); err != nil || len(after) != len(mediaBefore) {
			t.Errorf("media/ after the failed reload: %v, %d entries (was %d)", err, len(after), len(mediaBefore))
		}
		if !d.view().Dirty || len(d.view().ReadOnly) != 0 {
			t.Errorf("view = %+v", d.view())
		}
		sameBytes(t, p, external)

		// The document still works: with the converter back, an overwrite saves it.
		hc.beforeDocxToMd = nil
		e.dlg.conflicts = []conflictChoice{conflictOverwrite}
		exportSavedOK(t, m, d)
		if !strings.Contains(docxText(t, p), newText) {
			t.Error("the text is lost")
		}
	})
	t.Run("the list is answerable while the question is open", func(t *testing.T) {
		e, p, d, _ := setup(t)
		answered := make(chan struct{})
		e.dlg.during = func() {
			e.docs.all()
			e.docs.byPath(p)
			d.view()
			close(answered)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			if _, err := e.docs.export(t.Context(), d); err != nil {
				t.Error(err)
			}
		}()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("export is stuck: something is locked while the question is open")
		}
		select {
		case <-answered:
		default:
			t.Error("the question was not asked")
		}
	})
}

func TestWordDocsImportedDoc(t *testing.T) {
	prepare := func(t *testing.T) (*wordEnv, string, *wordDoc, []byte) {
		t.Helper()
		e := newWordEnv(t)
		e.needTextutil()
		p := fixture(t, "legacy.doc", e.user)
		orig := readFile(t, p)
		d := mustOpen(t, e.docs, p)
		edit(t, e.docs, d, newText)
		return e, p, d, orig
	}

	t.Run("the first save makes a new .docx", func(t *testing.T) {
		e, p, d, orig := prepare(t)
		docx := filepath.Join(e.user, "legacy.docx")
		res := exportSavedOK(t, e.docs, d)
		if res.Backup != "" {
			t.Errorf("backup %s of a file that did not exist", res.Backup)
		}
		sameBytes(t, p, orig)
		if !strings.Contains(docxText(t, docx), newText) {
			t.Error("the new file lacks the text")
		}
		if res.View.Path != docx || d.view().Path != docx || !res.View.Imported || res.View.Dirty {
			t.Errorf("view = %+v", res.View)
		}
		if got, ok := e.docs.byPath(docx); !ok || got != d {
			t.Error("byPath(new file)")
		}
		if _, _, n := e.dlg.counts(); n != 0 {
			t.Errorf("AskSavePath called %d times", n)
		}
		wc, _, err := e.ops.FindWork(t.Context(), docx)
		if err != nil || wc.Meta.Target != docx || wc.Meta.Origin != p || wc.Meta.Dirty {
			t.Errorf("meta.json = %+v (%v)", wc.Meta, err)
		}

		// The next export replaces the new file; there is nothing of someone else's to back up.
		edit(t, e.docs, d, newText+"2")
		res = exportSavedOK(t, e.docs, d)
		if res.Backup != "" {
			t.Errorf("backup %s of the file Pisar itself wrote", res.Backup)
		}
		if !strings.Contains(docxText(t, docx), newText+"2") {
			t.Error("the second text is missing")
		}
		sameBytes(t, p, orig)
		if len(backupFiles(t, e)) != 0 {
			t.Error("no backups expected")
		}
	})
	t.Run("the name is taken", func(t *testing.T) {
		e, p, d, orig := prepare(t)
		taken := fixture(t, "basic.docx", e.user)
		if err := os.Rename(taken, filepath.Join(e.user, "legacy.docx")); err != nil {
			t.Fatal(err)
		}
		takenBytes := readFile(t, filepath.Join(e.user, "legacy.docx"))
		chosen := filepath.Join(e.user, "chosen") // no extension
		e.dlg.paths = []string{chosen}
		res := exportSavedOK(t, e.docs, d)
		if _, _, n := e.dlg.counts(); n != 1 {
			t.Fatalf("AskSavePath called %d times", n)
		}
		if got := e.dlg.saveCalls[0]; got != (saveCall{e.text.ImportSaveTitle, e.user, "legacy.docx"}) {
			t.Errorf("AskSavePath(%+v)", got)
		}
		if res.View.Path != chosen+".docx" || !strings.Contains(docxText(t, chosen+".docx"), newText) {
			t.Errorf("view = %+v", res.View)
		}
		sameBytes(t, filepath.Join(e.user, "legacy.docx"), takenBytes)
		sameBytes(t, p, orig)
	})
	t.Run("the user names a taken file again", func(t *testing.T) {
		e, _, d, _ := prepare(t)
		existing := fixture(t, "basic.docx", e.user)
		existingBytes := readFile(t, existing)
		if err := os.WriteFile(filepath.Join(e.user, "legacy.docx"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		e.dlg.paths = []string{existing, filepath.Join(e.user, "second.docx")}
		exportSavedOK(t, e.docs, d)
		if _, _, n := e.dlg.counts(); n != 2 {
			t.Errorf("AskSavePath called %d times, want 2", n)
		}
		sameBytes(t, existing, existingBytes)
		mustExist(t, filepath.Join(e.user, "second.docx"))
	})
	t.Run("cancel", func(t *testing.T) {
		e, p, d, orig := prepare(t)
		if err := os.WriteFile(filepath.Join(e.user, "legacy.docx"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := e.docs.export(t.Context(), d)
		if err != nil || res.Outcome != exportCanceled {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		if !d.view().Dirty || d.view().Path != p {
			t.Errorf("view = %+v", d.view())
		}
		sameBytes(t, p, orig)
		sameBytes(t, filepath.Join(e.user, "legacy.docx"), []byte("x"))
		if got := userFiles(t, e); len(got) != 2 {
			t.Errorf("the user folder holds %v", got)
		}
	})
}

func TestWordDocsSaveCopy(t *testing.T) {
	t.Run("new file", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		orig := readFile(t, p)
		d := mustOpen(t, e.docs, p)
		edit(t, e.docs, d, newText)
		e.dlg.paths = []string{filepath.Join(e.user, "copy")}

		got, err := e.docs.saveCopy(t.Context(), d)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(e.user, "copy.docx")
		if got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if !strings.Contains(docxText(t, want), newText) {
			t.Error("the copy lacks the text of the working copy")
		}
		sameBytes(t, p, orig)
		if !d.view().Dirty {
			t.Error("a copy must not change Dirty")
		}
		if e.dlg.saveCalls[0] != (saveCall{e.text.SaveCopyTitle, e.user, "basic " + e.text.SaveCopySuffix + ".docx"}) {
			t.Errorf("AskSavePath(%+v)", e.dlg.saveCalls[0])
		}
		mustNotExist(t, filepath.Join(d.dir, "export.docx"))
	})
	t.Run("taken name", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		d := mustOpen(t, e.docs, p)
		other := fixture(t, "header-image.docx", e.user)
		otherBytes := readFile(t, other)
		e.dlg.paths = []string{other, filepath.Join(e.user, "free.docx")}
		got, err := e.docs.saveCopy(t.Context(), d)
		if err != nil || got != filepath.Join(e.user, "free.docx") {
			t.Fatalf("got %q, %v", got, err)
		}
		sameBytes(t, other, otherBytes)
		if _, _, n := e.dlg.counts(); n != 2 {
			t.Errorf("AskSavePath called %d times", n)
		}
	})
	t.Run("read-only document", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "comments.docx", e.user)
		orig := readFile(t, p)
		d := mustOpen(t, e.docs, p)
		e.dlg.paths = []string{filepath.Join(e.user, "mine.docx")}
		got, err := e.docs.saveCopy(t.Context(), d)
		if err != nil || got == "" {
			t.Fatalf("got %q, %v", got, err)
		}
		sameBytes(t, p, orig)
		mustExist(t, got)
	})
	t.Run("cancel", func(t *testing.T) {
		e := newWordEnv(t)
		d := mustOpen(t, e.docs, fixture(t, "basic.docx", e.user))
		got, err := e.docs.saveCopy(t.Context(), d)
		if err != nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
		if n := len(userFiles(t, e)); n != 1 {
			t.Errorf("the user folder holds %d files", n)
		}
	})
	t.Run("dialog error", func(t *testing.T) {
		e := newWordEnv(t)
		d := mustOpen(t, e.docs, fixture(t, "basic.docx", e.user))
		e.dlg.pathErr = errors.New("no window")
		if _, err := e.docs.saveCopy(t.Context(), d); !errors.Is(err, e.dlg.pathErr) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestWordDocsRecovery(t *testing.T) {
	// crash: a document edited and saved in one manager, then a second manager
	// with new Ops over the same folders, as after a crash.
	crash := func(t *testing.T) (*wordEnv, string, *wordDoc, *wordDocs) {
		t.Helper()
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		d := mustOpen(t, e.docs, p)
		edit(t, e.docs, d, newText)
		return e, p, d, e.manager(nil, nil)
	}

	t.Run("use", func(t *testing.T) {
		e, p, d, m2 := crash(t)
		e.dlg.recovers = []recoverChoice{recoverUse}
		d2 := mustOpen(t, m2, p)
		if n, _, _ := e.dlg.counts(); n != 1 || e.dlg.recoverCalls[0] != "basic.docx" {
			t.Fatalf("AskRecover calls: %v", e.dlg.recoverCalls)
		}
		if d2.dir != d.dir || !d2.view().Dirty || !strings.Contains(string(readFile(t, d2.markdownPath())), newText) {
			t.Fatalf("view = %+v", d2.view())
		}
		if n := len(workDirs(t, e)); n != 1 {
			t.Errorf("%d working folders", n)
		}
		exportSavedOK(t, m2, d2)
		if !strings.Contains(docxText(t, p), newText) {
			t.Error("the recovered text did not reach the file")
		}
	})
	t.Run("discard", func(t *testing.T) {
		e, p, _, m2 := crash(t)
		e.dlg.recovers = []recoverChoice{recoverDiscard}
		d2 := mustOpen(t, m2, p)
		md := string(readFile(t, d2.markdownPath()))
		if strings.Contains(md, newText) || !strings.Contains(md, "Naslov prve razine") || d2.view().Dirty {
			t.Errorf("view = %+v\n%s", d2.view(), md)
		}
		if n := len(workDirs(t, e)); n != 1 {
			t.Errorf("%d working folders", n)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		e, p, d, m2 := crash(t)
		e.dlg.recovers = []recoverChoice{recoverCancel}
		md := readFile(t, d.markdownPath())
		_, err := m2.open(t.Context(), p)
		if !errors.Is(err, errRecoverDeclined) || codeOf(err) != codeCanceled {
			t.Fatalf("err = %v (code %s)", err, codeOf(err))
		}
		sameBytes(t, d.markdownPath(), md)
		if n := len(workDirs(t, e)); n != 1 {
			t.Errorf("%d working folders", n)
		}
		wc, found, _ := e.ops.FindWork(t.Context(), p)
		if !found || !wc.Meta.Dirty {
			t.Error("the working copy must stay as it was")
		}
	})
	t.Run("damaged", func(t *testing.T) {
		for _, name := range []string{"doc.md", "reference.docx"} {
			t.Run(name, func(t *testing.T) {
				e, p, d, m2 := crash(t)
				if err := os.Remove(filepath.Join(d.dir, name)); err != nil {
					t.Fatal(err)
				}
				e.dlg.recovers = []recoverChoice{recoverUse}
				_, err := m2.open(t.Context(), p)
				if !errors.Is(err, docsafe.ErrDamagedWork) || codeOf(err) != codeDamagedWork {
					t.Fatalf("err = %v (code %s)", err, codeOf(err))
				}
				mustExist(t, d.dir)
				mustExist(t, filepath.Join(d.dir, "meta.json"))
				if len(m2.all()) != 0 {
					t.Error("a damaged copy must not be opened")
				}
			})
		}
	})
	t.Run("a clean copy is a leftover", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		old := mustOpen(t, e.docs, p)
		m2 := e.manager(nil, nil)
		d2 := mustOpen(t, m2, p)
		if n, _, _ := e.dlg.counts(); n != 0 {
			t.Errorf("AskRecover called %d times for a clean copy", n)
		}
		if d2.dir == old.dir {
			t.Error("a new working copy was expected")
		}
		if n := len(workDirs(t, e)); n != 1 {
			t.Errorf("%d working folders", n)
		}
	})
}

func TestWordDocsLifecycle(t *testing.T) {
	t.Run("sweep and close", func(t *testing.T) {
		e := newWordEnv(t)
		a := fixture(t, "basic.docx", e.user)
		b := filepath.Join(e.user, "second.docx")
		if err := os.WriteFile(b, readFile(t, a), 0o644); err != nil {
			t.Fatal(err)
		}
		dirtyDoc := mustOpen(t, e.docs, b)
		mustOpen(t, e.docs, a)
		edit(t, e.docs, dirtyDoc, newText)
		if n := len(workDirs(t, e)); n != 2 {
			t.Fatalf("%d working folders", n)
		}

		// sweep at the next start: the clean one goes, the unsaved one stays.
		if err := e.manager(nil, nil).sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := workDirs(t, e); len(got) != 1 || got[0] != dirtyDoc.dir {
			t.Errorf("after sweep: %v", got)
		}

		// close at the shutdown does the same for the documents that are open.
		e2 := newWordEnv(t)
		x := fixture(t, "basic.docx", e2.user)
		y := filepath.Join(e2.user, "y.docx")
		if err := os.WriteFile(y, readFile(t, x), 0o644); err != nil {
			t.Fatal(err)
		}
		mustOpen(t, e2.docs, x)
		dy := mustOpen(t, e2.docs, y)
		edit(t, e2.docs, dy, newText)
		if err := e2.docs.close(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := workDirs(t, e2); len(got) != 1 || got[0] != dy.dir {
			t.Errorf("after close: %v", got)
		}
	})
	t.Run("renamed", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		d := mustOpen(t, e.docs, p)
		np := filepath.Join(e.user, "renamed.docx")
		if err := os.Rename(p, np); err != nil {
			t.Fatal(err)
		}
		if err := e.docs.renamed(t.Context(), p, np); err != nil {
			t.Fatal(err)
		}
		if _, ok := e.docs.byPath(p); ok {
			t.Error("the old path still finds the document")
		}
		if got, ok := e.docs.byPath(np); !ok || got != d || d.view().Path != np {
			t.Errorf("view = %+v", d.view())
		}
		wc, found, err := e.ops.FindWork(t.Context(), np)
		if err != nil || !found || wc.Meta.Origin != np || wc.Meta.Target != np {
			t.Errorf("meta.json = %+v (%v, %v)", wc.Meta, found, err)
		}
		edit(t, e.docs, d, newText)
		exportSavedOK(t, e.docs, d)
		if !strings.Contains(docxText(t, np), newText) {
			t.Error("the export did not go to the new name")
		}
		mustNotExist(t, p)
		// A path nobody has open is not an error.
		if err := e.docs.renamed(t.Context(), filepath.Join(e.user, "none.docx"), p); err != nil {
			t.Error(err)
		}
	})
	t.Run("removed", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		mustOpen(t, e.docs, p)
		if err := e.docs.removed(t.Context(), p); err != nil {
			t.Fatal(err)
		}
		if _, ok := e.docs.byPath(p); ok || len(e.docs.all()) != 0 {
			t.Error("the document is still listed")
		}
		if n := len(workDirs(t, e)); n != 0 {
			t.Errorf("%d working folders left", n)
		}
		if err := e.docs.removed(t.Context(), p); err != nil {
			t.Error(err)
		}
	})
}

func TestWordDocsConcurrency(t *testing.T) {
	t.Run("concurrent opens", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		const n = 8
		docs := make([]*wordDoc, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() { docs[i], errs[i] = e.docs.open(t.Context(), p) })
		}
		wg.Wait()
		for i := range n {
			if errs[i] != nil || docs[i] != docs[0] {
				t.Fatalf("open %d: %v, %p (want %p)", i, errs[i], docs[i], docs[0])
			}
		}
		if got := len(workDirs(t, e)); got != 1 {
			t.Errorf("%d working folders, want 1", got)
		}
		if got := len(e.docs.all()); got != 1 {
			t.Errorf("%d documents, want 1", got)
		}
	})
	t.Run("export and markSaved", func(t *testing.T) {
		e := newWordEnv(t)
		p := fixture(t, "basic.docx", e.user)
		d := mustOpen(t, e.docs, p)
		for range 4 {
			var wg sync.WaitGroup
			wg.Go(func() {
				if err := e.docs.markSaved(t.Context(), d); err != nil {
					t.Error(err)
				}
			})
			wg.Go(func() {
				if _, err := e.docs.export(t.Context(), d); err != nil {
					t.Error(err)
				}
			})
			wg.Go(func() { d.view() })
			wg.Wait()
		}
		wc, found, err := e.ops.FindWork(t.Context(), p)
		if err != nil || !found {
			t.Fatalf("meta.json unreadable: %v %v", found, err)
		}
		if wc.Meta.Dirty != d.view().Dirty {
			t.Errorf("meta.json says dirty=%v, memory %v", wc.Meta.Dirty, d.view().Dirty)
		}
	})
}

func TestNewWordDocsNeedsDeps(t *testing.T) {
	if _, err := newWordDocs(wordDocsDeps{}); err == nil {
		t.Error("empty dependencies must be refused")
	}
}

// ExportAttempts is a limit from config: zero would make every export fail without trying.
func TestNewWordDocsNeedsAttempts(t *testing.T) {
	e := newWordEnv(t)
	_, err := newWordDocs(wordDocsDeps{
		Convert: e.conv, Examine: func(context.Context, string) (convert.Report, error) { return convert.Report{}, nil },
		Files: e.ops, Dialogs: e.dlg, Text: e.text,
	})
	if err == nil || !strings.Contains(err.Error(), "ExportAttempts") {
		t.Errorf("err = %v, want one about ExportAttempts", err)
	}
}

// release is what the end of an editor process uses: the documents it is given
// are forgotten, a clean working copy goes, a Dirty one stays for recovery, and
// a document that was not given (opened later, under a new editor) is not touched.
func TestWordDocsRelease(t *testing.T) {
	e := newWordEnv(t)
	clean := mustOpen(t, e.docs, fixture(t, "basic.docx", e.user))
	dirtyPath := filepath.Join(e.user, "dirty.docx")
	if err := os.WriteFile(dirtyPath, readFile(t, filepath.Join(e.user, "basic.docx")), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty := mustOpen(t, e.docs, dirtyPath)
	edit(t, e.docs, dirty, newText)
	laterPath := filepath.Join(e.user, "later.docx")
	if err := os.WriteFile(laterPath, readFile(t, filepath.Join(e.user, "basic.docx")), 0o644); err != nil {
		t.Fatal(err)
	}
	later := mustOpen(t, e.docs, laterPath)

	if err := e.docs.release(t.Context(), []*wordDoc{clean, dirty}); err != nil {
		t.Fatal(err)
	}
	if got := e.docs.all(); len(got) != 1 || got[0] != later {
		t.Errorf("open documents = %v, want only the one that was not released", got)
	}
	got := workDirs(t, e)
	slices.Sort(got)
	want := []string{dirty.dir, later.dir}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("working folders = %v, want %v (the clean one gone)", got, want)
	}

	// The released Dirty document is asked about at its next open, not found in the list.
	if _, ok := e.docs.byPath(dirtyPath); ok {
		t.Error("a released document is still found")
	}
	e.dlg.recovers = []recoverChoice{recoverUse}
	again := mustOpen(t, e.docs, dirtyPath)
	if !again.view().Dirty {
		t.Error("the recovered document lost its Dirty flag")
	}
	if n, _, _ := e.dlg.counts(); n != 1 {
		t.Errorf("%d recovery questions, want 1", n)
	}

	// close forgets everything, the Dirty ones included.
	if err := e.docs.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := e.docs.all(); len(got) != 0 {
		t.Errorf("open documents after close = %v", got)
	}
}
