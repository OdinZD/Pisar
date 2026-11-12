package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"Pisar/internal/config"
	"Pisar/internal/convert"
	"Pisar/internal/docsafe"
	"Pisar/internal/nvim"
	"Pisar/internal/preview"
	"Pisar/internal/project"
)

// fakeDialogs answers the dialogs without any UI.
type fakeDialogs struct {
	mu          sync.Mutex
	folder      string
	folderErr   error
	confirm     bool
	confirmErr  error
	chooseCalls int
	confirmed   []string // names the confirmation was asked for
}

func (f *fakeDialogs) ChooseFolder() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chooseCalls++
	return f.folder, f.folderErr
}

func (f *fakeDialogs) ConfirmTrash(name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirmed = append(f.confirmed, name)
	return f.confirm, f.confirmErr
}

func (f *fakeDialogs) set(fn func(*fakeDialogs)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// fakeOpen is the editor as the project service sees it: it records what it was
// told and answers what the test sets.
type fakeOpen struct {
	mu         sync.Mutex
	open       bool  // unsaved reports this
	unsavedErr error // unsaved fails with this
	tellErr    error // renamed and removed fail with this
	asked      []string
	renames    [][2]string
	removes    []string
}

func (f *fakeOpen) unsaved(_ context.Context, path string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, path)
	return f.open, f.unsavedErr
}

func (f *fakeOpen) renamed(_ context.Context, oldPath, newPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renames = append(f.renames, [2]string{oldPath, newPath})
	return f.tellErr
}

func (f *fakeOpen) removed(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removes = append(f.removes, path)
	return f.tellErr
}

func (f *fakeOpen) set(fn func(*fakeOpen)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// fakeSource is a project.Source driven by the test.
type fakeSource struct {
	mu     sync.Mutex
	chans  map[string]chan []project.Change // the stream started last for a root
	closed map[chan []project.Change]bool
	ends   map[chan []project.Change]*sync.Once
	// startErr, when set, makes Start fail: a project that cannot be watched.
	startErr error
}

func (f *fakeSource) Start(ctx context.Context, root string) (<-chan []project.Change, error) {
	f.mu.Lock()
	if f.startErr != nil {
		err := f.startErr
		f.mu.Unlock()
		return nil, err
	}
	ch := make(chan []project.Change, 16)
	once := new(sync.Once)
	if f.chans == nil {
		f.chans = map[string]chan []project.Change{}
		f.closed = map[chan []project.Change]bool{}
		f.ends = map[chan []project.Change]*sync.Once{}
	}
	f.chans[root] = ch
	f.ends[ch] = once
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.end(ch, once)
	}()
	return ch, nil
}

// end closes a stream once, whether the watcher stopped it or the test killed it.
func (f *fakeSource) end(ch chan []project.Change, once *sync.Once) {
	once.Do(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.closed[ch] = true
		close(ch)
	})
}

// die ends the stream of root on its own, as FSEvents does when it loses track:
// the watcher sees the channel close without having asked for it.
func (f *fakeSource) die(t *testing.T, root string) {
	t.Helper()
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	ch := f.chans[real]
	once := f.ends[ch]
	f.mu.Unlock()
	if ch == nil {
		t.Fatalf("no stream for %s", real)
	}
	f.end(ch, once)
}

// touch reports a change of a document file under root (a real path, as Start received it).
func (f *fakeSource) touch(t *testing.T, root string) {
	t.Helper()
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := f.chans[real]
	if ch == nil {
		t.Fatalf("no stream for %s", real)
	}
	if f.closed[ch] {
		return // a stopped stream delivers nothing
	}
	ch <- []project.Change{{Path: filepath.Join(real, "x.md")}}
}

type emitted struct{ name, data string }

type env struct {
	t       *testing.T
	home    string
	store   *config.Store
	svc     *ProjectService
	dlg     *fakeDialogs
	ed      *fakeOpen
	src     *fakeSource
	trashFn func(name string, args []string) error // fake trash tool

	mu     sync.Mutex
	events []emitted
	runs   [][]string // name followed by args of every fake trash call
}

func (e *env) emit(name, data string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, emitted{name, data})
}

func (e *env) emits() []emitted {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.events)
}

func (e *env) trashRuns() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.runs)
}

// count returns how many events with that name and data arrived.
func (e *env) count(name, data string) int {
	n := 0
	for _, ev := range e.emits() {
		if ev.name == name && ev.data == data {
			n++
		}
	}
	return n
}

func newEnv(t *testing.T, mutate ...func(*config.Limits)) *env {
	t.Helper()
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	limits := store.Limits()
	limits.WatchDebounce = 10 * time.Millisecond
	limits.WatchMaxDelay = 50 * time.Millisecond
	for _, m := range mutate {
		m(&limits)
	}
	e := &env{t: t, home: home, store: store, dlg: &fakeDialogs{}, ed: &fakeOpen{}, src: &fakeSource{}}
	// The fake tool really removes the file: docsafe checks that it is gone.
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		e.mu.Lock()
		e.runs = append(e.runs, append([]string{name}, args...))
		fn := e.trashFn
		e.mu.Unlock()
		if fn != nil {
			if err := fn(name, args); err != nil {
				return []byte("failed"), err
			}
			return nil, nil
		}
		return nil, os.Remove(args[len(args)-1])
	}
	docs, err := docsafe.New(docsafe.Options{TrashBin: store.TrashBin(), Timeout: limits.CommandTimeout, Run: run})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := newProjectService(projectServiceDeps{
		Store: store, Docs: docs, Dialogs: e.dlg, Editor: e.ed, Source: e.src, Emit: e.emit, Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.shutdown)
	e.svc = svc
	return e
}

