package convert

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Names inside a working folder. The folder itself belongs to the caller.
const (
	MarkdownName = "doc.md"
	MediaDirName = "media"
)

// Markdown dialects for reading a .docx. -smart keeps typographic quotes and
// dashes as written; without it they come back as straight quotes and "---".
// Pipe tables are stable across a round trip, but pandoc cannot write a table
// with block content in a cell (several paragraphs, a list) as one, and falls
// back to raw HTML, which is lost when the Markdown is written back to .docx.
// Grid tables can hold such cells, so they are the second attempt.
const (
	formatPipeTables = "markdown-smart-multiline_tables-simple_tables-grid_tables"
	formatGridTables = "markdown-smart-multiline_tables-simple_tables"

	// formatRead is the dialect Markdown is read in when it is written to
	// .docx: everything on, so that both table kinds and what the user types
	// by hand are understood.
	formatRead = "markdown-smart"

	rawTablePrefix = "<table"
)

// maxToolOutput bounds the stderr kept in a ToolError and the bytes the default
// Runner collects from a tool. pandoc and textutil write one line of complaint;
// anything longer is noise that must not grow without limit.
const maxToolOutput = 4096

// killGrace is how long a killed tool may keep its pipes open before the
// default Runner gives up waiting for them. Without it a grandchild that
// inherited stderr could hold Wait forever after the timeout.
const killGrace = 2 * time.Second

const (
	toolPandoc   = "pandoc"
	toolTextutil = "textutil"
)

// Runner runs an external command in dir and returns what it wrote.
type Runner func(ctx context.Context, dir, name string, args ...string) (stdout, stderr []byte, err error)

// Options configures a Converter.
type Options struct {
	PandocBin   string        // required
	TextutilBin string        // required
	Timeout     time.Duration // one external command; required, > 0
	Limits      InspectLimits // for every archive the package reads
	Run         Runner        // nil means exec.CommandContext
}

// Converter runs the conversions. It holds no state besides its options, so it
// is safe for concurrent use.
type Converter struct {
	pandocBin   string
	textutilBin string
	timeout     time.Duration
	limits      InspectLimits
	run         Runner
}

// New validates opts and returns a Converter.
func New(opts Options) (*Converter, error) {
	if opts.PandocBin == "" {
		return nil, errors.New("convert: PandocBin is required")
	}
	if opts.TextutilBin == "" {
		return nil, errors.New("convert: TextutilBin is required")
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("convert: Timeout must be positive, got %v", opts.Timeout)
	}
	// A zero InspectLimits refuses every archive. Failing here is better than a
	// converter that fails on its first document.
	if opts.Limits.MaxEntries <= 0 || opts.Limits.MaxPartBytes <= 0 {
		return nil, fmt.Errorf("convert: Limits must be positive, got %+v", opts.Limits)
	}
	run := opts.Run
	if run == nil {
		run = execRunner
	}
	return &Converter{
		pandocBin:   opts.PandocBin,
		textutilBin: opts.TextutilBin,
		timeout:     opts.Timeout,
		limits:      opts.Limits,
		run:         run,
	}, nil
}

// ErrToolMissing: pandoc or textutil is not installed (or not where it was
// expected).
var ErrToolMissing = errors.New("convert: converter is not installed")

// errNoOutput: a tool reported success but did not leave a usable output file.
var errNoOutput = errors.New("convert: the converter produced no usable output")

// ToolError is a failed run of pandoc or textutil.
type ToolError struct {
	Tool   string // "pandoc" or "textutil"
	Output string // what the tool wrote to stderr, trimmed and bounded
	Err    error
}

func (e *ToolError) Error() string {
	if e.Output == "" {
		return fmt.Sprintf("convert: %s failed: %v", e.Tool, e.Err)
	}
	return fmt.Sprintf("convert: %s failed: %v: %s", e.Tool, e.Err, e.Output)
}

func (e *ToolError) Unwrap() error { return e.Err }

