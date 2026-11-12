package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestExtendPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		dirs []string
		want string
	}{
		{"empty path", "", []string{"/a", "/b"}, "/a:/b"},
		{"already contains", "/usr/bin:/a", []string{"/a"}, "/usr/bin:/a"},
		{"trailing slash kept", "/usr/bin:/a/", []string{"/a"}, "/usr/bin:/a/"},
		{"one of two present", "/usr/bin:/b", []string{"/a", "/b"}, "/usr/bin:/b:/a"},
		{"no dirs", "/usr/bin::/bin", nil, "/usr/bin::/bin"},
		{"empty entry in the middle", "/usr/bin::/bin", []string{"/a"}, "/usr/bin::/bin:/a"},
		{"order of dirs", "/usr/bin", []string{"/c", "/a", "/b"}, "/usr/bin:/c:/a:/b"},
		{"duplicate in dirs", "/usr/bin", []string{"/a", "/a", "/a/"}, "/usr/bin:/a"},
		{"relative entry is not ours", "bin:/usr/bin", []string{"/a"}, "bin:/usr/bin:/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExtendPath(tt.path, tt.dirs); got != tt.want {
				t.Errorf("ExtendPath(%q, %q) = %q, want %q", tt.path, tt.dirs, got, tt.want)
			}
		})
	}
}

func TestToolDirs(t *testing.T) {
	// Not parallel: t.Setenv changes the process environment.
	tests := []struct {
		name  string
		unset bool
		value string
		want  []string
	}{
		{"unset gives built-in", true, "", []string{"/opt/homebrew/bin", "/usr/local/bin"}},
		{"set but empty", false, "", nil},
		{"two absolute", false, "/x/bin:/y/bin", []string{"/x/bin", "/y/bin"}},
		{"relative and empty dropped", false, "rel/bin::/x/bin:./y", []string{"/x/bin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvToolDirs, tt.value)
			if tt.unset {
				// t.Setenv restores the original state when the test ends.
				if err := os.Unsetenv(EnvToolDirs); err != nil {
					t.Fatal(err)
				}
			}
			if got := ToolDirs(); !slices.Equal(got, tt.want) {
				t.Errorf("ToolDirs() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolDirsBuiltinIsNotShared(t *testing.T) {
	t.Setenv(EnvToolDirs, "") // makes t restore the original value on cleanup
	if err := os.Unsetenv(EnvToolDirs); err != nil {
		t.Fatal(err)
	}
	first := ToolDirs()
	first[0] = "/changed"
	if got := ToolDirs()[0]; got != "/opt/homebrew/bin" {
		t.Errorf("mutating the result changed the built-in list: %q", got)
	}
}

func TestPrepareEnvironment(t *testing.T) {
	t.Run("path is extended once", func(t *testing.T) {
		a, b := t.TempDir(), t.TempDir()
		t.Setenv("PATH", "/usr/bin:/bin")
		t.Setenv(EnvToolDirs, a+":"+b)
		for range 2 {
			if err := PrepareEnvironment(); err != nil {
				t.Fatal(err)
			}
		}
		want := "/usr/bin:/bin:" + a + ":" + b
		if got := os.Getenv("PATH"); got != want {
			t.Errorf("PATH = %q, want %q", got, want)
		}
	})

	locales := []struct {
		name string
		env  map[string]string
		want map[string]string
	}{
		{"none set", nil, map[string]string{"LC_CTYPE": DefaultLocale, "LANG": "", "LC_ALL": ""}},
		{"LANG set", map[string]string{"LANG": "hr_HR.UTF-8"}, map[string]string{"LC_CTYPE": "", "LANG": "hr_HR.UTF-8"}},
		{"LC_ALL=C", map[string]string{"LC_ALL": "C"}, map[string]string{"LC_CTYPE": "", "LC_ALL": "C"}},
		{"LC_CTYPE kept", map[string]string{"LC_CTYPE": "UTF-8"}, map[string]string{"LC_CTYPE": "UTF-8"}},
	}
	for _, tt := range locales {
		t.Run("locale "+tt.name, func(t *testing.T) {
			for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
				t.Setenv(k, tt.env[k])
			}
			t.Setenv(EnvToolDirs, "")
			if err := PrepareEnvironment(); err != nil {
				t.Fatal(err)
			}
			for k, want := range tt.want {
				if got := os.Getenv(k); got != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
		})
	}
}

// The point of the whole change: a tool outside the minimal Finder PATH is
// found by exec.LookPath after PrepareEnvironment, and not before.
func TestPrepareEnvironmentMakesToolFindable(t *testing.T) {
	dir := t.TempDir()
	const name = "pisar-fake-tool"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv(EnvToolDirs, dir)

	if _, err := exec.LookPath(name); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("before PrepareEnvironment: err = %v, want exec.ErrNotFound", err)
	}
	if err := PrepareEnvironment(); err != nil {
		t.Fatal(err)
	}
	got, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("after PrepareEnvironment: %v", err)
	}
	if want := filepath.Join(dir, name); got != want {
		t.Errorf("LookPath = %q, want %q", got, want)
	}
}
