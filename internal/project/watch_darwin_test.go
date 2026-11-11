package project

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const fsWait = 10 * time.Second // real FSEvents: generous deadline for events that MUST arrive

func newFSWatcher(t *testing.T) (*Watcher, *recorder, string) {
	t.Helper()
	r := newRecorder()
	w, err := NewWatcher(WatchOptions{Source: FSEventsSource{}, Debounce: 50 * time.Millisecond, MaxDelay: time.Second, OnChange: r.onChange})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	root := t.TempDir()
	if err := w.Add("P", root); err != nil {
		t.Fatal(err)
	}
	return w, r, root
}

func (r *recorder) expectFS(t *testing.T, what string) {
	t.Helper()
	select {
	case <-r.calls:
	case <-time.After(fsWait):
		t.Fatalf("no OnChange after %s", what)
	}
}

// settle waits until the burst caused by the previous step has been delivered,
// so that the next step is judged on its own.
func (r *recorder) settle() {
	for {
		select {
		case <-r.calls:
		case <-time.After(700 * time.Millisecond):
			return
		}
	}
}

func TestFSEventsCreateFile(t *testing.T) {
	_, r, root := newFSWatcher(t)
	if err := os.WriteFile(filepath.Join(root, "novo.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.expectFS(t, "creating novo.md")
}

func TestFSEventsSubfolderThenDocument(t *testing.T) {
	_, r, root := newFSWatcher(t)
	sub := filepath.Join(root, "mapa")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	r.expectFS(t, "creating a folder")
	r.settle()
	if err := os.WriteFile(filepath.Join(sub, "b.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.expectFS(t, "creating a document in the new folder")
}

func TestFSEventsRenameAndRemove(t *testing.T) {
	_, r, root := newFSWatcher(t)
	a := filepath.Join(root, "a.md")
	if err := os.WriteFile(a, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.expectFS(t, "creating a.md")
	r.settle()
	b := filepath.Join(root, "b.md")
	if err := os.Rename(a, b); err != nil {
		t.Fatal(err)
	}
	r.expectFS(t, "renaming")
	r.settle()
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	r.expectFS(t, "removing")
}

func TestFSEventsIgnoresInvisible(t *testing.T) {
	_, r, root := newFSWatcher(t)
	for _, name := range []string{".skriveno", "slika.png", "~$Ugovor.docx"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.expectNone(t, 1500*time.Millisecond)
}

func TestFSEventsCloseLeavesNoGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	r := newRecorder()
	w, err := NewWatcher(WatchOptions{Source: FSEventsSource{}, Debounce: 20 * time.Millisecond, MaxDelay: time.Second, OnChange: r.onChange})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"A", "B"} {
		if err := w.Add(id, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()

	// Close waits for the project goroutines and the source channels, but the
	// goroutine that stops the stream may still be exiting: allow it a moment.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines: %d before, %d after Close", before, after)
	}
}
