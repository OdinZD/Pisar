package docsafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// newWorkEnv is an env whose Ops has a WorkDir of its own. The WorkDir does not
// exist yet; "user", the backups, the Replace sources (e.work) and the WorkDir
// are separate folders.
func newWorkEnv(t *testing.T) (*env, string) {
	t.Helper()
	e := newEnv(t, 3)
	workDir := filepath.Join(filepath.Dir(e.user), "pisar", "workcopies")
	ops, err := New(Options{
		TrashBin: "/fake/trash", Timeout: time.Second,
		BackupsDir: e.backups, MaxBackups: 3, Now: e.clock.Now, WorkDir: workDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.ops = ops
	return e, workDir
}

// captureLogs routes slog to a buffer for the test and restores the default.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func testMeta(e *env, name string) WorkMeta {
	p := filepath.Join(e.user, name)
	return WorkMeta{Origin: p, Target: p, Snapshot: Snapshot{Size: 12, SHA256: "ab12"}}
}

func mustCreateWork(t *testing.T, e *env, m WorkMeta) WorkCopy {
	t.Helper()
	w, err := e.ops.CreateWork(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func mustFindWork(t *testing.T, e *env, path string) (WorkCopy, bool) {
	t.Helper()
	w, ok, err := e.ops.FindWork(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return w, ok
}

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode()
}

func TestCreateWork_Success(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name       string
		opened     time.Time
		wantOpened func(e *env) time.Time
	}{
		{"opened from the clock when zero", time.Time{}, func(e *env) time.Time { return e.clock.cur.Add(-e.clock.step) }},
		{"given opened is kept", fixed, func(*env) time.Time { return fixed }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, workDir := newWorkEnv(t)
			m := testMeta(e, "Pismo.docx")
			m.Version = 99 // the function sets it
			m.Opened = tt.opened
			w := mustCreateWork(t, e, m)

			if filepath.Dir(w.Dir) != workDir || !validWorkID(filepath.Base(w.Dir)) {
				t.Errorf("Dir = %q, want a generated child of %q", w.Dir, workDir)
			}
			if got := modeOf(t, workDir).Perm(); got != 0o700 {
				t.Errorf("WorkDir mode = %o, want 700", got)
			}
			if fm := modeOf(t, w.Dir); !fm.IsDir() || fm.Perm() != 0o700 {
				t.Errorf("working folder mode = %v, want a folder with 700", fm)
			}
			metaPath := filepath.Join(w.Dir, "meta.json")
			if got := modeOf(t, metaPath).Perm(); got != 0o600 {
				t.Errorf("meta.json mode = %o, want 600", got)
			}
			if got := names(t, w.Dir); !reflect.DeepEqual(got, []string{"meta.json"}) {
				t.Errorf("folder = %q, want only meta.json", got)
			}

			var raw map[string]any
			if err := json.Unmarshal([]byte(read(t, metaPath)), &raw); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"version", "origin", "target", "snapshot", "dirty", "opened"} {
				if _, ok := raw[key]; !ok {
					t.Errorf("meta.json has no %q: %v", key, raw)
				}
			}
			if raw["version"] != float64(workMetaVersion) {
				t.Errorf("version = %v, want %d", raw["version"], workMetaVersion)
			}
			if w.Meta.Version != workMetaVersion {
				t.Errorf("returned Version = %d, want %d", w.Meta.Version, workMetaVersion)
			}
			if want := tt.wantOpened(e); !w.Meta.Opened.Equal(want) {
				t.Errorf("Opened = %v, want %v", w.Meta.Opened, want)
			}
			got, ok := mustFindWork(t, e, m.Origin)
			if !ok || got.Dir != w.Dir || !got.Meta.Opened.Equal(w.Meta.Opened) || got.Meta.Snapshot != m.Snapshot {
				t.Errorf("FindWork = %+v, %v; want the copy just made (%+v)", got, ok, w)
			}
		})
	}
}

