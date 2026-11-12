package convert

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	fakePandoc   = "/fake/pandoc"
	fakeTextutil = "/fake/textutil"
)

// call is one invocation seen by the fake Runner.
type call struct {
	dir, name string
	args      []string
}

// fakeRunner records the calls and lets a test decide what each one does.
// No real tool is involved.
type fakeRunner struct {
	calls []call
	fn    func(n int, c call) (stdout, stderr []byte, err error)
}

func (f *fakeRunner) run(_ context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	c := call{dir: dir, name: name, args: append([]string(nil), args...)}
	f.calls = append(f.calls, c)
	return f.fn(len(f.calls), c)
}

// argAfter returns the argument that follows flag.
func argAfter(t *testing.T, c call, flag string) string {
	t.Helper()
	for i, a := range c.args {
		if a == flag && i+1 < len(c.args) {
			return c.args[i+1]
		}
	}
	t.Fatalf("no %s in %q", flag, c.args)
	return ""
}

func newFake(t *testing.T, fn func(n int, c call) ([]byte, []byte, error)) (*Converter, *fakeRunner) {
	t.Helper()
	f := &fakeRunner{fn: fn}
	c, err := New(Options{
		PandocBin:   fakePandoc,
		TextutilBin: fakeTextutil,
		Timeout:     time.Minute,
		Limits:      testLimits,
		Run:         f.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}

func validDocx(t *testing.T) []byte {
	t.Helper()
	return mustZip(t, []entry{plainDoc()})
}

func writeAt(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestNew(t *testing.T) {
	ok := Options{PandocBin: "pandoc", TextutilBin: "textutil", Timeout: time.Second, Limits: testLimits}
	tests := []struct {
		name   string
		mutate func(*Options)
		wantOK bool
	}{
		{"valid", func(*Options) {}, true},
		{"no pandoc", func(o *Options) { o.PandocBin = "" }, false},
		{"no textutil", func(o *Options) { o.TextutilBin = "" }, false},
		{"zero timeout", func(o *Options) { o.Timeout = 0 }, false},
		{"negative timeout", func(o *Options) { o.Timeout = -time.Second }, false},
		{"zero limits", func(o *Options) { o.Limits = InspectLimits{} }, false},
		{"zero entries", func(o *Options) { o.Limits.MaxEntries = 0 }, false},
		{"zero part bytes", func(o *Options) { o.Limits.MaxPartBytes = 0 }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := ok
			tt.mutate(&o)
			c, err := New(o)
			if (err == nil) != tt.wantOK {
				t.Fatalf("New() error = %v, want ok = %v", err, tt.wantOK)
			}
			if tt.wantOK && c.run == nil {
				t.Error("nil Run must be replaced by the exec runner")
			}
		})
	}
}

func TestToolError(t *testing.T) {
	inner := errors.New("exit status 2")
	e := &ToolError{Tool: "pandoc", Output: "bad input", Err: inner}
	if !errors.Is(e, inner) {
		t.Error("ToolError must unwrap to its cause")
	}
	if got := e.Error(); !strings.Contains(got, "pandoc") || !strings.Contains(got, "bad input") || !strings.Contains(got, "exit status 2") {
		t.Errorf("Error() = %q", got)
	}
	if got := (&ToolError{Tool: "textutil", Err: inner}).Error(); strings.HasSuffix(got, ": ") {
		t.Errorf("Error() without output ends in a dangling separator: %q", got)
	}
}

func TestBoundOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want func(string) bool
	}{
		{"trimmed", "  boom \n\n", func(s string) bool { return s == "boom" }},
		{"empty", "", func(s string) bool { return s == "" }},
		{"bounded", strings.Repeat("x", 3*maxToolOutput), func(s string) bool {
			return strings.HasPrefix(s, "xxx") && strings.HasSuffix(s, "…") && len(s) <= maxToolOutput+len("…")
		}},
		{"not cut inside a rune", strings.Repeat("č", maxToolOutput), func(s string) bool {
			return strings.HasSuffix(s, "…") && strings.ToValidUTF8(s, "") == s
		}},
		{"invalid UTF-8", "a\xffb", func(s string) bool { return s == "a�b" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boundOutput([]byte(tt.in)); !tt.want(got) {
				t.Errorf("boundOutput(%.20q...) = %.60q (len %d)", tt.in, got, len(got))
			}
		})
	}
}

