package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// restoreDefaultLogger puts the current default logger back when the test ends,
// so that a test that installs its own does not leak it into the others.
func restoreDefaultLogger(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func readLogText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeLogFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenLogCreatesAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "pisar.log")

	f, err := openLog(path, 1024)
	if err != nil {
		t.Fatalf("openLog: %v", err)
	}
	if _, err := f.WriteString("first\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("folder mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}

	f, err = openLog(path, 1024)
	if err != nil {
		t.Fatalf("second openLog: %v", err)
	}
	if _, err := f.WriteString("second\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readLogText(t, path); got != "first\nsecond\n" {
		t.Errorf("content = %q, want both lines", got)
	}
}

func TestOpenLogKeepsExistingMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pisar.log")
	writeLogFile(t, path, "old\n")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := openLog(path, 1024)
	if err != nil {
		t.Fatalf("openLog: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("mode = %o, an existing file must not be changed", got)
	}
}

func TestOpenLogRotation(t *testing.T) {
	tests := []struct {
		name        string
		size        int
		older       string // content of an existing .1, "" for none
		wantRotated bool
	}{
		{name: "larger than the limit", size: 11, wantRotated: true},
		{name: "replaces an older .1", size: 11, older: "ancient", wantRotated: true},
		{name: "exactly at the limit", size: 10},
		{name: "smaller than the limit", size: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pisar.log")
			content := strings.Repeat("x", tt.size)
			writeLogFile(t, path, content)
			if tt.older != "" {
				writeLogFile(t, path+".1", tt.older)
			}

			f, err := openLog(path, 10)
			if err != nil {
				t.Fatalf("openLog: %v", err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}

			if tt.wantRotated {
				if got := readLogText(t, path+".1"); got != content {
					t.Errorf(".1 = %q, want the old log %q", got, content)
				}
				if got := readLogText(t, path); got != "" {
					t.Errorf("new log = %q, want it empty", got)
				}
				return
			}
			if got := readLogText(t, path); got != content {
				t.Errorf("log = %q, want it untouched", got)
			}
			if tt.older == "" {
				if _, err := os.Stat(path + ".1"); !errors.Is(err, os.ErrNotExist) {
					t.Errorf(".1 exists (err %v), want no rotation", err)
				}
			}
		})
	}
}

// blockLogsFolder puts an ordinary file where the folder "logs" would be.
func blockLogsFolder(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	writeLogFile(t, filepath.Join(home, "logs"), "not a folder")
	return home
}

func TestOpenLogFolderBlocked(t *testing.T) {
	home := blockLogsFolder(t)
	if f, err := openLog(filepath.Join(home, "logs", "pisar.log"), 1024); err == nil {
		_ = f.Close()
		t.Fatal("openLog succeeded although logs is a file")
	}
}

func TestSetupLoggingFolderBlocked(t *testing.T) {
	restoreDefaultLogger(t)
	t.Setenv("PISAR_HOME", blockLogsFolder(t))

	var stderr bytes.Buffer
	closeLog := installLogging(&stderr, false)
	if closeLog == nil {
		t.Fatal("installLogging returned no close function")
	}
	slog.Info("after the failure")
	closeLog()
	closeLog()

	out := stderr.String()
	if !strings.Contains(out, "logging to stderr only") || !strings.Contains(out, "level=WARN") {
		t.Errorf("stderr does not say why the file is not used: %q", out)
	}
	if !strings.Contains(out, "after the failure") {
		t.Errorf("stderr lost the record logged after the failure: %q", out)
	}

	// The real entry point must not panic either; its stderr is the test's.
	setupLogging(false)()
}

func TestSetupLoggingLevels(t *testing.T) {
	tests := []struct {
		name      string
		debug     bool
		wantDebug bool
	}{
		{name: "info", debug: false, wantDebug: false},
		{name: "debug", debug: true, wantDebug: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreDefaultLogger(t)
			home := t.TempDir()
			t.Setenv("PISAR_HOME", home)

			var stderr bytes.Buffer
			closeLog := installLogging(&stderr, tt.debug)
			slog.Debug("a debug record")
			slog.Info("an info record")
			closeLog()

			file := readLogText(t, filepath.Join(home, "logs", "pisar.log"))
			if !strings.Contains(file, "level=INFO") || !strings.Contains(file, `msg="an info record"`) {
				t.Errorf("the info record is not in the file: %q", file)
			}
			if got := strings.Contains(file, "a debug record"); got != tt.wantDebug {
				t.Errorf("debug record in file = %v, want %v: %q", got, tt.wantDebug, file)
			}
			if file != stderr.String() {
				t.Errorf("file and stderr differ:\nfile:   %q\nstderr: %q", file, stderr.String())
			}
		})
	}
}

// failingWriter is a file that cannot be written.
type failingWriter struct{ calls int }

func (w *failingWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, errors.New("disk full")
}

func TestFileWriterFailureKeepsOtherOutput(t *testing.T) {
	var stderr bytes.Buffer
	bad := &failingWriter{}
	fw := &fileWriter{w: bad, stderr: &stderr}
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(fw, &stderr), nil))

	logger.Info("one")
	logger.Info("two")

	out := stderr.String()
	if !strings.Contains(out, "msg=one") || !strings.Contains(out, "msg=two") {
		t.Errorf("stderr stopped receiving records after the file failed: %q", out)
	}
	if got := strings.Count(out, "disk full"); got != 1 {
		t.Errorf("the failure was reported %d times, want once: %q", got, out)
	}
	if bad.calls != 1 {
		t.Errorf("the failed file was written %d times, want 1", bad.calls)
	}
}

func TestFileClosedUnderTheLogger(t *testing.T) {
	f, err := openLog(filepath.Join(t.TempDir(), "pisar.log"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(&fileWriter{w: f, stderr: &stderr}, &stderr), nil))

	logger.Info("before")
	if err := f.Close(); err != nil { // the file goes away under the logger
		t.Fatal(err)
	}
	logger.Info("after")

	out := stderr.String()
	if !strings.Contains(out, "msg=before") || !strings.Contains(out, "msg=after") {
		t.Errorf("stderr lost a record when the file was closed: %q", out)
	}
	if !strings.Contains(out, "writing the log file failed") {
		t.Errorf("the failure was not reported: %q", out)
	}
}

func TestCloseLogTwice(t *testing.T) {
	restoreDefaultLogger(t)
	t.Setenv("PISAR_HOME", t.TempDir())
	closeLog := installLogging(&bytes.Buffer{}, false)
	closeLog()
	closeLog()
}

func TestLogStart(t *testing.T) {
	restoreDefaultLogger(t)
	home := t.TempDir()
	t.Setenv("PISAR_HOME", home)
	var stderr bytes.Buffer
	closeLog := installLogging(&stderr, false)
	logStart()
	closeLog()

	file := readLogText(t, filepath.Join(home, "logs", "pisar.log"))
	for _, want := range []string{`msg="pisar: start"`, "home=" + home, "pid=", "exe="} {
		if !strings.Contains(file, want) {
			t.Errorf("start record lacks %q: %q", want, file)
		}
	}
}