// addProject registers a fresh folder through the service and clears the events.
func (e *env) addProject() (ProjectInfo, string) {
	e.t.Helper()
	root := e.t.TempDir()
	e.dlg.set(func(d *fakeDialogs) { d.folder = root })
	info, err := e.svc.AddProject(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	e.mu.Lock()
	e.events = nil
	e.mu.Unlock()
	return info, root
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error %q, got nil", code)
	}
	var ae *apiError
	if !errors.As(err, &ae) {
		t.Fatalf("error %v (%T) is not an apiError", err, err)
	}
	if ae.code != code {
		t.Fatalf("code = %q, want %q (%v)", ae.code, code, err)
	}
}

func TestAddProject(t *testing.T) {
	t.Run("adds, persists, watches, emits once", func(t *testing.T) {
		e := newEnv(t)
		root := t.TempDir()
		e.dlg.set(func(d *fakeDialogs) { d.folder = root })
		info, err := e.svc.AddProject(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.ID == "" || info.Path != root || info.Name != filepath.Base(root) || info.Missing {
			t.Fatalf("info = %+v", info)
		}
		if got := e.svc.Projects(); len(got) != 1 || got[0] != info {
			t.Fatalf("Projects = %+v", got)
		}
		reopened, err := config.Open(e.home)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := reopened.Project(info.ID); !ok {
			t.Fatal("project not persisted")
		}
		if !e.svc.watcher.Watching(info.ID) {
			t.Fatal("project is not watched")
		}
		if n := e.count(eventProjectsChanged, ""); n != 1 || len(e.emits()) != 1 {
			t.Fatalf("events = %+v", e.emits())
		}
	})

	t.Run("cancel changes nothing", func(t *testing.T) {
		e := newEnv(t)
		info, err := e.svc.AddProject(context.Background()) // folder is ""
		if err != nil || info != (ProjectInfo{}) {
			t.Fatalf("info = %+v, err = %v", info, err)
		}
		if len(e.svc.Projects()) != 0 || len(e.emits()) != 0 {
			t.Fatalf("something changed: %+v", e.emits())
		}
		if exists(filepath.Join(e.home, "config.json")) {
			t.Fatal("config file written")
		}
	})

	t.Run("same folder twice is one project", func(t *testing.T) {
		e := newEnv(t)
		first, root := e.addProject()
		e.dlg.set(func(d *fakeDialogs) { d.folder = root })
		second, err := e.svc.AddProject(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if second.ID != first.ID || len(e.svc.Projects()) != 1 {
			t.Fatalf("second = %+v, projects = %+v", second, e.svc.Projects())
		}
	})

	t.Run("dialog error", func(t *testing.T) {
		e := newEnv(t)
		e.dlg.set(func(d *fakeDialogs) { d.folderErr = errors.New("boom /secret/path") })
		_, err := e.svc.AddProject(context.Background())
		wantCode(t, err, codeInternal)
		if want := "internal: unexpected error"; err.Error() != want {
			t.Fatalf("message = %q, want %q (no paths may leak)", err.Error(), want)
		}
		if len(e.svc.Projects()) != 0 || len(e.emits()) != 0 {
			t.Fatal("something changed")
		}
	})

	t.Run("not a folder", func(t *testing.T) {
		e := newEnv(t)
		file := filepath.Join(t.TempDir(), "f.md")
		write(t, file, "")
		e.dlg.set(func(d *fakeDialogs) { d.folder = file })
		_, err := e.svc.AddProject(context.Background())
		wantCode(t, err, codeNotADirectory)
	})
}

func TestRemoveProject(t *testing.T) {
	e := newEnv(t)
	info, root := e.addProject()
	write(t, filepath.Join(root, "a.md"), "keep")

	if err := e.svc.RemoveProject(info.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.svc.Projects()) != 0 {
		t.Fatal("project still listed")
	}
	reopened, err := config.Open(e.home)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Projects()) != 0 {
		t.Fatal("project still persisted")
	}
	if e.svc.watcher.Watching(info.ID) {
		t.Fatal("still watched")
	}
	if e.count(eventProjectsChanged, "") != 1 {
		t.Fatalf("events = %+v", e.emits())
	}
	if b, err := os.ReadFile(filepath.Join(root, "a.md")); err != nil || string(b) != "keep" {
		t.Fatalf("folder content touched: %q, %v", b, err)
	}

	before := len(e.emits())
	wantCode(t, e.svc.RemoveProject("nope"), codeNotFound)
	if len(e.emits()) != before {
		t.Fatal("event for unknown project")
	}
}

func TestProjectsMissingAndBack(t *testing.T) {
	e := newEnv(t)
	info, root := e.addProject()
	if got := e.svc.Projects(); got[0].Missing {
		t.Fatal("present folder reported missing")
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if got := e.svc.Projects(); !got[0].Missing {
		t.Fatalf("Missing = false after delete: %+v", got)
	}
	if e.svc.watcher.Watching(info.ID) {
		t.Fatal("still watched while missing")
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := e.svc.Projects(); got[0].Missing {
		t.Fatalf("Missing = true after the folder came back: %+v", got)
	}
	if !e.svc.watcher.Watching(info.ID) {
		t.Fatal("not watched again")
	}
}

func TestProjectsOrderAndEmpty(t *testing.T) {
	e := newEnv(t)
	if got := e.svc.Projects(); got == nil || len(got) != 0 {
		t.Fatalf("empty list must be non-nil and empty, got %#v", got)
	}
	a, _ := e.addProject()
	b, _ := e.addProject()
	got := e.svc.Projects()
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("order = %+v", got)
	}
}

func TestTree(t *testing.T) {
	e := newEnv(t)
	info, root := e.addProject()
	write(t, filepath.Join(root, "a.md"), "")
	write(t, filepath.Join(root, "sub", "b.txt"), "")
	write(t, filepath.Join(root, "sub", "deep", "c.docx"), "")
	write(t, filepath.Join(root, "ignored.png"), "")

	nodes, err := e.svc.Tree(context.Background(), info.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []project.Node{
		{Name: "a.md", Rel: "a.md", Kind: project.KindMD},
		{Name: "sub", Rel: "sub", Kind: project.KindDir, Children: []project.Node{
			{Name: "b.txt", Rel: "sub/b.txt", Kind: project.KindTXT},
			{Name: "deep", Rel: "sub/deep", Kind: project.KindDir, Children: []project.Node{
				{Name: "c.docx", Rel: "sub/deep/c.docx", Kind: project.KindDOCX},
			}},
		}},
	}
	if !reflect.DeepEqual(nodes, want) {
		t.Fatalf("tree =\n%+v\nwant\n%+v", nodes, want)
	}

	empty, _ := e.addProject()
	nodes, err = e.svc.Tree(context.Background(), empty.ID)
	if err != nil || nodes == nil || len(nodes) != 0 {
		t.Fatalf("empty tree = %#v, %v", nodes, err)
	}

	_, err = e.svc.Tree(context.Background(), "nope")
	wantCode(t, err, codeNotFound)

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Tree(context.Background(), info.ID)
	wantCode(t, err, codeMissing)
}

func TestTreeTooLargeAndCanceled(t *testing.T) {
	e := newEnv(t, func(l *config.Limits) { l.MaxTreeNodes = 3 })
	info, root := e.addProject()
	for i := range 5 {
		write(t, filepath.Join(root, fmt.Sprintf("f%d.md", i)), "")
	}
	_, err := e.svc.Tree(context.Background(), info.ID)
	wantCode(t, err, codeTooLarge)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = e.svc.Tree(ctx, info.ID)
	wantCode(t, err, codeCanceled)
}

func TestCreateDocument(t *testing.T) {
	e := newEnv(t)
	info, root := e.addProject()
	write(t, filepath.Join(root, "taken.md"), "")
	write(t, filepath.Join(root, "dir", "keep.md"), "")
	write(t, filepath.Join(root, "file.md"), "")

	node, err := e.svc.CreateDocument(context.Background(), info.ID, "dir", "new")
	if err != nil {
		t.Fatal(err)
	}
	if want := (project.Node{Name: "new.md", Rel: "dir/new.md", Kind: project.KindMD}); fmt.Sprintf("%+v", node) != fmt.Sprintf("%+v", want) {
		t.Fatalf("node = %+v", node)
	}
	if !exists(filepath.Join(root, "dir", "new.md")) {
		t.Fatal("file not created")
	}
	if e.count(eventProjectChanged, info.ID) != 1 {
		t.Fatalf("events = %+v", e.emits())
	}
	node, err = e.svc.CreateDocument(context.Background(), info.ID, "", "n.txt")
	if err != nil || node.Kind != project.KindTXT || node.Rel != "n.txt" {
		t.Fatalf("node = %+v, err = %v", node, err)
	}

	cases := []struct {
		name         string
		id, dir, doc string
		code         string
	}{
		{"exists", info.ID, "", "taken.md", codeExists},
		{"exists without extension", info.ID, "", "taken", codeExists},
		{"slash in name", info.ID, "", "a/b", codeInvalidName},
		{"docx", info.ID, "", "x.docx", codeUnsupportedKind},
		{"parent dir", info.ID, "../x", "n", codeInvalidPath},
		{"missing dir", info.ID, "nothere", "n", codeInvalidPath},
		{"dir is a file", info.ID, "file.md", "n", codeInvalidPath},
		{"unknown project", "nope", "", "n", codeNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := len(e.emits())
			_, err := e.svc.CreateDocument(context.Background(), c.id, c.dir, c.doc)
			wantCode(t, err, c.code)
			if len(e.emits()) != before {
				t.Fatal("event after a failure")
			}
		})
	}
}

func TestRenameDocument(t *testing.T) {
	e := newEnv(t)
	info, root := e.addProject()
	write(t, filepath.Join(root, "dir", "old.md"), "text")
	write(t, filepath.Join(root, "dir", "other.md"), "")

	node, err := e.svc.RenameDocument(context.Background(), info.ID, "dir/old.md", "fresh.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := (project.Node{Name: "fresh.md", Rel: "dir/fresh.md", Kind: project.KindMD}); fmt.Sprintf("%+v", node) != fmt.Sprintf("%+v", want) {
		t.Fatalf("node = %+v", node)
	}
	if exists(filepath.Join(root, "dir", "old.md")) || !exists(filepath.Join(root, "dir", "fresh.md")) {
		t.Fatal("file not renamed")
	}
	if e.count(eventProjectChanged, info.ID) != 1 {
		t.Fatalf("events = %+v", e.emits())
	}

	cases := []struct{ name, rel, newName, code string }{
		{"exists", "dir/fresh.md", "other.md", codeExists},
		{"invalid name", "dir/fresh.md", "a/b.md", codeInvalidName},
		{"other type", "dir/fresh.md", "x.txt", codeUnsupportedKind},
		{"folder", "dir", "x", codeNotDocument},
		{"missing file", "dir/gone.md", "x.md", codeNotDocument},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := len(e.emits())
			_, err := e.svc.RenameDocument(context.Background(), info.ID, c.rel, c.newName)
			wantCode(t, err, c.code)
			if len(e.emits()) != before {
				t.Fatal("event after a failure")
			}
		})
	}
	_, err = e.svc.RenameDocument(context.Background(), "nope", "a.md", "b.md")
	wantCode(t, err, codeNotFound)
}

