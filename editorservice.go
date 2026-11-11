package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path"
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

// Events sent to the frontend by the editor. Only the two document events carry
// a structure; the others are strings (see DocumentActive).
const (
	eventPtyData        = "pty:data"        // payload: a piece of terminal output
	eventNvimExit       = "nvim:exit"       // Neovim ended on its own; payload ""
	eventDocumentActive = "document:active" // payload: DocumentActive
	eventDocumentState  = "document:state"  // payload: DocumentState
	eventFocusTree      = "ui:focus-tree"   // the user pressed <leader>e in Neovim; payload ""
)

// DocumentActive describes the buffer Neovim has in front of it. ProjectID and
// Rel are empty when the file belongs to no project; everything is empty when
// the buffer has no file. It reaches JavaScript as a typed event payload: the
// binding generator emits the class from this struct (frontend/bindings/Pisar/models.ts).
type DocumentActive struct {
	ProjectID string `json:"projectId"`
	Rel       string `json:"rel"`  // path inside the project, "/" separators
	Path      string `json:"path"` // absolute, as Neovim reports it
	Name      string `json:"name"` // file name with extension
}

// DocumentState is a change of the "modified" flag of a buffer. Path is the
// path as Neovim reports it, "" for a buffer without a file.
type DocumentState struct {
	Path     string `json:"path"`
	Modified bool   `json:"modified"`
}

// The init.lua that ships with the application; Start writes it into the
// Neovim configuration directory the application owns.
//
//go:embed nvim/init.lua
var initLuaSource []byte

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
	}
	if d.InitialFile != "" {
		abs, err := filepath.Abs(d.InitialFile)
		if err != nil {
			_ = os.RemoveAll(runDir)
			return nil, fmt.Errorf("editor service: resolve %q: %w", d.InitialFile, err)
		}
		e.initial = abs
	}
	e.session, err = term.NewSession(term.Config{
		Nvim: nvim.Options{
			Bin:       paths.Bin,
			InitLua:   paths.InitLua,
			AppName:   paths.AppName,
			SocketDir: runDir,
			// Neovim's own state goes under the home, not into the user's
			// ~/.config, ~/.local and ~/.cache.
			Env: []string{
				"XDG_CONFIG_HOME=" + paths.ConfigHome,
				"XDG_DATA_HOME=" + paths.DataHome,
				"XDG_STATE_HOME=" + paths.StateHome,
				"XDG_CACHE_HOME=" + paths.CacheHome,
			},
			StartTimeout: d.Limits.NvimStartTimeout,
			StopTimeout:  d.Limits.NvimStopTimeout,
		},
		FlushInterval: d.Limits.TermFlushInterval,
		MaxChunk:      d.Limits.TermMaxChunk,
	}, editorEvents{e})
	if err != nil {
		_ = os.RemoveAll(runDir)
		return nil, fmt.Errorf("editor service: %w", err)
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

// writeConfig makes the Neovim folders and writes init.lua and the colorscheme.
func (e *EditorService) writeConfig(colorscheme string) error {
	for _, dir := range []string{
		filepath.Dir(e.paths.InitLua), filepath.Dir(e.paths.ColorschemeFile),
		e.paths.DataHome, e.paths.StateHome, e.paths.CacheHome,
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
	return nil
}

// Start launches Neovim at the given size with the colorscheme the frontend
// generated, or, when Neovim already runs (the page was reloaded), only resizes
// it and repaints; the colorscheme is then left as it is.
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
		if err := e.session.OpenFile(ctx, file); err != nil {
			return failEditor("open initial document", err)
		}
	}
	return nil
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
	// Converting .docx and .doc comes in a later phase; Neovim must never be handed their bytes.
	if kind, _ := project.KindOf(path.Base(rel)); kind == project.KindDOCX || kind == project.KindDOC {
		return failEditor("open document", fmt.Errorf("%w: %q", docsafe.ErrUnsupportedKind, rel))
	}
	abs, err := project.Resolve(root, rel)
	if err != nil {
		return failEditor("open document", err)
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
	if err := e.session.Save(ctx); err != nil {
		return failEditor("save", err)
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

// modified returns the buffers with unsaved changes, for the dialog that
// appears when the application quits. Unexported: not part of the frontend API.
func (e *EditorService) modified(ctx context.Context) ([]nvim.Buffer, error) {
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	bufs, err := e.session.Modified(ctx)
	if err != nil {
		return nil, failEditor("modified", err)
	}
	return bufs, nil
}

// saveAll writes every modified buffer. Unexported, like modified.
func (e *EditorService) saveAll(ctx context.Context) error {
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	if err := e.session.SaveAll(ctx); err != nil {
		return failEditor("save all", err)
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
func (e *EditorService) describe(file string) DocumentActive {
	if file == "" {
		return DocumentActive{}
	}
	d := DocumentActive{Path: file, Name: filepath.Base(file)}
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
	// The state change that follows a write (document:state) is what the frontend needs.
	slog.Debug("editor event", "event", "saved", "path", file)
}

func (v editorEvents) Active(file string, modified bool) {
	d := v.e.describe(file)
	slog.Debug("editor event", "event", eventDocumentActive, "project", d.ProjectID, "rel", d.Rel, "modified", modified)
	v.e.notify(eventDocumentActive, d)
	// The buffer that becomes active may already carry unsaved changes; without
	// this the toolbar would keep the state of the buffer that was left.
	if file != "" {
		v.e.notify(eventDocumentState, DocumentState{Path: file, Modified: modified})
	}
}

func (v editorEvents) Modified(file string, modified bool) {
	slog.Debug("editor event", "event", eventDocumentState, "path", file, "modified", modified)
	v.e.notify(eventDocumentState, DocumentState{Path: file, Modified: modified})
}

func (v editorEvents) FocusTree() {
	slog.Debug("editor event", "event", eventFocusTree)
	v.e.notify(eventFocusTree, "")
}

func (v editorEvents) Exited() {
	slog.Debug("editor event", "event", eventNvimExit)
	v.e.notify(eventNvimExit, "")
}
