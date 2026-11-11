package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"Pisar/spike/internal/term"
)

type fakeTerm struct {
	saveErr    error
	saveCalls  int
	visual     bool
	visualErr  error
	copyCalls  int
	sawTimeout bool
}

func (f *fakeTerm) Save(ctx context.Context) error {
	f.saveCalls++
	_, f.sawTimeout = ctx.Deadline()
	return f.saveErr
}

func (f *fakeTerm) CopyVisual(ctx context.Context) (bool, error) {
	f.copyCalls++
	return f.visual, f.visualErr
}

type emitted struct{ name, data string }

type fakeEmit struct {
	mu  sync.Mutex
	got []emitted
}

func (e *fakeEmit) emit(name, data string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got = append(e.got, emitted{name, data})
}

// newActions wires fakes and a trace file in t.TempDir(). traceText returns
// the trace written so far.
func newActions(t *testing.T, ft *fakeTerm) (*menuActions, *fakeEmit, func() string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace")
	tr, err := newTracer(path)
	if err != nil {
		t.Fatal(err)
	}
	fe := &fakeEmit{}
	a := newMenuActions(ft, fe.emit, tr)
	return a, fe, func() string {
		t.Helper()
		if err := tr.close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
}

func TestMenuSave(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantTrace string
	}{
		{"ok", nil, "menu save err=<nil>"},
		{"fail", errors.New("E32: No file name"), "menu save err=E32: No file name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ft := &fakeTerm{saveErr: tc.err}
			a, fe, traceText := newActions(t, ft)
			a.save() // must return normally even when Save fails
			if ft.saveCalls != 1 {
				t.Errorf("Save called %d times, want 1", ft.saveCalls)
			}
			if !ft.sawTimeout {
				t.Error("Save got a context without a deadline")
			}
			if len(fe.got) != 0 {
				t.Errorf("events = %v, want none", fe.got)
			}
			if tr := traceText(); !strings.Contains(tr, tc.wantTrace+"\n") {
				t.Errorf("trace = %q, want line %q", tr, tc.wantTrace)
			}
		})
	}
}

func TestMenuCopy(t *testing.T) {
	tests := []struct {
		name      string
		visual    bool
		err       error
		wantEvent int
		wantTrace string
	}{
		{"visual", true, nil, 0, "menu copy visual=true err=<nil>"},
		{"no visual", false, nil, 1, "menu copy visual=false err=<nil>"},
		{"error", false, errors.New("nvim is gone"), 0, "menu copy visual=false err=nvim is gone"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ft := &fakeTerm{visual: tc.visual, visualErr: tc.err}
			a, fe, traceText := newActions(t, ft)
			a.copy()
			if ft.copyCalls != 1 {
				t.Errorf("CopyVisual called %d times, want 1", ft.copyCalls)
			}
			if len(fe.got) != tc.wantEvent {
				t.Fatalf("events = %v, want %d", fe.got, tc.wantEvent)
			}
			if tc.wantEvent == 1 && fe.got[0] != (emitted{term.EventCopy, ""}) {
				t.Errorf("event = %v, want %s with an empty payload", fe.got[0], term.EventCopy)
			}
			if tr := traceText(); !strings.Contains(tr, tc.wantTrace+"\n") {
				t.Errorf("trace = %q, want line %q", tr, tc.wantTrace)
			}
		})
	}
}

func TestMenuRenderer(t *testing.T) {
	tests := []struct {
		kind string
		want []emitted
	}{
		{"webgl", []emitted{{eventRenderer, "webgl"}}},
		{"dom", []emitted{{eventRenderer, "dom"}}},
		{"canvas", nil},
		{"", nil},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			a, fe, traceText := newActions(t, &fakeTerm{})
			a.setRenderer(tc.kind)
			if len(fe.got) != len(tc.want) || (len(tc.want) == 1 && fe.got[0] != tc.want[0]) {
				t.Errorf("events = %v, want %v", fe.got, tc.want)
			}
			if tc.want != nil && !strings.Contains(traceText(), "menu renderer="+tc.kind+"\n") {
				t.Errorf("trace lacks the renderer line")
			}
		})
	}
}

func TestMenuTimeout(t *testing.T) {
	// The timeout is the shared constant, not a second one.
	a := newMenuActions(&fakeTerm{}, func(string, string) {}, nil)
	if a.timeout != callTimeout {
		t.Errorf("timeout = %v, want callTimeout (%v)", a.timeout, callTimeout)
	}
}

func TestCopyText(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		want    []string // texts the clipboard received
		wantErr bool
	}{
		{"text", "čćšžđ", []string{"čćšžđ"}, false},
		{"empty", "", nil, false},
		{"refused", "x", []string{"x"}, true},
		{"none", "x", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewTerminalService(nil, "", "", nil)
			var got []string
			if tc.name != "none" {
				svc.useClipboard(func(s string) bool {
					got = append(got, s)
					return tc.name != "refused"
				})
			}
			err := svc.CopyText(tc.text)
			if (err != nil) != tc.wantErr {
				t.Errorf("CopyText error = %v, want error: %v", err, tc.wantErr)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("clipboard got %q, want %q", got, tc.want)
			}
		})
	}
}
