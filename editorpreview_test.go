package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"Pisar/internal/nvim"
	"Pisar/internal/preview"
)

// The tests of the preview loop are in two groups. The first drives a
// previewLoop with a fake Neovim text, a fake renderer and a clock the test
// moves by hand, so no test waits for real time to pass. The second runs the
// whole editor service with a real Neovim (and, for one test, the real pandoc).
// Test names are short on purpose, see the note at the top of editorservice_test.go.

// ---------- fakes ----------

type fakeTimer struct {
	d       time.Duration
	ch      chan time.Time
	stopped atomic.Bool
}

func (t *fakeTimer) fire() { t.ch <- time.Time{} }

// fakeClock hands the timers the loop asks for to the test, in order.
type fakeClock struct {
	armed chan *fakeTimer
	mu    sync.Mutex
	made  int
}

func newFakeClock() *fakeClock { return &fakeClock{armed: make(chan *fakeTimer, 256)} }

func (c *fakeClock) newTimer(d time.Duration) (<-chan time.Time, func()) {
	t := &fakeTimer{d: d, ch: make(chan time.Time, 1)}
	c.mu.Lock()
	c.made++
	c.mu.Unlock()
	c.armed <- t
	return t.ch, func() { t.stopped.Store(true) }
}

// next waits for the loop to start its next timer.
func (c *fakeClock) next(tb testing.TB) *fakeTimer {
	tb.Helper()
	select {
	case t := <-c.armed:
		return t
	case <-time.After(edWait):
		tb.Fatal("the loop did not start a timer")
		return nil
	}
}

func (c *fakeClock) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.made
}

// fakeText is the Neovim the loop reads the text from.
type fakeText struct {
	mu    sync.Mutex
	text  string
	buf   nvim.Buffer
	err   error
	reads int
	max   int
}

func (f *fakeText) set(text, path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.text, f.buf, f.err = text, nvim.Buffer{Path: path}, nil
}

func (f *fakeText) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeText) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

func (f *fakeText) BufferText(_ context.Context, maxBytes int) ([]byte, nvim.Buffer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	f.max = maxBytes
	if f.err != nil {
		return nil, nvim.Buffer{}, f.err
	}
	return []byte(f.text), f.buf, nil
}

type renderCall struct {
	n    int
	ctx  context.Context
	text string
	dir  string
}

// fakeRender records every conversion; fn, when set, decides what it does.
type fakeRender struct {
	mu      sync.Mutex
	calls   []renderCall
	fn      func(c renderCall) ([]byte, error)
	started chan renderCall
}

func newFakeRender() *fakeRender { return &fakeRender{started: make(chan renderCall, 64)} }

func (r *fakeRender) Render(ctx context.Context, markdown []byte, dir string) ([]byte, error) {
	r.mu.Lock()
	c := renderCall{n: len(r.calls), ctx: ctx, text: string(markdown), dir: dir}
	r.calls = append(r.calls, c)
	fn := r.fn
	r.mu.Unlock()
	select {
	case r.started <- c:
	default:
	}
	if fn != nil {
		return fn(c)
	}
	return []byte("<p>" + c.text + "</p>"), nil
}

func (r *fakeRender) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *fakeRender) call(n int) renderCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[n]
}

// waitStarted waits for a conversion to begin.
func (r *fakeRender) waitStarted(tb testing.TB) renderCall {
	tb.Helper()
	select {
	case c := <-r.started:
		return c
	case <-time.After(edWait):
		tb.Fatal("no conversion started")
		return renderCall{}
	}
}

// blockUntilCanceled is a conversion that ends only when its context does,
// like pandoc killed by a cancellation.
func blockUntilCanceled(c renderCall) ([]byte, error) {
	<-c.ctx.Done()
	return nil, fmt.Errorf("preview: pandoc: %w", c.ctx.Err())
}

// ---------- the loop on its own ----------

const pvDebounce = 300 * time.Millisecond

type pvEnv struct {
	t    *testing.T
	loop *previewLoop
	src  *fakeText
	rend *fakeRender
	clk  *fakeClock

	mu    sync.Mutex
	out   []PreviewContent
	outCh chan PreviewContent
}

// pvTarget is a Target without a service: .md renders, with the folder of the
// file; anything else is "unsupported".
func pvTarget(b nvim.Buffer) previewTarget {
	if filepath.Ext(b.Path) == ".md" {
		return previewTarget{path: b.Path, dir: filepath.Dir(b.Path), render: true}
	}
	return previewTarget{path: b.Path}
}

