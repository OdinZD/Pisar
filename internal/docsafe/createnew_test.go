package docsafe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func nlinkOf(t *testing.T, p string) int {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Lstat(p, &st); err != nil {
		t.Fatal(err)
	}
	return int(st.Nlink)
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// createNew is CreateNew with a fresh source holding content.
func (e *env) createNew(dst, content string) (Snapshot, error) {
	e.t.Helper()
	return e.ops.CreateNew(context.Background(), e.src(content), dst)
}

func TestCreateNew_Success(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"small", "docx bytes"},
		{"binary", "PK\x03\x04\x00\xff\xfe"},
		{"several chunks", strings.Repeat("0123456789abcdef", (copyBufSize*2+100)/16)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, 3)
			src := e.src(tt.content)
			srcBefore := stateOf(t, src)
			dst := filepath.Join(e.user, "Kopija.docx")

			got, err := e.ops.CreateNew(context.Background(), src, dst)
			if err != nil {
				t.Fatal(err)
			}
			if read(t, dst) != tt.content {
				t.Error("content differs from the source")
			}
			if !got.Same(snap(t, dst)) || got.Size != int64(len(tt.content)) {
				t.Errorf("snapshot %+v does not describe the new file", got)
			}
			if m := modeOf(t, dst); m.Perm() != 0o644 || !m.IsRegular() {
				t.Errorf("mode = %v, want a regular 0644 file", m)
			}
			if n := nlinkOf(t, dst); n != 1 {
				t.Errorf("Nlink = %d, want 1", n)
			}
			if got := names(t, e.user); !reflect.DeepEqual(got, []string{"Kopija.docx"}) {
				t.Errorf("folder = %q, want only the new file (no temp file)", got)
			}
			if after := stateOf(t, src); !reflect.DeepEqual(after, srcBefore) {
				t.Errorf("the source changed: %+v", after)
			}
			if nlinkOf(t, src) != 1 {
				t.Error("the source got a second name")
			}
		})
	}
}

func TestCreateNew_ExistingDestination(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, e *env, dst string)
	}{
		{"a document", func(t *testing.T, e *env, dst string) {
			write(t, dst, "the user's text", 0o640)
			if err := os.Chmod(dst, 0o640); err != nil {
				t.Fatal(err)
			}
			setX(t, dst, "com.pisar.test", []byte("hello"))
		}},
		{"an empty file", func(t *testing.T, e *env, dst string) { write(t, dst, "", 0o640) }},
		{"a link to a missing file", func(t *testing.T, e *env, dst string) {
			if err := os.Symlink(filepath.Join(e.user, "nowhere"), dst); err != nil {
				t.Fatal(err)
			}
		}},
		{"a folder", func(t *testing.T, e *env, dst string) {
			write(t, filepath.Join(dst, "inside.txt"), "x", 0o644)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, 3)
			dst := filepath.Join(e.user, "Kopija.docx")
			tt.setup(t, e, dst)
			before := names(t, e.user)
			var beforeState fileState
			if fi, err := os.Lstat(dst); err == nil && fi.Mode().IsRegular() {
				beforeState = stateOf(t, dst)
			}

			snapshot, err := e.createNew(dst, "new")
			if !errors.Is(err, ErrExists) {
				t.Fatalf("err = %v, want ErrExists", err)
			}
			if snapshot != (Snapshot{}) {
				t.Errorf("snapshot = %+v, want zero", snapshot)
			}
			if got := names(t, e.user); !reflect.DeepEqual(got, before) {
				t.Errorf("folder = %q, want %q", got, before)
			}
			if fi, err := os.Lstat(dst); err == nil && fi.Mode().IsRegular() {
				if got := stateOf(t, dst); !reflect.DeepEqual(got, beforeState) {
					t.Errorf("the existing file changed:\n got %+v\nwant %+v", got, beforeState)
				}
			}
			if _, ok := e.ops.written[dst]; ok {
				t.Error("an existing file was recorded as written by Pisar")
			}
		})
	}
}

