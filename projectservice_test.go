package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"Pisar/internal/config"
	"Pisar/internal/docsafe"
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

// fakeSource is a project.Source driven by the test.
type fakeSource struct {
	mu     sync.Mutex
	chans  map[string]chan []project.Change
	closed map[string]bool
}

func (f *fakeSource) Start(ctx context.Context, root string) (<-chan []project.Change, error) {
	ch := make(chan []project.Change, 16)
	f.mu.Lock()
	if f.chans == nil {
		f.chans = map[string]chan []project.Change{}
		f.closed = map[string]bool{}
	}
	f.chans[root] = ch
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.mu.Lock()
		f.closed[root] = true
		close(ch)
		f.mu.Unlock()
	}()
	return ch, nil
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
	if f.closed[real] {
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
	e := &env{t: t, home: home, store: store, dlg: &fakeDialogs{}, src: &fakeSource{}}
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
		Store: store, Docs: docs, Dialogs: e.dlg, Source: e.src, Emit: e.emit, Limits: limits,
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
