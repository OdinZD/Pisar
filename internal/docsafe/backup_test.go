package docsafe

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// external is another program writing the document: Word, a sync client.
func external(t *testing.T, p, content string) {
	t.Helper()
	write(t, p, content, 0o640)
}

func contents(t *testing.T, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		out = append(out, read(t, f))
	}
	return out
}

func TestBackup_MadeOnFirstSaveOnly(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("Pismo.docx", "v0")

	r1, err := e.replace(dst, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Backup == "" || read(t, r1.Backup) != "v0" {
		t.Fatalf("first save: backup %q, want a copy of v0", r1.Backup)
	}

	// Saving again over what Pisar itself wrote needs no further backup.
	r2, err := e.ops.Replace(context.Background(), e.src("v2"), dst, r1.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Backup != "" {
		t.Errorf("second save made a backup: %q", r2.Backup)
	}
	r3, err := e.replace(dst, "v3")
	if err != nil {
		t.Fatal(err)
	}
	if r3.Backup != "" {
		t.Errorf("third save made a backup: %q", r3.Backup)
	}
	if got := contents(t, backupFiles(t, e.backups)); !reflect.DeepEqual(got, []string{"v0"}) {
		t.Errorf("backups = %q, want just v0", got)
	}
}

func TestBackup_ExternalChangeGetsABackup(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("Pismo.docx", "v0")
	if _, err := e.replace(dst, "v1"); err != nil {
		t.Fatal(err)
	}

	// Word changes the file. The caller asked the user, who said "overwrite",
	// and calls again with a fresh snapshot: the foreign text is backed up.
	external(t, dst, "typed in Word")
	res, err := e.replace(dst, "v2")
	if err != nil {
		t.Fatal(err)
	}
	if res.Backup == "" || read(t, res.Backup) != "typed in Word" {
		t.Fatalf("backup %q, want a copy of the foreign text", res.Backup)
	}
	if got := contents(t, backupFiles(t, e.backups)); !reflect.DeepEqual(got, []string{"v0", "typed in Word"}) {
		t.Errorf("backups = %q", got)
	}
}

func TestBackup_ConflictThenOverwriteKeepsTheForeignContent(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("Pismo.docx", "v0")
	stale := snap(t, dst)
	if _, err := e.replace(dst, "v1"); err != nil {
		t.Fatal(err)
	}
	external(t, dst, "typed in Word")

	if _, err := e.ops.Replace(context.Background(), e.src("v2"), dst, stale); err == nil {
		t.Fatal("want a conflict")
	}
	if read(t, dst) != "typed in Word" {
		t.Fatal("the conflict touched the file")
	}
	if _, err := e.replace(dst, "v2"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range contents(t, backupFiles(t, e.backups)) {
		found = found || c == "typed in Word"
	}
	if !found {
		t.Error("the foreign content was overwritten without a backup")
	}
}

func TestBackup_RevertedToOwnContentNeedsNoBackup(t *testing.T) {
	// "Touched" or rewritten with the very same bytes is not a foreign change.
	e := newEnv(t, 5)
	dst := e.doc("Pismo.docx", "v0")
	r1, err := e.replace(dst, "v1")
	if err != nil {
		t.Fatal(err)
	}
	external(t, dst, "v1")
	r2, err := e.ops.Replace(context.Background(), e.src("v2"), dst, r1.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Backup != "" {
		t.Errorf("backup %q for unchanged content", r2.Backup)
	}
}

func TestBackup_Rotation(t *testing.T) {
	e := newEnv(t, 3)
	dst := e.doc("Pismo.docx", "v0")
	dir := e.ops.backupDirFor(dst)
	// A file that is not one of ours must survive the rotation.
	write(t, filepath.Join(dir, "notes.txt"), "keep me", 0o600)
	write(t, filepath.Join(dir, "19990101T000000.000000000-other.docx"), "keep me too", 0o600)

	for i := 1; i <= 6; i++ {
		external(t, dst, "ext"+string(rune('0'+i)))
		if _, err := e.replace(dst, "own"+string(rune('0'+i))); err != nil {
			t.Fatal(err)
		}
	}
	var ours []string
	for _, f := range backupFiles(t, e.backups) {
		if strings.HasSuffix(f, "-Pismo.docx") {
			ours = append(ours, f)
		}
	}
	if got := contents(t, ours); !reflect.DeepEqual(got, []string{"ext4", "ext5", "ext6"}) {
		t.Errorf("kept %q, want the 3 newest", got)
	}
	for _, n := range []string{"notes.txt", "19990101T000000.000000000-other.docx"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("rotation removed %s: %v", n, err)
		}
	}
}

func TestBackup_MaxOneKeepsTheLatest(t *testing.T) {
	e := newEnv(t, 1)
	dst := e.doc("Pismo.docx", "v0")
	for _, c := range []string{"a", "b", "c"} {
		external(t, dst, c)
		if _, err := e.replace(dst, "own"+c); err != nil {
			t.Fatal(err)
		}
	}
	if got := contents(t, backupFiles(t, e.backups)); !reflect.DeepEqual(got, []string{"c"}) {
		t.Errorf("backups = %q, want [c]", got)
	}
}

func TestBackup_SameNameDifferentFolders(t *testing.T) {
	e := newEnv(t, 5)
	a := e.doc("Pismo.docx", "in root")
	sub := filepath.Join(e.user, "sub")
	b := filepath.Join(sub, "Pismo.docx")
	write(t, b, "in sub", 0o640)

	for _, d := range []string{a, b} {
		if _, err := e.replace(d, "new"); err != nil {
			t.Fatal(err)
		}
	}
	if e.ops.backupDirFor(a) == e.ops.backupDirFor(b) {
		t.Fatal("the two documents share a backup folder")
	}
	files := backupFiles(t, e.backups)
	if len(files) != 2 || filepath.Dir(files[0]) == filepath.Dir(files[1]) {
		t.Fatalf("backups = %q, want one in each of two folders", files)
	}
	for _, f := range files {
		if !strings.HasPrefix(filepath.Base(filepath.Dir(f)), "Pismo.docx-") {
			t.Errorf("folder name %q is not readable", filepath.Dir(f))
		}
	}
}

func TestBackup_SameInstantDoesNotOverwrite(t *testing.T) {
	e := newEnv(t, 10)
	e.clock.step = 0 // every backup gets the very same instant
	dst := e.doc("Pismo.docx", "v0")
	for _, c := range []string{"first", "second", "third"} {
		external(t, dst, c)
		if _, err := e.replace(dst, "own"); err != nil {
			t.Fatal(err)
		}
	}
	files := backupFiles(t, e.backups)
	if got := contents(t, files); !reflect.DeepEqual(got, []string{"first", "second", "third"}) {
		t.Errorf("backups = %q, want all three, oldest first (files %q)", got, files)
	}
}

func TestBackup_NamesPermissionsAndTime(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("Pismo.docx", "v0")
	res, err := e.replace(dst, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "20261111T120000.000000000-Pismo.docx"; filepath.Base(res.Backup) != want {
		t.Errorf("name = %q, want %q", filepath.Base(res.Backup), want)
	}
	if !strings.HasPrefix(res.Backup, e.backups+string(filepath.Separator)) {
		t.Errorf("backup %q is outside BackupsDir", res.Backup)
	}
	fi, err := os.Stat(res.Backup)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(res.Backup))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("folder mode = %v, want 0700", di.Mode().Perm())
	}
}

func TestBackup_FailureLeavesTheDocumentAlone(t *testing.T) {
	e := newEnv(t, 5)
	// BackupsDir below a regular file cannot be created.
	blocker := filepath.Join(e.work, "blocker")
	write(t, blocker, "x", 0o644)
	ops, err := New(Options{TrashBin: "t", Timeout: time.Second, BackupsDir: filepath.Join(blocker, "b"), MaxBackups: 3})
	if err != nil {
		t.Fatal(err)
	}
	dst := e.doc("Pismo.docx", "v0")
	decorate(t, dst)
	before := stateOf(t, dst)

	if _, err := ops.Replace(context.Background(), e.src("v1"), dst, snap(t, dst)); err == nil {
		t.Fatal("want an error")
	}
	if got := stateOf(t, dst); !reflect.DeepEqual(got, before) {
		t.Error("dst changed")
	}
	if got := names(t, e.user); !reflect.DeepEqual(got, []string{"Pismo.docx"}) {
		t.Errorf("folder = %q", got)
	}
}

func TestBackup_CancelledLeavesNoPartialFile(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("Pismo.docx", strings.Repeat("y", 3*copyBufSize))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	snapshot := snap(t, dst)
	// The backup step alone, with a context that is already done.
	if _, err := e.ops.backup(ctx, dst, snapshot); err == nil {
		t.Fatal("want an error")
	}
	if got := backupFiles(t, e.backups); len(got) != 0 {
		t.Errorf("leftovers: %q", got)
	}
}

func TestBackup_RejectsACopyThatDoesNotMatch(t *testing.T) {
	e := newEnv(t, 5)
	dst := e.doc("Pismo.docx", "v0")
	stale := snap(t, dst)
	external(t, dst, "changed after the snapshot")
	if _, err := e.ops.backup(context.Background(), dst, stale); err == nil {
		t.Fatal("want an error: the backup is not what the snapshot describes")
	}
	if got := backupFiles(t, e.backups); len(got) != 0 {
		t.Errorf("a bad backup was left behind: %q", got)
	}
}