func TestCreateNew_CaseOnlyDifference(t *testing.T) {
	e := newEnv(t, 3)
	// What this volume does with names that differ in case decides the expectation.
	write(t, filepath.Join(e.user, "ProbeA"), "", 0o644)
	_, err := os.Lstat(filepath.Join(e.user, "probea"))
	insensitive := err == nil

	existing := e.doc("Pismo.docx", "the user's text")
	before := stateOf(t, existing)
	_, err = e.createNew(filepath.Join(e.user, "pismo.docx"), "new")
	if insensitive {
		if !errors.Is(err, ErrExists) {
			t.Fatalf("case-insensitive volume: err = %v, want ErrExists", err)
		}
		if got := stateOf(t, existing); !reflect.DeepEqual(got, before) {
			t.Errorf("the existing file changed: %+v", got)
		}
		return
	}
	if err != nil {
		t.Fatalf("case-sensitive volume: %v", err)
	}
	if read(t, existing) != "the user's text" || read(t, filepath.Join(e.user, "pismo.docx")) != "new" {
		t.Error("on a case-sensitive volume the two names are two files")
	}
}

func TestCreateNew_InvalidInput(t *testing.T) {
	longName := strings.Repeat("a", maxNameBytes) + ".docx"
	tests := []struct {
		name  string
		setup func(t *testing.T, e *env) (src, dst string)
		want  error // nil: any error
	}{
		{"hidden name", func(t *testing.T, e *env) (string, string) { return e.src("x"), filepath.Join(e.user, ".hidden.docx") }, ErrInvalidName},
		{"lock-file name", func(t *testing.T, e *env) (string, string) { return e.src("x"), filepath.Join(e.user, "~$Pismo.docx") }, ErrInvalidName},
		{"colon in the name", func(t *testing.T, e *env) (string, string) { return e.src("x"), filepath.Join(e.user, "a:b.docx") }, ErrInvalidName},
		{"trailing space", func(t *testing.T, e *env) (string, string) { return e.src("x"), filepath.Join(e.user, "Pismo.docx ") }, ErrInvalidName},
		{"name too long", func(t *testing.T, e *env) (string, string) { return e.src("x"), filepath.Join(e.user, longName) }, ErrInvalidName},
		{"relative destination", func(t *testing.T, e *env) (string, string) { return e.src("x"), "Pismo.docx" }, nil},
		{"destination is a folder path", func(t *testing.T, e *env) (string, string) { return e.src("x"), e.user + string(os.PathSeparator) }, nil},
		{"folder does not exist", func(t *testing.T, e *env) (string, string) {
			return e.src("x"), filepath.Join(e.user, "missing", "Pismo.docx")
		}, os.ErrNotExist},
		{"folder is a regular file", func(t *testing.T, e *env) (string, string) {
			write(t, filepath.Join(e.user, "file"), "x", 0o644)
			return e.src("x"), filepath.Join(e.user, "file", "Pismo.docx")
		}, nil},
		{"folder is a link", func(t *testing.T, e *env) (string, string) {
			real := filepath.Join(filepath.Dir(e.user), "real")
			if err := os.MkdirAll(real, 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(e.user, "link")
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
			return e.src("x"), filepath.Join(link, "Pismo.docx")
		}, nil},
		{"empty source", func(t *testing.T, e *env) (string, string) { return e.src(""), filepath.Join(e.user, "Pismo.docx") }, nil},
		{"missing source", func(t *testing.T, e *env) (string, string) {
			return filepath.Join(e.work, "nope.docx"), filepath.Join(e.user, "Pismo.docx")
		}, os.ErrNotExist},
		{"source is a folder", func(t *testing.T, e *env) (string, string) { return e.work, filepath.Join(e.user, "Pismo.docx") }, nil},
		{"source is a link", func(t *testing.T, e *env) (string, string) {
			link := filepath.Join(e.work, "link.docx")
			if err := os.Symlink(e.src("x"), link); err != nil {
				t.Fatal(err)
			}
			return link, filepath.Join(e.user, "Pismo.docx")
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, 3)
			src, dst := tt.setup(t, e)
			userBefore := names(t, e.user)
			realBefore := names(t, filepath.Join(filepath.Dir(e.user)))

			got, err := e.ops.CreateNew(context.Background(), src, dst)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if got != (Snapshot{}) {
				t.Errorf("snapshot = %+v, want zero", got)
			}
			if after := names(t, e.user); !reflect.DeepEqual(after, userBefore) {
				t.Errorf("user's folder = %q, want %q", after, userBefore)
			}
			if after := names(t, filepath.Dir(e.user)); !reflect.DeepEqual(after, realBefore) {
				t.Errorf("parent folder = %q, want %q", after, realBefore)
			}
			if real := filepath.Join(filepath.Dir(e.user), "real"); dirExists(real) {
				if got := names(t, real); len(got) != 0 {
					t.Errorf("the folder behind the link got %q", got)
				}
			}
		})
	}
}

