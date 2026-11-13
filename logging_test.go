package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Pisar/internal/convert"
	"Pisar/internal/preview"
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

// The log is Pisar's own file and may name documents: an older file or folder that
// was left open to others is closed to them by the next start.
func TestOpenLogRestrictsExistingModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil { // Mkdir is subject to the umask
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pisar.log")
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
	for _, c := range []struct {
		path string
		want os.FileMode
	}{{dir, 0o700}, {path, 0o600}} {
		info, err := os.Stat(c.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != c.want {
			t.Errorf("mode of %s = %o, want %o", filepath.Base(c.path), got, c.want)
		}
	}
	if got := readLogText(t, path); got != "old\n" {
		t.Errorf("content = %q, the existing log must be kept", got)
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

const secretText = "TAJNI TEKST 9911"

// loggedOutput is what a logger installed by installLogging wrote to the file
// and to stderr.
type loggedOutput struct{ file, stderr string }

// logWith installs the real logger (a temporary home, a captured stderr), runs
// emit and returns what both outputs received. blocked makes the log folder
// impossible to create, which leaves the stderr-only logger.
func logWith(t *testing.T, debug, blocked bool, emit func()) loggedOutput {
	t.Helper()
	restoreDefaultLogger(t)
	home := t.TempDir()
	if blocked {
		home = blockLogsFolder(t)
	}
	t.Setenv("PISAR_HOME", home)
	var stderr bytes.Buffer
	closeLog := installLogging(&stderr, debug)
	emit()
	closeLog()
	out := loggedOutput{stderr: stderr.String()}
	if !blocked {
		out.file = readLogText(t, filepath.Join(home, "logs", "pisar.log"))
	}
	return out
}

func convertSecret() error {
	return &convert.ToolError{Tool: "pandoc", Output: "2 | title: \"" + secretText, Err: errors.New("exit status 64")}
}

func previewSecret() error {
	return &preview.ToolError{Tool: "pandoc", Output: "2 | title: \"" + secretText, Err: errors.New("exit status 64")}
}

func assertNoSecret(t *testing.T, out loggedOutput, blocked bool) {
	t.Helper()
	if strings.Contains(out.file, secretText) || strings.Contains(out.stderr, secretText) {
		t.Errorf("the document text is in the log:\nfile:   %q\nstderr: %q", out.file, out.stderr)
	}
	if !blocked && !strings.Contains(out.file, "level=") {
		t.Errorf("nothing was logged to the file: %q", out.file)
	}
	if !strings.Contains(out.stderr, "level=") {
		t.Errorf("nothing was logged to stderr: %q", out.stderr)
	}
}

func TestLogRedactsToolOutput(t *testing.T) {
	other := errors.New("other")
	tests := []struct {
		name string
		emit func()
		want []string
	}{
		{
			name: "convert",
			emit: func() { slog.Warn("x", "err", convertSecret()) },
			want: []string{"convert: pandoc failed: exit status 64", toolOmitted},
		},
		{
			name: "preview",
			emit: func() { slog.Warn("x", "err", previewSecret()) },
			want: []string{"preview: pandoc failed: exit status 64", toolOmitted},
		},
		{
			name: "wrapped",
			emit: func() { slog.Warn("x", "err", fmt.Errorf("export a.docx: %w", convertSecret())) },
			want: []string{"export a.docx: convert: pandoc failed: exit status 64", toolOmitted},
		},
		{
			name: "wrapped twice",
			emit: func() {
				slog.Warn("x", "err", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", previewSecret())))
			},
			want: []string{"outer: inner: preview: pandoc failed", toolOmitted},
		},
		{
			name: "joined",
			emit: func() { slog.Warn("x", "err", errors.Join(convertSecret(), other)) },
			want: []string{"other", "exit status 64", toolOmitted},
		},
		{
			name: "both kinds in one chain",
			emit: func() { slog.Warn("x", "err", errors.Join(convertSecret(), previewSecret())) },
			want: []string{"convert: pandoc failed", "preview: pandoc failed", toolOmitted},
		},
		{
			name: "another key",
			emit: func() { slog.Warn("x", "cause", convertSecret()) },
			want: []string{"cause=", toolOmitted},
		},
		{
			name: "inside a group",
			emit: func() { slog.Warn("x", slog.Group("g", slog.Any("err", convertSecret()))) },
			want: []string{"g.err=", toolOmitted},
		},
		{
			name: "inside a nested group",
			emit: func() { slog.Warn("x", slog.Group("a", slog.Group("b", "err", convertSecret()))) },
			want: []string{"a.b.err=", toolOmitted},
		},
		{
			name: "from a logger with attributes",
			emit: func() { slog.With("err", convertSecret()).Warn("x") },
			want: []string{"err=", toolOmitted},
		},
		{
			name: "a tool error inside a tool error",
			emit: func() {
				slog.Warn("x", "err", &convert.ToolError{Tool: "pandoc", Output: "outer " + secretText, Err: previewSecret()})
			},
			want: []string{"convert: pandoc failed", toolOmitted},
		},
	}
	for _, tt := range tests {
		for _, blocked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stderr only=%v", tt.name, blocked), func(t *testing.T) {
				out := logWith(t, false, blocked, tt.emit)
				assertNoSecret(t, out, blocked)
				for _, want := range tt.want {
					if !strings.Contains(out.stderr, want) {
						t.Errorf("stderr lacks %q: %q", want, out.stderr)
					}
					if !blocked && !strings.Contains(out.file, want) {
						t.Errorf("file lacks %q: %q", want, out.file)
					}
				}
			})
		}
	}
}

func TestLogRedactsAtEveryLevel(t *testing.T) {
	for _, c := range []struct {
		name  string
		level slog.Level
	}{
		{"debug", slog.LevelDebug}, {"info", slog.LevelInfo}, {"warn", slog.LevelWarn}, {"error", slog.LevelError},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := logWith(t, true, false, func() {
				slog.Log(context.Background(), c.level, "x", "err", convertSecret())
			})
			assertNoSecret(t, out, false)
			if !strings.Contains(out.file, "level="+c.level.String()) {
				t.Errorf("the record is not in the file at its level: %q", out.file)
			}
		})
	}
}

