package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"

	"Pisar/internal/config"
	"Pisar/internal/convert"
	"Pisar/internal/docsafe"
	"Pisar/internal/nvim"
	"Pisar/internal/preview"
	"Pisar/internal/project"
	"Pisar/internal/term"
)

// Events sent to the frontend (payload is always a string).
const (
	eventProjectsChanged = "projects:changed"  // the list of projects changed; payload ""
	eventProjectChanged  = "project:changed"   // the tree of one project may have changed; payload is the project ID
	eventMenuNewDocument = "menu:new-document" // payload ""
)

// dialogs is what the service needs from the UI toolkit.
type dialogs interface {
	ChooseFolder() (path string, err error) // "" when the user cancelled
	ConfirmTrash(name string) (bool, error)
}

// openDocuments is what the project service needs to know about files the
// editor has open. The editor ignores paths it does not hold, and when no
// editor runs, unsaved reports false and the others do nothing.
type openDocuments interface {
	unsaved(ctx context.Context, path string) (bool, error) // open in the editor with unsaved changes
	renamed(ctx context.Context, oldPath, newPath string) error
	removed(ctx context.Context, path string) error
}

// noEditor is the openDocuments of a service built without an editor.
type noEditor struct{}

func (noEditor) unsaved(context.Context, string) (bool, error) { return false, nil }
func (noEditor) renamed(context.Context, string, string) error { return nil }
func (noEditor) removed(context.Context, string) error         { return nil }

// ProjectInfo is one row of the project list.
type ProjectInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Missing bool   `json:"missing"` // the folder does not exist (or is not a folder) right now
}

// projectServiceDeps are the parts a ProjectService is built from.
type projectServiceDeps struct {
	Store   *config.Store
	Docs    *docsafe.Ops
	Dialogs dialogs
	Editor  openDocuments           // nil: no editor, nothing is ever open
	Source  project.Source          // file system notifications
	Emit    func(name, data string) // to the frontend
	Limits  config.Limits
}

// ProjectService is the Wails service behind the project list and the document
// tree. Wails binds every exported method, so everything that is not meant for
// the frontend is unexported. Calls may run concurrently.
type ProjectService struct {
	store   *config.Store
	docs    *docsafe.Ops
	dialogs dialogs
	editor  openDocuments
	emit    func(name, data string)
	limits  config.Limits
	watcher *project.Watcher

	// scan reads a project tree; project.Scan, and a counting fake in tests.
	scan func(ctx context.Context, root string, maxNodes int) (project.Tree, error)
	// trees keeps the trees the search has scanned (see treeCache).
	trees treeCache

	// watchMu serialises changes to the set of watched projects, so two
	// concurrent calls do not both try to Add the same project.
	watchMu sync.Mutex

	// closeMu guards closed. Emitting holds the read lock, so once shutdown has
	// taken the write lock no emit is running and none will start.
	closeMu sync.RWMutex
	closed  bool
}

func newProjectService(d projectServiceDeps) (*ProjectService, error) {
	s := &ProjectService{
		store:   d.Store,
		docs:    d.Docs,
		dialogs: d.Dialogs,
		editor:  d.Editor,
		emit:    d.Emit,
		limits:  d.Limits,
		scan:    project.Scan,
	}
	if s.editor == nil {
		s.editor = noEditor{}
	}
	w, err := project.NewWatcher(project.WatchOptions{
		Source:   d.Source,
		Debounce: d.Limits.WatchDebounce,
		MaxDelay: d.Limits.WatchMaxDelay,
		// Must not call the Watcher (see project.Watcher); notify does not.
		OnChange: func(id string) { s.notify(eventProjectChanged, id) },
	})
	if err != nil {
		return nil, fmt.Errorf("project service: %w", err)
	}
	s.watcher = w
	return s, nil
}