// newStages is the order CreateNew visits its stages on a volume with hard links.
var newStages = []string{
	stageNewTemp, stageNewCopy, stageNewChmod, stageNewFsync, stageNewClose,
	stageNewLink, stageNewUnlink, stageNewFsyncDir,
}

func TestCreateNew_StagesAreVisitedInOrder(t *testing.T) {
	e := newEnv(t, 3)
	var seen []string
	e.ops.fail = func(s string) error { seen = append(seen, s); return nil }
	if _, err := e.createNew(filepath.Join(e.user, "Kopija.docx"), "x"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, newStages) {
		t.Errorf("stages = %q, want %q", seen, newStages)
	}
}

// TestCreateNew_FaultAtEveryStage is the "power cut" test: before the name is
// claimed there is no dst and no stray file; after it, dst is whole.
func TestCreateNew_FaultAtEveryStage(t *testing.T) {
	for _, stage := range newStages {
		t.Run(stage, func(t *testing.T) {
			e := newEnv(t, 3)
			neighbour := e.doc("Susjed.docx", "somebody else's text")
			neighbourBefore := stateOf(t, neighbour)
			dst := filepath.Join(e.user, "Kopija.docx")
			e.ops.fail = func(s string) error {
				if s == stage {
					return errInjected
				}
				return nil
			}

			got, err := e.createNew(dst, "new content")
			if !errors.Is(err, errInjected) {
				t.Fatalf("err = %v, want the injected fault", err)
			}
			if after := stateOf(t, neighbour); !reflect.DeepEqual(after, neighbourBefore) {
				t.Errorf("an existing file changed: %+v", after)
			}
			if stage == stageNewUnlink || stage == stageNewFsyncDir {
				// The name is claimed: the file is whole, has one name, and the result says so.
				if read(t, dst) != "new content" || nlinkOf(t, dst) != 1 {
					t.Errorf("dst = %q with %d names, want the new content and 1", read(t, dst), nlinkOf(t, dst))
				}
				if !got.Same(snap(t, dst)) {
					t.Errorf("snapshot %+v does not describe the new file", got)
				}
				if want := []string{"Kopija.docx", "Susjed.docx"}; !reflect.DeepEqual(names(t, e.user), want) {
					t.Errorf("folder = %q, want %q", names(t, e.user), want)
				}
				return
			}
			if got != (Snapshot{}) {
				t.Errorf("a failed create returned a snapshot: %+v", got)
			}
			if want := []string{"Susjed.docx"}; !reflect.DeepEqual(names(t, e.user), want) {
				t.Errorf("folder = %q, want %q (dst or a temp file is left)", names(t, e.user), want)
			}
			if _, ok := e.ops.written[dst]; ok {
				t.Error("a failed create was recorded as written")
			}
		})
	}
}