func TestExecRunner(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()

	t.Run("stderr and exit code", func(t *testing.T) {
		_, stderr, err := execRunner(t.Context(), dir, sh, "-c", "echo oops >&2; exit 3")
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 3 {
			t.Fatalf("err = %v, want exit code 3", err)
		}
		if strings.TrimSpace(string(stderr)) != "oops" {
			t.Errorf("stderr = %q", stderr)
		}
	})
	t.Run("runs in dir", func(t *testing.T) {
		stdout, _, err := execRunner(t.Context(), dir, sh, "-c", "pwd -P")
		if err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(stdout)); got != want {
			t.Errorf("working directory = %q, want %q", got, want)
		}
	})
	t.Run("output is capped", func(t *testing.T) {
		_, stderr, err := execRunner(t.Context(), dir, sh, "-c", "head -c 100000 /dev/zero >&2")
		if err != nil {
			t.Fatal(err)
		}
		if len(stderr) != maxToolOutput {
			t.Errorf("collected %d bytes, want %d", len(stderr), maxToolOutput)
		}
	})
	t.Run("arguments are not parsed by a shell", func(t *testing.T) {
		stdout, _, err := execRunner(t.Context(), dir, "echo", "a; touch pwned", "$(id)")
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(stdout)); got != "a; touch pwned $(id)" {
			t.Errorf("stdout = %q", got)
		}
		if exists(filepath.Join(dir, "pwned")) {
			t.Error("an argument was run as a command")
		}
	})
	t.Run("missing program", func(t *testing.T) {
		for _, bin := range []string{"definitely-not-installed-xyz", filepath.Join(dir, "nope")} {
			_, _, err := execRunner(t.Context(), dir, bin)
			if !toolMissing(err, bin) {
				t.Errorf("toolMissing(%v, %q) = false", err, bin)
			}
		}
	})
	t.Run("missing working folder is not a missing tool", func(t *testing.T) {
		_, _, err := execRunner(t.Context(), filepath.Join(dir, "gone"), sh, "-c", "true")
		if err == nil {
			t.Fatal("expected an error")
		}
		if toolMissing(err, sh) {
			t.Errorf("toolMissing(%v) = true for a missing folder", err)
		}
	})
}

func TestDocxToMarkdownCalls(t *testing.T) {
	long := strings.Repeat("a", 3*64<<10)
	tests := []struct {
		name      string
		outputs   []string // doc.md written by the 1st, 2nd... call
		wantCalls int
		wantLoss  bool // a table is still raw HTML after the last pass
	}{
		{"plain markdown", []string{"# Naslov\n\ntekst\n"}, 1, false},
		{"table at the start of a line", []string{"<table>\n", "| a |\n"}, 2, false},
		{"table after other lines", []string{"tekst\n\n<table style=\"width:48%;\">\n</table>\n", "ok\n"}, 2, false},
		{"table on the last line without newline", []string{"tekst\n<table>", "ok\n"}, 2, false},
		{"text before the tag", []string{"tekst <table>\n"}, 1, false},
		{"escaped tag", []string{"\\<table border=1>\n"}, 1, false},
		{"indented tag", []string{"    <table>\n"}, 1, false},
		{"other tag with the same start", []string{"<tab>\n"}, 1, false},
		{"long line, then a table", []string{long + "\n<table>\n", "ok\n"}, 2, false},
		{"long line that merely contains the tag", []string{long + "<table>\n"}, 1, false},
		{"long line that starts with the tag", []string{"<table>" + long + "\n", "ok\n"}, 2, false},
		{"grid dialect still gives a table: no third try, loss reported", []string{"<table>\n", "<table>\n"}, 2, true},
		{"grid dialect fixes it: no loss", []string{"<table>\n", "+---+\n"}, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(t.TempDir(), "in.docx")
			writeAt(t, src, validDocx(t))
			c, f := newFake(t, func(n int, c call) ([]byte, []byte, error) {
				out := tt.outputs[min(n, len(tt.outputs))-1]
				return nil, nil, os.WriteFile(filepath.Join(c.dir, argAfter(t, c, "-o")), []byte(out), 0o600)
			})

			losses, err := c.DocxToMarkdown(t.Context(), src, dir)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantLoss != (losses != nil) || (tt.wantLoss && !slices.Equal(losses, []Loss{LossTables})) {
				t.Errorf("losses = %v, want loss = %v", losses, tt.wantLoss)
			}
			if !exists(filepath.Join(dir, MarkdownName)) {
				t.Error("doc.md is gone")
			}
			if len(f.calls) != tt.wantCalls {
				t.Fatalf("pandoc ran %d times, want %d", len(f.calls), tt.wantCalls)
			}
			wantFormats := []string{formatPipeTables, formatGridTables}
			for i, got := range f.calls {
				want := []string{"-f", "docx", "-t", wantFormats[i], "--wrap=none", "--extract-media=.",
					"--lua-filter=" + filepath.Join(dir, flattenLinksName), "-o", MarkdownName, src}
				if got.name != fakePandoc || got.dir != dir || strings.Join(got.args, "\x00") != strings.Join(want, "\x00") {
					t.Errorf("call %d = {%q %q %q}, want {%q %q %q}", i+1, got.name, got.dir, got.args, fakePandoc, dir, want)
				}
			}
		})
	}
}