func TestCreateWork_TwoCopiesDiffer(t *testing.T) {
	e, _ := newWorkEnv(t)
	a := mustCreateWork(t, e, testMeta(e, "A.docx"))
	b := mustCreateWork(t, e, testMeta(e, "B.docx"))
	if a.Dir == b.Dir {
		t.Fatalf("both copies are in %q", a.Dir)
	}
}

func TestCreateWork_ExistingCopy(t *testing.T) {
	tests := []struct {
		name  string
		first func(e *env) WorkMeta
		again func(e *env) WorkMeta
	}{
		{"same origin", func(e *env) WorkMeta { return testMeta(e, "A.docx") }, func(e *env) WorkMeta { return testMeta(e, "A.docx") }},
		{"unclean spelling of the same origin", func(e *env) WorkMeta { return testMeta(e, "A.docx") },
			func(e *env) WorkMeta {
				// filepath.Join would clean the spelling away, so concatenate.
				return WorkMeta{Origin: e.user + "/x/../A.docx"}
			}},
		{"origin equals the target of another copy",
			func(e *env) WorkMeta {
				m := testMeta(e, "Old.doc")
				m.Target = filepath.Join(e.user, "Old.docx")
				return m
			},
			func(e *env) WorkMeta { return testMeta(e, "Old.docx") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, workDir := newWorkEnv(t)
			mustCreateWork(t, e, tt.first(e))
			_, err := e.ops.CreateWork(context.Background(), tt.again(e))
			if !errors.Is(err, ErrExists) {
				t.Fatalf("err = %v, want ErrExists", err)
			}
			if got := names(t, workDir); len(got) != 1 {
				t.Errorf("work folders = %q, want just the first", got)
			}
		})
	}
}

func TestCreateWork_ConcurrentSameOrigin(t *testing.T) {
	e, workDir := newWorkEnv(t)
	m := testMeta(e, "A.docx")
	const n = 12
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.ops.CreateWork(context.Background(), m)
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrExists):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || len(names(t, workDir)) != 1 {
		t.Errorf("%d succeeded, folders %q; want exactly one of each", ok, names(t, workDir))
	}
}

func TestCreateWork_InvalidMeta(t *testing.T) {
	tests := []struct {
		name string
		mod  func(m *WorkMeta)
	}{
		{"relative origin", func(m *WorkMeta) { m.Origin = "docs/A.docx" }},
		{"empty origin", func(m *WorkMeta) { m.Origin = "" }},
		{"relative target", func(m *WorkMeta) { m.Target = "A.docx" }},
		{"too large", func(m *WorkMeta) { m.Origin = "/" + strings.Repeat("a", maxWorkMetaBytes) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, workDir := newWorkEnv(t)
			m := testMeta(e, "A.docx")
			tt.mod(&m)
			if _, err := e.ops.CreateWork(context.Background(), m); err == nil {
				t.Fatal("no error")
			}
			if _, err := os.Lstat(workDir); err == nil {
				if got := names(t, workDir); len(got) != 0 {
					t.Errorf("work folders left behind: %q", got)
				}
			}
		})
	}
}

func TestCreateWork_FaultRemovesTheNewFolder(t *testing.T) {
	e, workDir := newWorkEnv(t)
	e.ops.fail = func(s string) error {
		if s == stageMetaRename {
			return errInjected
		}
		return nil
	}
	if _, err := e.ops.CreateWork(context.Background(), testMeta(e, "A.docx")); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected fault", err)
	}
	if got := names(t, workDir); len(got) != 0 {
		t.Errorf("work folders = %q, want none", got)
	}
}

func TestWork_RequiresWorkDir(t *testing.T) {
	e := newEnv(t, 3) // Ops without WorkDir
	ctx := context.Background()
	w := WorkCopy{Dir: filepath.Join(e.work, "0123456789abcdef"), Meta: testMeta(e, "A.docx")}
	calls := map[string]func() error{
		"CreateWork":   func() error { _, err := e.ops.CreateWork(ctx, w.Meta); return err },
		"FindWork":     func() error { _, _, err := e.ops.FindWork(ctx, w.Meta.Origin); return err },
		"SaveWorkMeta": func() error { return e.ops.SaveWorkMeta(ctx, w) },
		"DiscardWork":  func() error { return e.ops.DiscardWork(ctx, w) },
		"SweepWork":    func() error { _, err := e.ops.SweepWork(ctx); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil || !strings.Contains(err.Error(), "WorkDir is required") {
				t.Errorf("err = %v, want a WorkDir is required error", err)
			}
		})
	}
}