func TestRenameOpen(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*env, ProjectInfo, string) {
		e := newEnv(t)
		info, root := e.addProject()
		write(t, filepath.Join(root, "dir", "old.md"), "text")
		return e, info, root
	}

	t.Run("unsaved", func(t *testing.T) {
		e, info, root := setup(t)
		e.ed.set(func(f *fakeOpen) { f.open = true })
		_, err := e.svc.RenameDocument(ctx, info.ID, "dir/old.md", "fresh.md")
		wantCode(t, err, codeUnsavedChanges)
		if !exists(filepath.Join(root, "dir", "old.md")) || exists(filepath.Join(root, "dir", "fresh.md")) {
			t.Fatal("the disk changed")
		}
		if len(e.emits()) != 0 || len(e.ed.renames) != 0 {
			t.Fatalf("events %+v, renamed calls %v", e.emits(), e.ed.renames)
		}
	})

	t.Run("saved", func(t *testing.T) {
		e, info, root := setup(t)
		if _, err := e.svc.RenameDocument(ctx, info.ID, "dir/old.md", "fresh.md"); err != nil {
			t.Fatal(err)
		}
		if !exists(filepath.Join(root, "dir", "fresh.md")) || exists(filepath.Join(root, "dir", "old.md")) {
			t.Fatal("not renamed")
		}
		want := [][2]string{{filepath.Join(root, "dir", "old.md"), filepath.Join(root, "dir", "fresh.md")}}
		if !reflect.DeepEqual(e.ed.renames, want) {
			t.Fatalf("renamed calls = %v, want %v", e.ed.renames, want)
		}
		if !slices.Equal(e.ed.asked, []string{want[0][0]}) {
			t.Fatalf("asked about %v", e.ed.asked)
		}
		if e.count(eventProjectChanged, info.ID) != 1 {
			t.Fatalf("events = %+v", e.emits())
		}
	})

	t.Run("editor not told is not an error", func(t *testing.T) {
		e, info, root := setup(t)
		e.ed.set(func(f *fakeOpen) { f.tellErr = errors.New("nvim went away") })
		node, err := e.svc.RenameDocument(ctx, info.ID, "dir/old.md", "fresh.md")
		if err != nil || node.Rel != "dir/fresh.md" {
			t.Fatalf("got %+v, %v; want success", node, err)
		}
		if !exists(filepath.Join(root, "dir", "fresh.md")) || e.count(eventProjectChanged, info.ID) != 1 {
			t.Fatalf("renamed = %v, events = %+v", exists(filepath.Join(root, "dir", "fresh.md")), e.emits())
		}
	})

	t.Run("editor cannot tell: refused", func(t *testing.T) {
		e, info, root := setup(t)
		e.ed.set(func(f *fakeOpen) { f.unsavedErr = errors.New("rpc broke") })
		_, err := e.svc.RenameDocument(ctx, info.ID, "dir/old.md", "fresh.md")
		wantCode(t, err, codeInternal)
		if !exists(filepath.Join(root, "dir", "old.md")) || len(e.emits()) != 0 || len(e.ed.renames) != 0 {
			t.Fatal("renamed blind")
		}
	})
}

