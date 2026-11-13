package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"Pisar/internal/config"
	"Pisar/internal/convert"
	"Pisar/internal/preview"
)

// openLog returns the log file opened for appending. It creates the folder (0700)
// and the file (0600), and sets both modes on an existing folder and file too: the
// log is Pisar's own file and may name documents, so nobody but the owner reads it.
// A file larger than maxBytes is first renamed to path+".1", replacing an older one.
func openLog(path string, maxBytes int64) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, config.LogDirPerm); err != nil {
		return nil, fmt.Errorf("log: create folder: %w", err)
	}
	// MkdirAll leaves an existing folder as it is, and the mode is subject to the umask.
	if err := os.Chmod(dir, config.LogDirPerm); err != nil {
		slog.Warn("log: cannot restrict the folder to its owner", "path", dir, "err", err)
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// First start: nothing to rotate.
	case err != nil:
		return nil, fmt.Errorf("log: stat %s: %w", path, err)
	case info.Size() > maxBytes:
		if err := os.Rename(path, path+".1"); err != nil {
			// A log that grows is better than no log: keep appending and say so.
			slog.Warn("log: cannot move the large log file aside", "path", path, "err", err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, config.LogFilePerm)
	if err != nil {
		return nil, fmt.Errorf("log: open %s: %w", path, err)
	}
	if err := f.Chmod(config.LogFilePerm); err != nil {
		slog.Warn("log: cannot restrict the file to its owner", "path", path, "err", err)
	}
	return f, nil
}

// fileWriter writes to the log file and never returns an error. io.MultiWriter
// stops at the first writer that fails, so a full disk or a closed file would
// silence the other output too. The first failure is reported on stderr (so it is
// not swallowed) and the file is dropped from then on: a log line that cannot be
// written must not stop or crash the application.
type fileWriter struct {
	mu     sync.Mutex
	w      io.Writer
	stderr io.Writer
	failed bool
}

func (f *fileWriter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed {
		return len(p), nil
	}
	if _, err := f.w.Write(p); err != nil {
		f.failed = true
		// Nothing more can be done if stderr fails as well.
		_, _ = fmt.Fprintf(f.stderr, "pisar: writing the log file failed, logging to stderr only: %v\n", err)
	}
	return len(p), nil
}

// drop stops writes to the file, so that a closed file is not reported as a failure.
func (f *fileWriter) drop() {
	f.mu.Lock()
	f.failed = true
	f.mu.Unlock()
}

// toolOmitted marks where the output of a tool was left out of a log line.
const toolOmitted = "[tool output omitted]"

// redactToolOutput is the handler's ReplaceAttr. pandoc quotes the line of the
// document it fails on in its stderr, and convert.ToolError and preview.ToolError
// carry that stderr in their text, so logging such an error would write the
// user's text into the log. The log must never hold document text. The rule is
// applied here, on the way out, and not where errors are logged: a call that
// is added later cannot forget it, and it covers every level and both outputs.
//
// An attribute that is not an error, or an error that holds no tool output,
// is returned as it came, without allocating: this runs for every attribute.
func redactToolOutput(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() != slog.KindAny {
		return a
	}
	err, ok := a.Value.Any().(error)
	if !ok {
		return a
	}
	if text, redacted := redactedError(err); redacted {
		a.Value = slog.StringValue(text)
	}
	return a
}

// toolFailure is a ToolError that carries output, in the terms redaction needs.
type toolFailure struct {
	own    string // the full text of the ToolError, output included
	prefix string // "convert" or "preview"
	tool   string
	cause  error
}

// safe is the text of the ToolError without its output.
func (f toolFailure) safe() string {
	return fmt.Sprintf("%s: %s failed: %s %s", f.prefix, f.tool, errorText(f.cause), toolOmitted)
}

// errorText is the text of err with any tool output removed; "<nil>" for nil.
func errorText(err error) string {
	if err == nil {
		return "<nil>"
	}
	if text, redacted := redactedError(err); redacted {
		return text
	}
	return err.Error()
}

// redactedError returns the text of err without the output of any ToolError in
// its chain, and true. It returns false when the chain holds none with output.
// The text keeps the wrappers around the ToolError ("export a.docx: ...").
func redactedError(err error) (string, bool) {
	failures := toolFailures(err, nil)
	if len(failures) == 0 {
		return "", false
	}
	text := err.Error()
	for _, f := range failures {
		if !strings.Contains(text, f.own) {
			// A wrapper that formats the error its own way: keep nothing of
			// the original text rather than risk a piece of the output.
			parts := make([]string, len(failures))
			for i, g := range failures {
				parts[i] = g.safe()
			}
			return strings.Join(parts, "; "), true
		}
		text = strings.ReplaceAll(text, f.own, f.safe())
	}
	return text, true
}

// toolFailures collects the ToolErrors with output in the tree of err (errors.Is
// and errors.As walk the same tree, Join included), appending to acc. It does
// not look inside one it found: errorText handles what that one wraps.
//
// The type switches below are on the error itself, not on what it wraps: this
// function is the walk that errors.As does, and it needs every match, not the first.
func toolFailures(err error, acc []toolFailure) []toolFailure {
	switch e := err.(type) { //nolint:errorlint // walks the tree itself
	case nil:
		return acc
	case *convert.ToolError:
		if e == nil {
			return acc
		}
		if e.Output != "" {
			return append(acc, toolFailure{own: e.Error(), prefix: "convert", tool: e.Tool, cause: e.Err})
		}
	case *preview.ToolError:
		if e == nil {
			return acc
		}
		if e.Output != "" {
			return append(acc, toolFailure{own: e.Error(), prefix: "preview", tool: e.Tool, cause: e.Err})
		}
	}
	switch u := err.(type) { //nolint:errorlint // walks the tree itself
	case interface{ Unwrap() error }:
		return toolFailures(u.Unwrap(), acc)
	case interface{ Unwrap() []error }:
		for _, inner := range u.Unwrap() {
			acc = toolFailures(inner, acc)
		}
	}
	return acc
}

// setupLogging makes slog write to stderr and to the log file. debug selects the
// level (Debug, otherwise Info). It never fails: when the file cannot be opened
// the log goes to stderr alone and says why. The returned function closes the file.
func setupLogging(debug bool) (closeLog func()) {
	return installLogging(os.Stderr, debug)
}

// installLogging is setupLogging with the stderr writer as a parameter, so that
// tests need not redirect the process's stderr.
func installLogging(stderr io.Writer, debug bool) (closeLog func()) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: redactToolOutput}

	path, err := config.LogFile()
	var f *os.File
	if err == nil {
		f, err = openLog(path, config.DefaultMaxLogBytes)
	}
	if err != nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(stderr, opts)))
		slog.Warn("pisar: the log file is not used, logging to stderr only", "err", err)
		return func() {}
	}

	fw := &fileWriter{w: f, stderr: stderr}
	// The file writer comes first because it never fails; stderr last, so that
	// whatever stderr returns cannot keep the file from being written.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(fw, stderr), opts)))

	var once sync.Once
	return func() {
		once.Do(func() {
			fw.drop()
			if err := f.Close(); err != nil {
				slog.Warn("log: close the log file", "err", err)
			}
		})
	}
}

// logStart writes the first record of a run. It carries no document text and no
// terminal output, only where things are.
func logStart() {
	home, homeErr := config.DefaultHome()
	logPath, logErr := config.LogFile()
	exe, exeErr := os.Executable()
	slog.Info("pisar: start", "home", home, "log", logPath, "exe", exe, "pid", os.Getpid())
	if err := errors.Join(homeErr, logErr, exeErr); err != nil {
		slog.Warn("pisar: start: some details are not known", "err", err)
	}
}
