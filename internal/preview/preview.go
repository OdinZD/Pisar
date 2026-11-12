// Package preview turns the Markdown of a buffer into the HTML of a page body
// for the preview pane.
//
// The text may be somebody else's document, and the HTML is shown inside the
// application, so the guarantee of this package is about the output: nothing in
// it runs, nothing in it is loaded from the network, and no file outside the
// document's folder is read into it. The guarantee is kept by pandoc itself and
// is not repaired in Go afterwards: a Lua filter (filters/preview.lua) removes all
// raw content, links, foreign pictures and attributes, and the reader is run
// without raw TeX and raw attributes. Output is returned exactly as pandoc wrote it.
package preview

import (
	"bytes"
	"context"
	_ "embed" // filterLua
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// filterName is the file in Options.Dir that holds the filter. It is Pisar's own,
// so a file of that name is replaced.
const filterName = "preview-filter.lua"

const (
	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
)

// readerFormat is the Markdown dialect the text is read in. -smart keeps the
// typographic quotes and dashes as the author typed them. Raw HTML stays on
// (with native_divs, native_spans and markdown_in_html_blocks, which come with
// it): pandoc writes "<!-- -->" between two lists when it converts a .docx, and
// with raw HTML off that comment would be shown as text. Nothing raw reaches the
// page all the same, because the filter removes every RawBlock and RawInline and
// strips the attributes of the Div and Span that HTML turns into. Raw TeX and raw
// attributes have no use here and stay off.
const readerFormat = "markdown-smart-raw_tex-raw_attribute"

// maxToolOutput bounds the stderr kept in a ToolError and the bytes the default
// Runner collects from stderr. pandoc writes a line or two of complaint; more is
// noise that must not grow without limit.
const maxToolOutput = 4096

// killGrace is how long a killed pandoc may keep its pipes open before the
// default Runner stops waiting for them.
const killGrace = 2 * time.Second

const toolPandoc = "pandoc"

// filterLua is the second layer of the guarantee; see the comment in the file.
//
//go:embed filters/preview.lua
var filterLua []byte

// Runner runs an external command in dir with stdin as its standard input and
// returns what it wrote. It must stop the command and return ErrTooLarge as soon
// as stdout would grow past maxStdout bytes, instead of collecting the rest.
type Runner func(ctx context.Context, dir, name string, stdin []byte, maxStdout int, args ...string) (stdout, stderr []byte, err error)

// Options configures a Renderer.
type Options struct {
	PandocBin string        // required
	Dir       string        // a folder Pisar owns, absolute; the filter is written there and pandoc runs there. required
	Timeout   time.Duration // one conversion; required, > 0
	MaxSource int           // bytes of Markdown accepted; required, > 0
	MaxHTML   int           // bytes of HTML produced; required, > 0
	Run       Runner        // nil means exec.CommandContext
}

// Renderer converts Markdown to HTML. It is safe for concurrent use.
type Renderer struct {
	pandocBin string
	dir       string
	timeout   time.Duration
	maxSource int
	maxHTML   int
	run       Runner

	mu sync.Mutex // guards the filter file
}

// New validates opts and returns a Renderer. It touches nothing on disk; the
// filter is written by the first Render.
func New(opts Options) (*Renderer, error) {
	switch {
	case opts.PandocBin == "":
		return nil, errors.New("preview: PandocBin is required")
	case opts.Dir == "":
		return nil, errors.New("preview: Dir is required")
	// pandoc runs in Dir, so a relative filter path would not mean what the
	// caller thinks it means.
	case !filepath.IsAbs(opts.Dir):
		return nil, fmt.Errorf("preview: Dir must be an absolute path, got %q", opts.Dir)
	case opts.Timeout <= 0:
		return nil, fmt.Errorf("preview: Timeout must be positive, got %v", opts.Timeout)
	case opts.MaxSource <= 0:
		return nil, fmt.Errorf("preview: MaxSource must be positive, got %d", opts.MaxSource)
	case opts.MaxHTML <= 0:
		return nil, fmt.Errorf("preview: MaxHTML must be positive, got %d", opts.MaxHTML)
	}
	run := opts.Run
	if run == nil {
		run = execRunner
	}
	return &Renderer{
		pandocBin: opts.PandocBin,
		dir:       filepath.Clean(opts.Dir),
		timeout:   opts.Timeout,
		maxSource: opts.MaxSource,
		maxHTML:   opts.MaxHTML,
		run:       run,
	}, nil
}

var (
	// ErrToolMissing: pandoc is not installed (or not where it was expected).
	ErrToolMissing = errors.New("preview: pandoc is not installed")
	// ErrTooLarge: the Markdown or the HTML is over the limit.
	ErrTooLarge = errors.New("preview: document exceeds a limit")
)

// ToolError is a failed run of pandoc.
type ToolError struct {
	Tool   string // "pandoc"
	Output string // what the tool wrote to stderr, trimmed and bounded
	Err    error
}

func (e *ToolError) Error() string {
	if e.Output == "" {
		return fmt.Sprintf("preview: %s failed: %v", e.Tool, e.Err)
	}
	return fmt.Sprintf("preview: %s failed: %v: %s", e.Tool, e.Err, e.Output)
}

func (e *ToolError) Unwrap() error { return e.Err }

// Render turns Markdown into the HTML of the page body. resourceDir is the
// folder images are looked up in ("" when the document has no folder: then no
// image is embedded). It must be absolute.
//
// A picture that is not a readable local image file of that folder is replaced
// by its description, and a link by its text; see filters/preview.lua. A warning
// pandoc prints while it still succeeds is not an error.
func (r *Renderer) Render(ctx context.Context, markdown []byte, resourceDir string) ([]byte, error) {
	if len(markdown) > r.maxSource {
		return nil, fmt.Errorf("%w: text is %d bytes, the limit is %d", ErrTooLarge, len(markdown), r.maxSource)
	}
	images, err := imageDir(resourceDir)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("preview: %w", err)
	}
	filter, err := r.ensureFilter()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	stdout, stderr, err := r.run(ctx, r.dir, r.pandocBin, markdown, r.maxHTML, r.args(filter, images)...)
	if err != nil {
		switch {
		case errors.Is(err, ErrTooLarge):
			return nil, fmt.Errorf("%w: the page is larger than %d bytes", ErrTooLarge, r.maxHTML)
		// A timeout or a cancel is not pandoc's fault, so it is looked at first.
		case ctx.Err() != nil:
			return nil, fmt.Errorf("preview: %s: %w", toolPandoc, ctx.Err())
		case toolMissing(err, r.pandocBin):
			return nil, fmt.Errorf("%w: %w", ErrToolMissing, err)
		}
		return nil, &ToolError{Tool: toolPandoc, Output: boundOutput(stderr), Err: err}
	}
	// A Runner that does not enforce the bound is caught here.
	if len(stdout) > r.maxHTML {
		return nil, fmt.Errorf("%w: the page is larger than %d bytes", ErrTooLarge, r.maxHTML)
	}
	if len(stderr) > 0 {
		// Only sizes are logged: a warning quotes the document (a file name, a
		// formula).
		slog.Debug("preview: pandoc wrote warnings", "bytes", len(stderr), "lines", bytes.Count(stderr, []byte{'\n'}))
	}
	return stdout, nil
}