func TestTrashOpen(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*env, ProjectInfo, string) {
		e := newEnv(t)
		info, root := e.addProject()
		write(t, filepath.Join(root, "dir", "doc.md"), "text")
		return e, info, root
	}

	t.Run("unsaved", func(t *testing.T) {
		e, info, root := setup(t)
		e.dlg.set(func(d *fakeDialogs) { d.confirm = true })
		e.ed.set(func(f *fakeOpen) { f.open = true })
		wantCode(t, e.svc.TrashDocument(ctx, info.ID, "dir/doc.md"), codeUnsavedChanges)
		if len(e.dlg.confirmed) != 0 || len(e.trashRuns()) != 0 || len(e.ed.removes) != 0 {
			t.Fatalf("dialog %v, tool %v, removed %v", e.dlg.confirmed, e.trashRuns(), e.ed.removes)
		}
		if !exists(filepath.Join(root, "dir", "doc.md")) || len(e.emits()) != 0 {
			t.Fatal("the disk or the tree changed")
		}
	})

	t.Run("confirmed", func(t *testing.T) {
		e, info, root := setup(t)
		e.dlg.set(func(d *fakeDialogs) { d.confirm = true })
		if err := e.svc.TrashDocument(ctx, info.ID, "dir/doc.md"); err != nil {
			t.Fatal(err)
		}
		if len(e.trashRuns()) != 1 || !slices.Equal(e.ed.removes, []string{filepath.Join(root, "dir", "doc.md")}) {
			t.Fatalf("tool %v, removed %v", e.trashRuns(), e.ed.removes)
		}
		if e.count(eventProjectChanged, info.ID) != 1 {
			t.Fatalf("events = %+v", e.emits())
		}
	})

	t.Run("declined", func(t *testing.T) {
		e, info, _ := setup(t)
		if err := e.svc.TrashDocument(ctx, info.ID, "dir/doc.md"); err != nil {
			t.Fatal(err)
		}
		if len(e.trashRuns()) != 0 || len(e.ed.removes) != 0 || len(e.emits()) != 0 {
			t.Fatalf("tool %v, removed %v, events %v", e.trashRuns(), e.ed.removes, e.emits())
		}
	})

	t.Run("editor not told is not an error", func(t *testing.T) {
		e, info, root := setup(t)
		e.dlg.set(func(d *fakeDialogs) { d.confirm = true })
		e.ed.set(func(f *fakeOpen) { f.tellErr = errors.New("nvim went away") })
		if err := e.svc.TrashDocument(ctx, info.ID, "dir/doc.md"); err != nil {
			t.Fatal(err)
		}
		if exists(filepath.Join(root, "dir", "doc.md")) || e.count(eventProjectChanged, info.ID) != 1 {
			t.Fatalf("still there = %v, events = %+v", exists(filepath.Join(root, "dir", "doc.md")), e.emits())
		}
	})

	t.Run("editor cannot tell: refused, no dialog", func(t *testing.T) {
		e, info, root := setup(t)
		e.dlg.set(func(d *fakeDialogs) { d.confirm = true })
		e.ed.set(func(f *fakeOpen) { f.unsavedErr = errors.New("rpc broke") })
		wantCode(t, e.svc.TrashDocument(ctx, info.ID, "dir/doc.md"), codeInternal)
		if len(e.dlg.confirmed) != 0 || len(e.trashRuns()) != 0 || !exists(filepath.Join(root, "dir", "doc.md")) {
			t.Fatal("went ahead blind")
		}
	})
}

