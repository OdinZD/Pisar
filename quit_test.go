package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"Pisar/internal/nvim"
)

type fakeQuitEditor struct {
	mu          sync.Mutex
	isRunning   bool
	bufs        []nvim.Buffer
	modErr      error
	modBlocks   bool // modified waits for its context, like an editor that hangs
	saveErr     error
	modCalls    int
	saveCalls   int
	saveHadDL   bool
	modifiedHad bool
}

func (f *fakeQuitEditor) running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.isRunning
}

func (f *fakeQuitEditor) modified(ctx context.Context) ([]nvim.Buffer, error) {
	f.mu.Lock()
	f.modCalls++
	_, f.modifiedHad = ctx.Deadline()
	blocks, bufs, err := f.modBlocks, f.bufs, f.modErr
	f.mu.Unlock()
	if blocks {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return bufs, err
}

func (f *fakeQuitEditor) saveAll(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveCalls++
	_, f.saveHadDL = ctx.Deadline()
	return f.saveErr
}

func (f *fakeQuitEditor) counts() (mod, save int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.modCalls, f.saveCalls
}

func (f *fakeQuitEditor) setBufs(b []nvim.Buffer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bufs = b
}

type fakeQuitDialogs struct {
	mu          sync.Mutex
	choice      quitChoice
	anyway      bool
	unsaved     []string // messages
	unresponded int
	errors      []string

	// When set, AskUnsaved announces itself on entered and waits for release.
	entered chan struct{}
	release chan struct{}
}

func (d *fakeQuitDialogs) AskUnsaved(message string) quitChoice {
	d.mu.Lock()
	d.unsaved = append(d.unsaved, message)
	choice, entered, release := d.choice, d.entered, d.release
	d.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
		<-release
	}
	return choice
}

func (d *fakeQuitDialogs) AskUnresponsive() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.unresponded++
	return d.anyway
}

func (d *fakeQuitDialogs) ShowSaveError(message string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.errors = append(d.errors, message)
}

func (d *fakeQuitDialogs) dialogCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.unsaved) + d.unresponded + len(d.errors)
}

// quitProbe stands in for App.Quit: like Wails it asks the gate again.
type quitProbe struct {
	g       *quitGate
	mu      sync.Mutex
	calls   int
	allowed []bool
}

func (p *quitProbe) quit() {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	ok := p.g.request()
	p.mu.Lock()
	p.allowed = append(p.allowed, ok)
	p.mu.Unlock()
}

func (p *quitProbe) quits() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func newTestGate(t *testing.T, ed *fakeQuitEditor, dlg *fakeQuitDialogs) (*quitGate, *quitProbe) {
	t.Helper()
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	g := &quitGate{editor: ed, dialogs: dlg, text: text, timeout: time.Second, names: 5}
	p := &quitProbe{g: g}
	g.quit = p.quit
	return g, p
}

func doc(path string) nvim.Buffer { return nvim.Buffer{Path: path, Modified: true} }

func TestQuitEditorNotRunning(t *testing.T) {
	ed, dlg := &fakeQuitEditor{}, &fakeQuitDialogs{}
	g, p := newTestGate(t, ed, dlg)
	if !g.request() {
		t.Fatal("the request was refused although there is no editor")
	}
	g.wait()
	if mod, _ := ed.counts(); mod != 0 || dlg.dialogCount() != 0 || p.quits() != 0 {
		t.Errorf("editor asked %d times, %d dialogs, %d quits", mod, dlg.dialogCount(), p.quits())
	}
}

func TestQuitNoChanges(t *testing.T) {
	ed, dlg := &fakeQuitEditor{isRunning: true}, &fakeQuitDialogs{}
	g, p := newTestGate(t, ed, dlg)
	if g.request() {
		t.Fatal("the first request must be refused: the answer is not known yet")
	}
	g.wait()
	if p.quits() != 1 || len(p.allowed) != 1 || !p.allowed[0] {
		t.Errorf("quit calls = %d, the request it made was allowed = %v", p.quits(), p.allowed)
	}
	if !ed.modifiedHad {
		t.Error("modified got a context without a deadline")
	}
	if dlg.dialogCount() != 0 {
		t.Errorf("%d dialogs for a clean editor", dlg.dialogCount())
	}
}

func TestQuitSave(t *testing.T) {
	ed := &fakeQuitEditor{isRunning: true, bufs: []nvim.Buffer{doc("/p/a.md")}}
	dlg := &fakeQuitDialogs{choice: quitChoiceSave}
	g, p := newTestGate(t, ed, dlg)
	if g.request() {
		t.Fatal("must be refused")
	}
	g.wait()
	if _, save := ed.counts(); save != 1 || p.quits() != 1 || !p.allowed[0] {
		t.Errorf("saveAll %d, quits %d, allowed %v", save, p.quits(), p.allowed)
	}
	if !ed.saveHadDL {
		t.Error("saveAll got a context without a deadline")
	}
	if len(dlg.errors) != 0 {
		t.Errorf("error shown: %v", dlg.errors)
	}
}

