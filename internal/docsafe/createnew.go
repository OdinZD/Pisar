package docsafe

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Stages of CreateNew at which a test can inject a fault (Ops.fail).
const (
	stageNewTemp     = "new-create-temp" // the temp file exists and is empty
	stageNewCopy     = "new-copy"        // after the first chunk of the content
	stageNewChmod    = "new-chmod"       // after the permissions were set
	stageNewFsync    = "new-fsync"       // after the temp file was synced
	stageNewClose    = "new-close"       // after the temp file was closed
	stageNewLink     = "new-link"        // right before the name is claimed
	stageNewRename   = "new-rename"      // right before the rename of the fallback path
	stageNewUnlink   = "new-unlink"      // dst is complete; the temp name is about to go
	stageNewFsyncDir = "new-fsync-dir"   // dst is complete and the temp name is gone
)

// CreateNew writes a new file dst in the user's folder with the content of src
// (a file in a folder Pisar owns). It never overwrites: an existing dst is ErrExists.
//
// If it returns an error before dst got its name, dst does not exist and no
// stray file is left. Once dst is complete (the name is claimed) a later
// failure returns the snapshot of dst together with the error, as Replace does.
func (o *Ops) CreateNew(ctx context.Context, src, dst string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("docsafe: create new %q: %w", dst, err)
	}
	if !filepath.IsAbs(dst) {
		return Snapshot{}, fmt.Errorf("docsafe: create new %q: not an absolute path", dst)
	}
	abs := filepath.Clean(dst)
	if err := validName(filepath.Base(abs)); err != nil {
		return Snapshot{}, err
	}
	dir := filepath.Dir(abs)
	// Lstat so that a link to a folder is refused rather than followed.
	di, err := os.Lstat(dir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("docsafe: create new %q: folder: %w", dst, err)
	}
	if !di.IsDir() {
		return Snapshot{}, fmt.Errorf("docsafe: create new %q: %q is not a folder", dst, dir)
	}
	srcSize, err := newSourceSize(src)
	if err != nil {
		return Snapshot{}, fmt.Errorf("docsafe: create new %q: %w", dst, err)
	}
	// Cheap early answer, before any bytes are written. It decides nothing: the
	// claim below is what guarantees that nothing is overwritten.
	if _, err := os.Lstat(abs); err == nil {
		return Snapshot{}, fmt.Errorf("docsafe: create new %q: %w", dst, ErrExists)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, fmt.Errorf("docsafe: create new %q: %w", dst, err)
	}

	tmp, err := o.prepareNewTemp(ctx, src, srcSize, abs)
	if err != nil {
		return Snapshot{}, fmt.Errorf("docsafe: create new %q: %w", dst, err)
	}

	// From here tmp is ours and complete; until dst has its name it must be removed on failure.
	claimed, err := o.claimName(ctx, tmp, abs)
	if err != nil {
		if rerr := removeIfPresent(tmp); rerr != nil {
			err = errors.Join(err, rerr)
		}
		return Snapshot{}, fmt.Errorf("docsafe: create new %q: %w", dst, err)
	}

	// dst is complete now; nothing below may be cut short by ctx.
	done := context.WithoutCancel(ctx)
	var errs []error
	if o.fail != nil {
		if ferr := o.fail(stageNewUnlink); ferr != nil {
			errs = append(errs, ferr)
		}
	}
	if claimed == claimedByLink {
		// Also when the fault above fired: a second name left behind would make dst a hard link.
		if rerr := removeIfPresent(tmp); rerr != nil {
			errs = append(errs, rerr)
		}
	}
	snap, serr := TakeSnapshot(done, abs)
	if serr != nil {
		errs = append(errs, serr)
	} else {
		o.mu.Lock()
		if o.written == nil {
			o.written = make(map[string]Snapshot)
		}
		o.written[abs] = snap
		o.mu.Unlock()
	}
	if o.fail != nil {
		if ferr := o.fail(stageNewFsyncDir); ferr != nil {
			errs = append(errs, ferr)
		}
	}
	if serr := syncDir(dir); serr != nil {
		errs = append(errs, serr)
	}
	// One name only: a second one (e.g. a temp name that could not be removed)
	// would make the next Replace refuse dst as a hard link.
	var st unix.Stat_t
	if lerr := unix.Lstat(abs, &st); lerr != nil {
		errs = append(errs, fmt.Errorf("check the names of the new file: %w", lerr))
	} else if st.Nlink != 1 {
		errs = append(errs, fmt.Errorf("the new file has %d names, want 1", st.Nlink))
	}
	if len(errs) > 0 {
		return snap, fmt.Errorf("docsafe: create new %q: the new file is in place, but: %w", dst, errors.Join(errs...))
	}
	return snap, nil
}

