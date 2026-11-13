package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"Pisar/internal/config"
)

// writeScript writes an executable shell script and returns its path.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// WriteFile is subject to the umask, so set the mode explicitly.
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func v(parts ...int) toolVersion { return toolVersion(parts) }

func TestToolVersionParse(t *testing.T) {
	for _, c := range []struct {
		name string
		line string
		want toolVersion
		ok   bool
	}{
		{"nvim", "NVIM v0.12.6", v(0, 12, 6), true},
		{"nvim dev build", "NVIM v0.13.0-dev-12+gabc", v(0, 13, 0), true},
		{"nvim release candidate", "NVIM v0.12.0-rc1", v(0, 12, 0), true},
		{"pandoc", "pandoc 3.13", v(3, 13), true},
		{"pandoc four parts", "pandoc 3.1.11.1", v(3, 1, 11, 1), true},
		{"pandoc.exe", "pandoc.exe 3.2", v(3, 2), true},
		{"CRLF", "pandoc 3.13\r\n", v(3, 13), true},
		{"leading spaces", "   NVIM v0.12.6", v(0, 12, 6), true},
		{"only the first line", "pandoc 3.13\nFeatures: +server\nCopyright 2006-2025", v(3, 13), true},
		{"version only on the second line", "pandoc\n3.13", nil, false},
		{"two spaces after the name", "NVIM  v0.12.6", v(0, 12, 6), true},
		{"a number inside a wrapper's line", "pandoc shim 1 for 3.13", nil, false},
		{"a wrapper that names a version later", "Pandoc wrapper version 3.13", nil, false},
		{"the number is not after the first word", "wrapper pandoc 3.13", nil, false},
		{"empty", "", nil, false},
		{"name only", "NVIM", nil, false},
		{"no version", "pandoc version unknown", nil, false},
		{"v alone", "v", nil, false},
		{"digits glued to a name", "tool2 build", nil, false},
		{"too large a number", "pandoc 99999999999999999999", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseToolVersion(c.line)
			if ok != c.ok || (ok && got.String() != c.want.String()) {
				t.Errorf("parseToolVersion(%q) = %v, %v; want %v, %v", c.line, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestToolVersionParseNumber(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"0.12.0", "0.12.0", true},
		{"3.13", "3.13", true},
		{"", "", false},
		{"v0.12", "", false},
		{"0..12", "", false},
		{"0.12.", "", false},
		{"+1.2", "", false},
		{"-1.2", "", false},
		{"1.2-rc1", "", false},
	} {
		got, ok := parseVersionNumber(c.in)
		if ok != c.ok || (ok && got.String() != c.want) {
			t.Errorf("parseVersionNumber(%q) = %v, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestToolVersionLess(t *testing.T) {
	for _, c := range []struct {
		name string
		a, b toolVersion
		want bool
	}{
		{"older minor", v(0, 11, 9), v(0, 12, 0), true},
		{"equal", v(0, 12, 0), v(0, 12, 0), false},
		{"shorter equals padded", v(0, 12), v(0, 12, 0), false},
		{"padded equals shorter", v(0, 12, 0), v(0, 12), false},
		{"numeric, not lexical", v(3, 9), v(3, 13), true},
		{"numeric, not lexical, reversed", v(3, 13), v(3, 9), false},
		{"patch above", v(3, 13, 1), v(3, 13), false},
		{"patch below", v(3, 13), v(3, 13, 1), true},
		{"ten is above nine", v(10, 0), v(9, 9), false},
		{"four parts", v(3, 1, 11, 1), v(3, 1, 11, 2), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.less(c.b); got != c.want {
				t.Errorf("%v.less(%v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestToolVersionMinimumsParse(t *testing.T) {
	for name, s := range map[string]string{"MinNvimVersion": config.MinNvimVersion, "MinPandocVersion": config.MinPandocVersion} {
		if _, ok := parseVersionNumber(s); !ok {
			t.Errorf("%s = %q does not parse", name, s)
		}
	}
}

func TestToolVersionProbe(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{"valid", `echo "pandoc 3.13"`, "3.13", false},
		{"first line only", "echo 'NVIM v0.12.6'\necho 'Build type: Release'", "0.12.6", false},
		{"exit 1", `echo "pandoc 3.13"; exit 1`, "", true},
		{"empty output", `exit 0`, "", true},
		{"garbage", `echo "usage: tool [options]"`, "", true},
		{"stderr is not read", `echo "pandoc 3.13" >&2; echo "unrelated"`, "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := writeScript(t, t.TempDir(), "tool", c.body)
			got, err := probeVersion(ctx, p, nil)
			if (err != nil) != c.wantErr || (err == nil && got.String() != c.want) {
				t.Errorf("probeVersion = %v, %v; want %q, error=%v", got, err, c.want, c.wantErr)
			}
		})
	}
}

func TestToolVersionProbeEnv(t *testing.T) {
	p := writeScript(t, t.TempDir(), "tool", `echo "pandoc $FAKE_VERSION"`)
	got, err := probeVersion(context.Background(), p, []string{"FAKE_VERSION=3.14.2"})
	if err != nil || got.String() != "3.14.2" {
		t.Errorf("probeVersion = %v, %v; want 3.14.2", got, err)
	}
}

func TestToolVersionProbeStdinIsClosed(t *testing.T) {
	// A tool that waits for input would hang on an open stdin.
	p := writeScript(t, t.TempDir(), "tool", `cat >/dev/null; echo "pandoc 3.13"`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if got, err := probeVersion(ctx, p, nil); err != nil || got.String() != "3.13" {
		t.Errorf("probeVersion = %v, %v; want 3.13", got, err)
	}
}

// processGone polls until the process no longer exists.
func processGone(pid int, within time.Duration) bool {
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
	}
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func TestToolVersionProbeHangingToolIsKilledWithItsChild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	// The sleep is a child of the shell, not the shell itself.
	p := writeScript(t, dir, "tool", "sleep 30 &\necho $! > "+pidFile+"\nwait")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := probeVersion(ctx, p, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v, want about the deadline", d)
	}
	raw, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatalf("the script did not write its child's pid: %v", rerr)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if perr != nil {
		t.Fatal(perr)
	}
	if !processGone(pid, 2*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("the child %d is still alive", pid)
	}
}

func TestToolVersionProbeEndlessOutput(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	p := writeScript(t, dir, "tool", "echo $$ > "+pidFile+"\nwhile :; do echo 'pandoc 3.13 and more and more'; done")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := probeVersion(ctx, p, nil)
	if err == nil {
		t.Fatal("a tool that never stops printing was accepted")
	}
	if errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Errorf("the output limit did not end it quickly: %v after %v", err, time.Since(start))
	}
	raw, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if perr != nil {
		t.Fatal(perr)
	}
	if !processGone(pid, 2*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("the tool %d is still alive", pid)
	}
}

func TestToolVersionProbeMissingBinary(t *testing.T) {
	if _, err := probeVersion(context.Background(), filepath.Join(t.TempDir(), "nope"), nil); err == nil {
		t.Error("a missing binary gave no error")
	}
}

// countingTool is a fake that appends one line to a log on every run.
func countingTool(t *testing.T, dir, name, version string) (logFile string) {
	t.Helper()
	logFile = filepath.Join(dir, name+".log")
	writeScript(t, dir, name, "echo run >> "+logFile+"\necho \"pandoc "+version+"\"")
	return logFile
}

func runs(t *testing.T, logFile string) int {
	t.Helper()
	raw, err := os.ReadFile(logFile)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(raw), "\n")
}

// These tests change PATH, so none of them is parallel.
func TestToolVersionCacheAsksOnce(t *testing.T) {
	bin, work := t.TempDir(), t.TempDir()
	logFile := countingTool(t, work, "faketool", "3.13")
	if err := os.Symlink(filepath.Join(work, "faketool"), filepath.Join(bin, "faketool")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	var c versionCache
	for i := 0; i < 3; i++ {
		path, got, err := c.get(context.Background(), "faketool", nil)
		if err != nil || got.String() != "3.13" {
			t.Fatalf("get #%d = %v, %v", i, got, err)
		}
		if path != filepath.Join(bin, "faketool") {
			t.Errorf("path = %q, want the one found in PATH", path)
		}
	}
	if n := runs(t, logFile); n != 1 {
		t.Errorf("the tool ran %d times, want 1", n)
	}
}

func TestToolVersionCacheNoticesReplacement(t *testing.T) {
	bin := t.TempDir()
	logFile := countingTool(t, bin, "faketool", "3.13")
	t.Setenv("PATH", bin)

	var c versionCache
	if _, got, err := c.get(context.Background(), "faketool", nil); err != nil || got.String() != "3.13" {
		t.Fatalf("first get = %v, %v", got, err)
	}
	// A new content with a new modification time, as after an upgrade.
	countingTool(t, bin, "faketool", "3.14.1")
	if err := os.Chtimes(filepath.Join(bin, "faketool"), time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, got, err := c.get(context.Background(), "faketool", nil)
	if err != nil || got.String() != "3.14.1" {
		t.Fatalf("after the replacement get = %v, %v; want 3.14.1", got, err)
	}
	if runs(t, logFile) != 2 {
		t.Errorf("the tool ran %d times, want 2", runs(t, logFile))
	}
}

func TestToolVersionCacheNoticesReplacedSymlinkTarget(t *testing.T) {
	bin, work := t.TempDir(), t.TempDir()
	countingTool(t, work, "old", "3.13")
	countingTool(t, work, "new", "3.15")
	link := filepath.Join(bin, "faketool")
	if err := os.Symlink(filepath.Join(work, "old"), link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	var c versionCache
	if _, got, err := c.get(context.Background(), "faketool", nil); err != nil || got.String() != "3.13" {
		t.Fatalf("first get = %v, %v", got, err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(work, "new"), link); err != nil {
		t.Fatal(err)
	}
	if _, got, err := c.get(context.Background(), "faketool", nil); err != nil || got.String() != "3.15" {
		t.Fatalf("after the link moved get = %v, %v; want 3.15", got, err)
	}

	// The target rewritten in place, the link untouched.
	countingTool(t, work, "new", "3.16.2")
	if err := os.Chtimes(filepath.Join(work, "new"), time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, got, err := c.get(context.Background(), "faketool", nil); err != nil || got.String() != "3.16.2" {
		t.Fatalf("after the target changed get = %v, %v; want 3.16.2", got, err)
	}
}

func TestToolVersionCacheMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var c versionCache
	_, _, err := c.get(context.Background(), "faketool", nil)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("err = %v, want exec.ErrNotFound", err)
	}
}

func TestToolVersionCacheDoesNotRememberErrors(t *testing.T) {
	bin := t.TempDir()
	flag := filepath.Join(bin, "ready")
	logFile := filepath.Join(bin, "runs.log")
	writeScript(t, bin, "faketool", "echo run >> "+logFile+"\nif [ -e "+flag+" ]; then echo 'pandoc 3.13'; else exit 1; fi")
	t.Setenv("PATH", bin)

	var c versionCache
	_, _, err := c.get(context.Background(), "faketool", nil)
	if err == nil || errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("first get err = %v, want a probe error", err)
	}
	if err := os.WriteFile(flag, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, got, err := c.get(context.Background(), "faketool", nil); err != nil || got.String() != "3.13" {
		t.Fatalf("second get = %v, %v; want 3.13", got, err)
	}
	if n := runs(t, logFile); n != 2 {
		t.Errorf("the tool ran %d times, want 2", n)
	}
}

func TestToolVersionCacheConcurrent(t *testing.T) {
	bin := t.TempDir()
	countingTool(t, bin, "faketool", "3.13")
	t.Setenv("PATH", bin)

	var c versionCache
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, got, err := c.get(context.Background(), "faketool", nil)
			if err == nil && got.String() != "3.13" {
				err = errors.New("wrong version " + got.String())
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

// listTree returns every path under dir, relative to it.
func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestToolVersionRealNvim(t *testing.T) {
	if _, err := exec.LookPath(config.NvimBin); err != nil {
		t.Skip("nvim not found in PATH")
	}
	// HOME points at an empty folder: whatever the probe writes where an
	// unredirected Neovim would (~/.local/state, ~/.config), ends up there and
	// is seen. The real home is never at risk, because the probe environment
	// redirects the XDG folders as well.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "NVIM_APPNAME"} {
		t.Setenv(k, filepath.Join(fakeHome, "unset-"+k))
	}
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := store.Nvim()

	var c versionCache
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, got, err := c.get(ctx, config.NvimBin, nvimProbeEnv(paths))
	if err != nil {
		t.Fatal(err)
	}
	min, _ := parseVersionNumber(config.MinNvimVersion)
	if got.less(min) {
		t.Errorf("nvim %v is below the minimum %v", got, min)
	}
	if left := listTree(t, fakeHome); len(left) != 0 {
		t.Errorf("the probe wrote outside the application home: %v", left)
	}
}

func TestToolVersionRealPandoc(t *testing.T) {
	if _, err := exec.LookPath(config.PandocBin); err != nil {
		t.Skip("pandoc not found in PATH")
	}
	var c versionCache
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, got, err := c.get(ctx, config.PandocBin, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	min, _ := parseVersionNumber(config.MinPandocVersion)
	if got.less(min) {
		t.Errorf("pandoc %v is below the minimum %v", got, min)
	}
}