func newPv(t *testing.T) *pvEnv {
	t.Helper()
	e := &pvEnv{t: t, src: &fakeText{}, rend: newFakeRender(), clk: newFakeClock(), outCh: make(chan PreviewContent, 64)}
	e.src.set("", "")
	e.loop = newPreviewLoop(previewLoopDeps{
		Source:   e.src,
		Renderer: e.rend,
		Emit: func(c PreviewContent) {
			e.mu.Lock()
			e.out = append(e.out, c)
			e.mu.Unlock()
			e.outCh <- c
		},
		Target:      pvTarget,
		Debounce:    pvDebounce,
		ReadTimeout: time.Minute,
		MaxSource:   1234,
		NewTimer:    e.clk.newTimer,
	})
	t.Cleanup(e.loop.stop)
	return e
}

// fire lets the quiet time of the next timer pass.
func (e *pvEnv) fire() *fakeTimer {
	e.t.Helper()
	t := e.clk.next(e.t)
	t.fire()
	return t
}

// want waits for the next event the loop sends.
func (e *pvEnv) want() PreviewContent {
	e.t.Helper()
	select {
	case c := <-e.outCh:
		return c
	case <-time.After(edWait):
		e.t.Fatal("the loop sent nothing")
		return PreviewContent{}
	}
}

func (e *pvEnv) sent() []PreviewContent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]PreviewContent(nil), e.out...)
}

func TestPvCoalesce(t *testing.T) {
	e := newPv(t)
	e.src.set("a", "/d/a.md")
	e.loop.enable(true)
	timers := []*fakeTimer{e.clk.next(t)}
	for _, text := range []string{"ab", "abc"} {
		e.src.set(text, "/d/a.md")
		e.loop.changed()
		// Each change is waited for, so none is folded into the one before by
		// the loop being slow: what is under test is the restart of the wait.
		timers = append(timers, e.clk.next(t))
	}
	for i, tm := range timers {
		if tm.d != pvDebounce {
			t.Errorf("timer %d waits %v, want the debounce %v", i, tm.d, pvDebounce)
		}
		if i < len(timers)-1 && !tm.stopped.Load() {
			t.Errorf("timer %d was not released when the next change came", i)
		}
	}
	if e.rend.count() != 0 {
		t.Fatalf("a conversion ran inside the quiet time")
	}
	timers[len(timers)-1].fire()
	got := e.want()
	want := PreviewContent{Path: "/d/a.md", State: "html", HTML: "<p>abc</p>"}
	if got != want {
		t.Errorf("sent %+v, want %+v", got, want)
	}
	if e.rend.count() != 1 || e.rend.call(0).text != "abc" || e.rend.call(0).dir != "/d" {
		t.Errorf("conversions: %+v", e.rend.calls)
	}
	if e.src.max != 1234 {
		t.Errorf("the text was asked for with limit %d, want Limits.MaxPreviewSource", e.src.max)
	}
}

func TestPvOff(t *testing.T) {
	e := newPv(t)
	e.src.set("a", "/d/a.md")
	e.loop.changed() // the preview is off: nothing may start
	e.loop.enable(true)
	e.fire()
	e.want()
	// The change before enable armed no timer of its own.
	if e.clk.count() != 1 || e.rend.count() != 1 {
		t.Errorf("timers %d, conversions %d, want 1 and 1", e.clk.count(), e.rend.count())
	}
	e.loop.enable(false)
	e.loop.changed()
	e.src.set("b", "/d/a.md")
	// The loop stays idle: a probe after switching on again is the first work it does.
	e.loop.enable(true)
	e.fire()
	if got := e.want(); got.HTML != "<p>b</p>" {
		t.Errorf("sent %+v", got)
	}
	if e.clk.count() != 2 || e.rend.count() != 2 {
		t.Errorf("timers %d, conversions %d, want 2 and 2", e.clk.count(), e.rend.count())
	}
}

// Every call of enable(true) gives a refresh that is sent, also when nothing
// changed: a page that was loaded again asks again and has an empty pane.
func TestPvEnableRepeats(t *testing.T) {
	e := newPv(t)
	e.src.set("same", "/d/a.md")
	for i := range 3 {
		e.loop.enable(true)
		e.fire()
		if got := e.want(); got.HTML != "<p>same</p>" {
			t.Fatalf("round %d: sent %+v", i, got)
		}
	}
}