func TestTrashDocument(t *testing.T) {
	setup := func(t *testing.T) (*env, ProjectInfo, string) {
		e := newEnv(t)
		info, root := e.addProject()
		write(t, filepath.Join(root, "dir", "doc.md"), "text")
		write(t, filepath.Join(root, "folder.md"), "") // placeholder so root is not empty
		return e, info, root
	}

	t.Run("confirmed", func(t *testing.T) {
		e, info, root := setup(t)
		e.dlg.set(func(d *fakeDialogs) { d.confirm = true })
		if err := e.svc.TrashDocument(context.Background(), info.ID, "dir/doc.md"); err != nil {
			t.Fatal(err)
		}
		runs := e.trashRuns()
		if len(runs) != 1 || runs[0][0] != e.store.TrashBin() || runs[0][len(runs[0])-1] != filepath.Join(root, "dir", "doc.md") {
			t.Fatalf("runs = %v", runs)
		}
		if exists(filepath.Join(root, "dir", "doc.md")) {
			t.Fatal("file still there")
		}
		if e.count(eventProjectChanged, info.ID) != 1 {
			t.Fatalf("events = %+v", e.emits())
		}
		if !slices.Equal(e.dlg.confirmed, []string{"doc.md"}) {
			t.Fatalf("asked about %v", e.dlg.confirmed)
		}
	})

	t.Run("declined", func(t *testing.T) {
		e, info, root := setup(t)
		if err := e.svc.TrashDocument(context.Background(), info.ID, "dir/doc.md"); err != nil {
			t.Fatal(err)
		}
		if len(e.trashRuns()) != 0 || len(e.emits()) != 0 || !exists(filepath.Join(root, "dir", "doc.md")) {
			t.Fatal("declining must change nothing")
		}
	})

	t.Run("not a document: no dialog", func(t *testing.T) {
		e, info, _ := setup(t)
		e.dlg.set(func(d *fakeDialogs) { d.confirm = true })
		for _, rel := range []string{"dir", "", "dir/gone.md", "dir/doc.png"} {
			err := e.svc.TrashDocument(context.Background(), info.ID, rel)
			wantCode(t, err, codeNotDocument)
		}
		wantCode(t, e.svc.TrashDocument(context.Background(), info.ID, "../x.md"), codeInvalidPath)
		wantCode(t, e.svc.TrashDocument(context.Background(), "nope", "dir/doc.md"), codeNotFound)
		if len(e.dlg.confirmed) != 0 || len(e.trashRuns()) != 0 {
			t.Fatal("dialog shown or tool run for a non-document")
		}
	})

	t.Run("tool fails", func(t *testing.T) {
		e, info, root := setup(t)
		e.dlg.set(func(d *fakeDialogs) { d.confirm = true })
		e.trashFn = func(string, []string) error { return errors.New("exit status 5") }
		err := e.svc.TrashDocument(context.Background(), info.ID, "dir/doc.md")
		wantCode(t, err, codeInternal)
		if !exists(filepath.Join(root, "dir", "doc.md")) || len(e.emits()) != 0 {
			t.Fatal("file touched or event emitted after failure")
		}
	})

	t.Run("dialog fails", func(t *testing.T) {
		e, info, _ := setup(t)
		e.dlg.set(func(d *fakeDialogs) { d.confirmErr = errors.New("no ui") })
		wantCode(t, e.svc.TrashDocument(context.Background(), info.ID, "dir/doc.md"), codeInternal)
		if len(e.trashRuns()) != 0 {
			t.Fatal("tool run without confirmation")
		}
	})
}

func TestWatcherEvent(t *testing.T) {
	e := newEnv(t)
	info, root := e.addProject()
	e.src.touch(t, root)
	waitFor(t, "project:changed", func() bool { return e.count(eventProjectChanged, info.ID) >= 1 })
}

func TestShutdown(t *testing.T) {
	e := newEnv(t)
	info, root := e.addProject()
	e.src.touch(t, root) // a change is pending in the debounce window when we shut down
	e.svc.shutdown()
	e.src.touch(t, root)
	// Also a service call after shutdown must not emit.
	write(t, filepath.Join(root, "a.md"), "")
	if _, err := e.svc.RenameDocument(context.Background(), info.ID, "a.md", "b.md"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := e.emits(); len(got) != 0 {
		t.Fatalf("events after shutdown: %+v", got)
	}
	e.svc.shutdown() // second call: no panic, no effect
}

func TestConcurrentCalls(t *testing.T) {
	e := newEnv(t)
	info, root := e.addProject()
	write(t, filepath.Join(root, "seed.md"), "")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			if _, err := e.svc.Tree(context.Background(), info.ID); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := e.svc.CreateDocument(context.Background(), info.ID, "", fmt.Sprintf("doc%d", i)); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if got := e.svc.Projects(); len(got) != 1 {
				t.Errorf("Projects = %+v", got)
			}
		}()
	}
	wg.Wait()
	if n := e.count(eventProjectChanged, info.ID); n != 8 {
		t.Fatalf("project:changed = %d, want 8", n)
	}
}

