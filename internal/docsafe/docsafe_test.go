package docsafe

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"Pisar/internal/project"
)

type call struct {
	name string
	args []string
}

// fakeRun records calls and runs fn (may be nil) as the "tool".
type fakeRun struct {
	calls []call
	fn    func(ctx context.Context, args []string) ([]byte, error)
}

func (f *fakeRun) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name, args})
	if f.fn == nil {
		return nil, nil
	}
	return f.fn(ctx, args)
}

func newOps(t *testing.T, f *fakeRun) *Ops {
	t.Helper()
	o, err := New(Options{TrashBin: "/fake/trash", Timeout: time.Second, Run: f.run})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func write(t *testing.T, p, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func caseSensitiveFS(t *testing.T) bool {
	t.Helper()
	d := t.TempDir()
	write(t, filepath.Join(d, "x"), "", 0o644)
	_, err := os.Lstat(filepath.Join(d, "X"))
	return err != nil
}

func TestNew(t *testing.T) {
	for _, o := range []Options{
		{Timeout: time.Second},
		{TrashBin: "t"},
		{TrashBin: "t", Timeout: -time.Second},
	} {
		if _, err := New(o); err == nil {
			t.Errorf("New(%+v) = nil error", o)
		}
	}
	if o, err := New(Options{TrashBin: "t", Timeout: time.Second}); err != nil || o.run == nil {
		t.Errorf("default runner not set: %v", err)
	}
}

func TestExecRunner(t *testing.T) {
	out, err := ExecRunner(context.Background(), "/bin/echo", "-n", "hi")
	if err != nil || string(out) != "hi" {
		t.Fatalf("got %q, %v", out, err)
	}
	if _, err := ExecRunner(context.Background(), "/nonexistent/pisar-cmd"); err == nil {
		t.Fatal("want error for missing command")
	}
}

var badNames = []string{
	"", ".", "..", ".skriveno", "~$x", "a/b", "a:b", "a\x00b", "a\nb", "a\tb",
	" a", "a ", strings.Repeat("a", 256),
}

func TestCreateFile(t *testing.T) {
	ok := []struct{ in, want string }{
		{"Bilješke", "Bilješke.md"},
		{"popis.txt", "popis.txt"},
		{"Plan.MD", "Plan.MD"},
		{"v1.2", "v1.2.md"},
		{"moj čćšžđ dokument", "moj čćšžđ dokument.md"},
	}
	for _, tc := range ok {
		t.Run(tc.in, func(t *testing.T) {
			root := t.TempDir()
			o := newOps(t, &fakeRun{})
			rel, err := o.CreateFile(context.Background(), root, "", tc.in)
			if err != nil || rel != tc.want {
				t.Fatalf("got %q, %v", rel, err)
			}
			info, err := os.Stat(filepath.Join(root, tc.want))
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != 0 || info.Mode().Perm() != 0o644&^umaskBits(t) {
				t.Errorf("size %d mode %v", info.Size(), info.Mode())
			}
		})
	}

	t.Run("nested", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
			t.Fatal(err)
		}
		rel, err := newOps(t, &fakeRun{}).CreateFile(context.Background(), root, "a/b", "n")
		if err != nil || rel != "a/b/n.md" {
			t.Fatalf("got %q, %v", rel, err)
		}
		if _, err := os.Stat(filepath.Join(root, "a", "b", "n.md")); err != nil {
			t.Fatal(err)
		}
	})

	for _, n := range []string{"x.docx", "x.DOC"} {
		if _, err := newOps(t, &fakeRun{}).CreateFile(context.Background(), t.TempDir(), "", n); !errors.Is(err, ErrUnsupportedKind) {
			t.Errorf("%q: got %v", n, err)
		}
	}

	for _, n := range append(badNames, strings.Repeat("a", 253)) { // 253+".md" = 256
		root := t.TempDir()
		if _, err := newOps(t, &fakeRun{}).CreateFile(context.Background(), root, "", n); !errors.Is(err, ErrInvalidName) {
			t.Errorf("%q: got %v", n, err)
		}
		if got := names(t, root); len(got) != 0 {
			t.Errorf("%q: created %v", n, got)
		}
	}
}

