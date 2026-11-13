package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"Pisar/internal/config"
	"Pisar/internal/convert"
)

// recordingConverter remembers which methods reached it.
type recordingConverter struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingConverter) note(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *recordingConverter) DocxToMarkdown(context.Context, string, string) ([]convert.Loss, error) {
	r.note("DocxToMarkdown")
	return nil, nil
}

func (r *recordingConverter) MarkdownToDocx(context.Context, string, string, string) error {
	r.note("MarkdownToDocx")
	return nil
}

func (r *recordingConverter) DocToDocx(context.Context, string, string) error {
	r.note("DocToDocx")
	return nil
}

func (r *recordingConverter) MakeReference(context.Context, string, string) error {
	r.note("MakeReference")
	return nil
}

func (r *recordingConverter) Render(context.Context, []byte, string) ([]byte, error) {
	r.note("Render")
	return []byte("<p>x</p>"), nil
}

func (r *recordingConverter) called() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// Every method that runs pandoc is refused when ready says no, and the inner
// one is not reached; the ones that do not run pandoc are not asked at all.
func TestEdToolsRefuseGuard(t *testing.T) {
	tooOld := &toolTooOldError{tool: toolPandoc, version: "3.9", minimum: config.MinPandocVersion}
	ctx := context.Background()
	for _, c := range []struct {
		method string
		pandoc bool // the method runs pandoc
		run    func(g pandocGuardedConverter, r pandocGuardedRenderer) error
	}{
		{"DocxToMarkdown", true, func(g pandocGuardedConverter, _ pandocGuardedRenderer) error {
			_, err := g.DocxToMarkdown(ctx, "a", "b")
			return err
		}},
		{"MarkdownToDocx", true, func(g pandocGuardedConverter, _ pandocGuardedRenderer) error {
			return g.MarkdownToDocx(ctx, "a", "b", "c")
		}},
		{"DocToDocx", false, func(g pandocGuardedConverter, _ pandocGuardedRenderer) error {
			return g.DocToDocx(ctx, "a", "b")
		}},
		{"MakeReference", false, func(g pandocGuardedConverter, _ pandocGuardedRenderer) error {
			return g.MakeReference(ctx, "a", "b")
		}},
		{"Render", true, func(_ pandocGuardedConverter, r pandocGuardedRenderer) error {
			_, err := r.Render(ctx, []byte("x"), "d")
			return err
		}},
	} {
		t.Run(c.method, func(t *testing.T) {
			inner := &recordingConverter{}
			asked := 0
			ready := func(context.Context) error { asked++; return tooOld }
			g := pandocGuardedConverter{inner: inner, ready: ready}
			r := pandocGuardedRenderer{inner: inner, ready: ready}

			err := c.run(g, r)
			if c.pandoc {
				if !errors.Is(err, error(tooOld)) && !errors.As(err, new(*toolTooOldError)) {
					t.Errorf("err = %v, want the too-old error", err)
				}
				if got := inner.called(); len(got) != 0 {
					t.Errorf("the inner %v was called although pandoc is too old", got)
				}
				return
			}
			if err != nil {
				t.Errorf("err = %v, want nil: this method does not run pandoc", err)
			}
			if got := inner.called(); len(got) != 1 || got[0] != c.method {
				t.Errorf("inner calls = %v, want [%s]", got, c.method)
			}
			if asked != 0 {
				t.Errorf("ready was asked %d times for a method without pandoc", asked)
			}
		})
	}

	// A pandoc that may run lets everything through.
	inner := &recordingConverter{}
	ready := func(context.Context) error { return nil }
	g := pandocGuardedConverter{inner: inner, ready: ready}
	r := pandocGuardedRenderer{inner: inner, ready: ready}
	_, _ = g.DocxToMarkdown(ctx, "a", "b")
	_ = g.MarkdownToDocx(ctx, "a", "b", "c")
	_, _ = r.Render(ctx, nil, "")
	if got := strings.Join(inner.called(), ","); got != "DocxToMarkdown,MarkdownToDocx,Render" {
		t.Errorf("inner calls = %s", got)
	}
}