// cappedBuffer keeps the first max bytes written to it and drops the rest. It
// never fails, so a chatty tool is not stopped by it.
type cappedBuffer struct {
	buf []byte
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.max - len(b.buf); room > 0 {
		b.buf = append(b.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// execRunner is the Runner used when Options.Run is nil. Arguments are passed
// as separate elements; nothing goes through a shell.
func execRunner(ctx context.Context, dir, name string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = killGrace
	out := &cappedBuffer{max: maxToolOutput}
	errb := &cappedBuffer{max: maxToolOutput}
	cmd.Stdout = out
	cmd.Stderr = errb
	err = cmd.Run()
	return out.buf, errb.buf, err
}

// boundOutput turns raw stderr into the short text a ToolError carries.
func boundOutput(b []byte) string {
	s := strings.TrimSpace(strings.ToValidUTF8(string(b), "�"))
	if len(s) <= maxToolOutput {
		return s
	}
	cut := maxToolOutput
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// call runs one external command under the converter's timeout and sorts the
// failure: the context first (a timeout or a cancel is not the tool's fault),
// then a missing tool, then everything else as a ToolError.
func (c *Converter) call(ctx context.Context, tool, bin, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	_, stderr, err := c.run(ctx, dir, bin, args...)
	if err == nil {
		return nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("convert: %s: %w", tool, cerr)
	}
	if toolMissing(err, bin) {
		return fmt.Errorf("%w: %s: %w", ErrToolMissing, tool, err)
	}
	return &ToolError{Tool: tool, Output: boundOutput(stderr), Err: err}
}

// toolMissing tells "there is no such program" from a failure of something
// else that also reports "no such file", such as a working folder that does
// not exist (that one is a PathError for the folder, not for the program).
func toolMissing(err error, bin string) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var pe *fs.PathError
	return errors.As(err, &pe) && pe.Path == bin && errors.Is(err, fs.ErrNotExist)
}

// absPath makes a path absolute, so that a name starting with "-" can never
// be taken for an option, and so that it means the same in any working folder.
func absPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("convert: empty path")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("convert: %w", err)
	}
	return abs, nil
}

// checkRegular fails unless path is a regular file. A FIFO would block a tool
// until somebody writes to it.
func checkRegular(path string) (fs.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("convert: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("convert: %s is not a regular file", path)
	}
	return info, nil
}

// checkOutputFree refuses an output path that already holds something. The
// only thing allowed there is an empty regular file, which is what a caller
// that made a temp file for the tool leaves behind. This keeps a stale output
// from passing for a fresh one, and a wrong dst from overwriting a document.
func checkOutputFree(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("convert: %w", err)
	case !info.Mode().IsRegular() || info.Size() != 0:
		return fmt.Errorf("convert: output %s: %w", path, fs.ErrExist)
	}
	return nil
}

// removePartial removes a failed conversion's output and returns err, with the
// failure to remove it joined on.
func removePartial(path string, err error) error {
	if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
		return errors.Join(err, fmt.Errorf("convert: remove partial output: %w", rerr))
	}
	return err
}

// checkDocx verifies a freshly written .docx: it exists, is not empty and is a
// valid archive with word/document.xml.
func (c *Converter) checkDocx(ctx context.Context, path string) error {
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return fmt.Errorf("%w: %w", errNoOutput, err)
	case !info.Mode().IsRegular() || info.Size() == 0:
		return fmt.Errorf("%w: %s is empty", errNoOutput, filepath.Base(path))
	}
	if _, err := Inspect(ctx, path, c.limits); err != nil {
		return fmt.Errorf("convert: invalid output: %w", err)
	}
	return nil
}

