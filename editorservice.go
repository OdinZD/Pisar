package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"Pisar/internal/config"
	"Pisar/internal/docsafe"
	"Pisar/internal/nvim"
	"Pisar/internal/project"
	"Pisar/internal/term"
)

// Events sent to the frontend by the editor. The document events carry a
// structure (DocumentActive, DocumentState, and DocumentExport in editorwords.go);
// the others are strings.
const (
	eventPtyData        = "pty:data"          // payload: a piece of terminal output
	eventNvimExit       = "nvim:exit"         // Neovim ended on its own; payload ""
	eventDocumentActive = "document:active"   // payload: DocumentActive
	eventDocumentState  = "document:state"    // payload: DocumentState
	eventFocusTree      = "ui:focus-tree"     // the user pressed <leader>e in Neovim; payload ""
	eventOpenShortcuts  = "ui:open-shortcuts" // the user pressed <leader>? in Neovim; payload ""
)

// DocumentActive describes the buffer Neovim has in front of it. ProjectID and
// Rel are empty when the file belongs to no project; everything is empty when
// the buffer has no file. For a Word document Path, Rel and Name are those of
// the user's .docx or .doc, never of the Markdown working copy Neovim edits. It
// reaches JavaScript as a typed event payload: the binding generator emits the
// class from this struct (frontend/bindings/Pisar/models.ts).
type DocumentActive struct {
	ProjectID string `json:"projectId"`
	Rel       string `json:"rel"`  // path inside the project, "/" separators
	Path      string `json:"path"` // absolute, as Neovim reports it
	Name      string `json:"name"` // file name with extension

	// ReadOnly lists the reasons why the document must not be saved (codes
	// such as "comments"); it is an empty array, never null, when it may be.
	ReadOnly []string `json:"readOnly"`
	// Imported: the document came from a .doc, so its first save makes a new .docx.
	Imported bool `json:"imported"`
	// Unexported: the working copy has text that is not in the user's file yet
	// (it was written but not exported, or the export is on its way).
	Unexported bool `json:"unexported"`
	// Simplified lists the kinds of direct formatting (codes such as "alignment")
	// that saving would flatten; an empty array, never null, when there is none or
	// the document has been saved since. The application only warns about them.
	Simplified []string `json:"simplified"`
}

// DocumentState is a change of the "modified" flag of a buffer. Path is the
// path as Neovim reports it ("" for a buffer without a file); for a Word
// document it is the user's file.
type DocumentState struct {
	Path     string `json:"path"`
	Modified bool   `json:"modified"`
}

// The init.lua that ships with the application; Start writes it into the
// Neovim configuration directory the application owns.
//
//go:embed nvim/init.lua
var initLuaSource []byte

// The Croatian dictionary (Vim's own hr.utf-8.spl, LGPL; see nvim/spell/README_hr.txt).
// Neovim ships only English and would ask the user whether to download any other
// language, a question nobody can answer over RPC, so the application brings
// this one and init.lua switches the download off.
//
//go:embed nvim/spell/hr.utf-8.spl
var hrDictionary []byte

const (
	// Files and folders under <home>/nvim belong to the application.
	nvimDirPerm  os.FileMode = 0o700
	nvimFilePerm os.FileMode = 0o600
)

// editorServiceDeps are the parts an EditorService is built from.
type editorServiceDeps struct {
	Store     *config.Store
	Emit      func(name string, data any) // to the frontend; the payload types are the ones registered in main.go
	Clipboard func(text string) bool      // puts text on the system clipboard; reports success
	Limits    config.Limits

	// NvimBin overrides the binary name from the configuration (tests use it
	// to simulate a missing Neovim). RunDir is the folder that holds the
	// socket folder; "" means os.TempDir().
	NvimBin string
	RunDir  string

	// InitialFile is the document named on the command line, "" if none.
	InitialFile string

	// Words opens .docx and .doc documents. Without it they are refused as
	// unsupported, which is how the service behaved before they were supported.
	Words *wordDocs

	// Preview turns the text of the current buffer into the HTML of the preview
	// pane. Without it the service behaves as if there were no preview:
	// SetPreview succeeds and nothing is ever sent. PreviewTimer replaces the
	// real timers of the loop (tests); nil means real ones.
	Preview      previewRenderer
	PreviewTimer previewTimerFunc

	// Text is what Neovim shows to the user, by key (see nvimText).
	Text map[string]string

	// UserLuaTemplate is the text a new user.lua starts with when the user opens
	// it before the file exists (uiText.UserLuaTemplate). It is not among Text:
	// it is not shown as a message, Go hands it to Neovim with the open call.
	UserLuaTemplate string
}

