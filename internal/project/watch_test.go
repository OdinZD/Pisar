package project

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testDebounce = 25 * time.Millisecond
	testMaxDelay = 150 * time.Millisecond
	testWait     = 5 * time.Second // deadline for events that MUST arrive
	testQuiet    = 6 * testDebounce
)

// fakeStream is one watched root of a fakeSource. The test drives ch.
type fakeStream struct {
	ctx  context.Context
	ch   chan []Change
	once sync.Once
}

func (s *fakeStream) send(c ...Change) { s.ch <- c }

// kill closes the channel as a dying stream would.
func (s *fakeStream) kill() { s.once.Do(func() { close(s.ch) }) }

// closed reports whether the channel is closed (and drained).
func (s *fakeStream) closed() bool {
	select {
	case _, ok := <-s.ch:
		return !ok
	default:
		return false
	}
}

type fakeSource struct {
	mu       sync.Mutex
	streams  map[string]*fakeStream
	startErr error
	wg       sync.WaitGroup
}

func (f *fakeSource) Start(ctx context.Context, root string) (<-chan []Change, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	s := &fakeStream{ctx: ctx, ch: make(chan []Change, 1024)}
	f.mu.Lock()
	if f.streams == nil {
		f.streams = map[string]*fakeStream{}
	}
	f.streams[root] = s
	f.mu.Unlock()
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		<-ctx.Done()
		time.Sleep(10 * time.Millisecond) // a source needs a moment to stop; Remove/Close must wait for it
		s.kill()
	}()
	return s.ch, nil
}

func (f *fakeSource) stream(t *testing.T, root string) *fakeStream {
	t.Helper()
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.streams[realRoot]
	if s == nil {
		t.Fatalf("no stream for %s", realRoot)
	}
	return s
}

type recorder struct {
	calls   chan string
	total   atomic.Int64
	active  atomic.Int32
	maxSeen atomic.Int32
	sleep   time.Duration
}

func newRecorder() *recorder { return &recorder{calls: make(chan string, 4096)} }

func (r *recorder) onChange(id string) {
	n := r.active.Add(1)
	for {
		m := r.maxSeen.Load()
		if n <= m || r.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	r.total.Add(1)
	r.calls <- id
	if r.sleep > 0 {
		time.Sleep(r.sleep)
	}
	r.active.Add(-1)
}

func (r *recorder) expect(t *testing.T, id string) {
	t.Helper()
	select {
	case got := <-r.calls:
		if got != id {
			t.Fatalf("OnChange(%q), want %q", got, id)
		}
	case <-time.After(testWait):
		t.Fatalf("no OnChange(%q) within %v", id, testWait)
	}
}

func (r *recorder) expectNone(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case got := <-r.calls:
		t.Fatalf("unexpected OnChange(%q)", got)
	case <-time.After(d):
	}
}

func newTestWatcher(t *testing.T, src Source, r *recorder) *Watcher {
	t.Helper()
	w, err := NewWatcher(WatchOptions{Source: src, Debounce: testDebounce, MaxDelay: testMaxDelay, OnChange: r.onChange})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w
}

func addRoot(t *testing.T, w *Watcher, src *fakeSource, id string) (root string, s *fakeStream) {
	t.Helper()
	// Sources report real paths (/private/var/... on macOS), so tests use them too.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Add(id, root); err != nil {
		t.Fatal(err)
	}
	return root, src.stream(t, root)
}

func TestWatcherSingleChange(t *testing.T) {
	src, r := &fakeSource{}, newRecorder()
	w := newTestWatcher(t, src, r)
	root, s := addRoot(t, w, src, "A")
	s.send(Change{Path: filepath.Join(root, "a.md")})
	r.expect(t, "A")
	r.expectNone(t, testQuiet)
}

func TestWatcherBurstIsOneCall(t *testing.T) {
	src, r := &fakeSource{}, newRecorder()
	w := newTestWatcher(t, src, r)
	root, s := addRoot(t, w, src, "A")
	for range 50 {
		s.send(Change{Path: filepath.Join(root, "a.md")})
	}
	r.expect(t, "A")
	r.expectNone(t, testQuiet)
}

