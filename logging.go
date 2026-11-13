package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"Pisar/internal/config"
)

const (
	logDirPerm  os.FileMode = 0o700
	logFilePerm os.FileMode = 0o600
)

// openLog returns the log file opened for appending. It creates the folder (0700)
// and the file (0600). A file larger than maxBytes is first renamed to path+".1",
// replacing an older one. An existing file keeps its permissions.
func openLog(path string, maxBytes int64) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), logDirPerm); err != nil {
		return nil, fmt.Errorf("log: create folder: %w", err)
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
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, logFilePerm)
	if err != nil {
		return nil, fmt.Errorf("log: open %s: %w", path, err)
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
	opts := &slog.HandlerOptions{Level: level}

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