// args is the whole command line. images is "" when no picture may be embedded.
func (r *Renderer) args(filter, images string) []string {
	args := []string{
		"-f", readerFormat,
		"-t", "html5", // a body only: there is no --standalone
		"--syntax-highlighting=none",
		"--math-method=mathml",
		"--wrap=none",
		"--embed-resources", // a local picture becomes a data: URI; works without --standalone
		"--lua-filter=" + filter,
	}
	if images == "" {
		return append(args, "--metadata=pisar-images=off")
	}
	// The same folder goes to pandoc and to the filter: pandoc embeds what it
	// finds there, and the filter lets through only what it can read there.
	return append(args,
		"--metadata=pisar-images=on",
		"--metadata=pisar-resource-dir="+images,
		"--resource-path="+images)
}

// imageDir validates resourceDir and returns the folder pictures may be read
// from, or "" when there is none.
func imageDir(resourceDir string) (string, error) {
	if resourceDir == "" {
		return "", nil
	}
	if !filepath.IsAbs(resourceDir) {
		return "", fmt.Errorf("preview: resourceDir must be an absolute path, got %q", resourceDir)
	}
	// pandoc splits --resource-path at every ':', so "/a/Notes: 2024" would also
	// search "/a/Notes", a different folder. There is no way to quote it, so such
	// a folder gets a preview without pictures.
	if strings.Contains(resourceDir, ":") {
		slog.Debug("preview: pictures are off for a folder whose name has a colon")
		return "", nil
	}
	return filepath.Clean(resourceDir), nil
}