// notify emits unless the service has been shut down. A change of a project's
// tree also drops the tree the search has cached for it, whoever reports the
// change (the file watcher, or a method of this service that does not wait for it).
func (s *ProjectService) notify(name, data string) {
	if name == eventProjectChanged {
		s.trees.invalidate(data)
	}
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()
	if s.closed {
		return
	}
	s.emit(name, data)
}

// shutdown stops watching and silences events. Idempotent. Unexported so
// Wails does not expose it to the frontend.
func (s *ProjectService) shutdown() {
	// closed first: Watcher.Close waits for a running OnChange, which may be in
	// notify; after this lock is released notify returns without emitting.
	s.closeMu.Lock()
	s.closed = true
	s.closeMu.Unlock()
	s.watcher.Close()
}

// bounded applies the limit for blocking calls. Dialogs are not bounded: the
// user may take as long as they like.
func (s *ProjectService) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.limits.CommandTimeout)
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// syncWatchers brings the watched set in line with the disk: a folder that
// came back is watched again (also after its stream died), one that went away
// is dropped. It returns the projects with their Missing flag.
func (s *ProjectService) syncWatchers() []ProjectInfo {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	projects := s.store.Projects()
	out := make([]ProjectInfo, 0, len(projects))
	for _, p := range projects {
		missing := !isDir(p.Path)
		watching := s.watcher.Watching(p.ID)
		switch {
		case missing && watching:
			s.watcher.Remove(p.ID)
			s.trees.invalidate(p.ID)
		case !missing && !watching:
			if err := s.watcher.Add(p.ID, p.Path); err != nil {
				slog.Warn("cannot watch project", "project", p.ID, "err", err)
			}
			// Whatever was scanned before the watcher ran may be out of date.
			s.trees.invalidate(p.ID)
		}
		out = append(out, ProjectInfo{ID: p.ID, Name: p.Name, Path: p.Path, Missing: missing})
	}
	return out
}

// Projects returns the registered projects in the order they were added.
func (s *ProjectService) Projects() []ProjectInfo {
	out := s.syncWatchers()
	slog.Debug("service call", "method", "Projects", "projects", len(out))
	return out
}

// AddProject asks for a folder and registers it. Cancelling is not an error:
// the result is the zero ProjectInfo.
func (s *ProjectService) AddProject(ctx context.Context) (ProjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ProjectInfo{}, s.fail("add project", err)
	}
	dir, err := s.dialogs.ChooseFolder()
	if err != nil {
		return ProjectInfo{}, s.fail("choose folder", err)
	}
	if dir == "" {
		return ProjectInfo{}, nil
	}
	p, err := s.store.AddProject(dir)
	if err != nil {
		return ProjectInfo{}, s.fail("add project", err)
	}
	s.watchMu.Lock()
	if !s.watcher.Watching(p.ID) {
		if err := s.watcher.Add(p.ID, p.Path); err != nil {
			// The project is registered anyway; Projects retries later.
			slog.Warn("cannot watch project", "project", p.ID, "err", err)
		}
	}
	s.watchMu.Unlock()
	s.trees.invalidate(p.ID)
	s.notify(eventProjectsChanged, "")
	return ProjectInfo{ID: p.ID, Name: p.Name, Path: p.Path, Missing: !isDir(p.Path)}, nil
}

// RemoveProject forgets a project. The folder on disk is never touched.
func (s *ProjectService) RemoveProject(id string) error {
	s.watchMu.Lock()
	s.watcher.Remove(id)
	s.watchMu.Unlock()
	s.trees.forget(id)
	if err := s.store.RemoveProject(id); err != nil {
		return s.fail("remove project", err)
	}
	s.notify(eventProjectsChanged, "")
	return nil
}

// errMissing marks a project whose folder is not there right now.
var errMissing = errors.New("project folder is missing")