func TestQuitSaveFails(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name    string
		err     error
		wantMsg string
	}{
		{"unnamed", &apiError{code: codeUnnamedBuffer, cause: fmt.Errorf("%w (1)", nvim.ErrUnnamedBuffer)}, text.ErrorUnnamedBuffer},
		{"unnamed, bare", nvim.ErrUnnamedBuffer, text.ErrorUnnamedBuffer},
		{"plain", errors.New("disk on fire"), "disk on fire"},
		{"service error", &apiError{code: codeInternal, cause: errors.New("nvim: save all: /p/a.md: E45: 'readonly' option is set")}, "E45: 'readonly' option is set"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ed := &fakeQuitEditor{isRunning: true, bufs: []nvim.Buffer{doc("/p/a.md")}, saveErr: c.err}
			dlg := &fakeQuitDialogs{choice: quitChoiceSave}
			g, p := newTestGate(t, ed, dlg)
			if g.request() {
				t.Fatal("must be refused")
			}
			g.wait()
			if _, save := ed.counts(); save != 1 {
				t.Errorf("saveAll calls = %d", save)
			}
			if p.quits() != 0 {
				t.Fatal("the application quit although saving failed")
			}
			if len(dlg.errors) != 1 || !strings.Contains(dlg.errors[0], c.wantMsg) || !strings.Contains(dlg.errors[0], strings.SplitN(text.QuitSaveFailed, "\n", 2)[0]) {
				t.Errorf("error messages = %q, want one containing %q", dlg.errors, c.wantMsg)
			}
			// The next request asks again.
			if g.request() {
				t.Fatal("a failed save must not let the next request through")
			}
			g.wait()
			if len(dlg.unsaved) != 2 {
				t.Errorf("asked %d times, want 2", len(dlg.unsaved))
			}
		})
	}
}

func TestQuitDiscard(t *testing.T) {
	ed := &fakeQuitEditor{isRunning: true, bufs: []nvim.Buffer{doc("/p/a.md")}}
	dlg := &fakeQuitDialogs{choice: quitChoiceDiscard}
	g, p := newTestGate(t, ed, dlg)
	if g.request() {
		t.Fatal("must be refused")
	}
	g.wait()
	if _, save := ed.counts(); save != 0 {
		t.Errorf("saveAll called %d times on Discard", save)
	}
	if p.quits() != 1 || !p.allowed[0] {
		t.Errorf("quits %d, allowed %v", p.quits(), p.allowed)
	}
}

func TestQuitCancel(t *testing.T) {
	ed := &fakeQuitEditor{isRunning: true, bufs: []nvim.Buffer{doc("/p/a.md")}}
	dlg := &fakeQuitDialogs{choice: quitChoiceCancel}
	g, p := newTestGate(t, ed, dlg)
	if g.request() {
		t.Fatal("must be refused")
	}
	g.wait()
	if _, save := ed.counts(); save != 0 || p.quits() != 0 {
		t.Errorf("saveAll %d, quits %d after Cancel", save, p.quits())
	}
	if g.request() {
		t.Fatal("the next request must ask again")
	}
	g.wait()
	if len(dlg.unsaved) != 2 {
		t.Errorf("asked %d times, want 2", len(dlg.unsaved))
	}
}

func TestQuitEditorDoesNotAnswer(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*fakeQuitEditor)
		anyway bool
		quit   int
	}{
		{"error, close anyway", func(e *fakeQuitEditor) { e.modErr = errors.New("rpc: broken pipe") }, true, 1},
		{"error, cancel", func(e *fakeQuitEditor) { e.modErr = errors.New("rpc: broken pipe") }, false, 0},
		{"timeout, close anyway", func(e *fakeQuitEditor) { e.modBlocks = true }, true, 1},
		{"timeout, cancel", func(e *fakeQuitEditor) { e.modBlocks = true }, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ed := &fakeQuitEditor{isRunning: true}
			c.setup(ed)
			dlg := &fakeQuitDialogs{anyway: c.anyway}
			g, p := newTestGate(t, ed, dlg)
			g.timeout = 20 * time.Millisecond
			if g.request() {
				t.Fatal("must be refused")
			}
			g.wait()
			if dlg.unresponded != 1 || len(dlg.unsaved) != 0 {
				t.Errorf("unresponsive dialogs %d, unsaved dialogs %d", dlg.unresponded, len(dlg.unsaved))
			}
			if p.quits() != c.quit {
				t.Errorf("quits = %d, want %d", p.quits(), c.quit)
			}
			if _, save := ed.counts(); save != 0 {
				t.Errorf("saveAll called %d times", save)
			}
		})
	}
}

func TestQuitSecondRequestWhileDialogOpen(t *testing.T) {
	ed := &fakeQuitEditor{isRunning: true, bufs: []nvim.Buffer{doc("/p/a.md")}}
	dlg := &fakeQuitDialogs{choice: quitChoiceCancel, entered: make(chan struct{}), release: make(chan struct{})}
	g, _ := newTestGate(t, ed, dlg)
	if g.request() {
		t.Fatal("must be refused")
	}
	<-dlg.entered // the dialog is open
	if g.request() {
		t.Fatal("a request while the dialog is open must be refused")
	}
	if g.request() {
		t.Fatal("and so must the next one")
	}
	close(dlg.release)
	g.wait()
	if n := dlg.dialogCount(); n != 1 {
		t.Errorf("%d dialogs, want 1", n)
	}
	if mod, _ := ed.counts(); mod != 1 {
		t.Errorf("the editor was asked %d times, want 1", mod)
	}
}

