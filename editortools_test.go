package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"Pisar/internal/config"
)

// fakeTool creates an executable (or, with mode 0o644, a non-executable) file.
func fakeTool(t *testing.T, dir, name string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	// WriteFile is subject to the umask, so set the mode explicitly.
	if err := os.Chmod(filepath.Join(dir, name), mode); err != nil {
		t.Fatal(err)
	}
}

// These tests change PATH, so none of them is parallel.
func TestTools(t *testing.T) {
	const otherNvim = "pisar-other-nvim"
	for _, c := range []struct {
		name     string
		nvimBin  string // NvimBin override, "" keeps the configured name
		files    map[string]os.FileMode
		wantNvim bool
		wantPand bool
	}{
		{name: "both", files: map[string]os.FileMode{"nvim": 0o755, "pandoc": 0o755}, wantNvim: true, wantPand: true},
		{name: "only nvim", files: map[string]os.FileMode{"nvim": 0o755}, wantNvim: true},
		{name: "only pandoc", files: map[string]os.FileMode{"pandoc": 0o755}, wantPand: true},
		{name: "none", files: map[string]os.FileMode{}},
		{name: "not executable", files: map[string]os.FileMode{"nvim": 0o644, "pandoc": 0o644}},
		{name: "other nvim name found", nvimBin: otherNvim, files: map[string]os.FileMode{otherNvim: 0o755}, wantNvim: true},
		{name: "other nvim name missing", nvimBin: "nema-takvog-nvima", files: map[string]os.FileMode{"nvim": 0o755, "pandoc": 0o755}, wantPand: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEd(t, func(d *editorServiceDeps) { d.NvimBin = c.nvimBin })
			bin := t.TempDir()
			for name, mode := range c.files {
				fakeTool(t, bin, name, mode)
			}
			t.Setenv("PATH", bin)

			got := e.svc.Tools(context.Background())
			want := []ToolStatus{{Name: "nvim", Found: c.wantNvim}, {Name: "pandoc", Found: c.wantPand}}
			if !slices.Equal(got, want) {
				t.Errorf("Tools = %+v, want %+v", got, want)
			}
		})
	}
}

// A folder named like the tool is not the tool.
func TestToolsFolder(t *testing.T) {
	e := newEd(t)
	bin := t.TempDir()
	for _, name := range []string{"nvim", "pandoc"} {
		if err := os.Mkdir(filepath.Join(bin, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	for _, s := range e.svc.Tools(context.Background()) {
		if s.Found {
			t.Errorf("%s: a folder was found", s.Name)
		}
	}
}

// Nothing is remembered: a tool installed while the application runs is seen by the next call.
func TestToolsLooksAgain(t *testing.T) {
	e := newEd(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin)

	if got := e.svc.Tools(context.Background()); got[0].Found || got[1].Found {
		t.Fatalf("first call found %+v in an empty folder", got)
	}
	fakeTool(t, bin, "nvim", 0o755)
	got := e.svc.Tools(context.Background())
	if !got[0].Found || got[1].Found {
		t.Errorf("second call = %+v, want only nvim found", got)
	}
	fakeTool(t, bin, "pandoc", 0o755)
	if got := e.svc.Tools(context.Background()); !got[0].Found || !got[1].Found {
		t.Errorf("third call = %+v, want both found", got)
	}
}

func TestStartFailureDialog(t *testing.T) {
	text := uiTextFor(t)
	plain := errors.New("create folder: permission denied")
	for _, c := range []struct {
		name        string
		err         error
		wantTitle   string
		wantMessage string
	}{
		{"newer config", config.ErrNewerVersion, text.ConfigNewerTitle, text.ConfigNewer},
		{"wrapped newer config", fmt.Errorf("open: %w", config.ErrNewerVersion), text.ConfigNewerTitle, text.ConfigNewer},
		{"plain error", plain, text.StartFailedTitle, fill(text.StartFailed, "reason", plain.Error())},
	} {
		t.Run(c.name, func(t *testing.T) {
			title, message := startFailureDialog(text, c.err)
			if title != c.wantTitle || message != c.wantMessage {
				t.Errorf("got (%q, %q), want (%q, %q)", title, message, c.wantTitle, c.wantMessage)
			}
		})
	}
	// The reason really is in the text, and no placeholder is left over.
	_, message := startFailureDialog(text, plain)
	if want := "Pokretanje nije uspjelo: " + plain.Error(); message != want {
		t.Errorf("message = %q, want %q", message, want)
	}
}