func TestCodeOf(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", err)) }
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"not found", config.ErrNotFound, codeNotFound},
		{"missing", errMissing, codeMissing},
		{"too large", project.ErrTooLarge, codeTooLarge},
		{"invalid path", project.ErrInvalidPath, codeInvalidPath},
		{"outside root", project.ErrOutsideRoot, codeInvalidPath},
		{"exists", docsafe.ErrExists, codeExists},
		{"invalid name", docsafe.ErrInvalidName, codeInvalidName},
		{"unsupported kind", docsafe.ErrUnsupportedKind, codeUnsupportedKind},
		{"not document", docsafe.ErrNotDocument, codeNotDocument},
		{"not a directory", config.ErrNotADirectory, codeNotADirectory},
		{"unsaved", errUnsaved, codeUnsavedChanges},
		{"pandoc missing", fmt.Errorf("%w: pandoc: %w", convert.ErrToolMissing, exec.ErrNotFound), codePandocMissing},
		{"convert failed", &convert.ToolError{Tool: "pandoc", Err: errors.New("exit status 1")}, codeConvertFailed},
		{"not a Word document", convert.ErrNotDocx, codeNotWord},
		{"document too large", convert.ErrTooLarge, codeDocumentTooLarge},
		{"read only", &docsafe.ReadOnlyError{Path: "/x", Reason: docsafe.ReadOnlyLocked}, codeReadOnly},
		{"conflict", docsafe.ErrConflict, codeConflict},
		{"damaged work", docsafe.ErrDamagedWork, codeDamagedWork},
		{"buffer modified", nvim.ErrBufferModified, codeUnsavedChanges},
		{"preview too large", preview.ErrTooLarge, codePreviewTooLarge},
		{"preview source too large", nvim.ErrBufferTooLarge, codePreviewTooLarge},
		{"preview without pandoc", fmt.Errorf("%w: %w", preview.ErrToolMissing, exec.ErrNotFound), codePandocMissing},
		{"preview tool failed", &preview.ToolError{Tool: "pandoc", Err: errors.New("exit status 1")}, codeConvertFailed},
		{"unknown format", nvim.ErrUnknownFormat, codeInvalidFormat},
		{"deadline", context.DeadlineExceeded, codeCanceled},
		{"canceled", context.Canceled, codeCanceled},
		{"unknown", errors.New("whatever"), codeInternal},
		{"nil-ish fs error", os.ErrNotExist, codeInternal},
		{"already an apiError", &apiError{code: codeExists}, codeExists},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := codeOf(c.err); got != c.want {
				t.Errorf("codeOf(%v) = %q, want %q", c.err, got, c.want)
			}
			if got := codeOf(wrap(c.err)); got != c.want {
				t.Errorf("wrapped: codeOf = %q, want %q", got, c.want)
			}
		})
	}
	// Every code has a description, and none leaks a path.
	for _, c := range cases {
		ae := &apiError{code: codeOf(c.err), cause: c.err}
		if codeText[ae.code] == "" {
			t.Errorf("no text for %q", ae.code)
		}
	}
}

func TestMarshalError(t *testing.T) {
	if got := string(marshalError(&apiError{code: codeExists})); got != `{"code":"exists"}` {
		t.Errorf("got %s", got)
	}
	if got := marshalError(fmt.Errorf("x: %w", &apiError{code: codeMissing})); string(got) != `{"code":"missing"}` {
		t.Errorf("wrapped: got %s", got)
	}
	if got := marshalError(errors.New("plain")); got != nil {
		t.Errorf("plain error must fall back to the default, got %s", got)
	}
}

// scanSpy counts the scans of the project service and how many run at once per
// folder. gate, when set before the first search, holds every scan until it is
// closed.
type scanSpy struct {
	mu      sync.Mutex
	scans   map[string]int
	live    map[string]int
	maxLive map[string]int
	gate    chan struct{}
}

func (e *env) spyScans() *scanSpy {
	sp := &scanSpy{scans: map[string]int{}, live: map[string]int{}, maxLive: map[string]int{}}
	orig := e.svc.scan
	e.svc.scan = func(ctx context.Context, root string, maxNodes int) (project.Tree, error) {
		sp.mu.Lock()
		sp.scans[root]++
		sp.live[root]++
		sp.maxLive[root] = max(sp.maxLive[root], sp.live[root])
		gate := sp.gate
		sp.mu.Unlock()
		defer func() {
			sp.mu.Lock()
			sp.live[root]--
			sp.mu.Unlock()
		}()
		if gate != nil {
			select {
			case <-gate:
			case <-ctx.Done():
				return project.Tree{}, ctx.Err()
			}
		}
		return orig(ctx, root, maxNodes)
	}
	return sp
}

func (sp *scanSpy) count(root string) int {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.scans[root]
}

func (e *env) search(query, projectID string) []project.Hit {
	e.t.Helper()
	hits, err := e.svc.SearchDocuments(context.Background(), query, projectID)
	if err != nil {
		e.t.Fatalf("SearchDocuments(%q, %q): %v", query, projectID, err)
	}
	if hits == nil {
		e.t.Fatalf("SearchDocuments(%q, %q) returned nil, want an empty slice", query, projectID)
	}
	return hits
}

func hitRels(hits []project.Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.ProjectID+":"+h.Rel)
	}
	slices.Sort(out)
	return out
}

func TestSearchDocuments(t *testing.T) {
	e := newEnv(t)
	a, rootA := e.addProject()
	b, rootB := e.addProject()
	write(t, filepath.Join(rootA, "Plan puta.md"), "")
	write(t, filepath.Join(rootA, "sub", "Plan rada.docx"), "")
	write(t, filepath.Join(rootA, "bilješke.txt"), "")
	write(t, filepath.Join(rootB, "plan B.txt"), "")
	write(t, filepath.Join(rootB, "Izvještaj.md"), "")
	write(t, filepath.Join(rootB, "slika.png"), "")

	all := e.search("plan", "")
	want := []string{a.ID + ":Plan puta.md", a.ID + ":sub/Plan rada.docx", b.ID + ":plan B.txt"}
	slices.Sort(want)
	if got := hitRels(all); !slices.Equal(got, want) {
		t.Errorf("hits = %v, want %v", got, want)
	}
	for _, h := range all {
		if h.ProjectName == "" || h.Name == "" || len(h.Matches) == 0 {
			t.Errorf("incomplete hit %+v", h)
		}
	}
	// Both projects answered.
	if got := hitRels(e.search("plan", a.ID)); len(got) != 2 || !strings.HasPrefix(got[0], a.ID) || !strings.HasPrefix(got[1], a.ID) {
		t.Errorf("narrowed to %s: %v", a.ID, got)
	}
	if got := hitRels(e.search("plan", b.ID)); !slices.Equal(got, []string{b.ID + ":plan B.txt"}) {
		t.Errorf("narrowed to %s: %v", b.ID, got)
	}
	// Without diacritics, and only documents.
	if got := hitRels(e.search("izvjestaj", "")); !slices.Equal(got, []string{b.ID + ":Izvještaj.md"}) {
		t.Errorf("izvjestaj: %v", got)
	}
	if got := e.search("slika", ""); len(got) != 0 {
		t.Errorf("a picture was found: %+v", got)
	}

	_, err := e.svc.SearchDocuments(context.Background(), "plan", "nepoznat")
	wantCode(t, err, codeNotFound)
}

