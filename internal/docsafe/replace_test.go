package docsafe

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// testClock hands out one instant per call, step apart.
type testClock struct {
	mu   sync.Mutex
	cur  time.Time
	step time.Duration
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.cur
	c.cur = c.cur.Add(c.step)
	return n
}

// env is a "user's folder", Pisar's backup folder and Pisar's work folder, all
// separate sub-folders of one temp dir.
type env struct {
	t                   *testing.T
	user, backups, work string
	ops                 *Ops
	clock               *testClock
	nsrc                int
}

func newEnv(t *testing.T, maxBackups int) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{
		t:       t,
		user:    filepath.Join(root, "user"),
		backups: filepath.Join(root, "pisar", "backups"),
		work:    filepath.Join(root, "pisar", "work"),
		clock:   &testClock{cur: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), step: time.Second},
	}
	for _, d := range []string{e.user, e.work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ops, err := New(Options{
		TrashBin: "/fake/trash", Timeout: time.Second,
		BackupsDir: e.backups, MaxBackups: maxBackups, Now: e.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.ops = ops
	return e
}

// doc creates a document in the user's folder with mode 0640 (umask-proof).
func (e *env) doc(name, content string) string {
	e.t.Helper()
	p := filepath.Join(e.user, name)
	write(e.t, p, content, 0o640)
	if err := os.Chmod(p, 0o640); err != nil {
		e.t.Fatal(err)
	}
	return p
}

// src creates a new file in the work folder, as pandoc's output would be.
func (e *env) src(content string) string {
	e.t.Helper()
	e.nsrc++
	p := filepath.Join(e.work, "out"+string(rune('a'+e.nsrc))+".docx")
	write(e.t, p, content, 0o644)
	return p
}

// replace saves content over dst the way a caller that is up to date does.
func (e *env) replace(dst, content string) (ReplaceResult, error) {
	e.t.Helper()
	return e.ops.Replace(context.Background(), e.src(content), dst, snap(e.t, dst))
}

func snap(t *testing.T, p string) Snapshot {
	t.Helper()
	s, err := TakeSnapshot(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// xattrsOf lists all attributes of p through the path-based API, independent of
// the code under test.
func xattrsOf(t *testing.T, p string, withKernel bool) map[string]string {
	t.Helper()
	size, err := unix.Listxattr(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	if size == 0 {
		return out
	}
	buf := make([]byte, size)
	if size, err = unix.Listxattr(p, buf); err != nil {
		t.Fatal(err)
	}
	for _, n := range strings.Split(strings.TrimSuffix(string(buf[:size]), "\x00"), "\x00") {
		if kernelManaged[n] && !withKernel {
			continue
		}
		vsz, err := unix.Getxattr(p, n, nil)
		if err != nil {
			t.Fatal(err)
		}
		v := make([]byte, vsz)
		if vsz, err = unix.Getxattr(p, n, v); err != nil {
			t.Fatal(err)
		}
		out[n] = string(v[:vsz])
	}
	return out
}

func setX(t *testing.T, p, name string, v []byte) {
	t.Helper()
	if err := unix.Setxattr(p, name, v, 0); err != nil {
		t.Fatalf("setxattr %s on %s: %v", name, p, err)
	}
}

// fileState is everything that must survive a failed save.
type fileState struct {
	Content string
	Mode    os.FileMode
	Xattrs  map[string]string
}

func stateOf(t *testing.T, p string) fileState {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fileState{Content: read(t, p), Mode: fi.Mode(), Xattrs: xattrsOf(t, p, true)}
}

// binaryValue has a NUL and bytes that are not UTF-8.
var binaryValue = []byte{0x00, 0xff, 0xfe, 0x01, 0x00, 0x80}

// decorate gives p the attributes a real document may have.
func decorate(t *testing.T, p string) {
	t.Helper()
	setX(t, p, "com.pisar.test", []byte("hello"))
	setX(t, p, "com.pisar.binary", binaryValue)
	setX(t, p, "com.pisar.empty", nil)
	setX(t, p, "com.apple.quarantine", []byte("0081;00000000;Pisar;"))
	setX(t, p, "com.apple.FinderInfo", append(make([]byte, 8), 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18))
	setX(t, p, "com.apple.ResourceFork", []byte("resource fork bytes"))
}

func TestReplace_Success(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("Pismo.docx", "old content")
	decorate(t, dst)
	wantX := xattrsOf(t, dst, false)
	src := e.src("new content")
	srcBefore := stateOf(t, src)
	before := snap(t, dst)

	res, err := e.ops.Replace(context.Background(), src, dst, before)
	if err != nil {
		t.Fatal(err)
	}

	if got := read(t, dst); got != "new content" {
		t.Errorf("content = %q", got)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}
	// com.apple.provenance is the kernel's; every other attribute must be there, intact.
	if got := xattrsOf(t, dst, false); !reflect.DeepEqual(got, wantX) {
		t.Errorf("xattrs = %q, want %q", got, wantX)
	}
	if !reflect.DeepEqual(stateOf(t, src), srcBefore) {
		t.Error("the source file changed")
	}
	if got := names(t, e.user); !reflect.DeepEqual(got, []string{"Pismo.docx"}) {
		t.Errorf("folder = %q, want only the document", got)
	}
	if now := snap(t, dst); !res.Snapshot.Same(now) || !res.Snapshot.ModTime.Equal(now.ModTime) {
		t.Errorf("result snapshot %+v is not the file as it is (%+v)", res.Snapshot, now)
	}
	if res.Backup == "" || read(t, res.Backup) != "old content" {
		t.Errorf("backup %q does not hold the old content", res.Backup)
	}
}

func TestReplace_MultiChunkAndEmptyAttributeSet(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("big.docx", "old")
	big := strings.Repeat("0123456789abcdef", 3*copyBufSize/16) + "tail" // more than two chunks
	if _, err := e.replace(dst, big); err != nil {
		t.Fatal(err)
	}
	if read(t, dst) != big {
		t.Error("content differs after a multi-chunk copy")
	}
	if got := xattrsOf(t, dst, false); len(got) != 0 {
		t.Errorf("a file without attributes ended up with %q", got)
	}
}

// TestReplace_TempFile looks at the temp file while it exists.
func TestReplace_TempFile(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("Pismo.docx", "old")
	var seen []string
	e.ops.fail = func(stage string) error {
		if stage != stageCreateTemp {
			return nil
		}
		for _, n := range names(t, e.user) {
			if n == "Pismo.docx" {
				continue
			}
			seen = append(seen, n)
			fi, err := os.Lstat(filepath.Join(e.user, n))
			if err != nil {
				t.Error(err)
				continue
			}
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("temp mode = %v, want 0600", fi.Mode().Perm())
			}
		}
		return nil
	}
	if _, err := e.replace(dst, "new"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("temp files seen: %q", seen)
	}
	n := seen[0]
	if !strings.HasPrefix(n, ".") || !strings.Contains(n, "Pismo.docx") || strings.HasSuffix(strings.ToLower(n), ".docx") {
		t.Errorf("temp name %q: want hidden, containing the document name, not ending in .docx", n)
	}
}

var errInjected = errors.New("injected fault")

// allStages is every injection point in the order Replace passes them.
var allStages = []string{
	stageBackup, stageCreateTemp, stageCopy, stageXattr, stageChmod,
	stageFsync, stageClose, stageRecheck, stageRename, stageFsyncDir,
}

func TestReplace_StagesAreVisitedInOrder(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("a.docx", "old")
	var seen []string
	e.ops.fail = func(stage string) error { seen = append(seen, stage); return nil }
	if _, err := e.replace(dst, "new"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, allStages) {
		t.Errorf("stages = %q, want %q", seen, allStages)
	}
}

// TestReplace_FaultAtEveryStage is the "power cut" test: whatever fails and
// whenever, the document is as it was and nothing is left beside it.
func TestReplace_FaultAtEveryStage(t *testing.T) {
	for _, stage := range allStages {
		t.Run(stage, func(t *testing.T) {
			e := newEnv(t, 5)
			dst := e.doc("Pismo.docx", "old content")
			decorate(t, dst)
			before := stateOf(t, dst)
			folderBefore := names(t, e.user)
			e.ops.fail = func(s string) error {
				if s != stage {
					return nil
				}
				if s == stageBackup {
					// The backup must exist, complete, while dst is still the old file.
					files := backupFiles(t, e.backups)
					if len(files) != 1 || read(t, files[0]) != "old content" {
						t.Errorf("at %q: backups = %q, want one with the old content", s, files)
					}
					if got := stateOf(t, dst); !reflect.DeepEqual(got, before) {
						t.Errorf("at %q: dst already changed: %+v", s, got)
					}
				}
				return errInjected
			}

			res, err := e.replace(dst, "new content")
			if !errors.Is(err, errInjected) {
				t.Fatalf("err = %v, want the injected fault", err)
			}
			if got := names(t, e.user); !reflect.DeepEqual(got, folderBefore) {
				t.Errorf("folder = %q, want %q", got, folderBefore)
			}
			if stage == stageFsyncDir {
				// Past the rename: the new, complete content is in place and the
				// error still comes out, with a result that describes the file.
				if got := read(t, dst); got != "new content" {
					t.Errorf("content = %q, want the new content", got)
				}
				if !res.Snapshot.Same(snap(t, dst)) {
					t.Errorf("result snapshot %+v does not describe the new file", res.Snapshot)
				}
				if got := xattrsOf(t, dst, false); !reflect.DeepEqual(got, withoutKernel(before.Xattrs)) {
					t.Errorf("xattrs = %q", got)
				}
				return
			}
			if got := stateOf(t, dst); !reflect.DeepEqual(got, before) {
				t.Errorf("dst changed:\n got %+v\nwant %+v", got, before)
			}
			if res.Snapshot != (Snapshot{}) {
				t.Errorf("a failed save returned a snapshot: %+v", res.Snapshot)
			}
		})
	}
}

func withoutKernel(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if !kernelManaged[k] {
			out[k] = v
		}
	}
	return out
}

// backupFiles lists every backup file under dir, sorted.
func backupFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == dir {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func TestReplace_FailedSaveCanBeRetried(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("a.docx", "old")
	e.ops.fail = func(s string) error {
		if s == stageRename {
			return errInjected
		}
		return nil
	}
	if _, err := e.replace(dst, "new"); !errors.Is(err, errInjected) {
		t.Fatal(err)
	}
	e.ops.fail = nil
	if _, err := e.replace(dst, "new"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if read(t, dst) != "new" {
		t.Error("the retry did not write")
	}
}

func TestReplace_Cancellation(t *testing.T) {
	t.Run("before the start", func(t *testing.T) {
		e := newEnv(t, 5)
		dst := e.doc("a.docx", "old")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := e.ops.Replace(ctx, e.src("new"), dst, snap(t, dst))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		if read(t, dst) != "old" || len(backupFiles(t, e.backups)) != 0 {
			t.Error("something was touched")
		}
	})
	t.Run("in the middle of the copy", func(t *testing.T) {
		e := newEnv(t, 5)
		dst := e.doc("a.docx", "old")
		before := stateOf(t, dst)
		ctx, cancel := context.WithCancel(context.Background())
		e.ops.fail = func(s string) error {
			if s == stageCopy {
				cancel() // the next chunk read sees it
			}
			return nil
		}
		big := strings.Repeat("x", 3*copyBufSize)
		_, err := e.ops.Replace(ctx, e.src(big), dst, snap(t, dst))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		if got := stateOf(t, dst); !reflect.DeepEqual(got, before) {
			t.Error("dst changed")
		}
		if got := names(t, e.user); !reflect.DeepEqual(got, []string{"a.docx"}) {
			t.Errorf("folder = %q", got)
		}
	})
	t.Run("after the rename the bookkeeping still completes", func(t *testing.T) {
		e := newEnv(t, 5)
		dst := e.doc("a.docx", "old")
		ctx, cancel := context.WithCancel(context.Background())
		e.ops.fail = func(s string) error {
			if s == stageRename {
				cancel() // the rename itself does not look at ctx; what follows must not either
			}
			return nil
		}
		res, err := e.ops.Replace(ctx, e.src("new"), dst, snap(t, dst))
		if err != nil {
			t.Fatal(err)
		}
		if !res.Snapshot.Same(snap(t, dst)) {
			t.Error("snapshot does not describe the new file")
		}
	})
}

func TestReplace_ConflictBeforeStart(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, dst string)
	}{
		{"content changed, same size", func(t *testing.T, dst string) { write(t, dst, "OLD", 0o640) }},
		{"content grew", func(t *testing.T, dst string) { write(t, dst, "old and more", 0o640) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, 5)
			dst := e.doc("a.docx", "old")
			expect := snap(t, dst)
			tt.change(t, dst)
			before := stateOf(t, dst)

			_, err := e.ops.Replace(context.Background(), e.src("new"), dst, expect)
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("err = %v, want ErrConflict", err)
			}
			if got := stateOf(t, dst); !reflect.DeepEqual(got, before) {
				t.Error("dst changed")
			}
			if got := backupFiles(t, e.backups); len(got) != 0 {
				t.Errorf("backups = %q, want none", got)
			}
			if got := names(t, e.user); !reflect.DeepEqual(got, []string{"a.docx"}) {
				t.Errorf("folder = %q", got)
			}
		})
	}
}

func TestReplace_ZeroExpectIsConflict(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("a.docx", "old")
	if _, err := e.ops.Replace(context.Background(), e.src("new"), dst, Snapshot{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestReplace_TouchIsNotAConflict(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("a.docx", "old")
	expect := snap(t, dst)
	later := expect.ModTime.Add(48 * time.Hour)
	if err := os.Chtimes(dst, later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ops.Replace(context.Background(), e.src("new"), dst, expect); err != nil {
		t.Fatal(err)
	}
	if read(t, dst) != "new" {
		t.Error("not replaced")
	}
}

func TestReplace_ConflictDuringWork(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("a.docx", "old")
	// Another program writes dst after the first check, before the second.
	e.ops.fail = func(s string) error {
		if s == stageRecheck {
			write(t, dst, "written by Word", 0o640)
		}
		return nil
	}
	_, err := e.replace(dst, "new")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if read(t, dst) != "written by Word" {
		t.Error("the other program's content was overwritten")
	}
	if got := names(t, e.user); !reflect.DeepEqual(got, []string{"a.docx"}) {
		t.Errorf("folder = %q, want only the document", got)
	}
}

func TestReplace_MissingDestination(t *testing.T) {
	e := newEnv(t, 5)
	dst := filepath.Join(e.user, "gone.docx")
	_, err := e.ops.Replace(context.Background(), e.src("new"), dst, Snapshot{Size: 1})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
	if got := names(t, e.user); len(got) != 0 {
		t.Errorf("folder = %q, want empty: a missing document must not be created", got)
	}
}

func TestReplace_ReadOnly(t *testing.T) {
	for _, tt := range roCases() {
		if tt.want == ReadOnlyNone {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, 5)
			dst := tt.setup(t, e.user)
			// What else lives in the folder must be untouched too.
			beforeNames := names(t, filepath.Dir(dst))
			var beforeState fileState
			var other string
			if tt.want == ReadOnlyHardLinks {
				other = filepath.Join(e.user, "other.docx")
				beforeState = stateOf(t, other)
			}
			src := e.src("new")

			_, err := e.ops.Replace(context.Background(), src, dst, Snapshot{})
			if !errors.Is(err, ErrReadOnly) {
				t.Fatalf("err = %v, want ErrReadOnly", err)
			}
			var ro *ReadOnlyError
			if !errors.As(err, &ro) || ro.Reason != tt.want {
				t.Fatalf("reason = %+v, want %q", ro, tt.want)
			}
			if !strings.Contains(err.Error(), string(tt.want)) {
				t.Errorf("message %q does not say the reason", err)
			}
			if got := names(t, filepath.Dir(dst)); !reflect.DeepEqual(got, beforeNames) {
				t.Errorf("folder = %q, want %q", got, beforeNames)
			}
			if got := backupFiles(t, e.backups); len(got) != 0 {
				t.Errorf("backups = %q, want none", got)
			}
			if tt.want == ReadOnlyHardLinks {
				// The other name still sees the same file.
				if got := stateOf(t, other); !reflect.DeepEqual(got, beforeState) {
					t.Errorf("other link changed: %+v", got)
				}
				if read(t, dst) != "x" {
					t.Error("dst changed")
				}
			}
			if tt.want == ReadOnlyNotRegular && tt.name == "symbolic link" {
				if read(t, filepath.Join(e.user, "target.docx")) != "x" {
					t.Error("the link target changed")
				}
				if fi, err := os.Lstat(dst); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
					t.Errorf("the link was replaced: %v %v", fi, err)
				}
			}
		})
	}
}

func TestReplace_BadSource(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, e *env, dst string) string
	}{
		{"missing", func(t *testing.T, e *env, dst string) string { return filepath.Join(e.work, "none.docx") }},
		{"empty", func(t *testing.T, e *env, dst string) string {
			p := filepath.Join(e.work, "empty.docx")
			write(t, p, "", 0o644)
			return p
		}},
		{"folder", func(t *testing.T, e *env, dst string) string { return e.work }},
		{"symbolic link", func(t *testing.T, e *env, dst string) string {
			p := filepath.Join(e.work, "link.docx")
			if err := os.Symlink(e.src("content"), p); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"the destination itself", func(t *testing.T, e *env, dst string) string { return dst }},
		{"a second name of the destination", func(t *testing.T, e *env, dst string) string {
			// Hard link: Replace refuses it earlier as hard-links, either way an error.
			p := filepath.Join(e.work, "alias.docx")
			if err := os.Link(dst, p); err != nil {
				t.Fatal(err)
			}
			return p
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, 5)
			dst := e.doc("a.docx", "old")
			before := stateOf(t, dst)
			src := tt.setup(t, e, dst)
			_, err := e.ops.Replace(context.Background(), src, dst, snap(t, dst))
			if err == nil {
				t.Fatal("want an error")
			}
			if got := stateOf(t, dst); !reflect.DeepEqual(got, before) {
				t.Error("dst changed")
			}
			if got := backupFiles(t, e.backups); len(got) != 0 {
				t.Errorf("backups = %q, want none: a bad source must be refused before anything is done", got)
			}
		})
	}
}