func TestDocxToMarkdownArguments(t *testing.T) {
	t.Run("relative paths become absolute", func(t *testing.T) {
		base := t.TempDir()
		t.Chdir(base)
		writeAt(t, filepath.Join(base, "-evil.docx"), validDocx(t))
		if err := os.Mkdir(filepath.Join(base, "work"), 0o700); err != nil {
			t.Fatal(err)
		}
		c, f := newFake(t, func(_ int, c call) ([]byte, []byte, error) {
			return nil, nil, os.WriteFile(filepath.Join(c.dir, MarkdownName), []byte("x\n"), 0o600)
		})
		if _, err := c.DocxToMarkdown(t.Context(), "-evil.docx", "work"); err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(base, "-evil.docx")
		last := f.calls[0].args[len(f.calls[0].args)-1]
		if last != want {
			t.Errorf("input argument = %q, want %q", last, want)
		}
		if f.calls[0].dir != filepath.Join(base, "work") {
			t.Errorf("dir = %q", f.calls[0].dir)
		}
	})

	t.Run("bad arguments never reach the tool", func(t *testing.T) {
		dir := t.TempDir()
		good := filepath.Join(t.TempDir(), "in.docx")
		writeAt(t, good, validDocx(t))
		file := filepath.Join(t.TempDir(), "file")
		writeAt(t, file, []byte("x"))
		tests := []struct{ name, src, dir string }{
			{"empty src", "", dir},
			{"empty dir", good, ""},
			{"missing src", filepath.Join(dir, "missing.docx"), dir},
			{"src is a directory", dir, dir},
			{"missing dir", good, filepath.Join(dir, "missing")},
			{"dir is a file", good, file},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
				if _, err := c.DocxToMarkdown(t.Context(), tt.src, tt.dir); err == nil {
					t.Error("expected an error")
				}
				if len(f.calls) != 0 {
					t.Errorf("the tool ran %d times", len(f.calls))
				}
			})
		}
	})
}

func TestDocxToMarkdownRefusesExistingMarkdown(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.docx")
	writeAt(t, src, validDocx(t))

	t.Run("an old doc.md", func(t *testing.T) {
		dir := t.TempDir()
		md := filepath.Join(dir, MarkdownName)
		writeAt(t, md, []byte("stale"))
		c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
		losses, err := c.DocxToMarkdown(t.Context(), src, dir)
		if !errors.Is(err, fs.ErrExist) || losses != nil {
			t.Errorf("losses, err = %v, %v; want nil, fs.ErrExist", losses, err)
		}
		if len(f.calls) != 0 {
			t.Error("the tool ran")
		}
		sameBytes(t, md, []byte("stale"))
	})

	t.Run("a dangling symlink named doc.md", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, MarkdownName)); err != nil {
			t.Skipf("no symlinks: %v", err)
		}
		c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
		if _, err := c.DocxToMarkdown(t.Context(), src, dir); !errors.Is(err, fs.ErrExist) || len(f.calls) != 0 {
			t.Errorf("err = %v, %d calls", err, len(f.calls))
		}
	})

	t.Run("an existing media folder is not a reason", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, MediaDirName), 0o700); err != nil {
			t.Fatal(err)
		}
		c, f := newFake(t, func(_ int, c call) ([]byte, []byte, error) {
			return nil, nil, os.WriteFile(filepath.Join(c.dir, MarkdownName), []byte("x\n"), 0o600)
		})
		if _, err := c.DocxToMarkdown(t.Context(), src, dir); err != nil || len(f.calls) != 1 {
			t.Errorf("err = %v, %d calls", err, len(f.calls))
		}
	})
}

// filterArgs returns the filter path of every --lua-filter argument of a call.
func filterArgs(c call) []string {
	var out []string
	for _, a := range c.args {
		if p, ok := strings.CutPrefix(a, "--lua-filter="); ok {
			out = append(out, p)
		}
	}
	return out
}

// The filter is on the command line of both passes, is the embedded one, is
// there while pandoc runs, and is gone when the call returns.
func TestDocxToMarkdownFilter(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.docx")
	writeAt(t, src, validDocx(t))
	outputs := []string{"<table>\n", "ok\n"} // the first pass asks for the second
	dir := t.TempDir()
	want := filepath.Join(dir, flattenLinksName)

	c, f := newFake(t, func(n int, c call) ([]byte, []byte, error) {
		got := filterArgs(c)
		if len(got) != 1 || got[0] != want {
			t.Errorf("pass %d: filters = %q, want [%q]", n, got, want)
		}
		body, err := os.ReadFile(want)
		if err != nil {
			t.Errorf("pass %d: the filter is not in the folder: %v", n, err)
		} else if !slices.Equal(body, flattenLinksLua) {
			t.Errorf("pass %d: the filter in the folder is not the embedded one", n)
		}
		if info, err := os.Stat(want); err == nil && info.Mode().Perm() != 0o600 {
			t.Errorf("pass %d: filter mode = %v, want 0600", n, info.Mode().Perm())
		}
		return nil, nil, os.WriteFile(filepath.Join(c.dir, argAfter(t, c, "-o")), []byte(outputs[n-1]), 0o600)
	})
	if _, err := c.DocxToMarkdown(t.Context(), src, dir); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("pandoc ran %d times, want 2 (pipe and grid)", len(f.calls))
	}
	if exists(want) {
		t.Error("the filter is still in the folder")
	}
	if got := listDir(t, dir); !slices.Equal(got, []string{MarkdownName}) {
		t.Errorf("folder = %q, want only doc.md", got)
	}
}