func TestFindWork(t *testing.T) {
	e, _ := newWorkEnv(t)
	a := mustCreateWork(t, e, testMeta(e, "A.docx"))
	imported := WorkMeta{Origin: filepath.Join(e.user, "Old.doc"), Target: filepath.Join(e.user, "Old.docx")}
	b := mustCreateWork(t, e, imported)
	noTarget := WorkMeta{Origin: filepath.Join(e.user, "Fresh.doc")}
	c := mustCreateWork(t, e, noTarget)

	tests := []struct {
		name string
		path string
		want string // Dir, "" for none
	}{
		{"by origin", a.Meta.Origin, a.Dir},
		{"by origin of an import", imported.Origin, b.Dir},
		{"by target of an import", imported.Target, b.Dir},
		{"unclean spelling", filepath.Join(e.user, "sub", "..", "A.docx"), a.Dir},
		{"empty target does not match anything", filepath.Join(e.user, "Fresh.docx"), ""},
		{"origin of a copy without target", noTarget.Origin, c.Dir},
		{"unknown", filepath.Join(e.user, "Nope.docx"), ""},
		{"case differs (paths are compared as given)", filepath.Join(e.user, "a.DOCX"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mustFindWork(t, e, tt.path)
			if (tt.want != "") != ok || got.Dir != tt.want {
				t.Errorf("FindWork(%q) = %q, %v; want %q", tt.path, got.Dir, ok, tt.want)
			}
		})
	}

	if _, _, err := e.ops.FindWork(context.Background(), "A.docx"); err == nil {
		t.Error("a relative path was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := e.ops.FindWork(ctx, a.Meta.Origin); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: err = %v, want context.Canceled", err)
	}
}

func TestFindWork_NoWorkDirYet(t *testing.T) {
	e, _ := newWorkEnv(t)
	if w, ok := mustFindWork(t, e, filepath.Join(e.user, "A.docx")); ok {
		t.Errorf("found %+v in a WorkDir that does not exist", w)
	}
}

func TestFindWork_PrefersUnsavedThenNewest(t *testing.T) {
	e, _ := newWorkEnv(t)
	ctx := context.Background()
	shared := filepath.Join(e.user, "Shared.docx")
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(origin string, opened time.Time) WorkCopy {
		return mustCreateWork(t, e, WorkMeta{Origin: filepath.Join(e.user, origin), Opened: opened})
	}
	older := mk("one.doc", t1)
	newer := mk("two.doc", t1.Add(time.Hour))
	// Both now name Shared.docx as their target.
	for _, w := range []*WorkCopy{&older, &newer} {
		w.Meta.Target = shared
		if err := e.ops.SaveWorkMeta(ctx, *w); err != nil {
			t.Fatal(err)
		}
	}
	steps := []struct {
		name  string
		set   func()
		wantW *WorkCopy
	}{
		{"both clean: the newest", func() {}, &newer},
		{"older one unsaved: it wins", func() { older.Meta.Dirty = true }, &older},
		{"both unsaved: the newest of them", func() { newer.Meta.Dirty = true }, &newer},
	}
	for _, s := range steps {
		s.set()
		for _, w := range []WorkCopy{older, newer} {
			if err := e.ops.SaveWorkMeta(ctx, w); err != nil {
				t.Fatal(err)
			}
		}
		got, ok := mustFindWork(t, e, shared)
		if !ok || got.Dir != s.wantW.Dir {
			t.Errorf("%s: got %q (%v), want %q", s.name, got.Dir, ok, s.wantW.Dir)
		}
	}
}