// umaskBits returns the process umask so the 0644 check holds under a strict umask.
func umaskBits(t *testing.T) os.FileMode {
	t.Helper()
	d := t.TempDir()
	p := filepath.Join(d, "probe")
	write(t, p, "", 0o666)
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return 0o666 &^ info.Mode().Perm()
}

func TestCreateFileExists(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "A.md"), "keep", 0o644)
	o := newOps(t, &fakeRun{})
	if _, err := o.CreateFile(context.Background(), root, "", "A"); !errors.Is(err, ErrExists) {
		t.Fatalf("got %v", err)
	}
	if read(t, filepath.Join(root, "A.md")) != "keep" {
		t.Error("content changed")
	}
	if caseSensitiveFS(t) {
		t.Log("case-sensitive filesystem: skipping case-only subcase")
		return
	}
	if _, err := o.CreateFile(context.Background(), root, "", "a"); !errors.Is(err, ErrExists) {
		t.Fatalf("case-only: got %v", err)
	}
	if read(t, filepath.Join(root, "A.md")) != "keep" || len(names(t, root)) != 1 {
		t.Error("existing file touched")
	}
}

func TestCreateFileBadDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(root, "f.md"), "", 0o644)
	if err := os.Mkdir(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "lnk")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "esc")); err != nil {
		t.Fatal(err)
	}
	o := newOps(t, &fakeRun{})
	for _, d := range []string{"nope", "f.md", "lnk"} {
		if _, err := o.CreateFile(context.Background(), root, d, "x"); err == nil {
			t.Errorf("%q: want error", d)
		}
	}
	if len(names(t, filepath.Join(root, "real"))) != 0 {
		t.Error("file created through link")
	}
	if _, err := os.Stat(filepath.Join(root, "nope")); err == nil {
		t.Error("folder created")
	}
	if _, err := o.CreateFile(context.Background(), root, "../x", "n"); !errors.Is(err, project.ErrInvalidPath) {
		t.Errorf("../x: %v", err)
	}
	if _, err := o.CreateFile(context.Background(), root, "esc", "n"); !errors.Is(err, project.ErrOutsideRoot) {
		t.Errorf("esc: %v", err)
	}
	if len(names(t, outside)) != 0 {
		t.Error("file created outside")
	}
}

func TestRename(t *testing.T) {
	tests := []struct{ name, rel, newName, want string }{
		{"plain", "a.md", "b", "b.md"},
		{"keeps ext case", "Ugovor.DOCX", "Novi", "Novi.DOCX"},
		{"same ext not doubled", "a.md", "novo.md", "novo.md"},
		{"same ext other case", "a.md", "novo.MD", "novo.md"},
		{"dotted", "a.txt", "v1.2", "v1.2.txt"},
		{"nested", "d/e/a.md", "b", "d/e/b.md"},
		{"doc", "stari.doc", "novi", "novi.doc"},
		{"croatian", "a.md", "čćšžđ x", "čćšžđ x.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, filepath.Join(root, filepath.FromSlash(tc.rel)), "sadržaj", 0o600)
			old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
			if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(tc.rel)), old, old); err != nil {
				t.Fatal(err)
			}
			got, err := newOps(t, &fakeRun{}).Rename(context.Background(), root, tc.rel, tc.newName)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
			p := filepath.Join(root, filepath.FromSlash(got))
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if read(t, p) != "sadržaj" || info.Mode().Perm() != 0o600 || !info.ModTime().Equal(old) {
				t.Errorf("content/mode/mtime changed: %v %v", info.Mode(), info.ModTime())
			}
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(tc.rel))); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("old name still there: %v", err)
			}
		})
	}
}

func TestRenameExists(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "A", 0o644)
	write(t, filepath.Join(root, "b.md"), "B", 0o644)
	if _, err := newOps(t, &fakeRun{}).Rename(context.Background(), root, "a.md", "b"); !errors.Is(err, ErrExists) {
		t.Fatalf("got %v", err)
	}
	if read(t, filepath.Join(root, "a.md")) != "A" || read(t, filepath.Join(root, "b.md")) != "B" {
		t.Error("content changed")
	}
}