// root returns the folder of a known project whose folder exists.
func (s *ProjectService) root(id string) (string, error) {
	p, ok := s.store.Project(id)
	if !ok {
		return "", config.ErrNotFound
	}
	if !isDir(p.Path) {
		return "", errMissing
	}
	return p.Path, nil
}

// Tree returns the top level of the project tree (folders carry children).
func (s *ProjectService) Tree(ctx context.Context, id string) ([]project.Node, error) {
	root, err := s.root(id)
	if err != nil {
		return nil, s.fail("tree", err)
	}
	ctx, cancel := s.bounded(ctx)
	defer cancel()
	tree, err := s.scan(ctx, root, s.limits.MaxTreeNodes)
	if err != nil {
		return nil, s.fail("tree", err)
	}
	if len(tree.Skipped) > 0 {
		// Relative paths only; the frontend does not need to know.
		slog.Warn("unreadable folders skipped", "project", id, "count", len(tree.Skipped), "folders", tree.Skipped)
	}
	slog.Debug("service call", "method", "Tree", "project", id, "top", len(tree.Nodes))
	if tree.Nodes == nil {
		return []project.Node{}, nil // JSON [] rather than null
	}
	return tree.Nodes, nil
}

// treeCache keeps the last tree scanned for each project, so a search while
// the user types does not walk the folders again for every keystroke.
//
// A cached tree is trusted only while something would have said it went stale:
// the file watcher of that project is running. So it is used only when the
// project is watched now, and it is stored only when the project was watched
// before the scan began and has not been invalidated since (the generation).
// Whatever stops the watching (a folder that went away, a stream that died)
// reports through invalidate, and a project that is not watched is scanned
// again on every search, never served from an old tree.
type treeCache struct {
	mu      sync.Mutex
	entries map[string]*cachedTree
}

type cachedTree struct {
	// slot has room for one: the call that scans a project holds it, so one
	// project is never scanned twice at the same time, and callers that come
	// meanwhile wait for the result instead of repeating the work.
	slot chan struct{}

	// The rest is guarded by treeCache.mu.
	gen   uint64 // bumped by every invalidation
	valid bool
	nodes []project.Node
}

// entry returns the cache entry of a project, made on first use.
func (c *treeCache) entry(id string) *cachedTree {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]*cachedTree)
	}
	e, ok := c.entries[id]
	if !ok {
		e = &cachedTree{slot: make(chan struct{}, 1)}
		c.entries[id] = e
	}
	return e
}

// invalidate drops the tree of a project and makes a scan that is running
// unable to store its result.
func (c *treeCache) invalidate(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[id]; ok {
		e.gen++
		e.valid, e.nodes = false, nil
	}
}

// forget is invalidate for a project that is gone.
func (c *treeCache) forget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[id]; ok {
		e.gen++
		e.valid, e.nodes = false, nil
		delete(c.entries, id)
	}
}

// cachedNodes returns the tree of a project for the search: the cached one when
// it can be trusted, otherwise a fresh scan.
func (s *ProjectService) cachedNodes(ctx context.Context, p ProjectInfo) ([]project.Node, error) {
	e := s.trees.entry(p.ID)
	select {
	case e.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-e.slot }()

	// Asked outside treeCache.mu: the watcher has a lock of its own.
	if s.watcher.Watching(p.ID) {
		s.trees.mu.Lock()
		nodes, ok := e.nodes, e.valid
		s.trees.mu.Unlock()
		if ok {
			return nodes, nil
		}
	}

	// The generation before the watcher is asked: a watcher that starts in
	// between has invalidated by the time it is seen, so the result is not stored.
	s.trees.mu.Lock()
	gen := e.gen
	s.trees.mu.Unlock()
	watchedBefore := s.watcher.Watching(p.ID)

	tree, err := s.scan(ctx, p.Path, s.limits.MaxTreeNodes)
	if err != nil {
		return nil, err
	}
	if len(tree.Skipped) > 0 {
		slog.Warn("unreadable folders skipped", "project", p.ID, "count", len(tree.Skipped), "folders", tree.Skipped)
	}
	if watchedBefore && s.watcher.Watching(p.ID) {
		s.trees.mu.Lock()
		if e.gen == gen {
			e.nodes, e.valid = tree.Nodes, true
		}
		s.trees.mu.Unlock()
	}
	return tree.Nodes, nil
}