func TestCreateNew_Cancellation(t *testing.T) {
	t.Run("before the start", func(t *testing.T) {
		e := newEnv(t, 3)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := e.ops.CreateNew(ctx, e.src("x"), filepath.Join(e.user, "Kopija.docx"))
		if !errors.Is(err, context.Canceled) || len(names(t, e.user)) != 0 {
			t.Errorf("err = %v, folder %q; want context.Canceled and nothing created", err, names(t, e.user))
		}
	})
	t.Run("in the middle", func(t *testing.T) {
		e := newEnv(t, 3)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.ops.fail = func(s string) error {
			if s == stageNewFsync {
				cancel()
			}
			return nil
		}
		_, err := e.ops.CreateNew(ctx, e.src("x"), filepath.Join(e.user, "Kopija.docx"))
		if !errors.Is(err, context.Canceled) || len(names(t, e.user)) != 0 {
			t.Errorf("err = %v, folder %q; want context.Canceled and nothing left", err, names(t, e.user))
		}
	})
	t.Run("after the name is claimed", func(t *testing.T) {
		// The file is whole by then; cutting the clean-up short would leave a second name.
		e := newEnv(t, 3)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.ops.fail = func(s string) error {
			if s == stageNewUnlink {
				cancel()
			}
			return nil
		}
		dst := filepath.Join(e.user, "Kopija.docx")
		got, err := e.ops.CreateNew(ctx, e.src("x"), dst)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Same(snap(t, dst)) || nlinkOf(t, dst) != 1 || len(names(t, e.user)) != 1 {
			t.Errorf("result %+v, %d names, folder %q", got, nlinkOf(t, dst), names(t, e.user))
		}
	})
}

// noLinks makes os.Link fail the way a volume without hard links does.
func noLinks(oldname, newname string) error {
	return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: unix.ENOTSUP}
}

func TestCreateNew_VolumeWithoutHardLinks(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		e := newEnv(t, 3)
		e.ops.link = noLinks
		dst := filepath.Join(e.user, "Kopija.docx")
		got, err := e.createNew(dst, "new")
		if err != nil {
			t.Fatal(err)
		}
		if read(t, dst) != "new" || nlinkOf(t, dst) != 1 || modeOf(t, dst).Perm() != 0o644 || !got.Same(snap(t, dst)) {
			t.Errorf("dst = %q, names %d, mode %v, snapshot %+v", read(t, dst), nlinkOf(t, dst), modeOf(t, dst), got)
		}
		if names := names(t, e.user); !reflect.DeepEqual(names, []string{"Kopija.docx"}) {
			t.Errorf("folder = %q", names)
		}
	})
	t.Run("existing destination", func(t *testing.T) {
		e := newEnv(t, 3)
		e.ops.link = noLinks
		dst := e.doc("Kopija.docx", "the user's text")
		before := stateOf(t, dst)
		_, err := e.ops.CreateNew(context.Background(), e.src("new"), dst)
		// The early check answers first; either way nothing is replaced.
		if !errors.Is(err, ErrExists) || !reflect.DeepEqual(stateOf(t, dst), before) {
			t.Errorf("err = %v, state %+v", err, stateOf(t, dst))
		}
	})
	t.Run("a file appears after the early check", func(t *testing.T) {
		// The window the fallback has to close by checking again before it renames.
		e := newEnv(t, 3)
		dst := filepath.Join(e.user, "Kopija.docx")
		e.ops.link = func(oldname, newname string) error {
			write(t, dst, "somebody else's text", 0o644)
			return noLinks(oldname, newname)
		}
		_, err := e.createNew(dst, "new")
		if !errors.Is(err, ErrExists) || read(t, dst) != "somebody else's text" {
			t.Errorf("err = %v, dst = %q", err, read(t, dst))
		}
		if got := names(t, e.user); !reflect.DeepEqual(got, []string{"Kopija.docx"}) {
			t.Errorf("folder = %q, want no temp file", got)
		}
	})
	t.Run("fault before the rename", func(t *testing.T) {
		e := newEnv(t, 3)
		e.ops.link = noLinks
		e.ops.fail = func(s string) error {
			if s == stageNewRename {
				return errInjected
			}
			return nil
		}
		if _, err := e.createNew(filepath.Join(e.user, "Kopija.docx"), "new"); !errors.Is(err, errInjected) {
			t.Fatalf("err = %v, want the injected fault", err)
		}
		if got := names(t, e.user); len(got) != 0 {
			t.Errorf("folder = %q, want empty", got)
		}
	})
	t.Run("another link error is not mistaken for a missing feature", func(t *testing.T) {
		// EXDEV and friends also take the fallback; what matters is that it still
		// ends with a whole file or a clean folder.
		e := newEnv(t, 3)
		e.ops.link = func(oldname, newname string) error {
			return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: unix.EXDEV}
		}
		dst := filepath.Join(e.user, "Kopija.docx")
		if _, err := e.createNew(dst, "new"); err != nil {
			t.Fatal(err)
		}
		if read(t, dst) != "new" || len(names(t, e.user)) != 1 {
			t.Errorf("dst = %q, folder %q", read(t, dst), names(t, e.user))
		}
	})
}