func TestLogLeavesOtherAttributesAlone(t *testing.T) {
	plain := errors.New("plain: " + secretText) // not a tool error: its text is the caller's business
	wrapped := fmt.Errorf("wrap: %w", plain)
	noOutput := &convert.ToolError{Tool: "textutil", Err: errors.New("exit status 1")}
	out := logWith(t, false, false, func() {
		slog.Warn("x", "plain", plain, "wrapped", wrapped, "text", "just "+secretText, "n", 42, "nooutput", noOutput)
	})
	for _, got := range []string{out.file, out.stderr} {
		for _, want := range []string{
			`plain="plain: ` + secretText + `"`,
			`wrapped="wrap: plain: ` + secretText + `"`,
			`text="just ` + secretText + `"`,
			"n=42",
			`nooutput="convert: textutil failed: exit status 1"`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("record lacks %q: %q", want, got)
			}
		}
		if strings.Contains(got, toolOmitted) {
			t.Errorf("something was redacted that holds no tool output: %q", got)
		}
	}
}

// A wrapper that formats the error its own way hides where the ToolError's text
// is; the whole value must then be the redacted ToolError, not the original.
type reformatting struct{ err error }

func (r reformatting) Error() string { return strings.ToUpper(r.err.Error()) }
func (r reformatting) Unwrap() error { return r.err }

func TestLogRedactsWhenTheTextIsNotFound(t *testing.T) {
	out := logWith(t, false, false, func() {
		slog.Warn("x", "err", reformatting{convertSecret()})
	})
	assertNoSecret(t, out, false)
	if strings.Contains(strings.ToUpper(out.file), strings.ToUpper(secretText)) {
		t.Errorf("the text survived in another case: %q", out.file)
	}
	if !strings.Contains(out.file, "convert: pandoc failed: exit status 64 "+toolOmitted) {
		t.Errorf("the redacted ToolError is not the value: %q", out.file)
	}
}

func TestRedactToolOutputDoesNotAllocateForOrdinaryAttributes(t *testing.T) {
	plain := errors.New("plain")
	wrapped := fmt.Errorf("wrap: %w", plain)
	attrs := []slog.Attr{slog.String("k", "v"), slog.Int("n", 1), slog.Any("err", plain), slog.Any("err", wrapped)}
	got := testing.AllocsPerRun(100, func() {
		for _, a := range attrs {
			_ = redactToolOutput(nil, a)
		}
	})
	if got != 0 {
		t.Errorf("%v allocations for attributes that need no change, want 0", got)
	}
}

// The path the review found: a failed export logs the error as it is.
func TestExportFailedLogsNoDocumentText(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"convert", fmt.Errorf("export a.docx: %w", convertSecret())},
		{"preview", fmt.Errorf("export a.docx: %w", previewSecret())},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := &EditorService{emit: func(string, any) {}}
			out := logWith(t, true, false, func() {
				if got := e.exportFailed("/docs/a.docx", tt.err); !errors.Is(got, tt.err) {
					t.Errorf("exportFailed returned %v, want the error it was given", got)
				}
				// Not the path above but the same shape, for the other log sites.
				_ = failEditor("start", tt.err)
			})
			assertNoSecret(t, out, false)
			if !strings.Contains(out.file, "editor: export failed") {
				t.Errorf("the failure is not in the log: %q", out.file)
			}
		})
	}
}