// SearchDocuments finds documents by name in all projects, or in one when
// projectID is not empty. A blank query gives no hits. The result is never
// null: JSON [] when there is nothing. A project whose folder is missing or
// whose tree is too large to scan is left out, and the others are searched.
func (s *ProjectService) SearchDocuments(ctx context.Context, query, projectID string) ([]project.Hit, error) {
	slog.Debug("service call", "method", "SearchDocuments", "query_len", len(query), "project", projectID)
	if strings.TrimSpace(query) == "" {
		return []project.Hit{}, nil
	}
	ctx, cancel := s.bounded(ctx)
	defer cancel()

	var sources []project.SearchSource
	known := projectID == ""
	for _, p := range s.syncWatchers() {
		if projectID != "" && p.ID != projectID {
			continue
		}
		known = true
		if p.Missing {
			s.trees.invalidate(p.ID)
			continue
		}
		nodes, err := s.cachedNodes(ctx, p)
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return nil, s.fail("search", err)
		default:
			// Not the search's failure: the others are still searched.
			slog.Warn("search: project skipped", "project", p.ID, "tooLarge", errors.Is(err, project.ErrTooLarge), "err", err)
			continue
		}
		sources = append(sources, project.SearchSource{ProjectID: p.ID, ProjectName: p.Name, Nodes: nodes})
	}
	if !known {
		return nil, s.fail("search", config.ErrNotFound)
	}
	hits := project.Search(sources, query, s.limits.MaxSearchResults)
	if hits == nil {
		return []project.Hit{}, nil // JSON [] rather than null
	}
	return hits, nil
}

// requireFolder checks that dirRel names an existing folder. A missing folder
// is reported as invalid-path: the frontend only sends paths from its tree, so
// a path that is gone means the tree is stale.
func requireFolder(root, dirRel string) error {
	abs, err := project.Resolve(root, dirRel)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %q is not an existing folder", project.ErrInvalidPath, dirRel)
	}
	return nil
}

func nodeFor(rel string) (project.Node, error) {
	name := path.Base(rel)
	kind, ok := project.KindOf(name)
	if !ok {
		return project.Node{}, fmt.Errorf("no document kind for %q", name)
	}
	return project.Node{Name: name, Rel: rel, Kind: kind}, nil
}

// CreateDocument creates an empty document in the folder dirRel ("" is the project root).
func (s *ProjectService) CreateDocument(ctx context.Context, id, dirRel, name string) (project.Node, error) {
	root, err := s.root(id)
	if err != nil {
		return project.Node{}, s.fail("create document", err)
	}
	ctx, cancel := s.bounded(ctx)
	defer cancel()
	if err := requireFolder(root, dirRel); err != nil {
		return project.Node{}, s.fail("create document", err)
	}
	rel, err := s.docs.CreateFile(ctx, root, dirRel, name)
	if err != nil {
		return project.Node{}, s.fail("create document", err)
	}
	node, err := nodeFor(rel)
	if err != nil {
		return project.Node{}, s.fail("create document", err)
	}
	s.notify(eventProjectChanged, id) // do not wait for FSEvents
	return node, nil
}

// errUnsaved marks a document that cannot be renamed or removed because the
// editor holds changes that are not on disk yet.
var errUnsaved = errors.New("the document has unsaved changes")