// newSourceSize checks that src is a regular, non-empty file and returns its size.
func newSourceSize(src string) (size int64, err error) {
	f, fi, err := openRegular(src)
	if err != nil {
		return 0, fmt.Errorf("source: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("source %q: close: %w", src, err)
	}
	// An empty source is never a real conversion result.
	if fi.Size() == 0 {
		return 0, fmt.Errorf("source %q is empty", src)
	}
	return fi.Size(), nil
}

// prepareNewTemp builds the finished file next to dst: content, 0644, on disk.
// On error the temp file is gone again.
func (o *Ops) prepareNewTemp(ctx context.Context, srcPath string, srcSize int64, dstAbs string) (_ string, err error) {
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
		if rerr := removeIfPresent(tmp); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}()
	if err := o.step(ctx, stageNewTemp); err != nil {
		return "", err
	}

	src, _, err := openRegular(srcPath)
	if err != nil {
		return "", fmt.Errorf("open the source: %w", err)
	}
	n, err := copyCtx(ctx, f, src, func() error { return o.step(ctx, stageNewCopy) })
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

	// createTemp makes 0600 files; a document is 0644. fchmod ignores the umask.
	if err := f.Chmod(0o644); err != nil {
		return "", fmt.Errorf("set permissions: %w", err)
	}
	if err := o.step(ctx, stageNewChmod); err != nil {
		return "", err
	}
	// On macOS File.Sync is F_FULLFSYNC (see prepareTemp).
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync the temporary file: %w", err)
	}
	if err := o.step(ctx, stageNewFsync); err != nil {
		return "", err
	}
	closed = true // Close releases the descriptor even when it fails
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close the temporary file: %w", err)
	}
	if err := o.step(ctx, stageNewClose); err != nil {
		return "", err
	}
	return tmp, nil
}

type claim int

const (
	claimedByLink   claim = iota + 1 // dst is a second name of tmp; tmp still exists
	claimedByRename                  // tmp was renamed to dst; tmp is gone
)

// claimName gives the finished tmp the name dst without ever replacing an
// existing file. On error dst was not created by this call (tmp is the caller's to remove).
func (o *Ops) claimName(ctx context.Context, tmp, dst string) (claim, error) {
	if err := o.step(ctx, stageNewLink); err != nil {
		return 0, err
	}
	link := o.link
	if link == nil {
		link = os.Link
	}
	// Unlike os.Rename, a hard link fails if dst exists, so "is the name free"
	// and "take it" are one step. On a case-insensitive volume it also refuses a
	// name that differs from an existing one only in case.
	linkErr := link(tmp, dst)
	switch {
	case linkErr == nil:
		return claimedByLink, nil
	case errors.Is(linkErr, fs.ErrExist):
		return 0, ErrExists
	}

	// The volume may not support hard links. Fall back to check-then-rename; a
	// file created at dst between the check and the rename would be replaced
	// (small race window, only on such volumes).
	if err := o.step(ctx, stageNewRename); err != nil {
		return 0, err
	}
	if _, err := os.Lstat(dst); err == nil {
		return 0, ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("check the name: %w (link: %w)", err, linkErr)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return 0, fmt.Errorf("rename: %w (link: %w)", err, linkErr)
	}
	return claimedByRename, nil
}

// removeIfPresent removes one of our own temp files; already gone is fine.
func removeIfPresent(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the temporary file %q: %w", p, err)
	}
	return nil
}