// EditorService is the Wails service behind the editor: it runs Neovim, moves
// terminal bytes and keys, and reports which document is active. Wails binds
// every exported method, so everything that is not meant for the frontend is
// unexported (including the term.Emitter, see editorEvents). Calls may run
// concurrently.
type EditorService struct {
	store     *config.Store
	limits    config.Limits
	paths     config.NvimPaths
	emit      func(name string, data any)
	clipboard func(text string) bool
	session   *term.Session
	runDir    string // holds the RPC socket; removed by shutdown

	userLuaTemplate string // see editorServiceDeps.UserLuaTemplate

	// tools remembers the tools' versions between Tools calls (editortools.go).
	tools toolsProbe

	// words, queue and marks are the Word documents' part (editorwords.go);
	// queue is nil exactly when words is.
	words *wordDocs
	queue *workQueue
	marks exportMarks

	// preview is the loop that renders the preview, nil without a Preview.
	// previewMu serialises SetPreview and Start's use of previewWanted, the
	// last state the frontend asked for; it outlives a Neovim process.
	preview       *previewLoop
	previewMu     sync.Mutex
	previewWanted bool

	// startMu serialises Start: it writes the configuration and may open the
	// initial document, which must happen once.
	startMu sync.Mutex

	initialMu sync.Mutex
	initial   string // absolute path; "" once opened or when there was none

	// closeMu guards closed. Emitting holds the read lock, so once shutdown has
	// taken the write lock no emit is running and none will start.
	closeMu      sync.RWMutex
	closed       bool
	shutdownOnce sync.Once
}

func newEditorService(d editorServiceDeps) (*EditorService, error) {
	if d.Store == nil || d.Emit == nil {
		return nil, errors.New("editor service: store and emit are required")
	}
	paths := d.Store.Nvim()
	if d.NvimBin != "" {
		paths.Bin = d.NvimBin
	}
	parent := d.RunDir
	if parent == "" {
		parent = os.TempDir()
	}
	for _, p := range removeStaleRunDirs(parent, paths.RunDirPrefix, pidAlive) {
		slog.Debug("editor: removed a socket folder left by an earlier run", "dir", p)
	}
	// The PID in the name is what lets a later run tell a left-over folder from
	// one that belongs to a live application.
	runDir, err := os.MkdirTemp(parent, paths.RunDirPrefix+strconv.Itoa(os.Getpid())+"-")
	if err != nil {
		return nil, fmt.Errorf("editor service: create socket folder: %w", err)
	}

	e := &EditorService{
		store:     d.Store,
		limits:    d.Limits,
		paths:     paths,
		emit:      d.Emit,
		clipboard: d.Clipboard,
		runDir:    runDir,
		words:     d.Words,

		userLuaTemplate: d.UserLuaTemplate,
	}
	if d.InitialFile != "" {
		abs, err := filepath.Abs(d.InitialFile)
		if err != nil {
			_ = os.RemoveAll(runDir)
			return nil, fmt.Errorf("editor service: resolve %q: %w", d.InitialFile, err)
		}
		// Decided here, not in Start: a document that is refused must leave
		// HasInitialDocument false, so the frontend does not take the
		// "open the editor straight away" path for it.
		if reason := e.initialRefusal(abs); reason != "" {
			// Only the name, never the content.
			slog.Warn("editor: ignoring the document on the command line", "file", filepath.Base(abs), "reason", reason)
		} else {
			e.initial = abs
		}
	}
	e.session, err = term.NewSession(term.Config{
		Nvim: nvim.Options{
			Bin:       paths.Bin,
			InitLua:   paths.InitLua,
			AppName:   paths.AppName,
			SocketDir: runDir,
			// Neovim's own state goes under the home, not into the user's
			// ~/.config, ~/.local and ~/.cache.
			Env: append(nvimXDGEnv(paths),
				// Where zg stores the user's own words; init.lua must not guess
				// a path outside its own folder.
				config.EnvNvimSpellFile+"="+paths.UserWords,
				// The user's own Neovim file; init.lua loads it when it exists.
				config.EnvNvimUserLua+"="+paths.UserLua,
			),
			StartTimeout: d.Limits.NvimStartTimeout,
			StopTimeout:  d.Limits.NvimStopTimeout,
			Text:         d.Text,
		},
		FlushInterval: d.Limits.TermFlushInterval,
		MaxChunk:      d.Limits.TermMaxChunk,
	}, editorEvents{e})
	if err != nil {
		_ = os.RemoveAll(runDir)
		return nil, fmt.Errorf("editor service: %w", err)
	}
	if e.words != nil {
		// A start of the application: working copies of an earlier run that hold
		// nothing unsaved go, the others wait for the recovery question.
		ctx, cancel := context.WithTimeout(context.Background(), d.Limits.CommandTimeout)
		if err := e.words.sweep(ctx); err != nil {
			slog.Warn("editor: cannot clean up the working copies of an earlier run", "err", err)
		}
		cancel()
		e.queue = newWorkQueue(e.runTask)
	}
	if d.Preview != nil {
		// Last: nothing after this can fail, so the goroutine is never left behind.
		e.preview = newPreviewLoop(previewLoopDeps{
			Source:   e.session,
			Renderer: d.Preview,
			Emit: func(c PreviewContent) {
				// The size only, never the HTML: it is the user's document.
				slog.Debug("editor event", "event", eventPreviewContent, "file", filepath.Base(c.Path), "state", c.State, "code", c.Code, "html_bytes", len(c.HTML))
				e.notify(eventPreviewContent, c)
			},
			Target:      e.previewTargetOf,
			Debounce:    d.Limits.PreviewDebounce,
			ReadTimeout: d.Limits.CommandTimeout,
			MaxSource:   d.Limits.MaxPreviewSource,
			NewTimer:    d.PreviewTimer,
		})
	}
	return e, nil
}