func TestWatcherRelevance(t *testing.T) {
	cases := []struct {
		name   string
		change func(root, outside string) Change
		want   bool
	}{
		{"git index", func(r, _ string) Change { return Change{Path: r + "/.git/index"} }, false},
		{"DS_Store", func(r, _ string) Change { return Change{Path: r + "/.DS_Store"} }, false},
		{"hidden dir", func(r, _ string) Change { return Change{Path: r + "/mapa/.skriveno/a.md"} }, false},
		{"hidden dir itself", func(r, _ string) Change { return Change{Path: r + "/mapa/.skriveno", Dir: true} }, false},
		{"lock file", func(r, _ string) Change { return Change{Path: r + "/~$Ugovor.docx"} }, false},
		{"unsupported", func(r, _ string) Change { return Change{Path: r + "/slika.png"} }, false},
		{"outside", func(_, o string) Change { return Change{Path: o + "/a.md"} }, false},
		{"sibling with same prefix", func(r, _ string) Change { return Change{Path: r + "x/a.md"} }, false},
		{"relative path", func(_, _ string) Change { return Change{Path: "a.md"} }, false},
		{"md", func(r, _ string) Change { return Change{Path: r + "/a.md"} }, true},
		{"upper-case docx", func(r, _ string) Change { return Change{Path: r + "/mapa/b.DOCX"} }, true},
		{"new dir", func(r, _ string) Change { return Change{Path: r + "/mapa/nova", Dir: true} }, true},
		{"root itself", func(r, _ string) Change { return Change{Path: r, Dir: true} }, false},
		{"rescan any path", func(r, _ string) Change { return Change{Path: r + "/slika.png", Rescan: true} }, true},
		{"rescan hidden", func(r, _ string) Change { return Change{Path: r + "/.git", Rescan: true} }, true},
		{"rescan outside", func(_, o string) Change { return Change{Path: o, Rescan: true} }, true},
	}
	for _, viaLink := range []bool{false, true} {
		for _, tc := range cases {
			name := tc.name
			if viaLink {
				name += "/symlinked root"
			}
			t.Run(name, func(t *testing.T) {
				src, r := &fakeSource{}, newRecorder()
				w := newTestWatcher(t, src, r)
				// Sources report real paths; the root may be given through a link.
				realRoot, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				given := realRoot
				if viaLink {
					given = filepath.Join(t.TempDir(), "link")
					if err := os.Symlink(realRoot, given); err != nil {
						t.Fatal(err)
					}
				}
				if err := w.Add("A", given); err != nil {
					t.Fatal(err)
				}
				outside, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				src.stream(t, realRoot).send(tc.change(realRoot, outside))
				if tc.want {
					r.expect(t, "A")
				} else {
					r.expectNone(t, testQuiet)
				}
			})
		}
	}
}

func TestWatcherMaxDelay(t *testing.T) {
	src, r := &fakeSource{}, newRecorder()
	w := newTestWatcher(t, src, r)
	root, s := addRoot(t, w, src, "A")

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // a change every half debounce keeps the quiet period from ever elapsing
		defer wg.Done()
		tick := time.NewTicker(testDebounce / 2)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				s.send(Change{Path: filepath.Join(root, "a.md")})
			}
		}
	}()
	defer func() { close(stop); wg.Wait() }()

	r.expect(t, "A") // arrives while changes are still coming
}

func TestWatcherProjectsAreIndependent(t *testing.T) {
	src, r := &fakeSource{}, newRecorder()
	w := newTestWatcher(t, src, r)
	rootA, sA := addRoot(t, w, src, "A")
	_, _ = addRoot(t, w, src, "B")
	sA.send(Change{Path: filepath.Join(rootA, "a.md")})
	r.expect(t, "A")
	r.expectNone(t, testQuiet)
}

func TestWatcherCallbacksNeverOverlap(t *testing.T) {
	src, r := &fakeSource{}, newRecorder()
	r.sleep = 100 * time.Millisecond
	w := newTestWatcher(t, src, r)
	rootA, sA := addRoot(t, w, src, "A")
	rootB, sB := addRoot(t, w, src, "B")

	sA.send(Change{Path: filepath.Join(rootA, "a.md")})
	sB.send(Change{Path: filepath.Join(rootB, "b.md")})
	// Both initial calls have started once two ids arrived; the second one is
	// still sleeping inside OnChange. (Sending earlier could merge the new
	// change into a debounce window that has not fired yet.)
	counts := map[string]int{}
	for range 2 {
		select {
		case id := <-r.calls:
			counts[id]++
		case <-time.After(testWait):
			t.Fatalf("calls so far: %v", counts)
		}
	}
	// New changes of both projects arrive while a call is running.
	sA.send(Change{Path: filepath.Join(rootA, "a2.md")})
	sB.send(Change{Path: filepath.Join(rootB, "b2.md")})

	// One more call per project for the changes made during a call.
	deadline := time.After(testWait)
	for counts["A"]+counts["B"] < 4 {
		select {
		case id := <-r.calls:
			counts[id]++
		case <-deadline:
			t.Fatalf("calls so far: %v", counts)
		}
	}
	r.expectNone(t, testQuiet)
	if counts["A"] != 2 || counts["B"] != 2 {
		t.Fatalf("calls = %v, want 2 each (changes during a call must not be lost)", counts)
	}
	if m := r.maxSeen.Load(); m > 1 {
		t.Fatalf("OnChange ran %d at once", m)
	}
}