func TestCreateNew_FirstReplaceNeedsNoBackup(t *testing.T) {
	t.Run("untouched since CreateNew", func(t *testing.T) {
		e := newEnv(t, 3)
		dst := filepath.Join(e.user, "Kopija.docx")
		snapshot, err := e.createNew(dst, "v1")
		if err != nil {
			t.Fatal(err)
		}
		res, err := e.ops.Replace(context.Background(), e.src("v2"), dst, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if res.Backup != "" {
			t.Errorf("Backup = %q, want none: the content was Pisar's own", res.Backup)
		}
		if _, err := os.Stat(e.backups); err == nil {
			if got := backupFiles(t, e.backups); len(got) != 0 {
				t.Errorf("backups = %q, want none", got)
			}
		}
		if read(t, dst) != "v2" {
			t.Errorf("content = %q", read(t, dst))
		}
	})
	t.Run("changed by another program since CreateNew", func(t *testing.T) {
		e := newEnv(t, 3)
		dst := filepath.Join(e.user, "Kopija.docx")
		if _, err := e.createNew(dst, "v1"); err != nil {
			t.Fatal(err)
		}
		write(t, dst, "edited elsewhere", 0o644)
		res, err := e.replace(dst, "v2")
		if err != nil {
			t.Fatal(err)
		}
		if res.Backup == "" || read(t, res.Backup) != "edited elsewhere" {
			t.Errorf("Backup = %q, want a backup of the foreign edit", res.Backup)
		}
	})
}

func TestCreateNew_ConcurrentSameName(t *testing.T) {
	e := newEnv(t, 3)
	// Two Ops, as two processes would be: only the file system arbitrates.
	other, err := New(Options{TrashBin: "/fake/trash", Timeout: time.Second, BackupsDir: e.backups, MaxBackups: 3})
	if err != nil {
		t.Fatal(err)
	}
	ops := []*Ops{e.ops, other}

	const n = 16
	srcs := make([]string, n)
	for i := range srcs {
		srcs[i] = e.src("content of " + strconv.Itoa(i))
	}
	dst := filepath.Join(e.user, "Kopija.docx")
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = ops[i%2].CreateNew(context.Background(), srcs[i], dst)
		}()
	}
	close(start)
	wg.Wait()

	winner := -1
	for i, err := range errs {
		switch {
		case err == nil:
			if winner != -1 {
				t.Fatalf("both %d and %d succeeded", winner, i)
			}
			winner = i
		case !errors.Is(err, ErrExists):
			t.Errorf("call %d: %v, want ErrExists", i, err)
		}
	}
	if winner == -1 {
		t.Fatal("nobody succeeded")
	}
	if got, want := read(t, dst), "content of "+strconv.Itoa(winner); got != want {
		t.Errorf("content = %q, want the winner's %q", got, want)
	}
	if nlinkOf(t, dst) != 1 || !reflect.DeepEqual(names(t, e.user), []string{"Kopija.docx"}) {
		t.Errorf("names %d, folder %q; want one name and no temp files", nlinkOf(t, dst), names(t, e.user))
	}
}
