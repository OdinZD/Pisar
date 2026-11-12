package docsafe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// maxLabelBytes keeps a file name short enough that the folder and backup names
// built from it stay under the 255-byte limit of one path component.
const maxLabelBytes = 100

// backupStamp is the time part of a backup name. It is UTC with nanoseconds and
// fixed width, so names sort in time order.
const backupStamp = "20060102T150405.000000000"

// backupNameRE matches the part of a backup name before the document name; an
// optional ".NN" follows the stamp when two backups share one instant.
var backupNameRE = regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}(\.\d{2})?-`)

// fileLabel shortens name to at most maxLabelBytes without cutting a character.
func fileLabel(name string) string {
	if len(name) <= maxLabelBytes {
		return name
	}
	name = name[:maxLabelBytes]
	for !utf8.ValidString(name) {
		name = name[:len(name)-1]
	}
	return name
}

// backupDirFor is the folder that holds the backups of the document at abs: a
// readable name plus a hash of the whole path, so same-named documents in
// different folders never share one.
func (o *Ops) backupDirFor(abs string) string {
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(o.backupsDir, fileLabel(filepath.Base(abs))+"-"+hex.EncodeToString(sum[:8]))
}

// backup copies the document at abs, whose state is cur, into its backup
// folder, checks the copy, and rotates old backups. It returns the new
// backup's path. The document itself is only read.
func (o *Ops) backup(ctx context.Context, abs string, cur Snapshot) (string, error) {
	dir := o.backupDirFor(abs)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup folder: %w", err)
	}
	path, err := o.copyIntoBackup(ctx, dir, abs, filepath.Base(abs), "", cur, nil)
	if err != nil {
		return "", err
	}
	if err := rotateBackups(dir, filepath.Base(abs), "", o.maxBackups); err != nil {
		return "", fmt.Errorf("rotate backups: %w", err)
	}
	return path, nil
}

// stageBackupText is the point of BackupText (the first chunk was written) at
// which a test can inject a fault (Ops.fail).
const stageBackupText = "backup-text"

// textBackupExt marks the Markdown copies of the text a user had in Pisar. It
// keeps them apart from the backups of the .docx itself in the same folder, so
// neither kind's rotation counts or removes the other.
const textBackupExt = ".md"

// BackupText saves a copy of src, a file in a folder Pisar owns that holds the
// text the user wrote, in the backup folder of the document forPath, as
// "<time>-<document name>.md". The copy is made like any other backup (0600,
// never over an existing one, synced, read back and compared by hash). Of
// these copies the newest MaxBackups are kept per document, independently of
// the backups of the document itself. It returns the path of the copy.
func (o *Ops) BackupText(ctx context.Context, src, forPath string) (string, error) {
	if o.backupsDir == "" {
		return "", errors.New("docsafe: backup text: BackupsDir is required")
	}
	if o.maxBackups < 1 {
		return "", fmt.Errorf("docsafe: backup text: MaxBackups must be at least 1, got %d", o.maxBackups)
	}
	abs, err := filepath.Abs(forPath)
	if err != nil {
		return "", fmt.Errorf("docsafe: backup text: %w", err)
	}
	// The hash is taken before the copy, so a source that changes meanwhile is caught.
	want, err := TakeSnapshot(ctx, src)
	if err != nil {
		return "", fmt.Errorf("docsafe: backup text: %w", err)
	}
	dir := o.backupDirFor(abs)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("docsafe: backup text: create backup folder: %w", err)
	}
	var after func() error
	if o.fail != nil {
		after = func() error { return o.fail(stageBackupText) }
	}
	path, err := o.copyIntoBackup(ctx, dir, src, filepath.Base(abs), textBackupExt, want, after)
	if err != nil {
		return "", fmt.Errorf("docsafe: backup text: %w", err)
	}
	if err := rotateBackups(dir, filepath.Base(abs), textBackupExt, o.maxBackups); err != nil {
		return "", fmt.Errorf("docsafe: backup text: rotate backups: %w", err)
	}
	return path, nil
}

// copyIntoBackup copies the file at srcPath into a new file in dir named after
// base plus ext, and reads the copy back: its hash must be want's, or the
// backup is worthless (a failed write, or the source changed meanwhile). On any
// error no file is left behind. after, if not nil, runs after each written chunk.
func (o *Ops) copyIntoBackup(ctx context.Context, dir, srcPath, base, ext string, want Snapshot, after func() error) (path string, err error) {
	src, _, err := openRegular(srcPath)
	if err != nil {
		return "", fmt.Errorf("open for backup: %w", err)
	}
	defer func() { err = errors.Join(err, src.Close()) }()

	out, path, err := createBackupFile(dir, base, ext, o.now())
	if err != nil {
		return "", err
	}
	// From here the file at path is ours and half-made until proven complete.
	fail := func(err error) (string, error) {
		// Backups are Pisar's own copies in Pisar's own folder, not user files.
		if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove the incomplete backup: %w", rerr))
		}
		return "", err
	}
	if _, err := copyCtx(ctx, out, src, after); err != nil {
		return fail(errors.Join(fmt.Errorf("copy to backup: %w", err), out.Close()))
	}
	if err := out.Sync(); err != nil {
		return fail(errors.Join(fmt.Errorf("sync backup: %w", err), out.Close()))
	}
	if err := out.Close(); err != nil {
		return fail(fmt.Errorf("close backup: %w", err))
	}
	got, err := TakeSnapshot(ctx, path)
	if err != nil {
		return fail(fmt.Errorf("verify backup: %w", err))
	}
	if !got.Same(want) {
		return fail(errors.New("the backup does not match the document (it may have changed while being copied)"))
	}
	return path, nil
}

// createBackupFile creates a new, empty, 0600 file in dir. O_EXCL makes sure an
// existing backup is never overwritten; two backups in one instant get ".NN".
func createBackupFile(dir, base, ext string, now time.Time) (*os.File, string, error) {
	stamp := now.UTC().Format(backupStamp)
	for seq := 0; seq < 100; seq++ {
		name := stamp
		if seq > 0 {
			name = fmt.Sprintf("%s.%02d", stamp, seq)
		}
		p := filepath.Join(dir, name+"-"+fileLabel(base)+ext)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("create backup file: %w", err)
		}
		return f, p, nil
	}
	return nil, "", fmt.Errorf("create backup file: 100 backups already share the instant %s", stamp)
}

// rotateBackups keeps the newest keep backups of the document called base, of
// the kind that ext names ("" for the document's own, textBackupExt for the
// copies of the user's text), and removes the rest. Only files named like our
// backups of that kind are considered.
func rotateBackups(dir, base, ext string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	suffix := "-" + fileLabel(base) + ext
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && strings.HasSuffix(n, suffix) && backupNameRE.MatchString(n) {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	if len(names) <= keep {
		return nil
	}
	var errs []error
	for _, n := range names[:len(names)-keep] { // oldest first
		// os.Remove, not Trash: these are Pisar's own copies in Pisar's own folder.
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