// pidAlive reports whether a process with this PID exists. EPERM means it
// exists but belongs to someone else, which is still alive for our purpose.
func pidAlive(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// removeStaleRunDirs removes the socket folders that earlier runs of the
// application left behind after a crash. It only touches real folders (not
// links, not files) in parent whose name is prefix + PID + "-" + something and
// whose PID belongs to no process. It returns what it removed.
func removeStaleRunDirs(parent, prefix string, alive func(pid int) bool) []string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		slog.Warn("editor: cannot look for left-over socket folders", "err", err)
		return nil
	}
	var removed []string
	for _, ent := range entries {
		pid, ok := runDirPID(ent.Name(), prefix)
		// DirEntry.Type does not follow links, so a link named like ours is skipped.
		if !ok || !ent.Type().IsDir() || alive(pid) {
			continue
		}
		dir := filepath.Join(parent, ent.Name())
		if err := os.RemoveAll(dir); err != nil {
			slog.Warn("editor: cannot remove a left-over socket folder", "dir", dir, "err", err)
			continue
		}
		removed = append(removed, dir)
	}
	return removed
}

// runDirPID extracts the PID from "<prefix><pid>-<random>".
func runDirPID(name, prefix string) (int, bool) {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return 0, false
	}
	digits, tail, ok := strings.Cut(rest, "-")
	if !ok || tail == "" || digits == "" {
		return 0, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	pid, err := strconv.Atoi(digits)
	// 0 and negative numbers would address process groups in kill(2); a number
	// beyond pid_t would wrap.
	if err != nil || pid < 1 || pid > math.MaxInt32 {
		return 0, false
	}
	return pid, true
}

// notify emits unless the service has been shut down.
func (e *EditorService) notify(name string, data any) {
	e.closeMu.RLock()
	defer e.closeMu.RUnlock()
	if e.closed {
		return
	}
	e.emit(name, data)
}

func (e *EditorService) isClosed() bool {
	e.closeMu.RLock()
	defer e.closeMu.RUnlock()
	return e.closed
}

// shutdown closes the editor: no event is emitted after it returns, Neovim is
// stopped (unsaved changes are discarded; the application asks the user
// first), and the socket folder is removed. Idempotent; a second caller waits
// for the first. Unexported so Wails does not expose it. Must not be called
// from an event callback: Session.Close waits for the callback in progress.
func (e *EditorService) shutdown() {
	e.shutdownOnce.Do(func() {
		e.closeMu.Lock()
		e.closed = true
		e.closeMu.Unlock()

		if e.preview != nil {
			// Cancels a conversion in progress and waits for the goroutine.
			e.preview.stop()
		}
		if e.words != nil {
			e.stopWords()
		}

		// Close escalates through three stages (RPC quit, SIGTERM, SIGKILL), each
		// bounded by NvimStopTimeout; one more is slack for waiting on goroutines.
		ctx, cancel := context.WithTimeout(context.Background(), 4*e.limits.NvimStopTimeout)
		defer cancel()
		if err := e.session.Close(ctx); err != nil {
			slog.Error("editor: stop Neovim", "err", err)
		}
		if err := os.RemoveAll(e.runDir); err != nil {
			slog.Warn("editor: remove socket folder", "err", err)
		}
	})
}

// bounded applies the limit for blocking calls.
func (e *EditorService) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, e.limits.CommandTimeout)
}

// failEditor logs the full error and returns the one the frontend may see.
func failEditor(op string, err error) error {
	code := codeOf(err)
	if code == codeInternal {
		slog.Error("editor service", "op", op, "err", err)
	} else {
		slog.Debug("editor service", "op", op, "code", code, "err", err)
	}
	return &apiError{code: code, cause: err}
}

// writeFileAtomic replaces path with data through a temp file in the same
// folder, so Neovim never reads half a file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// hasContent reports whether path is an ordinary file with exactly these bytes.
// Any failure to find out counts as "no": the caller then writes the file, and
// that write reports a real problem.
func hasContent(path string, want []byte) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != int64(len(want)) {
		return false
	}
	got, err := os.ReadFile(path)
	return err == nil && bytes.Equal(got, want)
}

