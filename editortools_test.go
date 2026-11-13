package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
			// The fake tools print nothing, so a found tool has no readable version: usable, not too old.
			want := []ToolStatus{
				{Name: "nvim", Found: c.wantNvim, Minimum: config.MinNvimVersion},
				{Name: "pandoc", Found: c.wantPand, Minimum: config.MinPandocVersion},
			}
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

// edTools puts fake nvim and pandoc into a folder that is the whole PATH. A script prints
// the given first line (an empty line means "print nothing"), so what the service does with it is
// what is tested, not a real tool. Not parallel: it changes PATH.
func edTools(t *testing.T, nvimLine, pandocLine string, mutate ...func(*editorServiceDeps)) (*edEnv, string) {
	t.Helper()
	e := newEd(t, mutate...)
	bin := t.TempDir()
	for name, line := range map[string]string{"nvim": nvimLine, "pandoc": pandocLine} {
		writeScript(t, bin, name, printLine(line))
	}
	t.Setenv("PATH", bin)
	return e, bin
}

func printLine(line string) string {
	if line == "" {
		return "exit 0"
	}
	return "printf '%s\\n' '" + line + "'"
}

func TestEdToolsCurrent(t *testing.T) {
	e, _ := edTools(t, "NVIM v0.12.6", "pandoc 3.13")
	want := []ToolStatus{
		{Name: "nvim", Found: true, Version: "0.12.6", Minimum: config.MinNvimVersion},
		{Name: "pandoc", Found: true, Version: "3.13", Minimum: config.MinPandocVersion},
	}
	if got := e.svc.Tools(context.Background()); !slices.Equal(got, want) {
		t.Errorf("Tools = %+v, want %+v", got, want)
	}
}

func TestEdToolsTooOld(t *testing.T) {
	for _, c := range []struct {
		name         string
		nvim, pandoc string
		wantNvim     bool
		wantPandoc   bool
	}{
		{"old nvim", "NVIM v0.11.4", "pandoc 3.13", true, false},
		// 3.9 is below 3.13 as numbers, though not as text.
		{"old pandoc", "NVIM v0.12.6", "pandoc 3.9", false, true},
		{"both old", "NVIM v0.11.4", "pandoc 3.9", true, true},
		{"on the minimum", "NVIM v0.12.0", "pandoc 3.13", false, false},
		{"minimum with a zero part", "NVIM v0.12.0", "pandoc 3.13.0", false, false},
		{"development build", "NVIM v0.13.0-dev-1+gabc", "pandoc 3.14", false, false},
		{"release candidate", "NVIM v0.12.0-rc1", "pandoc 3.13", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, _ := edTools(t, c.nvim, c.pandoc)
			got := e.svc.Tools(context.Background())
			if got[0].TooOld != c.wantNvim || got[1].TooOld != c.wantPandoc {
				t.Errorf("TooOld = %v, %v; want %v, %v (%+v)", got[0].TooOld, got[1].TooOld, c.wantNvim, c.wantPandoc, got)
			}
			if !got[0].Found || !got[1].Found {
				t.Errorf("a tool with a version is not found: %+v", got)
			}
		})
	}
	e, _ := edTools(t, "NVIM v0.11.4", "pandoc 3.9")
	got := e.svc.Tools(context.Background())
	if got[0].Version != "0.11.4" || got[1].Version != "3.9" {
		t.Errorf("versions = %q, %q", got[0].Version, got[1].Version)
	}
}

func TestEdToolsNotFound(t *testing.T) {
	e := newEd(t)
	t.Setenv("PATH", t.TempDir())
	for _, s := range e.svc.Tools(context.Background()) {
		if s.Found || s.Version != "" || s.TooOld {
			t.Errorf("%s: %+v, want not found, no version, not too old", s.Name, s)
		}
		if s.Minimum == "" {
			t.Errorf("%s: Minimum is empty", s.Name)
		}
	}
}

// A tool that cannot be asked is still usable: a change in what it prints must not lock the application.
func TestEdToolsUnreadableVersion(t *testing.T) {
	const hangFor = 300 * time.Millisecond
	shortLimit := func(d *editorServiceDeps) { d.Limits.CommandTimeout = hangFor }
	for _, c := range []struct {
		name string
		body string // the script of both tools
		slow bool
	}{
		{"no version in the output", "echo 'something else entirely'", false},
		{"no output", "exit 0", false},
		{"exit code 1", "echo 'NVIM v0.12.6'; exit 1", false},
		// An absolute path: PATH holds only the fake tools.
		{"hangs", "exec /bin/sleep 30", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEd(t, shortLimit)
			bin := t.TempDir()
			writeScript(t, bin, "nvim", c.body)
			writeScript(t, bin, "pandoc", c.body)
			t.Setenv("PATH", bin)

			start := time.Now()
			got := e.svc.Tools(context.Background())
			if c.slow && time.Since(start) > 5*hangFor {
				t.Errorf("Tools took %v with a limit of %v", time.Since(start), hangFor)
			}
			for _, s := range got {
				if !s.Found || s.Version != "" || s.TooOld {
					t.Errorf("%s: %+v, want found, no version, not too old", s.Name, s)
				}
			}
		})
	}
}