// Whatever ends the call, the filter does not stay behind.
func TestDocxToMarkdownRemovesFilter(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.docx")
	writeAt(t, src, validDocx(t))
	tests := []struct {
		name string
		fn   func(ctx context.Context, cancel context.CancelFunc, c call) ([]byte, []byte, error)
	}{
		{"pandoc fails", func(_ context.Context, _ context.CancelFunc, _ call) ([]byte, []byte, error) {
			return nil, []byte("boom"), errors.New("exit status 1")
		}},
		{"pandoc succeeds and writes nothing", func(_ context.Context, _ context.CancelFunc, _ call) ([]byte, []byte, error) {
			return nil, nil, nil
		}},
		{"canceled while pandoc runs", func(_ context.Context, cancel context.CancelFunc, _ call) ([]byte, []byte, error) {
			cancel()
			return nil, nil, context.Canceled
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c, f := newFake(t, func(_ int, c call) ([]byte, []byte, error) { return tt.fn(ctx, cancel, c) })
			if _, err := c.DocxToMarkdown(ctx, src, dir); err == nil {
				t.Fatal("expected an error")
			}
			if len(f.calls) != 1 {
				t.Errorf("pandoc ran %d times", len(f.calls))
			}
			if got := listDir(t, dir); len(got) != 0 {
				t.Errorf("folder = %q, want it empty", got)
			}
		})
	}
	t.Run("canceled before pandoc starts", func(t *testing.T) {
		dir := t.TempDir()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
		if _, err := c.DocxToMarkdown(ctx, src, dir); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		if len(f.calls) != 0 || len(listDir(t, dir)) != 0 {
			t.Errorf("%d calls, folder %q", len(f.calls), listDir(t, dir))
		}
	})
}

// A file of the filter's name from an earlier call is the package's own: it is
// replaced, never trusted and never written through.
func TestDocxToMarkdownReplacesExistingFilter(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.docx")
	writeAt(t, src, validDocx(t))
	for _, name := range []string{"a stale file", "a symlink"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			filter := filepath.Join(dir, flattenLinksName)
			var elsewhere string
			if name == "a symlink" {
				elsewhere = filepath.Join(t.TempDir(), "elsewhere")
				writeAt(t, elsewhere, []byte("not ours"))
				if err := os.Symlink(elsewhere, filter); err != nil {
					t.Skipf("no symlinks: %v", err)
				}
			} else {
				writeAt(t, filter, []byte("-- stale, and not the filter\n"))
			}
			c, _ := newFake(t, func(_ int, c call) ([]byte, []byte, error) {
				if body, err := os.ReadFile(filter); err != nil || !slices.Equal(body, flattenLinksLua) {
					t.Errorf("the filter during the run: %q, %v", body, err)
				}
				return nil, nil, os.WriteFile(filepath.Join(c.dir, MarkdownName), []byte("x\n"), 0o600)
			})
			if _, err := c.DocxToMarkdown(t.Context(), src, dir); err != nil {
				t.Fatal(err)
			}
			if exists(filter) {
				t.Error("the filter is still in the folder")
			}
			if elsewhere != "" {
				sameBytes(t, elsewhere, []byte("not ours"))
			}
		})
	}
	t.Run("a folder of that name is not replaced", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(dir, flattenLinksName)
		if err := os.MkdirAll(filepath.Join(blocker, "inner"), 0o700); err != nil {
			t.Fatal(err)
		}
		c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
		if _, err := c.DocxToMarkdown(t.Context(), src, dir); err == nil {
			t.Fatal("expected an error")
		}
		if len(f.calls) != 0 {
			t.Error("pandoc ran")
		}
		if !exists(filepath.Join(blocker, "inner")) {
			t.Error("the folder that was in the way is gone")
		}
	})
}