func TestPvSameTextOnce(t *testing.T) {
	e := newPv(t)
	e.src.set("same", "/d/a.md")
	e.loop.enable(true)
	e.fire()
	e.want()

	// A second notification for the same text (checktime after a write).
	e.loop.changed()
	e.fire()
	waitCond(t, "the text to be read again", func() bool { return e.src.readCount() >= 2 })
	e.loop.changed()
	e.src.set("other", "/d/a.md")
	e.fire()
	if got := e.want(); got.HTML != "<p>other</p>" {
		t.Errorf("sent %+v", got)
	}
	if n := len(e.sent()); n != 2 {
		t.Errorf("%d events, want 2: the unchanged text must not be sent again", n)
	}
	if n := e.rend.count(); n != 2 {
		t.Errorf("%d conversions, want 2: the unchanged text must not be converted again", n)
	}
	// The same text under another file is not the same content.
	e.src.set("other", "/d/b.md")
	e.loop.changed()
	e.fire()
	if got := e.want(); got.Path != "/d/b.md" {
		t.Errorf("sent %+v", got)
	}
}

// A change during a conversion cancels it, and what it produces is not sent,
// whether or not it noticed the cancellation.
func TestPvSupersede(t *testing.T) {
	for _, c := range []struct {
		name string
		obey bool
	}{{"obeys cancel", true}, {"ignores cancel", false}} {
		t.Run(c.name, func(t *testing.T) {
			e := newPv(t)
			release := make(chan struct{})
			e.rend.fn = func(rc renderCall) ([]byte, error) {
				if rc.n > 0 {
					return []byte("<p>" + rc.text + "</p>"), nil
				}
				if c.obey {
					return blockUntilCanceled(rc)
				}
				<-release
				return []byte("<p>" + rc.text + "</p>"), nil
			}
			e.src.set("one", "/d/a.md")
			e.loop.enable(true)
			e.fire()
			first := e.rend.waitStarted(t)

			e.src.set("two", "/d/a.md")
			e.loop.changed()
			select {
			case <-first.ctx.Done():
			case <-time.After(edWait):
				t.Fatal("the conversion in progress was not canceled")
			}
			close(release)
			e.fire() // the quiet time after the second change

			got := e.want()
			if got.State != "html" || got.HTML != "<p>two</p>" {
				t.Errorf("sent %+v, want the result of the second text", got)
			}
			if n := len(e.sent()); n != 1 {
				t.Errorf("%d events, want 1: %+v", n, e.sent())
			}
			if e.rend.count() != 2 {
				t.Errorf("%d conversions, want 2", e.rend.count())
			}
		})
	}
}

func TestPvOffDuringConversion(t *testing.T) {
	e := newPv(t)
	e.rend.fn = func(rc renderCall) ([]byte, error) {
		if rc.n == 0 {
			return blockUntilCanceled(rc)
		}
		return []byte("<p>" + rc.text + "</p>"), nil
	}
	e.src.set("one", "/d/a.md")
	e.loop.enable(true)
	e.fire()
	first := e.rend.waitStarted(t)
	e.loop.enable(false)
	select {
	case <-first.ctx.Done():
	case <-time.After(edWait):
		t.Fatal("switching off did not cancel the conversion")
	}
	// The probe: the loop is single-threaded, so when the second conversion
	// starts the first round is over, and what it did not send, it never will.
	e.src.set("two", "/d/a.md")
	e.loop.enable(true)
	e.fire()
	e.rend.waitStarted(t)
	if got := e.want(); got.HTML != "<p>two</p>" {
		t.Errorf("sent %+v", got)
	}
	// Events are kept in order: the first conversion's result would be before this.
	if got := e.sent(); len(got) != 1 {
		t.Errorf("sent %+v, want only the result of the second conversion", got)
	}
}

func TestPvKinds(t *testing.T) {
	e := newPv(t)
	for _, c := range []struct {
		name string
		path string
		want PreviewContent
	}{
		{"no file", "", PreviewContent{State: "unsupported"}},
		{"txt", "/d/a.txt", PreviewContent{Path: "/d/a.txt", State: "unsupported"}},
		{"unknown extension", "/d/a.xyz", PreviewContent{Path: "/d/a.xyz", State: "unsupported"}},
		{"md", "/d/a.md", PreviewContent{Path: "/d/a.md", State: "html", HTML: "<p>text</p>"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e.src.set("text", c.path)
			e.loop.enable(true)
			e.fire()
			if got := e.want(); got != c.want {
				t.Errorf("sent %+v, want %+v", got, c.want)
			}
		})
	}
	// Only the Markdown one was converted.
	if n := e.rend.count(); n != 1 {
		t.Errorf("%d conversions, want 1", n)
	}
}