func TestEdToolsShape(t *testing.T) {
	e := newEd(t)
	t.Setenv("PATH", t.TempDir())
	got := e.svc.Tools(context.Background())
	if len(got) != 2 || got[0].Name != "nvim" || got[1].Name != "pandoc" {
		t.Fatalf("Tools = %+v, want nvim then pandoc", got)
	}
	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	slices.Sort(names)
	if want := []string{"found", "minimum", "name", "tooOld", "version"}; !slices.Equal(names, want) {
		t.Errorf("JSON keys = %v, want %v", names, want)
	}
}

// An upgrade in place must not be hidden by what is remembered.
func TestEdToolsSeesUpgrade(t *testing.T) {
	e, bin := edTools(t, "NVIM v0.11.4", "pandoc 3.9")
	got := e.svc.Tools(context.Background())
	if !got[0].TooOld || !got[1].TooOld {
		t.Fatalf("first call = %+v, want both too old", got)
	}
	// The new scripts are longer, so the size differs even where the modification time is coarse.
	writeScript(t, bin, "nvim", printLine("NVIM v0.12.10"))
	writeScript(t, bin, "pandoc", printLine("pandoc 3.13.1"))
	got = e.svc.Tools(context.Background())
	if got[0].Version != "0.12.10" || got[1].Version != "3.13.1" || got[0].TooOld || got[1].TooOld {
		t.Errorf("second call = %+v, want the new versions, not too old", got)
	}
}

// Neovim writes into its state folder even for --version, so it is asked with Pisar's folders.
func TestEdToolsNvimEnvironment(t *testing.T) {
	e := newEd(t)
	bin, out := t.TempDir(), filepath.Join(t.TempDir(), "env")
	writeScript(t, bin, "nvim", "printf '%s\\n%s\\n' \"$XDG_STATE_HOME\" \"$NVIM_APPNAME\" > '"+out+"'; echo 'NVIM v0.12.6'")
	writeScript(t, bin, "pandoc", "echo 'pandoc 3.13'")
	t.Setenv("PATH", bin)
	// What the process has must lose against what Pisar sets.
	t.Setenv("XDG_STATE_HOME", "/somewhere/else")
	t.Setenv("NVIM_APPNAME", "other-app")

	e.svc.Tools(context.Background())
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := e.svc.paths.StateHome + "\n" + e.svc.paths.AppName + "\n"
	if string(data) != want {
		t.Errorf("nvim saw %q, want %q", data, want)
	}
	if e.svc.paths.AppName == "" || e.svc.paths.AppName == "other-app" || e.svc.paths.StateHome == "/somewhere/else" {
		t.Errorf("the test proves nothing: %+v", e.svc.paths)
	}
}

// The state of the tools reaches the log (Info) the first time and when it changes, not on every call.
func TestEdToolsLogsOnChange(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	e, bin := edTools(t, "NVIM v0.12.6", "pandoc 3.13")
	count := func() int { return strings.Count(buf.String(), `msg="editor: tool"`) }
	e.svc.Tools(context.Background())
	if n := count(); n != 2 {
		t.Fatalf("first call wrote %d tool records, want 2:\n%s", n, buf.String())
	}
	for _, want := range []string{"tool=nvim", "version=0.12.6", "tool=pandoc", "version=3.13", "minimum=" + config.MinPandocVersion, "tooOld=false"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, buf.String())
		}
	}
	e.svc.Tools(context.Background())
	if n := count(); n != 2 {
		t.Errorf("an unchanged answer was logged again: %d records", n)
	}
	writeScript(t, bin, "pandoc", printLine("pandoc 3.9.1"))
	e.svc.Tools(context.Background())
	if n := count(); n != 4 {
		t.Errorf("a changed answer wrote %d records in total, want 4", n)
	}
	if strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("a readable version produced a warning:\n%s", buf.String())
	}
}

// A version that cannot be read is a warning, written once and again only when the answer changes.
func TestEdToolsWarnsOnChange(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	e := newEd(t)
	bin := t.TempDir()
	writeScript(t, bin, "nvim", "echo 'NVIM v0.12.6'")
	writeScript(t, bin, "pandoc", "echo 'something else entirely'")
	t.Setenv("PATH", bin)
	warns := func() int { return strings.Count(buf.String(), "level=WARN") }

	e.svc.Tools(context.Background())
	if n := warns(); n != 1 || !strings.Contains(buf.String(), "tool=pandoc") {
		t.Fatalf("first call wrote %d warnings, want 1 for pandoc:\n%s", n, buf.String())
	}
	e.svc.Tools(context.Background())
	e.svc.Tools(context.Background())
	if n := warns(); n != 1 {
		t.Errorf("an unchanged answer repeated the warning: %d warnings", n)
	}
	// The tool changes and still cannot be read: a different answer (another message), so one more.
	writeScript(t, bin, "pandoc", "echo 'another text that holds no number'")
	e.svc.Tools(context.Background())
	if n := warns(); n != 2 {
		t.Errorf("a changed answer wrote %d warnings in total, want 2", n)
	}
}