// Nothing to search for is an empty array, never null, which the frontend
// would have to special-case.
func TestSearchEmpty(t *testing.T) {
	e := newEnv(t)
	_, root := e.addProject()
	write(t, filepath.Join(root, "a.md"), "")
	for _, q := range []string{"", "   ", "\t\n", "nema ga"} {
		hits := e.search(q, "")
		if b, err := json.Marshal(hits); err != nil || string(b) != "[]" {
			t.Errorf("query %q: JSON %s, %v", q, b, err)
		}
	}
	// A blank query is answered before the project is looked up.
	if hits, err := e.svc.SearchDocuments(context.Background(), " ", "nepoznat"); err != nil || len(hits) != 0 {
		t.Errorf("blank query for an unknown project: %v, %v", hits, err)
	}
	// No projects at all.
	e2 := newEnv(t)
	if b, _ := json.Marshal(e2.search("a", "")); string(b) != "[]" {
		t.Errorf("no projects: %s", b)
	}
}

func TestSearchMissingFolder(t *testing.T) {
	e := newEnv(t)
	a, rootA := e.addProject()
	b, rootB := e.addProject()
	write(t, filepath.Join(rootA, "plan a.md"), "")
	write(t, filepath.Join(rootB, "plan b.md"), "")
	if got := e.search("plan", ""); len(got) != 2 {
		t.Fatalf("hits = %v", hitRels(got))
	}
	if err := os.RemoveAll(rootB); err != nil {
		t.Fatal(err)
	}
	if got := hitRels(e.search("plan", "")); !slices.Equal(got, []string{a.ID + ":plan a.md"}) {
		t.Errorf("with a missing folder: %v", got)
	}
	if got := e.search("plan", b.ID); len(got) != 0 {
		t.Errorf("the missing project itself: %+v", got)
	}
	// The folder comes back with other content.
	write(t, filepath.Join(rootB, "plan novi.md"), "")
	if got := hitRels(e.search("plan", b.ID)); !slices.Equal(got, []string{b.ID + ":plan novi.md"}) {
		t.Errorf("after the folder came back: %v", got)
	}
}

func TestSearchLimit(t *testing.T) {
	e := newEnv(t, func(l *config.Limits) { l.MaxSearchResults = 3 })
	_, root := e.addProject()
	for i := range 10 {
		write(t, filepath.Join(root, fmt.Sprintf("dok %d.md", i)), "")
	}
	if got := e.search("dok", ""); len(got) != 3 {
		t.Errorf("%d hits, want the limit 3", len(got))
	}
}

// A project with too large a tree is left out and the others are searched.
func TestSearchTooLarge(t *testing.T) {
	e := newEnv(t, func(l *config.Limits) { l.MaxTreeNodes = 5 })
	_, big := e.addProject()
	small, rootSmall := e.addProject()
	for i := range 10 {
		write(t, filepath.Join(big, fmt.Sprintf("plan %d.md", i)), "")
	}
	write(t, filepath.Join(rootSmall, "plan.md"), "")
	if got := hitRels(e.search("plan", "")); !slices.Equal(got, []string{small.ID + ":plan.md"}) {
		t.Errorf("hits = %v", got)
	}
}

func TestSearchCanceled(t *testing.T) {
	e := newEnv(t)
	_, root := e.addProject()
	write(t, filepath.Join(root, "a.md"), "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.svc.SearchDocuments(ctx, "a", "")
	wantCode(t, err, codeCanceled)
}

// The tree is scanned once and used until something says it changed.
func TestSearchCache(t *testing.T) {
	e := newEnv(t)
	sp := e.spyScans()
	p, root := e.addProject()
	write(t, filepath.Join(root, "alfa.md"), "")
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		e.search("alfa", "")
		e.search("a", p.ID)
	}
	if n := sp.count(root); n != 1 {
		t.Fatalf("%d scans for 6 searches, want 1 (%v)", n, sp.scans)
	}

	// A change on disk, reported by the watcher: the next search sees it.
	write(t, filepath.Join(root, "beta.md"), "")
	e.src.touch(t, real)
	waitFor(t, "the watcher's report", func() bool { return e.count(eventProjectChanged, p.ID) >= 1 })
	if got := hitRels(e.search("beta", "")); !slices.Equal(got, []string{p.ID + ":beta.md"}) {
		t.Errorf("after a change: %v", got)
	}
	if n := sp.count(root); n != 2 {
		t.Errorf("%d scans, want 2", n)
	}
	e.search("beta", "")
	if n := sp.count(root); n != 2 {
		t.Errorf("%d scans after a search with no change, want 2", n)
	}

	if err := os.Remove(filepath.Join(root, "beta.md")); err != nil {
		t.Fatal(err)
	}
	e.src.touch(t, real)
	waitFor(t, "a search that no longer finds it", func() bool { return len(e.search("beta", "")) == 0 })
}