func TestReplace_Options(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"no BackupsDir", Options{TrashBin: "t", Timeout: time.Second, MaxBackups: 3}},
		{"no MaxBackups", Options{TrashBin: "t", Timeout: time.Second, BackupsDir: "b"}},
		{"negative MaxBackups", Options{TrashBin: "t", Timeout: time.Second, BackupsDir: "b", MaxBackups: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := New(tt.opts) // New does not ask for them: older callers keep working
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			dst := filepath.Join(dir, "a.docx")
			write(t, dst, "old", 0o644)
			src := filepath.Join(dir, "src.docx")
			write(t, src, "new", 0o644)
			if _, err := o.Replace(context.Background(), src, dst, snap(t, dst)); err == nil {
				t.Fatal("want an error")
			}
			if read(t, dst) != "old" {
				t.Error("dst changed")
			}
		})
	}
}

func TestReplace_LongNames(t *testing.T) {
	e := newEnv(t, 5)
	// 240 bytes: valid on APFS, but with a prefix the derived names would not fit
	// unless they are shortened (and "š" is two bytes, so the cut must not split it).
	name := strings.Repeat("š", 117) + "a.docx"
	dst := e.doc(name, "old")
	if _, err := e.replace(dst, "new"); err != nil {
		t.Fatal(err)
	}
	if read(t, dst) != "new" {
		t.Error("not replaced")
	}
	if got := backupFiles(t, e.backups); len(got) != 1 {
		t.Errorf("backups = %q", got)
	}
}