// DocxToMarkdown writes dir/doc.md and dir/media/* from src. dir must exist
// and must not hold a doc.md already (fs.ErrExist, before any tool runs), so
// an old Markdown can never pass for a fresh one. An existing media/ is not
// looked at. src is only read.
//
// It returns []Loss{LossTables} when even the grid dialect left a table as raw
// HTML (a line that begins with "<table"): that table would be lost on the
// way back to .docx. doc.md stays; what to do about it is the caller's. The
// result is nil otherwise.
func (c *Converter) DocxToMarkdown(ctx context.Context, src, dir string) ([]Loss, error) {
	srcAbs, err := absPath(src)
	if err != nil {
		return nil, err
	}
	dirAbs, err := absPath(dir)
	if err != nil {
		return nil, err
	}
	if _, err := checkRegular(srcAbs); err != nil {
		return nil, err
	}
	if info, err := os.Stat(dirAbs); err != nil {
		return nil, fmt.Errorf("convert: %w", err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("convert: %s is not a directory", dirAbs)
	}
	md := filepath.Join(dirAbs, MarkdownName)
	if _, err := os.Lstat(md); err == nil {
		return nil, fmt.Errorf("convert: %s: %w", md, fs.ErrExist)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("convert: %w", err)
	}

	// The second pass overwrites the doc.md of the first: same call, same output.
	for _, format := range [...]string{formatPipeTables, formatGridTables} {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("convert: %w", err)
		}
		// Working directory = dir, so doc.md and media/ are relative to it:
		// --extract-media=media would give media/media/, and an absolute
		// folder would put absolute paths in the Markdown.
		err := c.call(ctx, toolPandoc, c.pandocBin, dirAbs,
			"-f", "docx", "-t", format, "--wrap=none", "--extract-media=.",
			"-o", MarkdownName, srcAbs)
		if err != nil {
			return nil, err
		}
		if _, err := checkRegular(md); err != nil {
			return nil, fmt.Errorf("%w: %w", errNoOutput, err)
		}
		raw, err := startsRawTable(md)
		if err != nil {
			return nil, err
		}
		if !raw {
			return nil, nil
		}
	}
	// The grid dialect still gave a raw HTML table; pandoc has nothing better.
	return []Loss{LossTables}, nil
}

// startsRawTable reports whether some line of the file begins with "<table",
// which is how pandoc writes a table it cannot express in the dialect. Text
// that merely contains "<table" is escaped by pandoc (\<table) and does not
// begin with it. The file is read line by line so a very long paragraph
// (--wrap=none) costs no memory.
func startsRawTable(path string) (found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("convert: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only handle: a failed Close loses nothing

	r := bufio.NewReaderSize(f, 64<<10)
	atStart := true
	for {
		line, isPrefix, err := r.ReadLine()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("convert: read %s: %w", MarkdownName, err)
		}
		if atStart && bytes.HasPrefix(line, []byte(rawTablePrefix)) {
			return true, nil
		}
		atStart = !isPrefix
	}
}

// MarkdownToDocx writes dst from dir/doc.md, with reference as the reference document.
// dst must be inside a folder Pisar owns; this package never writes next to the user's file.
func (c *Converter) MarkdownToDocx(ctx context.Context, dir, reference, dst string) error {
	dirAbs, err := absPath(dir)
	if err != nil {
		return err
	}
	refAbs, err := absPath(reference)
	if err != nil {
		return err
	}
	dstAbs, err := absPath(dst)
	if err != nil {
		return err
	}
	md := filepath.Join(dirAbs, MarkdownName)
	if _, err := checkRegular(md); err != nil {
		return err
	}
	if _, err := checkRegular(refAbs); err != nil {
		return err
	}
	if dstAbs == refAbs || dstAbs == md {
		return fmt.Errorf("convert: output %s is also an input", dstAbs)
	}
	if err := checkOutputFree(dstAbs); err != nil {
		return err
	}

	err = c.call(ctx, toolPandoc, c.pandocBin, dirAbs,
		"-f", formatRead, "-t", "docx",
		"--reference-doc="+refAbs, "--resource-path="+dirAbs,
		"-o", dstAbs, md)
	if err == nil {
		err = c.checkDocx(ctx, dstAbs)
	}
	if err != nil {
		return removePartial(dstAbs, err)
	}
	return nil
}

// DocToDocx converts an old .doc into dst with textutil. src is only read.
func (c *Converter) DocToDocx(ctx context.Context, src, dst string) error {
	srcAbs, err := absPath(src)
	if err != nil {
		return err
	}
	dstAbs, err := absPath(dst)
	if err != nil {
		return err
	}
	if _, err := checkRegular(srcAbs); err != nil {
		return err
	}
	if dstAbs == srcAbs {
		return fmt.Errorf("convert: output %s is also the input", dstAbs)
	}
	if err := checkOutputFree(dstAbs); err != nil {
		return err
	}

	// textutil exits 0 even when it fails ("The file doesn't exist"), so the
	// output file is the only proof. The working folder is dst's: a tool must
	// never have the user's folder as its own.
	err = c.call(ctx, toolTextutil, c.textutilBin, filepath.Dir(dstAbs),
		"-convert", "docx", "-output", dstAbs, srcAbs)
	if err == nil {
		err = c.checkDocx(ctx, dstAbs)
	}
	if err != nil {
		return removePartial(dstAbs, err)
	}
	return nil
}