// Created, renamed and trashed through the service: the result is fresh at
// once, without waiting for the file watcher.
func TestSearchThroughService(t *testing.T) {
	e := newEnv(t)
	sp := e.spyScans()
	p, root := e.addProject()
	write(t, filepath.Join(root, "stari.md"), "")
	ctx := context.Background()
	if got := e.search("novi", ""); len(got) != 0 {
		t.Fatalf("hits = %+v", got)
	}

	if _, err := e.svc.CreateDocument(ctx, p.ID, "", "novi dokument.md"); err != nil {
		t.Fatal(err)
	}
	if got := hitRels(e.search("novi", "")); !slices.Equal(got, []string{p.ID + ":novi dokument.md"}) {
		t.Errorf("after create: %v", got)
	}
	if _, err := e.svc.RenameDocument(ctx, p.ID, "novi dokument.md", "drugi dokument.md"); err != nil {
		t.Fatal(err)
	}
	if got := e.search("novi", ""); len(got) != 0 {
		t.Errorf("after rename, the old name: %v", hitRels(got))
	}
	if got := hitRels(e.search("drugi", "")); !slices.Equal(got, []string{p.ID + ":drugi dokument.md"}) {
		t.Errorf("after rename: %v", got)
	}
	e.dlg.set(func(d *fakeDialogs) { d.confirm = true })
	if err := e.svc.TrashDocument(ctx, p.ID, "drugi dokument.md"); err != nil {
		t.Fatal(err)
	}
	if got := e.search("drugi", ""); len(got) != 0 {
		t.Errorf("after trash: %v", hitRels(got))
	}
	if n := sp.count(root); n < 4 {
		t.Errorf("%d scans, want a new one after each of the 3 changes", n)
	}
}

// Many searches at once scan a project once, and never two scans of it at the
// same time. Run with -race.
func TestSearchConcurrent(t *testing.T) {
	e := newEnv(t)
	sp := e.spyScans()
	sp.gate = make(chan struct{})
	a, rootA := e.addProject()
	b, rootB := e.addProject()
	write(t, filepath.Join(rootA, "plan a.md"), "")
	write(t, filepath.Join(rootB, "plan b.md"), "")

	const n = 8
	var ready, done sync.WaitGroup
	results := make(chan []string, n)
	errs := make(chan error, n)
	for i := range n {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			id := ""
			if i%4 == 1 {
				id = a.ID // some narrow to one project
			}
			hits, err := e.svc.SearchDocuments(context.Background(), "plan", id)
			if err != nil {
				errs <- err
				return
			}
			results <- hitRels(hits)
		}()
	}
	ready.Wait()
	waitFor(t, "a scan to start", func() bool { return sp.count(rootA)+sp.count(rootB) >= 1 })
	close(sp.gate)
	done.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for r := range results {
		if len(r) != 1 && len(r) != 2 {
			t.Errorf("hits = %v", r)
		}
	}
	for _, root := range []string{rootA, rootB} {
		if sp.count(root) != 1 {
			t.Errorf("%d scans of %s, want 1", sp.count(root), root)
		}
		if sp.maxLive[root] > 1 {
			t.Errorf("%d scans of %s at the same time", sp.maxLive[root], root)
		}
	}
	_ = b
}

// A tree is not served from the cache when nothing watches the project: the
// stream died, and whatever changed meanwhile would never be reported.
func TestSearchNotWatched(t *testing.T) {
	e := newEnv(t)
	sp := e.spyScans()
	p, root := e.addProject()
	write(t, filepath.Join(root, "alfa.md"), "")
	e.search("alfa", "")
	e.search("alfa", "")
	if n := sp.count(root); n != 1 {
		t.Fatalf("%d scans, want 1", n)
	}

	// The stream dies and cannot be started again.
	e.src.mu.Lock()
	e.src.startErr = errors.New("cannot watch")
	e.src.mu.Unlock()
	e.src.die(t, root)
	waitFor(t, "the watcher to notice", func() bool { return !e.svc.watcher.Watching(p.ID) })
	write(t, filepath.Join(root, "svjez.md"), "")
	for i := range 2 {
		if got := hitRels(e.search("svjez", "")); !slices.Equal(got, []string{p.ID + ":svjez.md"}) {
			t.Fatalf("search %d without a watcher: %v", i, got)
		}
	}
	if n := sp.count(root); n != 3 {
		t.Errorf("%d scans, want one per search while nothing watches (3)", n)
	}

	// Watched again: scanned once more, then served from the cache.
	e.src.mu.Lock()
	e.src.startErr = nil
	e.src.mu.Unlock()
	e.search("svjez", "")
	e.search("svjez", "")
	if n := sp.count(root); n != 4 {
		t.Errorf("%d scans, want 4", n)
	}
	if !e.svc.watcher.Watching(p.ID) {
		t.Error("the project is not watched again")
	}
}

// A folder that went away and came back must not show what was in it before,
// even if no change was ever reported.
func TestSearchFolderReplaced(t *testing.T) {
	e := newEnv(t)
	p, root := e.addProject()
	write(t, filepath.Join(root, "stari.md"), "")
	if got := e.search("stari", ""); len(got) != 1 {
		t.Fatalf("hits = %v", hitRels(got))
	}
	away := root + ".away"
	if err := os.Rename(root, away); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(away) })
	if got := e.search("stari", ""); len(got) != 0 {
		t.Errorf("a folder that is gone is searched: %v", hitRels(got))
	}
	write(t, filepath.Join(root, "novi.md"), "")
	if got := e.search("stari", ""); len(got) != 0 {
		t.Errorf("the old tree is served: %v", hitRels(got))
	}
	if got := hitRels(e.search("novi", "")); !slices.Equal(got, []string{p.ID + ":novi.md"}) {
		t.Errorf("the new folder: %v", got)
	}
}

func TestSearchRemovedProject(t *testing.T) {
	e := newEnv(t)
	p, root := e.addProject()
	write(t, filepath.Join(root, "alfa.md"), "")
	e.search("alfa", "")
	if err := e.svc.RemoveProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.search("alfa", ""); len(got) != 0 {
		t.Errorf("a removed project is searched: %+v", got)
	}
	_, err := e.svc.SearchDocuments(context.Background(), "alfa", p.ID)
	wantCode(t, err, codeNotFound)
}