func TestSaveWorkMeta_RoundTrip(t *testing.T) {
	e, _ := newWorkEnv(t)
	w := mustCreateWork(t, e, testMeta(e, "A.docx"))

	mod := time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC)
	w.Meta.Dirty = true
	w.Meta.Target = filepath.Join(e.user, "Other.docx")
	w.Meta.Snapshot = Snapshot{Size: 4096, ModTime: mod, SHA256: "deadbeef"}
	if err := e.ops.SaveWorkMeta(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	got, ok := mustFindWork(t, e, w.Meta.Origin)
	if !ok {
		t.Fatal("not found")
	}
	if !got.Meta.Dirty || got.Meta.Target != w.Meta.Target || got.Meta.Origin != w.Meta.Origin ||
		got.Meta.Snapshot.Size != 4096 || got.Meta.Snapshot.SHA256 != "deadbeef" || !got.Meta.Snapshot.ModTime.Equal(mod) ||
		!got.Meta.Opened.Equal(w.Meta.Opened) || got.Meta.Version != workMetaVersion {
		t.Errorf("round trip lost something:\n got %+v\nwant %+v", got.Meta, w.Meta)
	}
	if mode := modeOf(t, filepath.Join(w.Dir, "meta.json")).Perm(); mode != 0o600 {
		t.Errorf("meta.json mode = %o, want 600", mode)
	}
	if got := names(t, w.Dir); !reflect.DeepEqual(got, []string{"meta.json"}) {
		t.Errorf("folder = %q, want only meta.json", got)
	}
}

func TestSaveWorkMeta_Rename(t *testing.T) {
	e, _ := newWorkEnv(t)
	w := mustCreateWork(t, e, testMeta(e, "A.docx"))
	old := w.Meta.Origin
	renamed := filepath.Join(e.user, "Renamed.docx")
	w.Meta.Origin, w.Meta.Target = renamed, renamed
	if err := e.ops.SaveWorkMeta(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if got, ok := mustFindWork(t, e, renamed); !ok || got.Dir != w.Dir {
		t.Errorf("by the new path: %q, %v; want the same folder %q", got.Dir, ok, w.Dir)
	}
	if got, ok := mustFindWork(t, e, old); ok {
		t.Errorf("the old path still finds %q", got.Dir)
	}
}

func TestSaveWorkMeta_FaultAtEveryStage(t *testing.T) {
	stages := []string{stageMetaTemp, stageMetaWrite, stageMetaFsync, stageMetaClose, stageMetaRename, stageMetaFsyncDir}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			e, _ := newWorkEnv(t)
			w := mustCreateWork(t, e, testMeta(e, "A.docx"))
			oldBytes := read(t, filepath.Join(w.Dir, "meta.json"))

			e.ops.fail = func(s string) error {
				if s == stage {
					return errInjected
				}
				return nil
			}
			next := w
			next.Meta.Dirty = true
			next.Meta.Snapshot.Size = 777
			err := e.ops.SaveWorkMeta(context.Background(), next)
			if !errors.Is(err, errInjected) {
				t.Fatalf("err = %v, want the injected fault", err)
			}
			e.ops.fail = nil

			if got := names(t, w.Dir); !reflect.DeepEqual(got, []string{"meta.json"}) {
				t.Errorf("folder = %q, want only meta.json", got)
			}
			got, ok := mustFindWork(t, e, w.Meta.Origin)
			if !ok {
				t.Fatal("the working copy is unreadable after the fault")
			}
			if stage == stageMetaFsyncDir {
				// Past the rename: the new, complete file is in place.
				if !got.Meta.Dirty || got.Meta.Snapshot.Size != 777 {
					t.Errorf("meta = %+v, want the new one", got.Meta)
				}
				return
			}
			if read(t, filepath.Join(w.Dir, "meta.json")) != oldBytes || got.Meta.Dirty {
				t.Errorf("meta.json changed although the save failed before the rename")
			}
		})
	}
}