// writeConfig makes the Neovim folders and writes init.lua, the colorscheme and
// the Croatian dictionary. It never writes the user's own words: it only makes
// their folder, and Neovim creates the file at the first zg.
func (e *EditorService) writeConfig(colorscheme string) error {
	for _, dir := range []string{
		filepath.Dir(e.paths.InitLua), filepath.Dir(e.paths.ColorschemeFile), e.paths.SpellDir,
		e.paths.DataHome, e.paths.StateHome, e.paths.CacheHome,
		filepath.Dir(e.paths.UserWords),
	} {
		if err := os.MkdirAll(dir, nvimDirPerm); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := writeFileAtomic(e.paths.InitLua, initLuaSource, nvimFilePerm); err != nil {
		return fmt.Errorf("write init.lua: %w", err)
	}
	if err := writeFileAtomic(e.paths.ColorschemeFile, []byte(colorscheme), nvimFilePerm); err != nil {
		return fmt.Errorf("write colorscheme: %w", err)
	}
	// 1.2 MiB is not written at every start. A file that differs (a cut or
	// damaged one from a crash, a different version) is replaced.
	if !hasContent(e.paths.HrDictionary, hrDictionary) {
		if err := writeFileAtomic(e.paths.HrDictionary, hrDictionary, nvimFilePerm); err != nil {
			return fmt.Errorf("write Croatian dictionary: %w", err)
		}
	}
	return nil
}

// Start launches Neovim at the given size with the colorscheme the frontend
// generated, or, when Neovim already runs (the page was reloaded), only resizes
// it and repaints; the colorscheme of the running Neovim is then left as it is
// (a theme switch while Neovim runs goes through SetColorscheme).
func (e *EditorService) Start(ctx context.Context, cols, rows int, colorscheme string) error {
	slog.Debug("service call", "method", "Start", "cols", cols, "rows", rows, "colorscheme_bytes", len(colorscheme))
	// Checked first: Neovim executes this text, so nothing is written and nothing
	// is started for text that is not what the generator produces.
	if err := checkColorscheme(colorscheme, e.limits.MaxColorscheme); err != nil {
		return failEditor("start", err)
	}
	if e.isClosed() {
		return failEditor("start", term.ErrClosed)
	}

	e.startMu.Lock()
	defer e.startMu.Unlock()
	// Starting Neovim may take up to NvimStartTimeout; the rest of the time is for
	// opening the initial document (one RPC call, CommandTimeout).
	ctx, cancel := context.WithTimeout(ctx, e.limits.NvimStartTimeout+e.limits.CommandTimeout)
	defer cancel()

	wasRunning := e.session.Running()
	if !wasRunning {
		// Before anything is written or started: a Neovim that is too old is
		// refused like a missing one. A Neovim that is not found is left to
		// session.Start, which reports nvim-missing.
		pctx, pcancel := e.bounded(ctx)
		refused := e.toolState(pctx, toolNvim).refuseIfTooOld()
		pcancel()
		if refused != nil {
			return failEditor("start", refused)
		}
		if err := e.writeConfig(colorscheme); err != nil {
			return failEditor("start", err)
		}
		// macOS clears unused entries out of the temporary folder after a few
		// days; the application may run longer than that between two starts.
		if err := os.MkdirAll(e.runDir, nvimDirPerm); err != nil {
			return failEditor("start", fmt.Errorf("create socket folder: %w", err))
		}
	}
	if err := e.session.Start(ctx, cols, rows); err != nil {
		return failEditor("start", err)
	}
	if !wasRunning {
		// A new Neovim does not know that the preview is on.
		e.applyPreview(ctx)
		// The user's file is their own code, not a document: its path and the
		// first line of the error are fine to log, the text of the file is not
		// read by anyone but Neovim.
		if u := e.session.UserConfig(); u != (nvim.UserConfig{}) {
			slog.Warn("editor: user.lua", "file", e.paths.UserLua, "error", u.FirstLine(),
				"spell", u.Spell, "events", u.Events, "restore", u.Restore)
		}
	}
	if wasRunning {
		// A reloaded page has lost what it knew: tell it again which document
		// is active and whether it has unsaved changes.
		if buf, err := e.session.Current(ctx); err != nil {
			slog.Warn("editor: cannot report the active document after a restart of the page", "err", err)
		} else {
			editorEvents{e}.Active(buf.Path, buf.Modified)
		}
	}

	// Taken before the call, so a failed open is not retried by the next Start.
	e.initialMu.Lock()
	file := e.initial
	e.initial = ""
	e.initialMu.Unlock()
	if file != "" {
		if e.words != nil && isWordName(file) {
			// Not here: the conversion and the recovery question can take long,
			// and Start must not wait for them (see runOpen).
			t := newWorkTask(workOpen)
			t.path = file
			e.queue.add(t)
			return nil
		}
		if err := e.session.OpenFile(ctx, file); err != nil {
			return failEditor("open initial document", err)
		}
	}
	return nil
}

// SetColorscheme replaces the colorscheme while the application runs (a theme switch).
// The text is checked like Start's, then colors/pisar.lua is rewritten atomically
// and, when Neovim runs, it is told to load the file again. Without a running
// Neovim only the file changes: the next Start writes the colorscheme it is given.
// A Neovim that waits at a prompt does not answer, but keeps the call queued and
// applies it when the prompt is closed (see nvim.Process.Colorscheme), so that
// is not reported as a failure.
func (e *EditorService) SetColorscheme(ctx context.Context, colorscheme string) error {
	slog.Debug("service call", "method", "SetColorscheme", "colorscheme_bytes", len(colorscheme))
	// Checked first, for the same reason as in Start: Neovim executes this text.
	if err := checkColorscheme(colorscheme, e.limits.MaxColorscheme); err != nil {
		return failEditor("set colorscheme", err)
	}
	if e.isClosed() {
		return failEditor("set colorscheme", term.ErrClosed)
	}
	if err := e.replaceColorscheme(colorscheme); err != nil {
		return failEditor("set colorscheme", err)
	}
	if !e.session.Running() {
		return nil // the file is in place; Start writes its own text anyway
	}

	ctx, cancel := e.bounded(ctx)
	defer cancel()
	err := e.session.Colorscheme(ctx)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		// Neovim does not answer while it waits for a key, but the call stays in
		// its queue and runs when the prompt closes; the file is already written.
		slog.Info("editor: Neovim is at a prompt; the new colorscheme is applied when it closes")
		return nil
	case errors.Is(err, nvim.ErrNotRunning):
		// Neovim ended in the meantime; the next Start uses the file or rewrites it.
		slog.Info("editor: Neovim ended before it loaded the new colorscheme")
		return nil
	}
	return failEditor("set colorscheme", err)
}