// Failing to remove the filter is reported, next to the failure of the
// conversion if there is one, and never swallowed.
func TestDocxToMarkdownFilterRemovalFails(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.docx")
	writeAt(t, src, validDocx(t))
	// Pandoc "leaves" a folder with something in it where the filter was, so
	// that removing it fails.
	blockFilter := func(t *testing.T, c call) {
		t.Helper()
		p := filterArgs(c)[0]
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(p, "inner"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("after a conversion that worked", func(t *testing.T) {
		dir := t.TempDir()
		c, _ := newFake(t, func(_ int, c call) ([]byte, []byte, error) {
			blockFilter(t, c)
			return nil, nil, os.WriteFile(filepath.Join(c.dir, MarkdownName), []byte("x\n"), 0o600)
		})
		losses, err := c.DocxToMarkdown(t.Context(), src, dir)
		if err == nil || !strings.Contains(err.Error(), "remove filter") {
			t.Fatalf("err = %v, want the failure to remove the filter", err)
		}
		if losses != nil {
			t.Errorf("losses = %v", losses)
		}
		if !exists(filepath.Join(dir, MarkdownName)) {
			t.Error("doc.md is gone")
		}
	})
	t.Run("after a conversion that failed", func(t *testing.T) {
		dir := t.TempDir()
		c, _ := newFake(t, func(_ int, c call) ([]byte, []byte, error) {
			blockFilter(t, c)
			return nil, []byte("boom"), errors.New("exit status 1")
		})
		_, err := c.DocxToMarkdown(t.Context(), src, dir)
		var te *ToolError
		if !errors.As(err, &te) || !strings.Contains(err.Error(), "remove filter") {
			t.Fatalf("err = %v, want the tool error and the failure to remove the filter", err)
		}
	})
}

func TestDocxToMarkdownNoOutput(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.docx")
	writeAt(t, src, validDocx(t))
	dir := t.TempDir()
	c, _ := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil }) // "succeeds", writes nothing
	_, err := c.DocxToMarkdown(t.Context(), src, dir)
	if !errors.Is(err, errNoOutput) {
		t.Errorf("err = %v, want errNoOutput", err)
	}
}

func TestMarkdownToDocxCalls(t *testing.T) {
	work := t.TempDir()
	writeAt(t, filepath.Join(work, MarkdownName), []byte("# x\n"))
	ref := filepath.Join(t.TempDir(), "reference.docx")
	writeAt(t, ref, validDocx(t))
	dst := filepath.Join(t.TempDir(), "out.docx")
	docx := validDocx(t)

	c, f := newFake(t, func(_ int, c call) ([]byte, []byte, error) {
		return nil, nil, os.WriteFile(argAfter(t, c, "-o"), docx, 0o600)
	})
	if err := c.MarkdownToDocx(t.Context(), work, ref, dst); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-f", "markdown-smart", "-t", "docx",
		"--reference-doc=" + ref, "--resource-path=" + work,
		"-o", dst, filepath.Join(work, MarkdownName),
	}
	if len(f.calls) != 1 {
		t.Fatalf("%d calls", len(f.calls))
	}
	got := f.calls[0]
	if got.name != fakePandoc || got.dir != work || strings.Join(got.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("call = {%q %q %q}, want {%q %q %q}", got.name, got.dir, got.args, fakePandoc, work, want)
	}
}

func TestDocToDocxCalls(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "stari.doc")
	original := []byte("\xd0\xcf\x11\xe0 not really a .doc")
	writeAt(t, src, original)
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "novi.docx")
	docx := validDocx(t)

	c, f := newFake(t, func(_ int, c call) ([]byte, []byte, error) {
		return nil, nil, os.WriteFile(argAfter(t, c, "-output"), docx, 0o600)
	})
	if err := c.DocToDocx(t.Context(), src, dst); err != nil {
		t.Fatal(err)
	}
	want := []string{"-convert", "docx", "-output", dst, src}
	got := f.calls[0]
	if got.name != fakeTextutil || got.dir != dstDir || strings.Join(got.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("call = {%q %q %q}, want {%q %q %q}", got.name, got.dir, got.args, fakeTextutil, dstDir, want)
	}
	assertUnchanged(t, src, original)
	if entries, err := os.ReadDir(srcDir); err != nil || len(entries) != 1 {
		t.Errorf("the source folder changed: %v %v", entries, err)
	}
}

// conversion is one of the three that write a file, so the failure paths can
// be driven through the same table.
type conversion struct {
	name string
	bin  string
	// setup makes the inputs and returns the function to run and the output
	// file the conversion is supposed to leave.
	setup func(t *testing.T, c *Converter) (run func(context.Context) error, out string)
	// write is what a successful tool run does to the output file.
	write func(t *testing.T, c call, data []byte) error
}

func conversions() []conversion {
	return []conversion{
		{
			name: "MarkdownToDocx", bin: fakePandoc,
			setup: func(t *testing.T, c *Converter) (func(context.Context) error, string) {
				work := t.TempDir()
				writeAt(t, filepath.Join(work, MarkdownName), []byte("# x\n"))
				ref := filepath.Join(t.TempDir(), "reference.docx")
				writeAt(t, ref, validDocx(t))
				dst := filepath.Join(t.TempDir(), "out.docx")
				return func(ctx context.Context) error { return c.MarkdownToDocx(ctx, work, ref, dst) }, dst
			},
			write: func(t *testing.T, c call, data []byte) error {
				return os.WriteFile(argAfter(t, c, "-o"), data, 0o600)
			},
		},
		{
			name: "DocToDocx", bin: fakeTextutil,
			setup: func(t *testing.T, c *Converter) (func(context.Context) error, string) {
				src := filepath.Join(t.TempDir(), "stari.doc")
				writeAt(t, src, []byte("old"))
				dst := filepath.Join(t.TempDir(), "novi.docx")
				return func(ctx context.Context) error { return c.DocToDocx(ctx, src, dst) }, dst
			},
			write: func(t *testing.T, c call, data []byte) error {
				return os.WriteFile(argAfter(t, c, "-output"), data, 0o600)
			},
		},
	}
}

