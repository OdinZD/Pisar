package docsafe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// ReadOnlyReason says why a document must not be overwritten in place.
type ReadOnlyReason string

const (
	ReadOnlyNone       ReadOnlyReason = ""
	ReadOnlyNotRegular ReadOnlyReason = "not-regular"   // link, folder, device
	ReadOnlyPermission ReadOnlyReason = "no-permission" // file or its folder is not writable
	ReadOnlyLocked     ReadOnlyReason = "locked"        // Finder "Locked" (immutable flag)
	ReadOnlyHardLinks  ReadOnlyReason = "hard-links"    // more than one name; a rename would split them
)

// ReadOnlyError is what Replace returns for a document it refuses to
// overwrite. It matches ErrReadOnly with errors.Is; errors.As gives the reason.
type ReadOnlyError struct {
	Path   string
	Reason ReadOnlyReason
}

func (e *ReadOnlyError) Error() string {
	return fmt.Sprintf("%v: %q (%s)", ErrReadOnly, e.Path, e.Reason)
}

func (e *ReadOnlyError) Unwrap() error { return ErrReadOnly }

// CheckWritable reports whether the document at path may be replaced in place.
// The checks run in a fixed order: kind, hard links, lock flag, permissions.
// A missing path is an error wrapping fs.ErrNotExist, not a reason.
func CheckWritable(path string) (ReadOnlyReason, error) {
	// Lstat: a link is judged as a link, never followed to its target.
	info, err := os.Lstat(path)
	if err != nil {
		return ReadOnlyNone, fmt.Errorf("docsafe: check %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return ReadOnlyNotRegular, nil
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ReadOnlyNone, fmt.Errorf("docsafe: check %q: no stat data on this platform", path)
	}
	// Replacing means a new inode under the old name; any other name of the
	// old inode would keep the old content.
	if st.Nlink > 1 {
		return ReadOnlyHardLinks, nil
	}
	if st.Flags&(unix.UF_IMMUTABLE|unix.SF_IMMUTABLE) != 0 {
		return ReadOnlyLocked, nil
	}
	// The file is replaced by rename, which needs write access to the folder;
	// the file's own bit matters because a read-only file is read-only on purpose.
	for _, p := range []string{path, filepath.Dir(path)} {
		if err := unix.Access(p, unix.W_OK); err != nil {
			// EROFS: a read-only volume.
			if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EROFS) {
				return ReadOnlyPermission, nil
			}
			return ReadOnlyNone, fmt.Errorf("docsafe: check %q: access %q: %w", path, p, err)
		}
	}
	return ReadOnlyNone, nil
}