func TestPvErrors(t *testing.T) {
	cases := []struct {
		name       string
		bufErr     error // BufferText fails
		rendErr    error // Render fails
		code       string
		remembered bool // the same text is not tried again
	}{
		{"buffer too large", nvim.ErrBufferTooLarge, nil, codePreviewTooLarge, true},
		{"html too large", nil, fmt.Errorf("%w: the page is larger", preview.ErrTooLarge), codePreviewTooLarge, true},
		{"source too large", nil, fmt.Errorf("%w: text is 9 bytes", preview.ErrTooLarge), codePreviewTooLarge, true},
		{"no pandoc", nil, fmt.Errorf("%w: %w", preview.ErrToolMissing, exec.ErrNotFound), codePandocMissing, false},
		{"tool failed", nil, &preview.ToolError{Tool: "pandoc", Output: "secret text of the document", Err: errors.New("exit status 1")}, codeConvertFailed, false},
		{"timeout", nil, fmt.Errorf("preview: pandoc: %w", context.DeadlineExceeded), codeCanceled, false},
		{"unknown", nil, errors.New("boom"), codeInternal, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newPv(t)
			e.src.set("text", "/d/a.md")
			if c.bufErr != nil {
				e.src.fail(c.bufErr)
			}
			e.rend.fn = func(renderCall) ([]byte, error) { return nil, c.rendErr }
			e.loop.enable(true)
			e.fire()
			got := e.want()
			wantPath := "/d/a.md"
			if c.bufErr != nil {
				wantPath = "" // the buffer was too large to ask which file it is
			}
			if got.State != "error" || got.Code != c.code || got.HTML != "" || got.Path != wantPath {
				t.Errorf("sent %+v, want an error %q for %q", got, c.code, wantPath)
			}
			if b, err := json.Marshal(got); err != nil || !strings.Contains(string(b), `"html":""`) {
				t.Errorf("JSON %s, %v", b, err)
			}

			// The next notification with the same text: a failure that may pass
			// is tried again, one that cannot is not repeated.
			e.loop.changed()
			e.fire()
			if c.remembered {
				waitCond(t, "the text to be read again", func() bool { return e.src.readCount() >= 2 })
				e.src.set("fine", "/d/a.md")
				e.rend.fn = nil
				e.loop.changed()
				e.fire()
				if got := e.want(); got.State != "html" {
					t.Errorf("after the text changed: %+v", got)
				}
				if n := len(e.sent()); n != 2 {
					t.Errorf("%d events, want 2: the same failure was sent twice", n)
				}
				return
			}
			if again := e.want(); again != got {
				t.Errorf("second try sent %+v, want %+v", again, got)
			}
		})
	}
}

// Neovim ending is not an error of the preview: the frontend clears the pane
// itself when it hears nvim:exit.
func TestPvNeovimGone(t *testing.T) {
	e := newPv(t)
	e.src.fail(fmt.Errorf("term: no nvim process: %w", nvim.ErrNotRunning))
	e.loop.enable(true)
	e.fire()
	waitCond(t, "the text to be read", func() bool { return e.src.readCount() >= 1 })
	e.src.set("back", "/d/a.md")
	e.loop.changed()
	e.fire()
	got := e.want()
	if got.State != "html" || got.HTML != "<p>back</p>" {
		t.Errorf("sent %+v", got)
	}
	if n := len(e.sent()); n != 1 {
		t.Errorf("%d events, want 1", n)
	}
}

// After reset (Neovim ended) the same content is sent again.
func TestPvReset(t *testing.T) {
	e := newPv(t)
	e.src.set("same", "/d/a.md")
	e.loop.enable(true)
	e.fire()
	e.want()
	e.loop.reset()
	e.loop.changed()
	e.fire()
	if got := e.want(); got.HTML != "<p>same</p>" {
		t.Errorf("sent %+v", got)
	}
	// reset leaves the preview on.
	e.src.set("next", "/d/a.md")
	e.loop.changed()
	e.fire()
	if got := e.want(); got.HTML != "<p>next</p>" {
		t.Errorf("sent %+v", got)
	}
}

func goroutineRuns(name string) bool {
	buf := make([]byte, 1<<20)
	return strings.Contains(string(buf[:runtime.Stack(buf, true)]), name)
}