// replaceColorscheme writes colors/pisar.lua under startMu, so that the write
// does not interleave with Start's. init.lua and the dictionary are not touched.
// The lock is not held while Neovim reloads the file: that may wait for a prompt.
func (e *EditorService) replaceColorscheme(colorscheme string) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	dir := filepath.Dir(e.paths.ColorschemeFile)
	if err := os.MkdirAll(dir, nvimDirPerm); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := writeFileAtomic(e.paths.ColorschemeFile, []byte(colorscheme), nvimFilePerm); err != nil {
		return fmt.Errorf("write colorscheme: %w", err)
	}
	return nil
}

// neovimMayEdit reports whether Neovim may be handed the file called name. Only
// .md and .txt qualify: Neovim writes them itself on :w, which CLAUDE.md allows
// for those two kinds only. The bytes of a .docx or .doc must never reach it,
// because ⌘S would then write them outside internal/docsafe. This is the one
// copy of that rule; OpenDocument and the command-line document both use it.
func neovimMayEdit(name string) bool {
	kind, _ := project.KindOf(filepath.Base(name))
	return kind == project.KindMD || kind == project.KindTXT
}

// initialRefusal says why the document named on the command line is not
// opened, or "" when it is accepted. A .docx or .doc is accepted when Word
// documents are supported and the file exists as a regular file (never a link). A file that does not exist yet is fine
// (Neovim opens a new buffer with that name). Anything that exists must be a
// regular file: Lstat, not Stat, because a link named x.md may point at a
// .docx, and Neovim's :w writes through a link into its target.
func (e *EditorService) initialRefusal(abs string) string {
	if e.words != nil && isWordName(abs) {
		info, err := os.Lstat(abs)
		switch {
		case err != nil:
			return "cannot be examined"
		case !info.Mode().IsRegular():
			return "not a regular file (a folder, a link or a special file)"
		}
		return ""
	}
	return mdInitialRefusal(abs)
}

// mdInitialRefusal is initialRefusal for what Neovim opens itself.
func mdInitialRefusal(abs string) string {
	if !neovimMayEdit(abs) {
		return "not a .md or .txt file"
	}
	info, err := os.Lstat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ""
	case err != nil:
		return "cannot be examined"
	case !info.Mode().IsRegular():
		return "not a regular file (a folder, a link or a special file)"
	}
	return ""
}

// HasInitialDocument reports whether a document was named on the command line
// and has not been opened yet (it is opened by the first successful Start).
func (e *EditorService) HasInitialDocument() bool {
	e.initialMu.Lock()
	defer e.initialMu.Unlock()
	return e.initial != ""
}

// Write sends keystrokes, already encoded by the terminal, to Neovim.
func (e *EditorService) Write(data string) error {
	slog.Debug("service call", "method", "Write", "bytes", len(data)) // never the keys themselves
	if err := e.session.Write(data); err != nil {
		return failEditor("write", err)
	}
	return nil
}