func TestReplace_ConcurrentCallsAreSerialized(t *testing.T) {
	e := newEnv(t, 50)
	type job struct{ src, dst string }
	var jobs []job
	for i := range 4 {
		d := e.doc("d"+string(rune('a'+i))+".docx", "old")
		// Two callers per document, both holding the same snapshot of it.
		jobs = append(jobs, job{e.src("new0"), d}, job{e.src("new1"), d})
	}
	expects := map[string]Snapshot{}
	for _, j := range jobs {
		expects[j.dst] = snap(t, j.dst)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(jobs))
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The loser of a pair gets ErrConflict; nothing else may go wrong.
			_, err := e.ops.Replace(context.Background(), j.src, j.dst, expects[j.dst])
			if err != nil && !errors.Is(err, ErrConflict) {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for _, j := range jobs {
		if c := read(t, j.dst); c != "new0" && c != "new1" {
			t.Errorf("%s = %q", j.dst, c)
		}
	}
	for _, n := range names(t, e.user) {
		if strings.HasPrefix(n, ".") {
			t.Errorf("stray file %q", n)
		}
	}
}

func TestCopyXattrs(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "from")
	to := filepath.Join(dir, "to")
	write(t, from, "a", 0o644)
	write(t, to, "b", 0o644)
	decorate(t, from)
	// "to" starts with an attribute the original does not have, and another
	// with a different value.
	setX(t, to, "com.pisar.extra", []byte("should go"))
	setX(t, to, "com.pisar.test", []byte("different"))

	src, _, err := openRegular(from)
	if err != nil {
		t.Fatal(err)
	}
	want, err := readXattrs(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenFile(to, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyXattrs(dst, want); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	if got, wantMap := xattrsOf(t, to, false), xattrsOf(t, from, false); !reflect.DeepEqual(got, wantMap) {
		t.Errorf("xattrs = %q, want %q", got, wantMap)
	}
}

func TestListXattrs_None(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plain")
	write(t, p, "a", 0o644)
	f := mustOpen(t, p)
	got, err := readXattrs(f)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %q, %v; want none (the kernel's provenance is not listed)", got, err)
	}
}

func TestFileLabel(t *testing.T) {
	tests := []struct {
		name, in string
		wantLen  int
	}{
		{"short stays", "a.docx", 6},
		{"long ASCII is cut", strings.Repeat("a", 300), maxLabelBytes},
		{"two-byte characters are not split", strings.Repeat("š", 100), maxLabelBytes}, // 200 bytes, cut at 100 = 50 characters
		{"cut would split a character", "a" + strings.Repeat("š", 100), maxLabelBytes - 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileLabel(tt.in)
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
			if !strings.HasPrefix(tt.in, got) {
				t.Errorf("%q is not a prefix of the input", got)
			}
		})
	}
}