// A second hard link to the same file is another directory entry: renaming
// onto it must be refused, not reported as done while the old name stays.
func TestRenameOntoHardLink(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "text", 0o644)
	if err := os.Link(filepath.Join(root, "a.md"), filepath.Join(root, "b.md")); err != nil {
		t.Skipf("filesystem has no hard links: %v", err)
	}
	o := newOps(t, &fakeRun{})
	_, err := o.Rename(context.Background(), root, "a.md", "b")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("Rename onto a hard link of the same file = %v, want ErrExists", err)
	}
	if got, want := names(t, root), []string{"a.md", "b.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("directory = %v, want %v", got, want)
	}
}

func TestRenameCaseOnlyAndSame(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "A", 0o644)
	o := newOps(t, &fakeRun{})
	if got, err := o.Rename(context.Background(), root, "a.md", "a"); err != nil || got != "a.md" {
		t.Fatalf("same name: %q, %v", got, err)
	}
	if got, err := o.Rename(context.Background(), root, "a.md", "a.md"); err != nil || got != "a.md" {
		t.Fatalf("same name+ext: %q, %v", got, err)
	}
	if caseSensitiveFS(t) {
		t.Skip("case-sensitive filesystem")
	}
	got, err := o.Rename(context.Background(), root, "a.md", "A")
	if err != nil || got != "A.md" {
		t.Fatalf("got %q, %v", got, err)
	}
	if n := names(t, root); !reflect.DeepEqual(n, []string{"A.md"}) {
		t.Errorf("on disk: %v", n)
	}
}

func TestRenameInvalid(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "A", 0o644)
	o := newOps(t, &fakeRun{})
	for _, n := range append(badNames, strings.Repeat("a", 253)) {
		if _, err := o.Rename(context.Background(), root, "a.md", n); !errors.Is(err, ErrInvalidName) {
			t.Errorf("%q: got %v", n, err)
		}
	}
	// The extension of another supported type would change the document type:
	// that has its own error, so the user is not told the name is malformed.
	for _, n := range []string{"novo.txt", "novo.docx", "novo.DOC"} {
		if _, err := o.Rename(context.Background(), root, "a.md", n); !errors.Is(err, ErrUnsupportedKind) {
			t.Errorf("%q: got %v, want ErrUnsupportedKind", n, err)
		}
	}
	if n := names(t, root); !reflect.DeepEqual(n, []string{"a.md"}) {
		t.Errorf("on disk: %v", n)
	}
}

// notDocumentSetup builds a project with things that are not documents.
func notDocumentSetup(t *testing.T) (root string, rels []string) {
	t.Helper()
	root = t.TempDir()
	write(t, filepath.Join(root, "real.md"), "x", 0o644)
	write(t, filepath.Join(root, "slika.png"), "x", 0o644)
	if err := os.Mkdir(filepath.Join(root, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.md", filepath.Join(root, "lnk.md")); err != nil {
		t.Fatal(err)
	}
	return root, []string{"", "dir.md", "lnk.md", "slika.png", "missing.md"}
}

func TestNotDocument(t *testing.T) {
	root, rels := notDocumentSetup(t)
	f := &fakeRun{}
	o := newOps(t, f)
	for _, rel := range rels {
		if _, err := o.Rename(context.Background(), root, rel, "n"); !errors.Is(err, ErrNotDocument) {
			t.Errorf("rename %q: %v", rel, err)
		}
		if err := o.Trash(context.Background(), root, rel); !errors.Is(err, ErrNotDocument) {
			t.Errorf("trash %q: %v", rel, err)
		}
	}
	// Missing files are ErrNotDocument and also fs.ErrNotExist.
	if err := o.Trash(context.Background(), root, "missing.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("runner called: %v", f.calls)
	}
	if n := names(t, root); !reflect.DeepEqual(n, []string{"dir.md", "lnk.md", "real.md", "slika.png"}) {
		t.Errorf("disk changed: %v", n)
	}
}

func TestOutsideProject(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "o.md"), "x", 0o644)
	if err := os.Symlink(outside, filepath.Join(root, "esc")); err != nil {
		t.Fatal(err)
	}
	f := &fakeRun{}
	o := newOps(t, f)
	for rel, want := range map[string]error{"../o.md": project.ErrInvalidPath, "esc/o.md": project.ErrOutsideRoot} {
		if _, err := o.Rename(context.Background(), root, rel, "n"); !errors.Is(err, want) {
			t.Errorf("rename %q: %v", rel, err)
		}
		if err := o.Trash(context.Background(), root, rel); !errors.Is(err, want) {
			t.Errorf("trash %q: %v", rel, err)
		}
	}
	if len(f.calls) != 0 || len(names(t, outside)) != 1 {
		t.Error("something was touched")
	}
}