// Resize tells Neovim the terminal size changed.
func (e *EditorService) Resize(cols, rows int) error {
	slog.Debug("service call", "method", "Resize", "cols", cols, "rows", rows)
	if err := e.session.Resize(cols, rows); err != nil {
		return failEditor("resize", err)
	}
	return nil
}

// projectRoot returns the folder of a known project whose folder exists.
func (e *EditorService) projectRoot(id string) (string, error) {
	p, ok := e.store.Project(id)
	if !ok {
		return "", config.ErrNotFound
	}
	if !isDir(p.Path) {
		return "", errMissing
	}
	return p.Path, nil
}

// OpenDocument edits the document rel of a project in Neovim's current window.
func (e *EditorService) OpenDocument(ctx context.Context, projectID, rel string) error {
	slog.Debug("service call", "method", "OpenDocument", "project", projectID, "rel", rel)
	root, err := e.projectRoot(projectID)
	if err != nil {
		return failEditor("open document", err)
	}
	if err := requireDocument(root, rel); err != nil {
		return failEditor("open document", err)
	}
	// requireDocument has accepted the extension, so what neovimMayEdit refuses
	// here is .docx and .doc: opened through a working copy, or not at all
	// when Word documents are not supported.
	word := isWordName(rel)
	if !neovimMayEdit(rel) && (!word || e.words == nil) {
		return failEditor("open document", fmt.Errorf("%w: %q", docsafe.ErrUnsupportedKind, rel))
	}
	abs, err := project.Resolve(root, rel)
	if err != nil {
		return failEditor("open document", err)
	}
	if word {
		if err := e.openWord(ctx, abs); err != nil {
			return failEditor("open document", err)
		}
		return nil
	}
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	if err := e.session.OpenFile(ctx, abs); err != nil {
		return failEditor("open document", err)
	}
	return nil
}

// Save writes the current buffer to its file.
func (e *EditorService) Save(ctx context.Context) error {
	slog.Debug("service call", "method", "Save")
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	// A Word document: the write itself (of doc.md) is followed by an export
	// (see editorEvents.Saved), which a read-only document must not get.
	if err := e.refuseReadOnlySave(ctx); err != nil {
		return failEditor("save", err)
	}
	if err := e.session.Save(ctx); err != nil {
		return failEditor("save", err)
	}
	return nil
}

// OpenUserLua opens the user's own user.lua in Neovim. The application never
// creates the file: when it is not there, Neovim gets a new buffer with a
// template in it, and the file comes into being when the user saves it. A path
// that is not a regular file is refused (nvim.ErrNotRegularFile, with the
// internal code), and Neovim has already told the user why in its message line.
func (e *EditorService) OpenUserLua(ctx context.Context) error {
	slog.Debug("service call", "method", "OpenUserLua")
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	if err := e.session.OpenUserLua(ctx, e.paths.UserLua, e.userLuaTemplate); err != nil {
		return failEditor("open user.lua", err)
	}
	return nil
}

// Copy yanks Neovim's visual selection into the system clipboard. It reports
// false when Neovim has no visual selection.
func (e *EditorService) Copy(ctx context.Context) (bool, error) {
	slog.Debug("service call", "method", "Copy")
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	copied, err := e.session.CopyVisual(ctx)
	if err != nil {
		return false, failEditor("copy", err)
	}
	return copied, nil
}

// Format runs a Markdown formatting action on the current buffer: kind is
// "bold", "italic", "heading" or "list". An action that does not apply (Neovim
// waits for more input, the buffer is read-only) is not an error: it does
// nothing and returns nil, because the user's keys simply mean nothing there.
func (e *EditorService) Format(ctx context.Context, kind string) error {
	slog.Debug("service call", "method", "Format", "kind", kind)
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	if err := e.session.Format(ctx, kind); err != nil {
		return failEditor("format", err)
	}
	return nil
}

// Keymap is one shortcut of the list the frontend shows: a mapping of Neovim's
// that has a description.
type Keymap struct {
	Mode string `json:"mode"` // "n", "x" or "i"
	Keys string `json:"keys"` // as Neovim writes them: "<Space>f", "<Down>"
	Desc string `json:"desc"`
}

// Keymaps is the list of shortcuts, and the names of the groups the user gave
// in user.lua. It reaches JavaScript as a type generated from this struct.
type Keymaps struct {
	Maps   []Keymap          `json:"maps"`   // never null
	Groups map[string]string `json:"groups"` // keys after the leader -> name; never null
}

