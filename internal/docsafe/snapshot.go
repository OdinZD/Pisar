package docsafe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// Snapshot is the state of a file at one moment. It is stored as JSON.
type Snapshot struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	SHA256  string    `json:"sha256"` // hex
}

// Same reports whether two snapshots describe the same content: size and hash.
// ModTime is kept for display and is not compared (touching a file is not a change).
func (s Snapshot) Same(o Snapshot) bool {
	return s.Size == o.Size && s.SHA256 == o.SHA256
}

// copyBufSize is the chunk size of every copy and hash; ctx is checked between chunks.
const copyBufSize = 256 << 10

// ctxReader fails reads once ctx is done, so a long copy can be cancelled.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// hookWriter calls after once a Write has succeeded. Replace uses it to inject
// a fault in the middle of a copy.
type hookWriter struct {
	w     io.Writer
	after func() error
}

func (h hookWriter) Write(p []byte) (int, error) {
	n, err := h.w.Write(p)
	if err != nil {
		return n, err
	}
	if h.after != nil {
		return n, h.after()
	}
	return n, nil
}

// copyCtx copies src to dst in copyBufSize chunks. The wrappers hide
// WriterTo/ReaderFrom, so io.CopyBuffer really goes chunk by chunk.
func copyCtx(ctx context.Context, dst io.Writer, src io.Reader, after func() error) (int64, error) {
	return io.CopyBuffer(hookWriter{dst, after}, ctxReader{ctx, src}, make([]byte, copyBufSize))
}

// openRegular opens path read-only if, and only if, it is a regular file. The
// link is never followed: Lstat and O_NOFOLLOW refuse it, and the SameFile
// check catches a swap between the two calls.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	li, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !li.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%q is not a regular file", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, errors.Join(err, f.Close())
	}
	if !os.SameFile(li, fi) {
		return nil, nil, errors.Join(fmt.Errorf("%q changed while it was being opened", path), f.Close())
	}
	return f, fi, nil
}

// TakeSnapshot reads the file at path. A missing file is fs.ErrNotExist.
func TakeSnapshot(ctx context.Context, path string) (snap Snapshot, err error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("docsafe: snapshot %q: %w", path, err)
	}
	f, _, err := openRegular(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("docsafe: snapshot %q: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("docsafe: snapshot %q: close: %w", path, cerr))
		}
	}()

	h := sha256.New()
	n, err := copyCtx(ctx, h, f, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("docsafe: snapshot %q: %w", path, err)
	}
	// Stat after reading, so ModTime is not older than the bytes that were hashed.
	fi, err := f.Stat()
	if err != nil {
		return Snapshot{}, fmt.Errorf("docsafe: snapshot %q: %w", path, err)
	}
	return Snapshot{Size: n, ModTime: fi.ModTime(), SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
