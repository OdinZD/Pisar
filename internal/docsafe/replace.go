package docsafe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var (
	ErrConflict = errors.New("docsafe: the file changed since it was opened")
	ErrReadOnly = errors.New("docsafe: the file must not be overwritten")
)

// Stages of Replace at which a test can inject a fault (Ops.fail). Each one is
// a point after which the work so far must be undone.
const (
	stageBackup     = "backup"      // after the backup step (made or not needed)
	stageCreateTemp = "create-temp" // the temp file exists and is empty
	stageCopy       = "copy"        // after the first chunk of the content
	stageXattr      = "xattr"       // after the attributes were transferred
	stageChmod      = "chmod"       // after the permissions were transferred
	stageFsync      = "fsync"       // after the temp file was synced
	stageClose      = "close"       // after the temp file was closed
	stageRecheck    = "recheck"     // before the second conflict check
	stageRename     = "rename"      // right before the rename
	stageFsyncDir   = "fsync-dir"   // after the rename, before the folder sync
)

type ReplaceResult struct {
	Snapshot Snapshot // the file as it is now, for the next save
	Backup   string   // path of the backup made by this call, "" if none was needed
}

// step checks ctx and then the fault injection point for stage.
func (o *Ops) step(ctx context.Context, stage string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.fail != nil {
		return o.fail(stage)
	}
	return nil
}

// Replace puts the content of src (a file in a folder Pisar owns) in place of
// dst (the user's document). expect is the snapshot of dst the caller last saw.
//
// If it returns an error before the rename, dst is untouched (same bytes,
// permissions and attributes) and no stray file is left next to it; a backup
// made on the way stays. If the error comes after the rename, the new content
// is already in place: the returned ReplaceResult then describes it (its
// Snapshot is zero only if that could not be read) and the error says what
// could not be finished.
//
// The second conflict check and the rename are two calls, so a program that
// writes dst in between still wins that race; only a lock could close it.
func (o *Ops) Replace(ctx context.Context, src, dst string, expect Snapshot) (res ReplaceResult, err error) {
	if o.backupsDir == "" {
		return res, errors.New("docsafe: replace: BackupsDir is required")
	}
	if o.maxBackups < 1 {
		return res, fmt.Errorf("docsafe: replace: MaxBackups must be at least 1, got %d", o.maxBackups)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("docsafe: replace %q: %w", dst, err)
	}
	abs, err := filepath.Abs(dst)
	if err != nil {
		return res, fmt.Errorf("docsafe: replace %q: %w", dst, err)
	}

	// 1. What may be replaced by what.
	reason, err := CheckWritable(abs)
	if err != nil {
		return res, fmt.Errorf("docsafe: replace %q: %w", dst, err)
	}
	if reason != ReadOnlyNone {
		return res, fmt.Errorf("docsafe: replace %q: %w", dst, &ReadOnlyError{Path: abs, Reason: reason})
	}
	srcSize, err := checkSource(src, abs)
	if err != nil {
		return res, fmt.Errorf("docsafe: replace %q: %w", dst, err)
	}

	// 2. Conflict.
	cur, err := TakeSnapshot(ctx, abs)
	if err != nil {
		return res, fmt.Errorf("docsafe: replace %q: %w", dst, err)
	}
	if !cur.Same(expect) {
		return res, fmt.Errorf("docsafe: replace %q: %w", dst, ErrConflict)
	}

	// 3. Backup, unless the file is exactly what this Ops wrote last. The
	// comparison is with content, not with "was it written this session", so
	// another program's text is never overwritten without a backup either.
	if last, ok := o.written[abs]; !ok || !last.Same(cur) {
		res.Backup, err = o.backup(ctx, abs, cur)
		if err != nil {
			return ReplaceResult{}, fmt.Errorf("docsafe: replace %q: backup: %w", dst, err)
		}
	}
	if err := o.step(ctx, stageBackup); err != nil {
		return ReplaceResult{Backup: res.Backup}, fmt.Errorf("docsafe: replace %q: %w", dst, err)
	}

	// 4-6. The finished replacement, next to dst.
	tmp, err := o.prepareTemp(ctx, src, srcSize, abs)
	if err != nil {
		return ReplaceResult{Backup: res.Backup}, fmt.Errorf("docsafe: replace %q: %w", dst, err)
	}
	res = ReplaceResult{Backup: res.Backup}

	// 7-8. Last look at dst, then the rename.
	if err := o.commit(ctx, tmp, abs, cur); err != nil {
		// tmp is ours: created above, in dst's folder, with a name no document has.
		if rerr := os.Remove(tmp); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove the temporary file %q: %w", tmp, rerr))
		}
		return res, fmt.Errorf("docsafe: replace %q: %w", dst, err)
	}

	// 9. The rename is done; nothing below may be cut short by ctx.
	done := context.WithoutCancel(ctx)
	snap, serr := TakeSnapshot(done, abs)
	if serr == nil {
		if o.written == nil {
			o.written = make(map[string]Snapshot)
		}
		o.written[abs] = snap
		res.Snapshot = snap
	}
	var ferr error
	if o.fail != nil {
		ferr = o.fail(stageFsyncDir)
	}
	if ferr == nil {
		ferr = syncDir(filepath.Dir(abs))
	}
	if serr != nil || ferr != nil {
		return res, fmt.Errorf("docsafe: replace %q: the new content is in place, but: %w", dst, errors.Join(serr, ferr))
	}
	return res, nil
}