// Keymaps asks the running Neovim for the shortcuts to show, bounded by the
// limits of config (how many, how long a text). Without Neovim it fails with
// the not-running code; while Neovim waits in a prompt it refuses at once (see
// nvim.Process.Keymaps), and the frontend shows a note instead of the list.
func (e *EditorService) Keymaps(ctx context.Context) (Keymaps, error) {
	slog.Debug("service call", "method", "Keymaps")
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	got, err := e.session.Keymaps(ctx, e.limits.MaxKeymaps, e.limits.MaxKeymapText)
	if err != nil {
		return Keymaps{}, failEditor("keymaps", err)
	}
	out := Keymaps{Maps: make([]Keymap, 0, len(got.Maps)), Groups: map[string]string{}}
	for _, m := range got.Maps {
		out.Maps = append(out.Maps, Keymap{Mode: m.Mode, Keys: m.Keys, Desc: m.Desc})
	}
	for key, name := range got.Groups {
		out.Groups[key] = name
	}
	return out, nil
}

// CopyText puts text on the system clipboard (the terminal's own selection).
// Empty text leaves the clipboard alone.
func (e *EditorService) CopyText(text string) error {
	slog.Debug("service call", "method", "CopyText", "bytes", len(text))
	if text == "" {
		return nil
	}
	if e.clipboard == nil {
		return failEditor("copy text", errors.New("no clipboard available"))
	}
	if !e.clipboard(text) {
		return failEditor("copy text", errors.New("the system clipboard refused the text"))
	}
	return nil
}

// running reports whether the editor has work that must not be cut off: Neovim
// is running, or a Word document still has an export waiting or running after
// Neovim ended (:wq leaves the text in the working copy until the export is
// done). Unexported: the menu and the quit logic ask it, the frontend has the
// phase it learns from events.
func (e *EditorService) running() bool {
	return e.session.Running() || (e.queue != nil && !e.queue.idle())
}

// modified returns the buffers with unsaved changes, for the dialog that
// appears when the application quits. Unexported: not part of the frontend API.
func (e *EditorService) modified(ctx context.Context) ([]nvim.Buffer, error) {
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	bufs, err := e.session.Modified(ctx)
	if err != nil {
		return nil, failEditor("modified", err)
	}
	if e.words != nil {
		// A Word document is listed under the name of the user's file, and also
		// when only its export is missing.
		bufs = e.withWords(bufs)
	}
	return bufs, nil
}

// saveAll writes every modified buffer and, for the Word documents, exports
// them into the user's files. Unexported, like modified. ctx limits the calls to
// Neovim only; waiting for the exports does not depend on it (see saveAllWords).
func (e *EditorService) saveAll(ctx context.Context) error {
	if e.words != nil {
		if err := e.saveAllWords(ctx); err != nil {
			return failEditor("save all", err)
		}
		return nil
	}
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	if err := e.session.SaveAll(ctx); err != nil {
		return failEditor("save all", err)
	}
	return nil
}

// unsaved reports whether the file at path is open in Neovim with changes that
// are not on disk. Neovim holds the folder part of a buffer's path resolved
// (see resolveDir), so both sides are compared that way. Without a running
// Neovim nothing is open: false. Unexported, together with renamed and
// removed: they are what ProjectService asks of the editor (openDocuments).
func (e *EditorService) unsaved(ctx context.Context, path string) (bool, error) {
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	// A Word document counts as unsaved until the user's file has the text too,
	// not only until doc.md is written.
	if unsaved, isWord, err := e.wordUnsaved(ctx, path); isWord {
		if err != nil {
			return false, failEditor("unsaved", err)
		}
		return unsaved, nil
	}
	bufs, err := e.session.Buffers(ctx)
	if err != nil {
		return false, failEditor("unsaved", err)
	}
	want := resolveDir(path)
	for _, b := range bufs {
		if b.Modified && resolveDir(b.Path) == want {
			return true, nil
		}
	}
	return false, nil
}

// renamed closes the buffer of the file that was renamed from oldPath to
// newPath, so that a later write cannot bring the old file back. When that
// buffer was the one on screen, the document under its new name takes its place.
func (e *EditorService) renamed(ctx context.Context, oldPath, newPath string) error {
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	// A Word document keeps its buffer (doc.md); only its file's name changes.
	if handled, err := e.wordRenamed(ctx, oldPath, newPath); handled {
		if err != nil {
			return failEditor("renamed", err)
		}
		return nil
	}
	wasCurrent, err := e.session.Wipe(ctx, oldPath)
	if err != nil {
		return failEditor("renamed", err)
	}
	if !wasCurrent {
		return nil
	}
	if err := e.session.OpenFile(ctx, newPath); err != nil {
		return failEditor("renamed", err)
	}
	return nil
}

// removed closes the buffer of a file that was moved to the Trash. If it was
// on screen, Neovim shows another buffer or an empty one, and the frontend
// hears of it through the usual document:active event.
func (e *EditorService) removed(ctx context.Context, path string) error {
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	if handled, err := e.wordRemoved(ctx, path); handled {
		if err != nil {
			return failEditor("removed", err)
		}
		return nil
	}
	if _, err := e.session.Wipe(ctx, path); err != nil {
		return failEditor("removed", err)
	}
	return nil
}

