package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func mustOpen(t *testing.T, home string) *Store {
	t.Helper()
	s, err := Open(home)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOpenEmpty(t *testing.T) {
	home := filepath.Join(t.TempDir(), "a", "b", "Pisar")
	s := mustOpen(t, home)
	if len(s.Projects()) != 0 || s.Recovered() != "" {
		t.Fatalf("expected empty store, got %v %q", s.Projects(), s.Recovered())
	}
	if _, err := os.Stat(filepath.Join(home, fileName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("config.json must not exist yet: %v", err)
	}
	info, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("home perm = %v", info.Mode().Perm())
	}
	if s.Home() != home {
		t.Fatalf("Home = %q", s.Home())
	}
}

func TestLimits(t *testing.T) {
	l := mustOpen(t, t.TempDir()).Limits()
	if l.WatchDebounce != DefaultWatchDebounce || l.MaxTreeNodes != DefaultMaxTreeNodes || l.CommandTimeout != DefaultCommandTimeout {
		t.Fatalf("unexpected limits %+v", l)
	}
	if l.WatchDebounce <= 0 || l.MaxTreeNodes <= 0 || l.CommandTimeout <= 0 {
		t.Fatalf("limits must be positive: %+v", l)
	}
}

func TestPersistence(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	s := mustOpen(t, home)
	var want []Project
	for _, n := range []string{"one", "two", "three"} {
		p, err := s.AddProject(mkdir(t, root, n))
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != n || len(p.ID) != 32 {
			t.Fatalf("bad project %+v", p)
		}
		want = append(want, p)
	}
	s = nil // drop the first store; everything must come from disk
	reopened := mustOpen(t, home)
	if got := reopened.Projects(); !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if p, ok := reopened.Project(want[1].ID); !ok || p != want[1] {
		t.Fatalf("Project lookup: %v %v", p, ok)
	}
	if _, ok := reopened.Project("nope"); ok {
		t.Fatal("unexpected project")
	}
}

func TestAddDuplicate(t *testing.T) {
	root := t.TempDir()
	dir := mkdir(t, root, "proj")
	link := filepath.Join(root, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, path string }{
		{"same", dir},
		{"symlink", link},
		{"trailing slash", dir + "/"},
		{"dot segment", dir + "/."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mustOpen(t, filepath.Join(t.TempDir(), "home"))
			first, err := s.AddProject(dir)
			if err != nil {
				t.Fatal(err)
			}
			again, err := s.AddProject(tc.path)
			if err != nil || again.ID != first.ID {
				t.Fatalf("got %+v, %v; want %+v", again, err, first)
			}
			if n := len(s.Projects()); n != 1 {
				t.Fatalf("projects = %d", n)
			}
		})
	}
}

func TestAddInvalid(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path string
		want error
	}{
		{"missing", filepath.Join(root, "missing"), fs.ErrNotExist},
		{"file", file, ErrNotADirectory},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			s := mustOpen(t, home)
			if _, err := s.AddProject(tc.path); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(s.Projects()) != 0 {
				t.Fatal("projects changed")
			}
			if _, err := os.Stat(filepath.Join(home, fileName)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("file was written")
			}
		})
	}

	t.Run("existing file unchanged", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		s := mustOpen(t, home)
		if _, err := s.AddProject(mkdir(t, root, "ok")); err != nil {
			t.Fatal(err)
		}
		before := readFile(t, filepath.Join(home, fileName))
		if _, err := s.AddProject(file); err == nil {
			t.Fatal("expected error")
		}
		if readFile(t, filepath.Join(home, fileName)) != before || len(s.Projects()) != 1 {
			t.Fatal("state changed after failed add")
		}
	})
}