// The error of the guard reaches the preview pane as an error event with its
// own code, and is tried again at the next notification (a pandoc may be upgraded meanwhile).
func TestEdToolsRefusePreview(t *testing.T) {
	e := &pvEnv{t: t, src: &fakeText{}, rend: newFakeRender(), clk: newFakeClock(), outCh: make(chan PreviewContent, 64)}
	e.src.set("text", "/d/a.md")
	var old bool
	e.loop = newPreviewLoop(previewLoopDeps{
		Source: e.src,
		Renderer: pandocGuardedRenderer{inner: e.rend, ready: func(context.Context) error {
			if old {
				return fmt.Errorf("preview: %w", &toolTooOldError{tool: toolPandoc, version: "3.9", minimum: "3.13"})
			}
			return nil
		}},
		Emit: func(c PreviewContent) {
			e.mu.Lock()
			e.out = append(e.out, c)
			e.mu.Unlock()
			e.outCh <- c
		},
		Target:      pvTarget,
		Debounce:    pvDebounce,
		ReadTimeout: edWait,
		MaxSource:   1234,
		NewTimer:    e.clk.newTimer,
	})
	t.Cleanup(e.loop.stop)

	old = true
	e.loop.enable(true)
	e.fire()
	if got := e.want(); got.State != "error" || got.Code != codePandocTooOld || got.HTML != "" {
		t.Errorf("sent %+v, want an error %q", got, codePandocTooOld)
	}
	if n := e.rend.count(); n != 0 {
		t.Errorf("pandoc ran %d times although it is too old", n)
	}

	old = false
	e.loop.changed()
	e.fire()
	if got := e.want(); got.State != "html" {
		t.Errorf("after the upgrade sent %+v, want html", got)
	}
}

// pandocReady on real scripts: only a pandoc that is found, readable and below the minimum is refused.
func TestEdToolsPandocReady(t *testing.T) {
	for _, c := range []struct {
		name    string
		pandoc  string // first line the fake prints; "" prints nothing
		missing bool
		want    bool // refused
	}{
		{"old", "pandoc 3.9", false, true},
		{"on the minimum", "pandoc " + config.MinPandocVersion, false, false},
		{"newer", "pandoc 3.14", false, false},
		{"version cannot be read", "something else entirely", false, false},
		{"prints nothing", "", false, false},
		{"not installed", "", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, bin := edTools(t, "NVIM v0.12.6", c.pandoc)
			if c.missing {
				if err := os.Remove(filepath.Join(bin, "pandoc")); err != nil {
					t.Fatal(err)
				}
			}
			err := e.svc.pandocReady(context.Background())
			var oe *toolTooOldError
			if got := errors.As(err, &oe); got != c.want {
				t.Fatalf("err = %v, refused = %v, want %v", err, got, c.want)
			}
			if c.want && (oe.tool != toolPandoc || oe.version != "3.9" || oe.minimum != config.MinPandocVersion) {
				t.Errorf("error = %+v", oe)
			}
			if err != nil && codeOf(err) != codePandocTooOld {
				t.Errorf("code = %s", codeOf(err))
			}
		})
	}
}

// The guards are built before the service exists; a check that comes that early
// does not block and does not panic, and says so in the log.
func TestEdToolsPandocReadyBeforeTheService(t *testing.T) {
	restoreDefaultLogger(t)
	var logged bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))

	var svc *EditorService
	ready := pandocReadyOf(func() *EditorService { return svc })
	if err := ready(context.Background()); err != nil {
		t.Fatalf("ready before the service = %v, want nil", err)
	}
	if !strings.Contains(logged.String(), "before the editor service was built") {
		t.Errorf("the early call was not logged: %q", logged.String())
	}

	// Once the service exists the check is its own.
	e, _ := edTools(t, "NVIM v0.12.6", "pandoc 3.9")
	svc = e.svc
	var oe *toolTooOldError
	if err := ready(context.Background()); !errors.As(err, &oe) {
		t.Errorf("ready with an old pandoc = %v, want toolTooOldError", err)
	}
}