// resolveDir resolves the symbolic links of the folder part of file, and only
// of that. Neovim reports a buffer's path the same way (a symlinked folder
// becomes its real path, for example /var -> /private/var; the file name stays
// as given), so a link to a file inside a project still counts as inside it.
// Folders that no longer exist (the file was deleted under Neovim's feet) are
// left as they are, below the nearest folder that does exist.
func resolveDir(file string) string {
	file = filepath.Clean(file)
	dir, tail := filepath.Dir(file), filepath.Base(file)
	for {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(resolved, tail)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return file // nothing exists; nothing better to compare
		}
		dir, tail = parent, filepath.Join(filepath.Base(dir), tail)
	}
}

// locate finds the project that contains file and the file's path inside it.
// The comparison is made on resolved paths. When projects are nested the
// deepest root wins. It reads the current list of projects every time.
func (e *EditorService) locate(file string) (projectID, rel string, ok bool) {
	resolved := resolveDir(file)
	bestLen := -1
	for _, p := range e.store.Projects() {
		root := p.Path
		if r, err := filepath.EvalSymlinks(root); err == nil {
			root = r
		}
		r, err := filepath.Rel(root, resolved)
		if err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			continue
		}
		// Both roots contain the file, so one is a prefix of the other and the
		// longer one is the deeper one.
		if len(root) > bestLen {
			bestLen = len(root)
			projectID, rel, ok = p.ID, filepath.ToSlash(r), true
		}
	}
	return projectID, rel, ok
}

// describe builds the document:active payload for the path Neovim reported.
// The Markdown working copy of a Word document is reported as the user's file.
func (e *EditorService) describe(file string) DocumentActive {
	if d, ok := e.wordDoc(file); ok {
		return e.describeWord(d)
	}
	return e.describePath(file)
}

// describePath is describe for a file as it is.
func (e *EditorService) describePath(file string) DocumentActive {
	if file == "" {
		return DocumentActive{ReadOnly: []string{}, Simplified: []string{}}
	}
	d := DocumentActive{Path: file, Name: filepath.Base(file), ReadOnly: []string{}, Simplified: []string{}}
	if id, rel, ok := e.locate(file); ok {
		d.ProjectID, d.Rel = id, rel
	}
	return d
}

// editorEvents is the term.Emitter. It is a type of its own, not methods of
// EditorService, because Wails would bind every exported method of a service.
// Its methods run on the Session's goroutine and must not call Session.Close.
type editorEvents struct{ e *EditorService }

func (v editorEvents) Data(chunk string) {
	slog.Debug("editor event", "event", eventPtyData, "bytes", len(chunk)) // never the output itself
	v.e.notify(eventPtyData, chunk)
}

func (v editorEvents) Saved(file string) {
	// The state change that follows a write (document:state) is what the
	// frontend needs; for a Word document the write is also the cue to export.
	slog.Debug("editor event", "event", "saved", "path", file)
	v.e.queueExport(file)
}

func (v editorEvents) Active(file string, modified bool) {
	d := v.e.describe(file)
	slog.Debug("editor event", "event", eventDocumentActive, "project", d.ProjectID, "rel", d.Rel, "modified", modified)
	v.e.notify(eventDocumentActive, d)
	// The buffer that becomes active may already carry unsaved changes; without
	// this the toolbar would keep the state of the buffer that was left.
	if file != "" {
		v.e.notify(eventDocumentState, DocumentState{Path: d.Path, Modified: modified})
	}
}

func (v editorEvents) Modified(file string, modified bool) {
	slog.Debug("editor event", "event", eventDocumentState, "path", file, "modified", modified)
	path := file
	if d, ok := v.e.wordDoc(file); ok {
		path = d.view().Path
	}
	v.e.notify(eventDocumentState, DocumentState{Path: path, Modified: modified})
}

func (v editorEvents) FocusTree() {
	slog.Debug("editor event", "event", eventFocusTree)
	v.e.notify(eventFocusTree, "")
}

// Changed only tells the preview loop; the loop does the work on its own goroutine.
func (v editorEvents) Changed() {
	slog.Debug("editor event", "event", "changed")
	if v.e.preview != nil {
		v.e.preview.changed()
	}
}

func (v editorEvents) TogglePreview() {
	slog.Debug("editor event", "event", eventTogglePreview)
	v.e.notify(eventTogglePreview, "")
}

func (v editorEvents) OpenSearch() {
	slog.Debug("editor event", "event", eventOpenSearch)
	v.e.notify(eventOpenSearch, "")
}

func (v editorEvents) OpenShortcuts() {
	slog.Debug("editor event", "event", eventOpenShortcuts)
	v.e.notify(eventOpenShortcuts, "")
}

func (v editorEvents) Exited() {
	slog.Debug("editor event", "event", eventNvimExit)
	if v.e.preview != nil {
		v.e.preview.reset()
	}
	v.e.notify(eventNvimExit, "")
	// Behind the exports that the writes before the quit queued.
	v.e.closeWords()
}
