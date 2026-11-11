package docsafe

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// skipIfRoot: root passes every permission check, so permission cases mean nothing.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission checks do not apply")
	}
}

// lock sets the immutable flag (Finder "Locked") and clears it on cleanup, so
// that t.TempDir can remove the file. It skips the test if the system refuses.
func lock(t *testing.T, p string) {
	t.Helper()
	if err := unix.Chflags(p, unix.UF_IMMUTABLE); err != nil {
		t.Skipf("the file system does not allow the immutable flag: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Chflags(p, 0); err != nil {
			t.Errorf("unlock %s: %v", p, err)
		}
	})
}

// lockDown makes dir read-only for the test and restores it on cleanup.
func lockDown(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Errorf("restore %s: %v", dir, err)
		}
	})
}

// roCase prepares one read-only situation inside dir and returns the path
// that must be refused.
type roCase struct {
	name  string
	setup func(t *testing.T, dir string) string
	want  ReadOnlyReason
}

func roCases() []roCase {
	return []roCase{
		{"writable file", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "a.docx")
			write(t, p, "x", 0o644)
			return p
		}, ReadOnlyNone},
		{"file 0444", func(t *testing.T, dir string) string {
			skipIfRoot(t)
			p := filepath.Join(dir, "a.docx")
			write(t, p, "x", 0o644)
			if err := os.Chmod(p, 0o444); err != nil {
				t.Fatal(err)
			}
			return p
		}, ReadOnlyPermission},
		{"folder 0555", func(t *testing.T, dir string) string {
			skipIfRoot(t)
			p := filepath.Join(dir, "sub", "a.docx")
			write(t, p, "x", 0o644)
			lockDown(t, filepath.Dir(p))
			return p
		}, ReadOnlyPermission},
		{"hard link", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "a.docx")
			write(t, p, "x", 0o644)
			if err := os.Link(p, filepath.Join(dir, "other.docx")); err != nil {
				t.Fatal(err)
			}
			return p
		}, ReadOnlyHardLinks},
		{"symbolic link", func(t *testing.T, dir string) string {
			target := filepath.Join(dir, "target.docx")
			write(t, target, "x", 0o644)
			p := filepath.Join(dir, "link.docx")
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			return p
		}, ReadOnlyNotRegular},
		{"folder instead of file", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "a.docx")
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
			return p
		}, ReadOnlyNotRegular},
		{"locked file", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "a.docx")
			write(t, p, "x", 0o644)
			lock(t, p)
			return p
		}, ReadOnlyLocked},
		{"kind is checked before hard links", func(t *testing.T, dir string) string {
			target := filepath.Join(dir, "target.docx")
			write(t, target, "x", 0o644)
			if err := os.Link(target, filepath.Join(dir, "other.docx")); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(dir, "link.docx")
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			return p
		}, ReadOnlyNotRegular},
		{"hard links are checked before the lock", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "a.docx")
			write(t, p, "x", 0o644)
			if err := os.Link(p, filepath.Join(dir, "other.docx")); err != nil {
				t.Fatal(err)
			}
			lock(t, p)
			return p
		}, ReadOnlyHardLinks},
		{"lock is checked before permissions", func(t *testing.T, dir string) string {
			skipIfRoot(t)
			p := filepath.Join(dir, "a.docx")
			write(t, p, "x", 0o644)
			if err := os.Chmod(p, 0o444); err != nil {
				t.Fatal(err)
			}
			lock(t, p)
			return p
		}, ReadOnlyLocked},
	}
}

func TestCheckWritable(t *testing.T) {
	for _, tt := range roCases() {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.setup(t, t.TempDir())
			got, err := CheckWritable(p)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("reason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckWritable_Missing(t *testing.T) {
	got, err := CheckWritable(filepath.Join(t.TempDir(), "none.docx"))
	if !errors.Is(err, fs.ErrNotExist) || got != ReadOnlyNone {
		t.Fatalf("got %q, %v; want no reason and fs.ErrNotExist", got, err)
	}
}

func TestReadOnlyError(t *testing.T) {
	var err error = &ReadOnlyError{Path: "/x/a.docx", Reason: ReadOnlyLocked}
	if !errors.Is(err, ErrReadOnly) {
		t.Error("ReadOnlyError does not match ErrReadOnly")
	}
	var ro *ReadOnlyError
	if !errors.As(err, &ro) || ro.Reason != ReadOnlyLocked {
		t.Errorf("errors.As: %v", ro)
	}
	if got := err.Error(); got == "" {
		t.Error("empty message")
	}
}