// The whole way, with a real Neovim and real documents: once pandoc is replaced
// by a script that says it is 3.9, opening, "save as a copy" and saving a Word
// document are refused with pandoc-too-old, pandoc is only ever asked for its
// version, nothing of the user's is touched, and .md keeps working.
func TestEdToolsRefuseWord(t *testing.T) {
	parts := &wdParts{}
	var svc *EditorService
	parts.wrap = func(c wordConverter) wordConverter {
		return pandocGuardedConverter{inner: c, ready: func(ctx context.Context) error { return svc.pandocReady(ctx) }}
	}
	e := startEd(t, parts.apply(t))
	svc = e.svc
	p, dir := e.addProject("proj")
	w := &wdEnv{edEnv: e, wdParts: parts, proj: p, dir: dir}
	ctx := context.Background()

	// A document opened while pandoc is still the real one.
	w.open("basic.docx")
	basic := readFile(t, w.path("basic.docx"))
	fixture(t, "comments.docx", w.dir)
	comments := readFile(t, w.path("comments.docx"))
	opened := len(w.workFolders())

	// From here on pandoc is a script that says it is too old.
	realPath := os.Getenv("PATH")
	fake, log := t.TempDir(), filepath.Join(t.TempDir(), "pandoc.log")
	writeScript(t, fake, "pandoc", "printf '%s\\n' \"$*\" >> '"+log+"'\n"+printLine("pandoc 3.9"))
	t.Setenv("PATH", fake+string(os.PathListSeparator)+realPath)
	defer t.Setenv("PATH", realPath) // so the shutdown of the service does not meet the fake

	// Opening: refused, no working copy is left, the file is as it was.
	wantCode(t, w.svc.OpenDocument(ctx, w.proj.ID, "comments.docx"), codePandocTooOld)
	if n := len(w.workFolders()); n != opened {
		t.Errorf("%d working folders, want %d: the refused document left one", n, opened)
	}
	sameBytes(t, w.path("comments.docx"), comments)

	// Saving as a copy.
	copyPath := w.path("kopija.docx")
	w.dlg.paths = []string{copyPath}
	wantCode(t, w.svc.SaveCopy(ctx), codePandocTooOld)
	if _, err := os.Lstat(copyPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the copy was written: %v", err)
	}

	// Saving the open document: the export fails with the code and the file stays.
	w.typeText("basic.docx", newText)
	from := w.rec.mark()
	w.save()
	if x := w.rec.waitExport(t, from, phaseFailed); x.Code != codePandocTooOld {
		t.Errorf("export event = %+v, want code %s", x, codePandocTooOld)
	}
	sameBytes(t, w.path("basic.docx"), basic)

	// Markdown does not need pandoc.
	write(t, filepath.Join(w.dir, "a.md"), "# a\n")
	from = w.rec.mark()
	if err := w.svc.OpenDocument(ctx, w.proj.ID, "a.md"); err != nil {
		t.Fatalf("OpenDocument(a.md): %v", err)
	}
	w.rec.waitActive(t, from, "a.md", func(d DocumentActive) bool { return d.Name == "a.md" })

	// Pandoc was only asked for its version.
	for _, line := range strings.Split(strings.TrimSpace(string(readFile(t, log))), "\n") {
		if line != "--version" {
			t.Errorf("the fake pandoc was called with %q", line)
		}
	}
}

// The quit dialog explains a save that failed because pandoc is too old.
func TestEdToolsRefuseQuit(t *testing.T) {
	text := uiTextFor(t)
	g, _ := newTestGate(t, &fakeQuitEditor{}, &fakeQuitDialogs{})
	err := &apiError{code: codePandocTooOld, cause: &toolTooOldError{tool: toolPandoc, version: "3.9", minimum: "3.13"}}
	if got := g.saveFailure(err); got != text.ErrorPandocTooOld || got == "" {
		t.Errorf("saveFailure = %q, want %q", got, text.ErrorPandocTooOld)
	}
}