func TestConversionFailures(t *testing.T) {
	noDocument := mustZip(t, []entry{{"word/styles.xml", part("styles", "")}})
	missingErr := &exec.Error{Name: "tool", Err: exec.ErrNotFound}
	absentErr := &fs.PathError{Op: "fork/exec", Path: "", Err: syscall.ENOENT} // Path is set per tool below

	tests := []struct {
		name string
		// act is what the fake tool does; it may write to the output first.
		act     func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error)
		check   func(t *testing.T, err error)
		wantOut bool // the output file may stay
	}{
		{
			name: "non-zero exit code carries stderr",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				return nil, []byte("  something broke \n"), errors.New("exit status 1")
			},
			check: func(t *testing.T, err error) {
				var te *ToolError
				if !errors.As(err, &te) {
					t.Fatalf("err = %v, want *ToolError", err)
				}
				if te.Output != "something broke" {
					t.Errorf("Output = %q", te.Output)
				}
				if errors.Is(err, ErrToolMissing) {
					t.Error("a failed run is not a missing tool")
				}
			},
		},
		{
			name: "long stderr is bounded",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				return nil, []byte(strings.Repeat("e", 10*maxToolOutput)), errors.New("exit status 1")
			},
			check: func(t *testing.T, err error) {
				var te *ToolError
				if !errors.As(err, &te) || len(te.Output) > maxToolOutput+len("…") {
					t.Fatalf("err = %v", err)
				}
			},
		},
		{
			name: "partial output is removed after a failure",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				if err := cv.write(t, c, []byte("PK partial")); err != nil {
					return nil, nil, err
				}
				return nil, []byte("died"), errors.New("exit status 1")
			},
			check: func(t *testing.T, err error) {
				var te *ToolError
				if !errors.As(err, &te) {
					t.Fatalf("err = %v, want *ToolError", err)
				}
			},
		},
		{
			name: "tool not found in PATH",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				return nil, nil, missingErr
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrToolMissing) || !errors.Is(err, exec.ErrNotFound) {
					t.Errorf("err = %v, want ErrToolMissing wrapping ErrNotFound", err)
				}
			},
		},
		{
			name: "tool path does not exist",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				e := *absentErr
				e.Path = bin
				return nil, nil, &e
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrToolMissing) {
					t.Errorf("err = %v, want ErrToolMissing", err)
				}
			},
		},
		{
			name: "missing working folder is not a missing tool",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				return nil, nil, &fs.PathError{Op: "chdir", Path: c.dir, Err: syscall.ENOENT}
			},
			check: func(t *testing.T, err error) {
				var te *ToolError
				if errors.Is(err, ErrToolMissing) || !errors.As(err, &te) {
					t.Errorf("err = %v, want *ToolError and not ErrToolMissing", err)
				}
			},
		},
		{
			name: "success without an output file",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				return nil, nil, nil
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, errNoOutput) {
					t.Errorf("err = %v, want errNoOutput", err)
				}
			},
		},
		{
			name: "success with an empty output file",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				return nil, nil, cv.write(t, c, nil)
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, errNoOutput) {
					t.Errorf("err = %v, want errNoOutput", err)
				}
			},
		},
		{
			name: "success with output that is not an archive",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				return nil, nil, cv.write(t, c, []byte("this is plain text"))
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrNotDocx) {
					t.Errorf("err = %v, want ErrNotDocx", err)
				}
			},
		},
		{
			name: "success with an archive that has no word/document.xml",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				return nil, nil, cv.write(t, c, noDocument)
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrNotDocx) {
					t.Errorf("err = %v, want ErrNotDocx", err)
				}
			},
		},
		{
			name: "success with an archive over the limit",
			act: func(t *testing.T, cv conversion, c call, bin string) ([]byte, []byte, error) {
				entries := []entry{plainDoc()}
				for i := range testLimits.MaxEntries {
					entries = append(entries, entry{"word/x" + strings.Repeat("a", i%5) + string(rune('a'+i%26)) + ".bin", ""})
				}
				return nil, nil, cv.write(t, c, mustZip(t, entries))
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrTooLarge) {
					t.Errorf("err = %v, want ErrTooLarge", err)
				}
			},
		},
	}

	for _, cv := range conversions() {
		for _, tt := range tests {
			t.Run(cv.name+"/"+tt.name, func(t *testing.T) {
				var out string
				var runFn func(context.Context) error
				c, f := newFake(t, func(_ int, c call) ([]byte, []byte, error) {
					return tt.act(t, cv, c, cv.bin)
				})
				runFn, out = cv.setup(t, c)

				err := runFn(t.Context())
				if err == nil {
					t.Fatal("expected an error")
				}
				tt.check(t, err)
				if len(f.calls) != 1 {
					t.Errorf("the tool ran %d times", len(f.calls))
				}
				if exists(out) {
					t.Error("the output of a failed conversion was left behind")
				}
			})
		}
	}
}