func TestPvStopWaiting(t *testing.T) {
	e := newPv(t)
	e.src.set("a", "/d/a.md")
	e.loop.enable(true)
	timer := e.clk.next(t) // the loop is in its quiet time
	done := make(chan struct{})
	go func() { e.loop.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(edWait):
		t.Fatal("stop did not return while the loop was waiting")
	}
	if !timer.stopped.Load() {
		t.Error("the timer was left running")
	}
	waitCond(t, "the goroutine to end", func() bool { return !goroutineRuns("(*previewLoop).run") })
	// Calls after stop do not block or panic, and nothing is sent.
	e.loop.changed()
	e.loop.enable(true)
	e.loop.enable(false)
	e.loop.reset()
	e.loop.stop()
	if got := e.sent(); len(got) != 0 {
		t.Errorf("sent %+v", got)
	}
}

func TestPvStopConverting(t *testing.T) {
	e := newPv(t)
	e.rend.fn = blockUntilCanceled
	e.src.set("a", "/d/a.md")
	e.loop.enable(true)
	e.fire()
	c := e.rend.waitStarted(t)
	done := make(chan struct{})
	go func() { e.loop.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(edWait):
		t.Fatal("stop did not return while a conversion was running")
	}
	if c.ctx.Err() == nil {
		t.Error("the conversion was not canceled")
	}
	waitCond(t, "the goroutine to end", func() bool { return !goroutineRuns("(*previewLoop).run") })
	if got := e.sent(); len(got) != 0 {
		t.Errorf("sent %+v", got)
	}
}

// ---------- the service ----------

// withPreview builds the service with a renderer and a short quiet time.
func withPreview(r previewRenderer, debounce time.Duration) func(*editorServiceDeps) {
	return func(d *editorServiceDeps) {
		d.Preview = r
		d.Limits.PreviewDebounce = debounce
	}
}

const pvShort = 20 * time.Millisecond

func (r *recorder) previews(from int) []PreviewContent {
	var out []PreviewContent
	for _, ev := range r.since(from) {
		if c, ok := ev.data.(PreviewContent); ok && ev.name == eventPreviewContent {
			out = append(out, c)
		}
	}
	return out
}

func (r *recorder) waitPreview(t *testing.T, from int, what string, match func(PreviewContent) bool) PreviewContent {
	t.Helper()
	ev := r.wait(t, from, "preview:content "+what, func(ev recorded) bool {
		c, ok := ev.data.(PreviewContent)
		return ev.name == eventPreviewContent && ok && match(c)
	})
	return ev.data.(PreviewContent)
}

func (r *recorder) count(name string) int {
	n := 0
	for _, ev := range r.since(0) {
		if ev.name == name {
			n++
		}
	}
	return n
}

func TestPvToggleSearch(t *testing.T) {
	e := startEd(t)
	for _, c := range []struct{ keys, event string }{
		{" p", eventTogglePreview},
		{" f", eventOpenSearch},
	} {
		from := e.rec.mark()
		if err := e.svc.Write(c.keys); err != nil {
			t.Fatal(err)
		}
		ev := e.rec.waitName(t, from, c.event)
		if s, ok := ev.data.(string); !ok || s != "" {
			t.Errorf("%s payload %#v", c.event, ev.data)
		}
		// Neovim has answered a request after the key was handled; a second
		// event for one key would be here by now.
		e.eval(`['ok']`)
		if n := e.rec.count(c.event); n != 1 {
			t.Errorf("%d %s events for one key, want 1", n, c.event)
		}
	}
}

func TestPvPayloadJSON(t *testing.T) {
	ev := &application.CustomEvent{Name: eventPreviewContent, Data: PreviewContent{Path: "/x/a.md", State: "html", HTML: "<p>x</p>"}}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(ev.ToJSON()), &out); err != nil {
		t.Fatalf("%s: %v", ev.ToJSON(), err)
	}
	want := map[string]any{"path": "/x/a.md", "state": "html", "html": "<p>x</p>", "code": ""}
	if fmt.Sprint(out.Data) != fmt.Sprint(want) {
		t.Errorf("PreviewContent JSON = %v, want %v", out.Data, want)
	}
}