func TestRemove(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	dir := mkdir(t, root, "proj")
	s := mustOpen(t, home)
	p, err := s.AddProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddProject(mkdir(t, root, "other"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveProject("unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := s.RemoveProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := mustOpen(t, home).Projects(); !slices.Equal(got, []Project{other}) {
		t.Fatalf("after reopen: %v", got)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("folder must remain: %v", err)
	}
}

func TestDamagedFile(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"not json", "this is not json"},
		{"empty", ""},
		{"wrong shape", `{"version":1,"projects":"oops"}`},
		{"array", `[1,2,3]`},
		{"missing version", `{"projects":[]}`},
		{"unknown field", `{"version":1,"projects":[],"extra":true}`},
		{"trailing data", `{"version":1,"projects":[]} {}`},
		{"no id", `{"version":1,"projects":[{"name":"a","path":"/tmp/a"}]}`},
		{"no path", `{"version":1,"projects":[{"id":"x","name":"a"}]}`},
		{"relative path", `{"version":1,"projects":[{"id":"x","name":"a","path":"rel/a"}]}`},
		{"duplicate id", `{"version":1,"projects":[{"id":"x","path":"/a"},{"id":"x","path":"/b"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, fileName)
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			s := mustOpen(t, home)
			if len(s.Projects()) != 0 {
				t.Fatal("expected empty config")
			}
			rec := s.Recovered()
			if rec == "" || filepath.Dir(rec) != home || !strings.HasPrefix(filepath.Base(rec), "config.bad-") ||
				strings.ContainsAny(filepath.Base(rec), ": ") {
				t.Fatalf("Recovered = %q", rec)
			}
			if readFile(t, rec) != tc.content {
				t.Fatal("original bytes not preserved")
			}
			if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("config.json must be gone")
			}
		})
	}
}

func TestNewerVersion(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, fileName)
	content := `{"version":2,"projects":[],"future":{"x":1}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(home); !errors.Is(err, ErrNewerVersion) {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, path) != content {
		t.Fatal("file changed")
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 1 {
		t.Fatalf("home entries: %v %v", entries, err)
	}
}

func TestMissingFolderStays(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	dir := mkdir(t, root, "gone")
	p, err := mustOpen(t, home).AddProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if got := mustOpen(t, home).Projects(); !slices.Equal(got, []Project{p}) {
		t.Fatalf("got %v", got)
	}
}

func TestAtomicWrite(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	s := mustOpen(t, home)
	if _, err := s.AddProject(mkdir(t, root, "a")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != fileName {
		t.Fatalf("unexpected files: %v", entries)
	}
	info, err := os.Stat(filepath.Join(home, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", info.Mode().Perm())
	}
}

func TestFailedWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	s := mustOpen(t, home)
	a, err := s.AddProject(mkdir(t, root, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })

	if _, err := s.AddProject(mkdir(t, root, "b")); err == nil {
		t.Fatal("expected write error")
	}
	if got := s.Projects(); !slices.Equal(got, []Project{a}) {
		t.Fatalf("add not rolled back: %v", got)
	}
	if err := s.RemoveProject(a.ID); err == nil {
		t.Fatal("expected write error")
	}
	if got := s.Projects(); !slices.Equal(got, []Project{a}) {
		t.Fatalf("remove not rolled back: %v", got)
	}
}

func TestConcurrent(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	s := mustOpen(t, home)
	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		d := mkdir(t, root, fmt.Sprintf("p%02d", i))
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := s.AddProject(d); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			_ = s.Projects()
		}()
	}
	wg.Wait()
	if got := len(mustOpen(t, home).Projects()); got != n {
		t.Fatalf("persisted %d projects, want %d", got, n)
	}
}

func TestProjectsIsCopy(t *testing.T) {
	root := t.TempDir()
	s := mustOpen(t, filepath.Join(root, "home"))
	p, err := s.AddProject(mkdir(t, root, "a"))
	if err != nil {
		t.Fatal(err)
	}
	got := s.Projects()
	got[0].Name = "changed"
	if again := s.Projects(); !slices.Equal(again, []Project{p}) {
		t.Fatalf("store modified: %v", again)
	}
}

func TestDefaultHome(t *testing.T) {
	t.Run("absolute", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(EnvHome, dir)
		got, err := DefaultHome()
		if err != nil || got != dir {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("relative", func(t *testing.T) {
		t.Setenv(EnvHome, "relative/dir")
		if _, err := DefaultHome(); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("unset", func(t *testing.T) {
		t.Setenv(EnvHome, "")
		got, err := DefaultHome()
		if err != nil {
			t.Fatal(err)
		}
		base, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "Pisar" || filepath.Dir(got) != base {
			t.Fatalf("got %q, base %q", got, base)
		}
	})
}