func TestConversionContext(t *testing.T) {
	for _, cv := range conversions() {
		t.Run(cv.name+"/timeout", func(t *testing.T) {
			var gotDeadline bool
			c, err := New(Options{
				PandocBin: fakePandoc, TextutilBin: fakeTextutil, Limits: testLimits,
				Timeout: 20 * time.Millisecond,
				Run: func(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
					_, gotDeadline = ctx.Deadline()
					<-ctx.Done() // a tool that never finishes
					return nil, []byte("killed"), errors.New("signal: killed")
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			runFn, out := cv.setup(t, c)
			err = runFn(t.Context())
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("err = %v, want DeadlineExceeded", err)
			}
			var te *ToolError
			if errors.As(err, &te) {
				t.Errorf("a timeout must not look like a tool failure: %v", err)
			}
			if !gotDeadline {
				t.Error("the call had no deadline")
			}
			if exists(out) {
				t.Error("output left behind")
			}
		})

		t.Run(cv.name+"/cancelled while running", func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c, _ := newFake(t, func(int, call) ([]byte, []byte, error) {
				cancel()
				return nil, nil, errors.New("signal: killed")
			})
			runFn, _ := cv.setup(t, c)
			err := runFn(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("err = %v, want Canceled", err)
			}
		})

		t.Run(cv.name+"/cancelled before", func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			c, _ := newFake(t, func(int, call) ([]byte, []byte, error) {
				return nil, nil, errors.New("must not matter")
			})
			runFn, out := cv.setup(t, c)
			if err := runFn(ctx); !errors.Is(err, context.Canceled) {
				t.Errorf("err = %v, want Canceled", err)
			}
			if exists(out) {
				t.Error("output left behind")
			}
		})
	}

	t.Run("DocxToMarkdown/timeout", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "in.docx")
		writeAt(t, src, validDocx(t))
		c, err := New(Options{
			PandocBin: fakePandoc, TextutilBin: fakeTextutil, Limits: testLimits, Timeout: 20 * time.Millisecond,
			Run: func(ctx context.Context, _, _ string, _ ...string) ([]byte, []byte, error) {
				<-ctx.Done()
				return nil, nil, errors.New("signal: killed")
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.DocxToMarkdown(t.Context(), src, t.TempDir()); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want DeadlineExceeded", err)
		}
	})

	t.Run("DocxToMarkdown/cancelled before", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "in.docx")
		writeAt(t, src, validDocx(t))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
		if _, err := c.DocxToMarkdown(ctx, src, t.TempDir()); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want Canceled", err)
		}
		if len(f.calls) != 0 {
			t.Error("the tool ran with a cancelled context")
		}
	})
}