func TestQuitMessage(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	g := &quitGate{text: text, names: 5}
	list := func(lines ...string) string { return fill(text.QuitMessage, "names", strings.Join(lines, "\n")) }
	var seven []nvim.Buffer
	for i := 1; i <= 7; i++ {
		seven = append(seven, doc(fmt.Sprintf("/p/d%d.md", i)))
	}
	for _, c := range []struct {
		name string
		bufs []nvim.Buffer
		want string
	}{
		{"one", []nvim.Buffer{doc("/home/x/projekt/a.md")}, list("a.md")},
		{"unnamed", []nvim.Buffer{doc("")}, list(text.QuitUnnamed)},
		{"named and unnamed", []nvim.Buffer{doc("/p/a.md"), doc("")}, list("a.md", text.QuitUnnamed)},
		{"five", seven[:5], list("d1.md", "d2.md", "d3.md", "d4.md", "d5.md")},
		{"six", seven[:6], list("d1.md", "d2.md", "d3.md", "d4.md", "d5.md", fill(text.QuitMore, "count", "1"))},
		{"seven", seven, list("d1.md", "d2.md", "d3.md", "d4.md", "d5.md", fill(text.QuitMore, "count", "2"))},
		{"diacritics", []nvim.Buffer{doc("/p/čćšžđ.md"), doc("/p/Đurđevak ŠČĆŽ.txt")}, list("čćšžđ.md", "Đurđevak ŠČĆŽ.txt")},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := g.unsavedMessage(c.bufs); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	if got := g.unsavedMessage(seven); !strings.Contains(got, "…i još 2") {
		t.Errorf("seven documents: %q", got)
	}
}

// The message the dialog gets is the one built from the editor's buffers.
func TestQuitDialogGetsTheList(t *testing.T) {
	ed := &fakeQuitEditor{isRunning: true, bufs: []nvim.Buffer{doc("/p/čšž.md"), doc("")}}
	dlg := &fakeQuitDialogs{choice: quitChoiceCancel}
	g, _ := newTestGate(t, ed, dlg)
	g.request()
	g.wait()
	if len(dlg.unsaved) != 1 || !strings.Contains(dlg.unsaved[0], "čšž.md") || !strings.Contains(dlg.unsaved[0], g.text.QuitUnnamed) {
		t.Errorf("dialog message = %q", dlg.unsaved)
	}
}

func TestQuitDecidedStaysDecided(t *testing.T) {
	ed := &fakeQuitEditor{isRunning: true, bufs: []nvim.Buffer{doc("/p/a.md")}}
	dlg := &fakeQuitDialogs{choice: quitChoiceDiscard}
	g, p := newTestGate(t, ed, dlg)
	g.request()
	g.wait()
	if p.quits() != 1 {
		t.Fatalf("quits = %d", p.quits())
	}
	// Neovim got new changes in the meantime (or the quit is retried): no second question.
	ed.setBufs([]nvim.Buffer{doc("/p/b.md"), doc("")})
	for range 2 {
		if !g.request() {
			t.Error("a request after the decision was refused")
		}
	}
	g.wait()
	if dlg.dialogCount() != 1 {
		t.Errorf("%d dialogs, want 1", dlg.dialogCount())
	}
}

// Many requests at once (Cmd+Q and the close button together): one question,
// one quit. Run with -race.
func TestQuitConcurrentRequests(t *testing.T) {
	ed := &fakeQuitEditor{isRunning: true}
	dlg := &fakeQuitDialogs{}
	g, p := newTestGate(t, ed, dlg)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 50 {
		wg.Go(func() {
			<-start
			g.request()
		})
	}
	close(start)
	wg.Wait()
	g.wait()
	// Approval is recorded before the question is marked as finished, so no
	// second question can start after the first one has ended.
	if mod, _ := ed.counts(); mod != 1 {
		t.Errorf("editor asked %d times, want 1", mod)
	}
	if p.quits() != 1 {
		t.Errorf("quits = %d, want 1", p.quits())
	}
	if dlg.dialogCount() != 0 {
		t.Errorf("%d dialogs", dlg.dialogCount())
	}
	if !g.request() {
		t.Error("the request after the decision was refused")
	}
}

func TestQuitConcurrentWithDialog(t *testing.T) {
	ed := &fakeQuitEditor{isRunning: true, bufs: []nvim.Buffer{doc("/p/a.md")}}
	dlg := &fakeQuitDialogs{choice: quitChoiceCancel, entered: make(chan struct{}, 100), release: make(chan struct{})}
	g, _ := newTestGate(t, ed, dlg)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { g.request() })
	}
	wg.Wait()
	<-dlg.entered
	close(dlg.release)
	g.wait()
	if n := dlg.dialogCount(); n < 1 {
		t.Errorf("%d dialogs", n)
	}
}