// What a buffer is for the preview, without Neovim.
func TestPvTarget(t *testing.T) {
	e := newEd(t)
	dir := t.TempDir()
	cases := []struct {
		name string
		buf  string
		want previewTarget
	}{
		{"no file", "", previewTarget{}},
		{"md", filepath.Join(dir, "a.md"), previewTarget{path: filepath.Join(dir, "a.md"), dir: dir, render: true}},
		{"md upper case", filepath.Join(dir, "A.MD"), previewTarget{path: filepath.Join(dir, "A.MD"), dir: dir, render: true}},
		{"md in a folder that is gone", filepath.Join(dir, "gone", "a.md"), previewTarget{path: filepath.Join(dir, "gone", "a.md"), render: true}},
		{"md with a relative path", "a.md", previewTarget{path: "a.md", render: true}},
		{"txt", filepath.Join(dir, "a.txt"), previewTarget{path: filepath.Join(dir, "a.txt")}},
		{"unknown extension", filepath.Join(dir, "a.rs"), previewTarget{path: filepath.Join(dir, "a.rs")}},
		{"no extension", filepath.Join(dir, "README"), previewTarget{path: filepath.Join(dir, "README")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := e.svc.previewTargetOf(nvim.Buffer{Path: c.buf}); got != c.want {
				t.Errorf("target = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestPvService(t *testing.T) {
	rend := newFakeRender()
	e := startEd(t, withPreview(rend, pvShort))
	p, dir := docFixture(t, e)
	ctx := context.Background()

	// A buffer without a file.
	from := e.rec.mark()
	if err := e.svc.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got := e.rec.waitPreview(t, from, "unnamed", func(PreviewContent) bool { return true }); got != (PreviewContent{State: "unsupported"}) {
		t.Errorf("unnamed buffer: %+v", got)
	}

	// .txt
	from = e.rec.mark()
	if err := e.svc.OpenDocument(ctx, p.ID, "b.txt"); err != nil {
		t.Fatal(err)
	}
	got := e.rec.waitPreview(t, from, "b.txt", func(c PreviewContent) bool { return sameFile(c.Path, filepath.Join(dir, "b.txt")) })
	if got.State != "unsupported" || got.HTML != "" || got.Code != "" {
		t.Errorf("txt: %+v", got)
	}
	if rend.count() != 0 {
		t.Errorf("%d conversions for a buffer without a preview", rend.count())
	}

	// .md: the folder of the file is where pictures come from.
	from = e.rec.mark()
	if err := e.svc.OpenDocument(ctx, p.ID, "sub/c.md"); err != nil {
		t.Fatal(err)
	}
	c := filepath.Join(dir, "sub", "c.md")
	got = e.rec.waitPreview(t, from, "c.md", func(pc PreviewContent) bool { return sameFile(pc.Path, c) && pc.State == "html" })
	if got.HTML != "<p>c\n</p>" {
		t.Errorf("md: %+v", got)
	}
	if call := rend.call(rend.count() - 1); !sameFile(call.dir, filepath.Dir(c)) {
		t.Errorf("resourceDir = %q, want the folder of the file", call.dir)
	}

	// Typing arrives as the last text, the same as in the buffer.
	from = e.rec.mark()
	if err := e.svc.Write("GoNovi red\x1b"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitPreview(t, from, "typed text", func(pc PreviewContent) bool { return strings.Contains(pc.HTML, "Novi red") })

	// A second call is a refresh that is sent although nothing changed, and
	// gives exactly one event.
	e.eval(`['ok']`)
	from = e.rec.mark()
	if err := e.svc.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	e.rec.waitPreview(t, from, "repeat", func(pc PreviewContent) bool { return strings.Contains(pc.HTML, "Novi red") })
	e.eval(`['ok']`)
	if n := len(e.rec.previews(from)); n != 1 {
		t.Errorf("%d events for one SetPreview(true), want 1: %+v", n, e.rec.previews(from))
	}

	// Off: typing sends nothing and converts nothing.
	if err := e.svc.SetPreview(ctx, false); err != nil {
		t.Fatal(err)
	}
	calls, from := rend.count(), e.rec.mark()
	if err := e.svc.Write("oDrugi\x1b"); err != nil {
		t.Fatal(err)
	}
	e.eval(`['ok']`)
	if rend.count() != calls || len(e.rec.previews(from)) != 0 {
		t.Errorf("work after SetPreview(false): %d conversions, %+v", rend.count()-calls, e.rec.previews(from))
	}
}

func TestPvNoRenderer(t *testing.T) {
	e := startEd(t) // no Preview in the deps
	p, _ := docFixture(t, e)
	ctx := context.Background()
	for _, on := range []bool{true, false, true} {
		if err := e.svc.SetPreview(ctx, on); err != nil {
			t.Fatalf("SetPreview(%v): %v", on, err)
		}
	}
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Write("Ox\x1b"); err != nil {
		t.Fatal(err)
	}
	e.eval(`['ok']`)
	if n := e.rec.count(eventPreviewContent); n != 0 {
		t.Errorf("%d preview events from a service without a preview", n)
	}
}

func TestPvNotStarted(t *testing.T) {
	e := newEd(t, withPreview(newFakeRender(), pvShort))
	ctx := context.Background()
	// Neovim does not run: the state is kept and nothing fails or is sent.
	for _, on := range []bool{true, false, true} {
		if err := e.svc.SetPreview(ctx, on); err != nil {
			t.Fatalf("SetPreview(%v): %v", on, err)
		}
	}
	if n := e.rec.count(eventPreviewContent); n != 0 {
		t.Errorf("%d events without Neovim", n)
	}
	e.svc.shutdown()
	wantCode(t, e.svc.SetPreview(ctx, true), codeNotRunning)
}

// The preview stays on when Neovim ends and starts again.
func TestPvRestart(t *testing.T) {
	rend := newFakeRender()
	e := startEd(t, withPreview(rend, pvShort))
	p, _ := docFixture(t, e)
	ctx := context.Background()
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	from := e.rec.mark()
	if err := e.svc.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	e.rec.waitPreview(t, from, "html", func(c PreviewContent) bool { return c.State == "html" })

	from = e.rec.mark()
	if err := e.svc.Write(":qa!\r"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitName(t, from, eventNvimExit)
	// Without Neovim a call only remembers.
	if err := e.svc.SetPreview(ctx, true); err != nil {
		t.Fatalf("SetPreview without Neovim: %v", err)
	}

	from = e.rec.mark()
	if err := e.svc.Start(ctx, 80, 24, sample(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// An empty buffer of the new process, and the preview shows it unasked.
	e.rec.waitPreview(t, from, "after the restart", func(PreviewContent) bool { return true })
	from = e.rec.mark()
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	got := e.rec.waitPreview(t, from, "a.md after the restart", func(c PreviewContent) bool { return c.State == "html" })
	if !strings.Contains(got.HTML, "# a") {
		t.Errorf("sent %+v", got)
	}
}

// Off is remembered across a restart too.
func TestPvRestartOff(t *testing.T) {
	rend := newFakeRender()
	e := startEd(t, withPreview(rend, pvShort))
	p, _ := docFixture(t, e)
	ctx := context.Background()
	if err := e.svc.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetPreview(ctx, false); err != nil {
		t.Fatal(err)
	}
	from := e.rec.mark()
	if err := e.svc.Write(":qa!\r"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitName(t, from, eventNvimExit)
	if err := e.svc.Start(ctx, 80, 24, sample(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	e.eval(`['ok']`)
	calls := rend.count()
	from = e.rec.mark()
	if err := e.svc.Write("Ox\x1b"); err != nil {
		t.Fatal(err)
	}
	e.eval(`['ok']`)
	if rend.count() != calls || len(e.rec.previews(from)) != 0 {
		t.Errorf("the preview is on after a restart: %d conversions, %+v", rend.count()-calls, e.rec.previews(from))
	}
}

// Neovim ending cancels the conversion in progress.
func TestPvExit(t *testing.T) {
	rend := newFakeRender()
	rend.fn = blockUntilCanceled
	e := startEd(t, withPreview(rend, pvShort))
	if err := e.svc.SetPreview(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	// An unnamed buffer is not converted; a document is.
	p, _ := docFixture(t, e)
	if err := e.svc.OpenDocument(context.Background(), p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	c := rend.waitStarted(t)
	from := e.rec.mark()
	if err := e.svc.Write(":qa!\r"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitName(t, from, eventNvimExit)
	select {
	case <-c.ctx.Done():
	case <-time.After(edWait):
		t.Fatal("the conversion was not canceled when Neovim ended")
	}
	if n := len(e.rec.previews(from)); n != 0 {
		t.Errorf("%d preview events from the conversion that was canceled", n)
	}
}

func TestPvShutdownWaiting(t *testing.T) {
	rend := newFakeRender()
	e := startEd(t, withPreview(rend, time.Hour))
	if err := e.svc.SetPreview(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	n := e.rec.mark()
	done := make(chan struct{})
	go func() { e.svc.shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(edWait):
		t.Fatal("shutdown did not return while the loop waited")
	}
	waitCond(t, "the goroutine to end", func() bool { return !goroutineRuns("(*previewLoop).run") })
	if rend.count() != 0 {
		t.Errorf("%d conversions", rend.count())
	}
	if len(e.rec.previews(n)) != 0 {
		t.Errorf("events after shutdown: %+v", e.rec.since(n))
	}
}

func TestPvShutdownConverting(t *testing.T) {
	rend := newFakeRender()
	rend.fn = blockUntilCanceled
	e := startEd(t, withPreview(rend, pvShort))
	p, _ := docFixture(t, e)
	ctx := context.Background()
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	c := rend.waitStarted(t)
	n := e.rec.mark()
	done := make(chan struct{})
	go func() { e.svc.shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(edWait):
		t.Fatal("shutdown did not return while a conversion was running")
	}
	if c.ctx.Err() == nil {
		t.Error("the conversion was not canceled")
	}
	waitCond(t, "the goroutine to end", func() bool { return !goroutineRuns("(*previewLoop).run") })
	if len(e.rec.previews(n)) != 0 {
		t.Errorf("events after shutdown: %+v", e.rec.since(n))
	}
}

// A Word document is shown under the user's file, and its pictures are looked
// up in the working folder, not next to the user's file.
func TestPvWord(t *testing.T) {
	rend := newFakeRender()
	parts := &wdParts{}
	e := startEd(t, parts.apply(t), withPreview(rend, pvShort))
	p, dir := e.addProject("proj")
	fixture(t, "basic.docx", dir)
	ctx := context.Background()
	if err := e.svc.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	from := e.rec.mark()
	if err := e.svc.OpenDocument(ctx, p.ID, "basic.docx"); err != nil {
		t.Fatal(err)
	}
	got := e.rec.waitPreview(t, from, "basic.docx", func(c PreviewContent) bool { return c.State == "html" && strings.HasSuffix(c.Path, ".docx") })
	if !sameFile(got.Path, filepath.Join(dir, "basic.docx")) {
		t.Errorf("Path = %q, want the user's .docx", got.Path)
	}
	cur, err := e.svc.session.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cur.Path) != "doc.md" {
		t.Fatalf("the buffer is %q, want the working copy", cur.Path)
	}
	call := rend.call(rend.count() - 1)
	if !sameFile(call.dir, filepath.Dir(cur.Path)) || sameFile(call.dir, dir) {
		t.Errorf("resourceDir = %q, want the working folder %q (not the user's %q)", call.dir, filepath.Dir(cur.Path), dir)
	}
}

// The whole way with the real pandoc: a picture of the folder is embedded, a
// script in the text is not.
func TestPvPandoc(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc is not installed")
	}
	e := startEd(t, func(d *editorServiceDeps) {
		r, err := preview.New(preview.Options{
			PandocBin: d.Store.PandocBin(), Dir: d.Store.PreviewDir(), Timeout: d.Limits.PreviewTimeout,
			MaxSource: d.Limits.MaxPreviewSource, MaxHTML: d.Limits.MaxPreviewHTML,
		})
		if err != nil {
			t.Fatal(err)
		}
		d.Preview = r
		d.Limits.PreviewDebounce = pvShort
	})
	p, dir := e.addProject("proj")
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	f, err := os.Create(filepath.Join(dir, "slika.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "a.md"), "# Naslov\n\n![opis](slika.png)\n\n<script>alert(1)</script>\n\nOstatak.\n")

	ctx := context.Background()
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	from := e.rec.mark()
	if err := e.svc.SetPreview(ctx, true); err != nil {
		t.Fatal(err)
	}
	got := e.rec.waitPreview(t, from, "html", func(c PreviewContent) bool { return c.State == "html" })
	if !sameFile(got.Path, filepath.Join(dir, "a.md")) {
		t.Errorf("Path = %q", got.Path)
	}
	if !strings.Contains(got.HTML, "Naslov") || !strings.Contains(got.HTML, "Ostatak") {
		t.Errorf("the text is missing: %s", got.HTML)
	}
	if !strings.Contains(got.HTML, `src="data:image/png;base64,`) {
		t.Errorf("the picture is not embedded as a data: URI: %s", got.HTML)
	}
	if strings.Contains(strings.ToLower(got.HTML), "<script") {
		t.Errorf("a script tag reached the page: %s", got.HTML)
	}
}