func TestWatcherRemove(t *testing.T) {
	src, r := &fakeSource{}, newRecorder()
	w := newTestWatcher(t, src, r)
	root, s := addRoot(t, w, src, "A")

	s.send(Change{Path: filepath.Join(root, "a.md")})
	time.Sleep(testDebounce / 5) // pending, not yet delivered
	w.Remove("A")
	if s.ctx.Err() == nil {
		t.Fatal("source context was not cancelled")
	}
	if !s.closed() {
		t.Fatal("Remove returned before the source channel was closed")
	}
	if w.Watching("A") {
		t.Fatal("still watching")
	}
	r.expectNone(t, testQuiet)
	w.Remove("A")       // already removed
	w.Remove("unknown") // never existed

	// The id can be used again.
	if err := w.Add("A", root); err != nil {
		t.Fatal(err)
	}
	if !w.Watching("A") {
		t.Fatal("not watching after re-Add")
	}
}

func TestWatcherClose(t *testing.T) {
	src, r := &fakeSource{}, newRecorder()
	w := newTestWatcher(t, src, r)
	rootA, sA := addRoot(t, w, src, "A")
	_, sB := addRoot(t, w, src, "B")

	sA.send(Change{Path: filepath.Join(rootA, "a.md")})
	time.Sleep(testDebounce / 5) // pending, not yet delivered
	w.Close()
	for _, s := range []*fakeStream{sA, sB} {
		if s.ctx.Err() == nil {
			t.Fatal("source context not cancelled")
		}
		if !s.closed() {
			t.Fatal("Close returned before the source channel was closed")
		}
	}
	time.Sleep(testQuiet)
	if n := r.total.Load(); n != 0 {
		t.Fatalf("OnChange called after Close returned: %d calls", n)
	}
	w.Close() // idempotent
	if err := w.Add("C", t.TempDir()); !errors.Is(err, ErrWatcherClosed) {
		t.Fatalf("Add after Close: %v", err)
	}
	src.wg.Wait()
}

func TestWatcherAddErrors(t *testing.T) {
	src, r := &fakeSource{}, newRecorder()
	w := newTestWatcher(t, src, r)
	root := t.TempDir()
	if err := w.Add("A", root); err != nil {
		t.Fatal(err)
	}
	if err := w.Add("A", t.TempDir()); err == nil {
		t.Fatal("duplicate id accepted")
	}
	if err := w.Add("B", filepath.Join(root, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing root: %v", err)
	}
	file := filepath.Join(root, "f.md")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.Add("C", file); err == nil {
		t.Fatal("file accepted as root")
	}
	if w.Watching("B") || w.Watching("C") {
		t.Fatal("failed Add registered a project")
	}

	boom := errors.New("boom")
	w2 := newTestWatcher(t, &fakeSource{startErr: boom}, r)
	if err := w2.Add("D", root); !errors.Is(err, boom) {
		t.Fatalf("Start error: %v", err)
	}
	if w2.Watching("D") {
		t.Fatal("registered despite Start error")
	}
}

func TestWatcherSourceDies(t *testing.T) {
	src, r := &fakeSource{}, newRecorder()
	var watchingInCallback atomic.Bool
	var w *Watcher
	w, err := NewWatcher(WatchOptions{Source: src, Debounce: testDebounce, MaxDelay: testMaxDelay, OnChange: func(id string) {
		watchingInCallback.Store(w.Watching(id))
		r.onChange(id)
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	root, s := addRoot(t, w, src, "A")

	s.kill()
	r.expect(t, "A")
	r.expectNone(t, testQuiet)
	if w.Watching("A") || watchingInCallback.Load() {
		t.Fatal("still watching after the source died")
	}

	// A dead project can be added again; Close then finishes cleanly.
	if err := w.Add("A", root); err != nil {
		t.Fatalf("re-Add after death: %v", err)
	}
	w.Close()
}

func TestNewWatcherValidation(t *testing.T) {
	ok := WatchOptions{Source: &fakeSource{}, Debounce: time.Millisecond, MaxDelay: time.Millisecond, OnChange: func(string) {}}
	if _, err := NewWatcher(ok); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
	cases := map[string]func(*WatchOptions){
		"no source":          func(o *WatchOptions) { o.Source = nil },
		"no callback":        func(o *WatchOptions) { o.OnChange = nil },
		"zero debounce":      func(o *WatchOptions) { o.Debounce = 0 },
		"negative debounce":  func(o *WatchOptions) { o.Debounce = -1 },
		"max below debounce": func(o *WatchOptions) { o.MaxDelay = o.Debounce - 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			o := ok
			mutate(&o)
			if _, err := NewWatcher(o); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