func TestSaveWorkMeta_Refused(t *testing.T) {
	e, workDir := newWorkEnv(t)
	w := mustCreateWork(t, e, testMeta(e, "A.docx"))
	before := read(t, filepath.Join(w.Dir, "meta.json"))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	other := filepath.Join(filepath.Dir(workDir), "elsewhere", filepath.Base(w.Dir))
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	relative := w
	relative.Meta.Origin = "A.docx"

	tests := []struct {
		name string
		ctx  context.Context
		w    WorkCopy
		want error
	}{
		{"cancelled", cancelled, w, context.Canceled},
		{"folder outside WorkDir", context.Background(), WorkCopy{Dir: other, Meta: w.Meta}, ErrNotWork},
		{"WorkDir itself", context.Background(), WorkCopy{Dir: workDir, Meta: w.Meta}, ErrNotWork},
		{"nested folder", context.Background(), WorkCopy{Dir: filepath.Join(w.Dir, "sub"), Meta: w.Meta}, ErrNotWork},
		{"foreign name", context.Background(), WorkCopy{Dir: filepath.Join(workDir, "notes"), Meta: w.Meta}, ErrNotWork},
		{"relative origin", context.Background(), relative, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := e.ops.SaveWorkMeta(tt.ctx, tt.w)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if read(t, filepath.Join(w.Dir, "meta.json")) != before {
				t.Error("meta.json changed")
			}
			if _, err := os.Lstat(filepath.Join(other, "meta.json")); err == nil {
				t.Error("a meta.json was written outside WorkDir")
			}
		})
	}
}