// refuseUnsaved fails with errUnsaved when the document at abs is open in the
// editor with unsaved changes. When the editor cannot say, the action is
// refused too: it must not go ahead blind, because the open buffer would write
// the old file back at the next save.
func (s *ProjectService) refuseUnsaved(ctx context.Context, abs string) error {
	unsaved, err := s.editor.unsaved(ctx, abs)
	if err != nil {
		return fmt.Errorf("ask the editor about %q: %w", abs, err)
	}
	if unsaved {
		return errUnsaved
	}
	return nil
}

// tellEditor runs an editor notification after the disk has already changed.
// If it fails the action is not undone: the disk is the truth, and a tree that
// still shows the old state would be worse than a stale editor buffer, so the
// failure is logged and the caller carries on and reports success. It must not
// be cut short with the request: the disk change is done, so the editor is told
// even if the caller went away.
func (s *ProjectService) tellEditor(ctx context.Context, op string, notify func(context.Context) error) {
	ctx, cancel := s.bounded(context.WithoutCancel(ctx))
	defer cancel()
	if err := notify(ctx); err != nil {
		slog.Error("project service: the editor was not told about a change on disk", "op", op, "err", err)
	}
}

// RenameDocument renames the document rel and returns its new state. A
// document that is open in the editor with unsaved changes is not renamed: the
// editor would write the old file back at the next save. Otherwise the editor
// closes the old buffer and, when it was the current one, opens the new file.
func (s *ProjectService) RenameDocument(ctx context.Context, id, rel, newName string) (project.Node, error) {
	root, err := s.root(id)
	if err != nil {
		return project.Node{}, s.fail("rename document", err)
	}
	ctx, cancel := s.bounded(ctx)
	defer cancel()
	if err := requireDocument(root, rel); err != nil {
		return project.Node{}, s.fail("rename document", err)
	}
	oldPath, err := project.Resolve(root, rel)
	if err != nil {
		return project.Node{}, s.fail("rename document", err)
	}
	// Before anything changes on disk.
	if err := s.refuseUnsaved(ctx, oldPath); err != nil {
		return project.Node{}, s.fail("rename document", err)
	}
	newRel, err := s.docs.Rename(ctx, root, rel, newName)
	if err != nil {
		return project.Node{}, s.fail("rename document", err)
	}
	node, nodeErr := nodeFor(newRel)
	if newRel != rel {
		s.tellEditor(ctx, "renamed", func(ctx context.Context) error {
			newPath, err := project.Resolve(root, newRel)
			if err != nil {
				return err
			}
			return s.editor.renamed(ctx, oldPath, newPath)
		})
	}
	// The file is renamed whatever nodeFor says, so the tree has to refresh.
	s.notify(eventProjectChanged, id)
	if nodeErr != nil {
		return project.Node{}, s.fail("rename document", nodeErr)
	}
	return node, nil
}

// requireDocument checks, before any dialog is shown, that rel is a supported
// document inside the project. docsafe repeats the check before it acts.
func requireDocument(root, rel string) error {
	if rel == "" {
		return fmt.Errorf("%w: the project root", docsafe.ErrNotDocument)
	}
	abs, err := project.Resolve(root, rel)
	if err != nil {
		return err
	}
	if _, ok := project.KindOf(path.Base(rel)); !ok {
		return fmt.Errorf("%w: %q has no supported extension", docsafe.ErrNotDocument, rel)
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %q is not a file", docsafe.ErrNotDocument, rel)
	}
	return nil
}