func TestTrash(t *testing.T) {
	for _, name := range []string{"a.md", "-rf neki dokument.md", "-s.txt"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, "d", name)
			write(t, p, "x", 0o644)
			f := &fakeRun{fn: func(_ context.Context, args []string) ([]byte, error) {
				return nil, os.Rename(args[len(args)-1], filepath.Join(t.TempDir(), "moved")) // simulates the move
			}}
			if err := newOps(t, f).Trash(context.Background(), root, "d/"+name); err != nil {
				t.Fatal(err)
			}
			want := []call{{"/fake/trash", []string{"-s", p}}}
			if !reflect.DeepEqual(f.calls, want) {
				t.Errorf("calls %v, want %v", f.calls, want)
			}
		})
	}
}

func TestTrashToolError(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.md")
	write(t, p, "keep", 0o644)
	f := &fakeRun{fn: func(context.Context, []string) ([]byte, error) {
		return []byte("boom output"), errors.New("exit status 5")
	}}
	err := newOps(t, f).Trash(context.Background(), root, "a.md")
	if err == nil || !strings.Contains(err.Error(), "boom output") || strings.Contains(err.Error(), root) {
		t.Fatalf("got %v", err)
	}
	if read(t, p) != "keep" {
		t.Error("file changed")
	}
}

func TestTrashLyingTool(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "keep", 0o644)
	if err := newOps(t, &fakeRun{}).Trash(context.Background(), root, "a.md"); err == nil {
		t.Fatal("want error when file is still there")
	}
}

func TestTrashTimeout(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "keep", 0o644)
	f := &fakeRun{fn: func(ctx context.Context, _ []string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	o, err := New(Options{TrashBin: "t", Timeout: 50 * time.Millisecond, Run: f.run})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = o.Trash(context.Background(), root, "a.md")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("got %v after %v", err, time.Since(start))
	}
}

func TestCanceledContext(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "A", 0o644)
	f := &fakeRun{}
	o := newOps(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := o.CreateFile(ctx, root, "", "n"); !errors.Is(err, context.Canceled) {
		t.Errorf("create: %v", err)
	}
	if _, err := o.Rename(ctx, root, "a.md", "b"); !errors.Is(err, context.Canceled) {
		t.Errorf("rename: %v", err)
	}
	if err := o.Trash(ctx, root, "a.md"); !errors.Is(err, context.Canceled) {
		t.Errorf("trash: %v", err)
	}
	if len(f.calls) != 0 || !reflect.DeepEqual(names(t, root), []string{"a.md"}) {
		t.Error("something happened")
	}
}

// TestRealTrash uses the real macOS tool; it moves a temp file to the user's
// Trash, so it only runs when explicitly requested.
func TestRealTrash(t *testing.T) {
	if os.Getenv("PISAR_TEST_REAL_TRASH") != "1" {
		t.Skip("set PISAR_TEST_REAL_TRASH=1 to run")
	}
	bin, err := exec.LookPath("trash")
	if err != nil {
		t.Skip("no trash tool")
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "pisar-real-trash-test.md"), "x", 0o644)
	o, err := New(Options{TrashBin: bin, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Trash(context.Background(), root, "pisar-real-trash-test.md"); err != nil {
		t.Fatal(err)
	}
}