// metaJSON is a valid meta.json for origin, as text.
func metaJSON(t *testing.T, origin string, dirty bool) string {
	t.Helper()
	b, err := json.Marshal(WorkMeta{Version: workMetaVersion, Origin: origin, Target: origin, Dirty: dirty,
		Opened: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDamagedMeta(t *testing.T) {
	const id = "0123456789abcdef"
	e0, _ := newWorkEnv(t)
	origin := filepath.Join(e0.user, "Damaged.docx")
	good := metaJSON(t, origin, false)

	// Each case lays out the folder <WorkDir>/<id>; outside is a folder next to WorkDir.
	tests := []struct {
		name  string
		setup func(t *testing.T, dir, outside string)
	}{
		{"empty file", func(t *testing.T, dir, _ string) { write(t, filepath.Join(dir, "meta.json"), "", 0o600) }},
		{"truncated JSON", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), good[:len(good)/2], 0o600)
		}},
		{"unknown field", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), strings.Replace(good, `"dirty"`, `"surprise":1,"dirty"`, 1), 0o600)
		}},
		{"unknown field in the snapshot", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), strings.Replace(good, `"size"`, `"extra":1,"size"`, 1), 0o600)
		}},
		{"newer version", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), strings.Replace(good, `"version":1`, `"version":2`, 1), 0o600)
		}},
		{"version zero", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), strings.Replace(good, `"version":1`, `"version":0`, 1), 0o600)
		}},
		{"relative origin", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), metaJSON(t, "Damaged.docx", false), 0o600)
		}},
		{"relative target", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), strings.Replace(good, `"target":"`+origin+`"`, `"target":"x.docx"`, 1), 0o600)
		}},
		{"data after the object", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), good+good, 0o600)
		}},
		{"wrong type", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), strings.Replace(good, `"dirty":false`, `"dirty":"no"`, 1), 0o600)
		}},
		{"JSON null", func(t *testing.T, dir, _ string) { write(t, filepath.Join(dir, "meta.json"), "null", 0o600) }},
		{"too large", func(t *testing.T, dir, _ string) {
			write(t, filepath.Join(dir, "meta.json"), good+strings.Repeat(" ", maxWorkMetaBytes), 0o600)
		}},
		{"meta.json is a folder", func(t *testing.T, dir, _ string) {
			if err := os.Mkdir(filepath.Join(dir, "meta.json"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"meta.json is a link to a valid file", func(t *testing.T, dir, outside string) {
			write(t, filepath.Join(outside, "real-meta.json"), good, 0o600)
			if err := os.Symlink(filepath.Join(outside, "real-meta.json"), filepath.Join(dir, "meta.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"no meta.json", func(*testing.T, string, string) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			e, workDir := newWorkEnv(t)
			outside := filepath.Join(filepath.Dir(workDir), "outside")
			dir := filepath.Join(workDir, id)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			tt.setup(t, dir, outside)
			write(t, filepath.Join(dir, "doc.md"), "the only copy of the text", 0o600)
			layout := names(t, dir)

			if _, err := readWorkMeta(dir); !errors.Is(err, ErrDamagedWork) || !strings.Contains(err.Error(), id) {
				t.Errorf("readWorkMeta err = %v, want ErrDamagedWork naming the folder", err)
			}
			// A healthy neighbour, so a skip is told apart from "found nothing at all".
			goodCopy := mustCreateWork(t, e, testMeta(e, "Good.docx"))
			if got, ok := mustFindWork(t, e, origin); ok {
				t.Errorf("FindWork found the damaged copy %q", got.Dir)
			}
			if got, ok := mustFindWork(t, e, goodCopy.Meta.Origin); !ok || got.Dir != goodCopy.Dir {
				t.Errorf("FindWork skipped the healthy copy: %q, %v", got.Dir, ok)
			}
			if !strings.Contains(logs.String(), id) || strings.Contains(logs.String(), "the only copy") {
				t.Errorf("log should name the folder and nothing of its content: %q", logs.String())
			}

			kept, err := e.ops.SweepWork(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(kept) != 0 {
				t.Errorf("kept = %+v, want none (the healthy copy is clean, the damaged one is not reported)", kept)
			}
			if got := names(t, dir); !reflect.DeepEqual(got, layout) {
				t.Errorf("the damaged folder changed: %q, want %q", got, layout)
			}
			if got := read(t, filepath.Join(dir, "doc.md")); got != "the only copy of the text" {
				t.Errorf("doc.md = %q", got)
			}
			if _, err := os.Lstat(goodCopy.Dir); err == nil {
				t.Error("the clean healthy copy was not swept")
			}
		})
	}
}

func TestSweepWork(t *testing.T) {
	logs := captureLogs(t)
	e, workDir := newWorkEnv(t)
	ctx := context.Background()

	clean1 := mustCreateWork(t, e, testMeta(e, "Clean1.docx"))
	clean2 := mustCreateWork(t, e, testMeta(e, "Clean2.docx"))
	write(t, filepath.Join(clean2.Dir, "media", "x.png"), "png", 0o600)
	dirtyMeta := testMeta(e, "Dirty.docx")
	dirtyMeta.Dirty = true
	dirty := mustCreateWork(t, e, dirtyMeta)
	write(t, filepath.Join(dirty.Dir, "doc.md"), "unsaved text", 0o600)

	damaged := filepath.Join(workDir, "fedcba9876543210")
	write(t, filepath.Join(damaged, "meta.json"), "{", 0o600)
	write(t, filepath.Join(damaged, "doc.md"), "maybe the only copy", 0o600)

	write(t, filepath.Join(workDir, "stray.txt"), "a plain file", 0o600)
	foreign := filepath.Join(workDir, "notes")
	write(t, filepath.Join(foreign, "meta.json"), metaJSON(t, filepath.Join(e.user, "N.docx"), false), 0o600)

	// A link with a valid folder name to a folder outside WorkDir that has a
	// meta.json of its own saying "clean": it must not be followed or deleted.
	outside := filepath.Join(filepath.Dir(workDir), "outside")
	write(t, filepath.Join(outside, "meta.json"), metaJSON(t, filepath.Join(e.user, "O.docx"), false), 0o600)
	write(t, filepath.Join(outside, "precious.txt"), "keep", 0o600)
	link := filepath.Join(workDir, "00ff00ff00ff00ff")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	kept, err := e.ops.SweepWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Dir != dirty.Dir || !kept[0].Meta.Dirty || kept[0].Meta.Origin != dirtyMeta.Origin {
		t.Errorf("kept = %+v, want only the unsaved copy", kept)
	}
	for _, gone := range []string{clean1.Dir, clean2.Dir} {
		if _, err := os.Lstat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%q should be gone, Lstat err = %v", gone, err)
		}
	}
	intact := map[string]string{
		filepath.Join(dirty.Dir, "doc.md"):     "unsaved text",
		filepath.Join(damaged, "doc.md"):       "maybe the only copy",
		filepath.Join(workDir, "stray.txt"):    "a plain file",
		filepath.Join(foreign, "meta.json"):    metaJSON(t, filepath.Join(e.user, "N.docx"), false),
		filepath.Join(outside, "precious.txt"): "keep",
		filepath.Join(outside, "meta.json"):    metaJSON(t, filepath.Join(e.user, "O.docx"), false),
	}
	for p, want := range intact {
		if got := read(t, p); got != want {
			t.Errorf("%q = %q, want it untouched", p, got)
		}
	}
	if fm := modeOf(t, link); fm&os.ModeSymlink == 0 {
		t.Errorf("the link in WorkDir was replaced: %v", fm)
	}
	if !strings.Contains(logs.String(), "fedcba9876543210") {
		t.Errorf("the damaged folder was not logged: %q", logs.String())
	}

	// Running it again changes nothing more.
	again, err := e.ops.SweepWork(ctx)
	if err != nil || len(again) != 1 {
		t.Errorf("second sweep: %+v, %v", again, err)
	}
}

func TestSweepWork_NoWorkDirYet(t *testing.T) {
	e, workDir := newWorkEnv(t)
	kept, err := e.ops.SweepWork(context.Background())
	if err != nil || kept != nil {
		t.Errorf("SweepWork = %v, %v; want nil, nil", kept, err)
	}
	if _, err := os.Lstat(workDir); err == nil {
		t.Error("SweepWork created WorkDir")
	}
}

func TestSweepWork_Cancelled(t *testing.T) {
	e, _ := newWorkEnv(t)
	w := mustCreateWork(t, e, testMeta(e, "A.docx"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.ops.SweepWork(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if _, err := os.Lstat(w.Dir); err != nil {
		t.Errorf("a cancelled sweep removed the folder: %v", err)
	}
}

func TestDiscardWork(t *testing.T) {
	t.Run("removes its own folder with the contents", func(t *testing.T) {
		e, workDir := newWorkEnv(t)
		w := mustCreateWork(t, e, testMeta(e, "A.docx"))
		other := mustCreateWork(t, e, testMeta(e, "B.docx"))
		write(t, filepath.Join(w.Dir, "media", "deep", "x.png"), "png", 0o600)
		write(t, filepath.Join(w.Dir, "doc.md"), "text", 0o600)
		if err := e.ops.DiscardWork(context.Background(), w); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(w.Dir); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("folder still there: %v", err)
		}
		if got := names(t, workDir); !reflect.DeepEqual(got, []string{filepath.Base(other.Dir)}) {
			t.Errorf("WorkDir = %q, want only the other copy", got)
		}
		if _, ok := mustFindWork(t, e, w.Meta.Origin); ok {
			t.Error("FindWork still finds the discarded copy")
		}
	})

	t.Run("a folder that does not exist is an error", func(t *testing.T) {
		e, workDir := newWorkEnv(t)
		if err := os.MkdirAll(workDir, 0o700); err != nil {
			t.Fatal(err)
		}
		err := e.ops.DiscardWork(context.Background(), WorkCopy{Dir: filepath.Join(workDir, "0123456789abcdef")})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("err = %v, want fs.ErrNotExist", err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		e, _ := newWorkEnv(t)
		w := mustCreateWork(t, e, testMeta(e, "A.docx"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := e.ops.DiscardWork(ctx, w); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if _, err := os.Lstat(w.Dir); err != nil {
			t.Errorf("folder removed: %v", err)
		}
	})
}

func TestDiscardWork_Refused(t *testing.T) {
	// Every case names something that must survive; setup returns the folder to
	// hand to DiscardWork and the files that must still be there afterwards.
	tests := []struct {
		name  string
		setup func(t *testing.T, e *env, workDir string) (dir string, survivors map[string]string)
	}{
		{"a folder outside WorkDir with a valid name", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			dir := filepath.Join(filepath.Dir(workDir), "elsewhere", "0123456789abcdef")
			write(t, filepath.Join(dir, "keep.txt"), "keep", 0o600)
			return dir, map[string]string{filepath.Join(dir, "keep.txt"): "keep"}
		}},
		{"WorkDir itself", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			w := mustCreateWork(t, e, testMeta(e, "A.docx"))
			return workDir, map[string]string{filepath.Join(w.Dir, "meta.json"): read(t, filepath.Join(w.Dir, "meta.json"))}
		}},
		{"the parent of WorkDir", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			w := mustCreateWork(t, e, testMeta(e, "A.docx"))
			return filepath.Dir(workDir), map[string]string{filepath.Join(w.Dir, "meta.json"): read(t, filepath.Join(w.Dir, "meta.json"))}
		}},
		{"a nested folder", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			w := mustCreateWork(t, e, testMeta(e, "A.docx"))
			write(t, filepath.Join(w.Dir, "media", "x.png"), "png", 0o600)
			return filepath.Join(w.Dir, "media"), map[string]string{filepath.Join(w.Dir, "media", "x.png"): "png"}
		}},
		{"a nested folder with a valid name", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			w := mustCreateWork(t, e, testMeta(e, "A.docx"))
			inner := filepath.Join(w.Dir, "0123456789abcdef")
			write(t, filepath.Join(inner, "x.txt"), "x", 0o600)
			return inner, map[string]string{filepath.Join(inner, "x.txt"): "x"}
		}},
		{"a link to a folder outside", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			outside := filepath.Join(filepath.Dir(workDir), "outside")
			write(t, filepath.Join(outside, "keep.txt"), "keep", 0o600)
			if err := os.MkdirAll(workDir, 0o700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(workDir, "0123456789abcdef")
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			return link, map[string]string{filepath.Join(outside, "keep.txt"): "keep"}
		}},
		{"a folder with a foreign name", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			dir := filepath.Join(workDir, "Documents")
			write(t, filepath.Join(dir, "keep.txt"), "keep", 0o600)
			return dir, map[string]string{filepath.Join(dir, "keep.txt"): "keep"}
		}},
		{"a name of the right length but not hex", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			dir := filepath.Join(workDir, "0123456789abcdeg")
			write(t, filepath.Join(dir, "keep.txt"), "keep", 0o600)
			return dir, map[string]string{filepath.Join(dir, "keep.txt"): "keep"}
		}},
		{"upper-case hex", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			dir := filepath.Join(workDir, "0123456789ABCDEF")
			write(t, filepath.Join(dir, "keep.txt"), "keep", 0o600)
			return dir, map[string]string{filepath.Join(dir, "keep.txt"): "keep"}
		}},
		{"a regular file with a valid name", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			f := filepath.Join(workDir, "0123456789abcdef")
			write(t, f, "keep", 0o600)
			return f, map[string]string{f: "keep"}
		}},
		{"an empty path", func(t *testing.T, e *env, workDir string) (string, map[string]string) {
			w := mustCreateWork(t, e, testMeta(e, "A.docx"))
			return "", map[string]string{filepath.Join(w.Dir, "meta.json"): read(t, filepath.Join(w.Dir, "meta.json"))}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, workDir := newWorkEnv(t)
			dir, survivors := tt.setup(t, e, workDir)
			err := e.ops.DiscardWork(context.Background(), WorkCopy{Dir: dir})
			if !errors.Is(err, ErrNotWork) {
				t.Fatalf("err = %v, want ErrNotWork", err)
			}
			for p, want := range survivors {
				if got := read(t, p); got != want {
					t.Errorf("%q = %q, want %q (something was deleted)", p, got, want)
				}
			}
		})
	}
}