// TrashDocument moves a document to the Trash after the user confirms.
// Declining is not an error.
func (s *ProjectService) TrashDocument(ctx context.Context, id, rel string) error {
	root, err := s.root(id)
	if err != nil {
		return s.fail("trash document", err)
	}
	if err := requireDocument(root, rel); err != nil {
		return s.fail("trash document", err)
	}
	absPath, err := project.Resolve(root, rel)
	if err != nil {
		return s.fail("trash document", err)
	}
	// Before the dialog: the user must not be asked about something that is refused anyway.
	checkCtx, checkCancel := s.bounded(ctx)
	err = s.refuseUnsaved(checkCtx, absPath)
	checkCancel()
	if err != nil {
		return s.fail("trash document", err)
	}
	// The dialog is outside the time limit: the user may think.
	ok, err := s.dialogs.ConfirmTrash(path.Base(rel))
	if err != nil {
		return s.fail("confirm trash", err)
	}
	if !ok {
		return nil
	}
	ctx, cancel := s.bounded(ctx)
	defer cancel()
	if err := s.docs.Trash(ctx, root, rel); err != nil {
		return s.fail("trash document", err)
	}
	s.tellEditor(ctx, "removed", func(ctx context.Context) error { return s.editor.removed(ctx, absPath) })
	s.notify(eventProjectChanged, id)
	return nil
}

// Error codes the frontend can rely on (see frontend/src/lib/apiError.ts).
const (
	codeNotFound         = "not-found"
	codeMissing          = "missing"
	codeTooLarge         = "too-large"
	codeInvalidPath      = "invalid-path"
	codeExists           = "exists"
	codeInvalidName      = "invalid-name"
	codeUnsupportedKind  = "unsupported-kind"
	codeNotDocument      = "not-document"
	codeNotADirectory    = "not-a-directory"
	codeCanceled         = "canceled"
	codeNotRunning       = "not-running"
	codeNvimMissing      = "nvim-missing"
	codeNvimTooOld       = "nvim-too-old"
	codeUnnamedBuffer    = "unnamed-buffer"
	codeUnsavedChanges   = "unsaved-changes"
	codePandocMissing    = "pandoc-missing"
	codePandocTooOld     = "pandoc-too-old"
	codeConvertFailed    = "convert-failed"
	codeNotWord          = "not-word"
	codeDocumentTooLarge = "document-too-large"
	codeReadOnly         = "read-only"
	codeConflict         = "conflict"
	codeDamagedWork      = "damaged-work"
	codePreviewTooLarge  = "preview-too-large"
	codeInvalidFormat    = "invalid-format"
	codeInternal         = "internal"
)

// codeOf maps an error to its stable code, looking through wrapping. Package
// sentinels come before the context errors, so a specific meaning wins over
// "canceled" when both are in the chain.
func codeOf(err error) string {
	var ae *apiError
	var te *convert.ToolError
	var pe *preview.ToolError
	var oe *toolTooOldError
	switch {
	case errors.As(err, &ae):
		return ae.code
	// Before the cases below: a tool that is too old says so, whatever it wraps.
	case errors.As(err, &oe):
		return oe.code()
	case errors.Is(err, config.ErrNotFound):
		return codeNotFound
	case errors.Is(err, errMissing):
		return codeMissing
	case errors.Is(err, project.ErrTooLarge):
		return codeTooLarge
	case errors.Is(err, project.ErrInvalidPath), errors.Is(err, project.ErrOutsideRoot):
		return codeInvalidPath
	case errors.Is(err, docsafe.ErrExists):
		return codeExists
	case errors.Is(err, docsafe.ErrInvalidName):
		return codeInvalidName
	case errors.Is(err, docsafe.ErrUnsupportedKind):
		return codeUnsupportedKind
	case errors.Is(err, docsafe.ErrNotDocument):
		return codeNotDocument
	case errors.Is(err, config.ErrNotADirectory):
		return codeNotADirectory
	// A missing pandoc wraps exec.ErrNotFound, so it must come before the
	// nvim-missing case below.
	case errors.Is(err, convert.ErrToolMissing), errors.Is(err, preview.ErrToolMissing):
		return codePandocMissing
	case errors.As(err, &te), errors.As(err, &pe):
		return codeConvertFailed
	case errors.Is(err, convert.ErrNotDocx):
		return codeNotWord
	case errors.Is(err, convert.ErrTooLarge):
		return codeDocumentTooLarge
	case errors.Is(err, preview.ErrTooLarge), errors.Is(err, nvim.ErrBufferTooLarge):
		return codePreviewTooLarge
	case errors.Is(err, docsafe.ErrReadOnly):
		return codeReadOnly
	case errors.Is(err, docsafe.ErrConflict):
		return codeConflict
	case errors.Is(err, docsafe.ErrDamagedWork):
		return codeDamagedWork
	case errors.Is(err, nvim.ErrNotRunning), errors.Is(err, term.ErrClosed):
		return codeNotRunning
	// Only Neovim is looked up through PATH so far; a service that runs another
	// program this way needs its own sentinel, or its failure would read as this one.
	case errors.Is(err, exec.ErrNotFound):
		return codeNvimMissing
	case errors.Is(err, nvim.ErrUnknownFormat):
		return codeInvalidFormat
	case errors.Is(err, nvim.ErrUnnamedBuffer):
		return codeUnnamedBuffer
	case errors.Is(err, errUnsaved), errors.Is(err, nvim.ErrBufferModified):
		return codeUnsavedChanges
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return codeCanceled
	}
	return codeInternal
}