// checkSource checks that src can be the content of the replacement: a
// regular, non-empty file that is not dst itself. It returns the size.
func checkSource(src, dstAbs string) (size int64, err error) {
	f, fi, err := openRegular(src)
	if err != nil {
		return 0, fmt.Errorf("source: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("source %q: close: %w", src, err)
	}
	// An empty source would silently wipe the document; a real conversion never is.
	if fi.Size() == 0 {
		return 0, fmt.Errorf("source %q is empty", src)
	}
	di, err := os.Lstat(dstAbs)
	if err != nil {
		return 0, err
	}
	if os.SameFile(fi, di) {
		return 0, fmt.Errorf("source %q is the file being replaced", src)
	}
	return fi.Size(), nil
}

// prepareTemp builds the replacement next to dst: content, attributes,
// permissions, all on disk. On error the temp file is gone again.
func (o *Ops) prepareTemp(ctx context.Context, srcPath string, srcSize int64, dstAbs string) (_ string, err error) {
	// The attributes come from dst itself, read through its own descriptor.
	// It is closed before the temp file exists, so a failing Close needs no cleanup.
	orig, origInfo, err := openRegular(dstAbs)
	if err != nil {
		return "", fmt.Errorf("read the original: %w", err)
	}
	want, err := readXattrs(orig)
	if cerr := orig.Close(); err != nil || cerr != nil {
		return "", errors.Join(err, cerr)
	}

	f, tmp, err := createTemp(dstAbs) // tmp stays a plain local: a "return" would blank a named result before the defer runs
	if err != nil {
		return "", err
	}
	closed := false
	defer func() {
		if err == nil {
			return
		}
		if !closed {
			err = errors.Join(err, f.Close())
		}
		// Our own temp file, in dst's folder, not a document.
		if rerr := os.Remove(tmp); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove the temporary file %q: %w", tmp, rerr))
		}
	}()
	if err := o.step(ctx, stageCreateTemp); err != nil {
		return "", err
	}

	// 4. Content. The hook fires after the first chunk, i.e. in the middle of it.
	src, _, err := openRegular(srcPath)
	if err != nil {
		return "", fmt.Errorf("open the source: %w", err)
	}
	n, err := copyCtx(ctx, f, src, func() error { return o.step(ctx, stageCopy) })
	if cerr := src.Close(); err != nil || cerr != nil {
		return "", errors.Join(wrapIf("copy the content", err), wrapIf("close the source", cerr))
	}
	fi, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat the temporary file: %w", err)
	}
	if n != srcSize || fi.Size() != srcSize {
		return "", fmt.Errorf("copied %d bytes, the temporary file has %d, the source had %d", n, fi.Size(), srcSize)
	}

	// 5. Attributes before permissions: with the original's mode the temp file
	// may lose the write access that setting attributes needs.
	if err := copyXattrs(f, want); err != nil {
		return "", err
	}
	if err := o.step(ctx, stageXattr); err != nil {
		return "", err
	}
	if err := f.Chmod(origInfo.Mode()); err != nil { // fchmod; includes setuid/setgid/sticky
		return "", fmt.Errorf("transfer permissions: %w", err)
	}
	if err := o.step(ctx, stageChmod); err != nil {
		return "", err
	}

	// 6. On macOS File.Sync is fcntl(F_FULLFSYNC): it asks the drive to flush
	// its own cache, which plain fsync does not (it falls back to fsync only
	// where F_FULLFSYNC is unsupported, e.g. some network volumes).
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync the temporary file: %w", err)
	}
	if err := o.step(ctx, stageFsync); err != nil {
		return "", err
	}
	closed = true // Close releases the descriptor even when it fails
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close the temporary file: %w", err)
	}
	if err := o.step(ctx, stageClose); err != nil {
		return "", err
	}
	return tmp, nil
}

func wrapIf(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}

// createTemp creates the temporary file in dst's folder: hidden, with a random
// part, no document extension, 0600, and never an existing file (O_EXCL).
func createTemp(dstAbs string) (*os.File, string, error) {
	dir, base := filepath.Split(dstAbs)
	for range 10 {
		var rnd [8]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return nil, "", fmt.Errorf("random name: %w", err)
		}
		p := filepath.Join(dir, "."+fileLabel(base)+"."+hex.EncodeToString(rnd[:])+".tmp")
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("create the temporary file: %w", err)
		}
		return f, p, nil
	}
	return nil, "", errors.New("create the temporary file: no free name found")
}

// commit does the second conflict check and the rename. On error dst is untouched.
func (o *Ops) commit(ctx context.Context, tmp, dstAbs string, cur Snapshot) error {
	if err := o.step(ctx, stageRecheck); err != nil {
		return err
	}
	now, err := TakeSnapshot(ctx, dstAbs)
	if err != nil {
		return err
	}
	if !now.Same(cur) {
		return ErrConflict // somebody wrote dst while we were working
	}
	if err := o.step(ctx, stageRename); err != nil {
		return err
	}
	if err := os.Rename(tmp, dstAbs); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// syncDir makes the rename durable by syncing the folder (F_FULLFSYNC on macOS).
func syncDir(dir string) (err error) {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open the folder to sync it: %w", err)
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync the folder: %w", err)
	}
	return nil
}