// ensureFilter makes sure the filter file holds exactly the embedded text and
// returns its path. It writes only when it does not, so a refresh that runs
// every few hundred milliseconds costs a read and no write.
func (r *Renderer) ensureFilter() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	path := filepath.Join(r.dir, filterName)
	if filterCurrent(path) {
		return path, nil
	}
	if err := os.MkdirAll(r.dir, dirPerm); err != nil {
		return "", fmt.Errorf("preview: create folder: %w", err)
	}
	// Temp file and rename: pandoc may be reading the old file, and it must never
	// see half of the new one. Rename also replaces a symlink at path instead of
	// writing through it.
	tmp, err := os.CreateTemp(r.dir, "filter-*.tmp")
	if err != nil {
		return "", fmt.Errorf("preview: write filter: %w", err)
	}
	if err := writeAndClose(tmp, filterLua); err != nil {
		return "", errors.Join(fmt.Errorf("preview: write filter: %w", err), removeIfThere(tmp.Name()))
	}
	if err := os.Chmod(tmp.Name(), filePerm); err != nil {
		return "", errors.Join(fmt.Errorf("preview: write filter: %w", err), removeIfThere(tmp.Name()))
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", errors.Join(fmt.Errorf("preview: write filter: %w", err), removeIfThere(tmp.Name()))
	}
	return path, nil
}

// filterCurrent reports whether path is a regular file with the embedded text.
// Any doubt (missing, a link, unreadable, different) is a "no": the caller then
// writes the file, and if that fails it says why.
func filterCurrent(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(filterLua)) {
		return false
	}
	got, err := os.ReadFile(path)
	return err == nil && bytes.Equal(got, filterLua)
}

func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("preview: remove %s: %w", filepath.Base(path), err)
	}
	return nil
}

// toolMissing tells "there is no such program" from a failure of something else
// that also reports "no such file", such as a working folder that does not exist
// (that one is a PathError for the folder, not for the program).
func toolMissing(err error, bin string) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var pe *fs.PathError
	return errors.As(err, &pe) && pe.Path == bin && errors.Is(err, fs.ErrNotExist)
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

// limitedBuffer keeps what is written to it and, the moment more than max bytes
// would be held, calls stop and fails every write. stop kills the process, so a
// page that is too large is never read to its end.
type limitedBuffer struct {
	buf      []byte
	max      int
	stop     func()
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.exceeded || len(b.buf)+len(p) > b.max {
		if !b.exceeded {
			b.exceeded = true
			b.stop()
		}
		return 0, ErrTooLarge
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// execRunner is the Runner used when Options.Run is nil. Arguments are passed as
// separate elements; nothing goes through a shell.
func execRunner(ctx context.Context, dir, name string, stdin []byte, maxStdout int, args ...string) (stdout, stderr []byte, err error) {
	// A context of its own, so that the output limit can kill the process
	// without being taken for the caller's cancel.
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = killGrace
	cmd.Stdin = bytes.NewReader(stdin)
	out := &limitedBuffer{max: maxStdout, stop: stop}
	errb := &cappedBuffer{max: maxToolOutput}
	cmd.Stdout = out
	cmd.Stderr = errb
	err = cmd.Run()
	if out.exceeded {
		return nil, errb.buf, ErrTooLarge
	}
	return out.buf, errb.buf, err
}