func TestDocxToMarkdownToolFailures(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.docx")
	writeAt(t, src, validDocx(t))
	tests := []struct {
		name  string
		err   error
		check func(error) bool
	}{
		{"exit code", errors.New("exit status 63"), func(err error) bool {
			var te *ToolError
			return errors.As(err, &te) && te.Tool == "pandoc" && te.Output == "boom"
		}},
		{"missing", &exec.Error{Name: "pandoc", Err: exec.ErrNotFound}, func(err error) bool {
			return errors.Is(err, ErrToolMissing)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, []byte("boom\n"), tt.err })
			if _, err := c.DocxToMarkdown(t.Context(), src, t.TempDir()); !tt.check(err) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestOutputPathRules(t *testing.T) {
	docx := validDocx(t)
	for _, cv := range conversions() {
		write := func(c call) error { return cv.write(t, c, docx) }

		t.Run(cv.name+"/refuses an existing output", func(t *testing.T) {
			c, f := newFake(t, func(_ int, c call) ([]byte, []byte, error) { return nil, nil, write(c) })
			runFn, out := cv.setup(t, c)
			writeAt(t, out, []byte("the user's precious file"))
			err := runFn(t.Context())
			if !errors.Is(err, fs.ErrExist) {
				t.Errorf("err = %v, want fs.ErrExist", err)
			}
			assertUnchanged(t, out, []byte("the user's precious file"))
			if len(f.calls) != 0 {
				t.Error("the tool ran")
			}
		})

		t.Run(cv.name+"/refuses a symlink as output", func(t *testing.T) {
			c, f := newFake(t, func(_ int, c call) ([]byte, []byte, error) { return nil, nil, write(c) })
			runFn, out := cv.setup(t, c)
			target := filepath.Join(t.TempDir(), "target")
			if err := os.Symlink(target, out); err != nil {
				t.Skipf("no symlinks: %v", err)
			}
			if err := runFn(t.Context()); !errors.Is(err, fs.ErrExist) {
				t.Errorf("err = %v, want fs.ErrExist", err)
			}
			if exists(target) || len(f.calls) != 0 {
				t.Error("something was written through the link")
			}
		})

		t.Run(cv.name+"/accepts an empty placeholder", func(t *testing.T) {
			c, _ := newFake(t, func(_ int, c call) ([]byte, []byte, error) { return nil, nil, write(c) })
			runFn, out := cv.setup(t, c)
			writeAt(t, out, nil)
			if err := runFn(t.Context()); err != nil {
				t.Fatal(err)
			}
			if info, err := os.Stat(out); err != nil || info.Size() == 0 {
				t.Errorf("output not written: %v", err)
			}
		})
	}

	t.Run("MarkdownToDocx refuses its own inputs as output", func(t *testing.T) {
		work := t.TempDir()
		md := filepath.Join(work, MarkdownName)
		writeAt(t, md, []byte("# x\n"))
		ref := filepath.Join(t.TempDir(), "reference.docx")
		writeAt(t, ref, docx)
		c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
		for _, dst := range []string{ref, md} {
			if err := c.MarkdownToDocx(t.Context(), work, ref, dst); err == nil {
				t.Errorf("dst %s accepted", dst)
			}
		}
		assertUnchanged(t, ref, docx)
		assertUnchanged(t, md, []byte("# x\n"))
		if len(f.calls) != 0 {
			t.Error("the tool ran")
		}
	})

	t.Run("DocToDocx refuses the source as output", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "stari.doc")
		writeAt(t, src, []byte("old"))
		c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
		if err := c.DocToDocx(t.Context(), src, src); err == nil {
			t.Error("expected an error")
		}
		assertUnchanged(t, src, []byte("old"))
		if len(f.calls) != 0 {
			t.Error("the tool ran")
		}
	})

	t.Run("MarkdownToDocx needs its inputs", func(t *testing.T) {
		ref := filepath.Join(t.TempDir(), "reference.docx")
		writeAt(t, ref, docx)
		dst := filepath.Join(t.TempDir(), "out.docx")
		workWithoutMarkdown := t.TempDir()
		c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
		if err := c.MarkdownToDocx(t.Context(), workWithoutMarkdown, ref, dst); err == nil {
			t.Error("no doc.md: expected an error")
		}
		work := t.TempDir()
		writeAt(t, filepath.Join(work, MarkdownName), []byte("x"))
		if err := c.MarkdownToDocx(t.Context(), work, filepath.Join(t.TempDir(), "missing.docx"), dst); err == nil {
			t.Error("no reference: expected an error")
		}
		if err := c.MarkdownToDocx(t.Context(), "", ref, dst); err == nil {
			t.Error("empty dir: expected an error")
		}
		if len(f.calls) != 0 {
			t.Error("the tool ran")
		}
	})

	t.Run("DocToDocx needs its input", func(t *testing.T) {
		c, f := newFake(t, func(int, call) ([]byte, []byte, error) { return nil, nil, nil })
		if err := c.DocToDocx(t.Context(), filepath.Join(t.TempDir(), "missing.doc"), filepath.Join(t.TempDir(), "o.docx")); err == nil {
			t.Error("expected an error")
		}
		if len(f.calls) != 0 {
			t.Error("the tool ran")
		}
	})
}

// A fast timeout would not prove that the deadline is per command, so this
// checks the other half: every call of the two-pass read gets its own.
func TestTimeoutIsPerCommand(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.docx")
	writeAt(t, src, validDocx(t))
	var deadlines []time.Time
	c, err := New(Options{
		PandocBin: fakePandoc, TextutilBin: fakeTextutil, Limits: testLimits, Timeout: time.Hour,
		Run: func(ctx context.Context, dir, _ string, _ ...string) ([]byte, []byte, error) {
			d, ok := ctx.Deadline()
			if !ok {
				t.Error("no deadline")
			}
			deadlines = append(deadlines, d)
			content := "<table>\n"
			if len(deadlines) == 2 {
				content = "ok\n"
			}
			return nil, nil, os.WriteFile(filepath.Join(dir, MarkdownName), []byte(content), 0o600)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DocxToMarkdown(t.Context(), src, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(deadlines) != 2 || !deadlines[1].After(deadlines[0]) {
		t.Errorf("deadlines = %v", deadlines)
	}
}

// The default runner against a process that never ends: the timeout kills it
// and the error is the context's, not a ToolError.
func TestDefaultRunnerTimeout(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "slow-tool")
	// exec: the shell is replaced by sleep, so killing the process ends everything.
	writeAt(t, script, []byte("#!"+sh+"\nexec sleep 30\n"))
	if err := os.Chmod(script, 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "in.docx")
	writeAt(t, src, validDocx(t))
	work := t.TempDir()

	c, err := New(Options{PandocBin: script, TextutilBin: script, Timeout: 100 * time.Millisecond, Limits: testLimits})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = c.DocxToMarkdown(t.Context(), src, work)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("the tool was not stopped: %v", d)
	}
}