// codeText is the English description that travels with a code. It is for logs
// and developers; the user sees the Croatian text the frontend picks by code.
// It never contains paths: the full error, which may, is only logged.
var codeText = map[string]string{
	codeNotFound:         "unknown project",
	codeMissing:          "the project folder does not exist",
	codeTooLarge:         "the project has too many entries",
	codeInvalidPath:      "invalid path",
	codeExists:           "an item with that name already exists",
	codeInvalidName:      "invalid name",
	codeUnsupportedKind:  "unsupported document type",
	codeNotDocument:      "not a document",
	codeNotADirectory:    "not a folder",
	codeCanceled:         "the operation was canceled or timed out",
	codeNotRunning:       "the editor is not running",
	codeNvimMissing:      "the Neovim binary was not found",
	codeNvimTooOld:       "the Neovim binary is older than Pisar works with",
	codeUnnamedBuffer:    "a modified buffer has no file name",
	codeUnsavedChanges:   "the document has unsaved changes",
	codePandocMissing:    "pandoc is not installed",
	codePandocTooOld:     "pandoc is older than Pisar works with",
	codeConvertFailed:    "the document conversion failed",
	codeNotWord:          "not a valid Word document",
	codeDocumentTooLarge: "the document is too large to open",
	codeReadOnly:         "the document is open read-only",
	codeConflict:         "the file changed outside Pisar",
	codeDamagedWork:      "the working copy is damaged",
	codePreviewTooLarge:  "the document is too large for the preview",
	codeInvalidFormat:    "unknown formatting action",
	codeInternal:         "unexpected error",
}

// apiError is what service methods return. Wails delivers it to JavaScript as
// a RuntimeError whose message is Error() and whose cause is marshalError's
// JSON (checked in the Wails v3.1.0 sources, see frontend/src/lib/apiError.ts).
type apiError struct {
	code  string
	cause error // full error, for logs and errors.Is; never sent
}

func (e *apiError) Error() string { return e.code + ": " + codeText[e.code] }
func (e *apiError) Unwrap() error { return e.cause }

// fail logs the full error and returns the one the frontend may see.
func (s *ProjectService) fail(op string, err error) error {
	code := codeOf(err)
	if code == codeInternal {
		slog.Error("project service", "op", op, "err", err)
	} else {
		slog.Debug("project service", "op", op, "code", code, "err", err)
	}
	return &apiError{code: code, cause: err}
}

// marshalError is the service's ServiceOptions.MarshalError: it puts the code
// into the JavaScript error's cause. nil falls back to Wails' default.
func marshalError(err error) []byte {
	var ae *apiError
	if !errors.As(err, &ae) {
		return nil
	}
	b, jerr := json.Marshal(struct {
		Code string `json:"code"`
	}{ae.code})
	if jerr != nil {
		return nil
	}
	return b
}
