package docsafe

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// A working copy is a folder <WorkDir>/<id> that Pisar fills while a document
// is open (the Markdown, its media, a reference document); other packages put
// the content there, this file manages the folders and their meta.json.

const (
	// workMetaVersion is the meta.json format this code writes and the newest it reads.
	workMetaVersion = 1
	workMetaFile    = "meta.json"
	// maxWorkMetaBytes bounds what is read from meta.json: a real one is well
	// under 2 KiB, so a bigger file is damage, not data.
	maxWorkMetaBytes = 64 << 10
	// workIDBytes is the number of random bytes in a folder name (hex: twice as many characters).
	workIDBytes = 8
)

var (
	ErrDamagedWork = errors.New("docsafe: working copy metadata is damaged")
	// ErrNotWork is returned for a path that is not a working folder CreateWork could have made.
	ErrNotWork = errors.New("docsafe: not a working folder")
)

// Stages of SaveWorkMeta at which a test can inject a fault (Ops.fail).
const (
	stageMetaTemp     = "meta-create-temp" // the temp file exists and is empty
	stageMetaWrite    = "meta-write"       // after the content was written
	stageMetaFsync    = "meta-fsync"       // after the temp file was synced
	stageMetaClose    = "meta-close"       // after the temp file was closed
	stageMetaRename   = "meta-rename"      // right before the rename
	stageMetaFsyncDir = "meta-fsync-dir"   // after the rename, before the folder sync
)

// WorkMeta is the meta.json of one working copy.
type WorkMeta struct {
	Version  int       `json:"version"`
	Origin   string    `json:"origin"`   // absolute path of the user's document the copy was made from
	Target   string    `json:"target"`   // the .docx a save writes to: Origin for a .docx; "" for an imported .doc until its first save
	Snapshot Snapshot  `json:"snapshot"` // Target (or Origin while Target is "") as Pisar last saw it
	Dirty    bool      `json:"dirty"`    // the Markdown was saved after the last successful export
	Opened   time.Time `json:"opened"`
}

// WorkCopy is one working folder and its metadata.
type WorkCopy struct {
	Dir  string
	Meta WorkMeta
}

// validWorkID reports whether name has the form CreateWork gives its folders:
// workIDBytes random bytes in lowercase hex.
func validWorkID(name string) bool {
	if len(name) != 2*workIDBytes {
		return false
	}
	for _, c := range name {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// workRoot returns the absolute, cleaned WorkDir.
func (o *Ops) workRoot() (string, error) {
	if o.workDir == "" {
		return "", errors.New("docsafe: work: WorkDir is required")
	}
	root, err := filepath.Abs(o.workDir)
	if err != nil {
		return "", fmt.Errorf("docsafe: work: %w", err)
	}
	return root, nil
}

// workChild returns dir as an absolute path if, and only if, it is a real
// folder (not a link) directly inside WorkDir with a name CreateWork makes.
func (o *Ops) workChild(dir string) (string, error) {
	root, err := o.workRoot()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(dir) // also cleans, so "a/../b" and "a//b" cannot dodge the parent check
	if err != nil {
		return "", fmt.Errorf("docsafe: work folder %q: %w", dir, err)
	}
	if filepath.Dir(abs) != root || !validWorkID(filepath.Base(abs)) {
		return "", fmt.Errorf("%w: %q is not directly inside the work folder or has a foreign name", ErrNotWork, dir)
	}
	// Lstat: a link to a folder is not a folder here.
	fi, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("docsafe: work folder %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%w: %q is not a folder", ErrNotWork, dir)
	}
	return abs, nil
}

// workFolders lists the folders of root that look like working copies: real
// folders (links and files are skipped, never followed) with a valid name.
// A missing root is an empty list.
func workFolders(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("docsafe: list the work folder: %w", err)
	}
	var out []string
	for _, e := range entries {
		// DirEntry.IsDir comes from the entry itself, so a link to a folder is false.
		if e.IsDir() && validWorkID(e.Name()) {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	return out, nil
}

// readWorkMeta reads and validates the meta.json in dir. Every failure wraps ErrDamagedWork.
func readWorkMeta(dir string) (m WorkMeta, err error) {
	name := filepath.Base(dir)
	damaged := func(reason string, cause error) error {
		if cause != nil {
			return fmt.Errorf("%w: %s: %s: %w", ErrDamagedWork, name, reason, cause)
		}
		return fmt.Errorf("%w: %s: %s", ErrDamagedWork, name, reason)
	}

	// openRegular refuses a folder, a link or a device named meta.json.
	f, fi, err := openRegular(filepath.Join(dir, workMetaFile))
	if err != nil {
		return WorkMeta{}, damaged(workMetaFile+" cannot be opened", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			m, err = WorkMeta{}, damaged("close", cerr)
		}
	}()
	if fi.Size() > maxWorkMetaBytes {
		return WorkMeta{}, damaged(fmt.Sprintf("larger than %d bytes", maxWorkMetaBytes), nil)
	}
	// The file may grow after the Stat; the limit is enforced on what is read.
	data, err := io.ReadAll(io.LimitReader(f, maxWorkMetaBytes+1))
	if err != nil {
		return WorkMeta{}, damaged("read", err)
	}
	if len(data) > maxWorkMetaBytes {
		return WorkMeta{}, damaged(fmt.Sprintf("larger than %d bytes", maxWorkMetaBytes), nil)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return WorkMeta{}, damaged("empty", nil)
		}
		return WorkMeta{}, damaged("invalid JSON", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return WorkMeta{}, damaged("data after the JSON object", nil)
	}
	switch {
	case m.Version < 1:
		return WorkMeta{}, damaged(fmt.Sprintf("invalid version %d", m.Version), nil)
	case m.Version > workMetaVersion:
		return WorkMeta{}, damaged(fmt.Sprintf("version %d is newer than the known %d", m.Version, workMetaVersion), nil)
	}
	if err := checkMetaPaths(m); err != nil {
		return WorkMeta{}, damaged(err.Error(), nil)
	}
	return m, nil
}

// checkMetaPaths holds the rules for the paths in a meta that reading and writing share.
func checkMetaPaths(m WorkMeta) error {
	if !filepath.IsAbs(m.Origin) {
		return errors.New("origin is not an absolute path")
	}
	if m.Target != "" && !filepath.IsAbs(m.Target) {
		return errors.New("target is not an absolute path")
	}
	return nil
}

// encodeWorkMeta is the bytes of a meta.json for m, always at the current version.
func encodeWorkMeta(m WorkMeta) ([]byte, error) {
	m.Version = workMetaVersion
	if err := checkMetaPaths(m); err != nil {
		return nil, fmt.Errorf("docsafe: work metadata: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("docsafe: work metadata: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxWorkMetaBytes {
		return nil, fmt.Errorf("docsafe: work metadata: %d bytes is over the limit of %d", len(data), maxWorkMetaBytes)
	}
	return data, nil
}

// writeWorkMeta replaces dir/meta.json with data: temp file in the same folder,
// fsync, rename, fsync of the folder, so a cut never leaves half a JSON.
func (o *Ops) writeWorkMeta(ctx context.Context, dir string, data []byte) (err error) {
	final := filepath.Join(dir, workMetaFile)
	f, tmp, err := createTemp(final)
	if err != nil {
		return err
	}
	closed, renamed := false, false
	defer func() {
		if err == nil || renamed {
			return
		}
		if !closed {
			err = errors.Join(err, f.Close())
		}
		// Our own temp file, in our own folder.
		if rerr := os.Remove(tmp); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove the temporary file %q: %w", tmp, rerr))
		}
	}()
	if err := o.step(ctx, stageMetaTemp); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write the temporary file: %w", err)
	}
	if err := o.step(ctx, stageMetaWrite); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync the temporary file: %w", err)
	}
	if err := o.step(ctx, stageMetaFsync); err != nil {
		return err
	}
	closed = true // Close releases the descriptor even when it fails
	if err := f.Close(); err != nil {
		return fmt.Errorf("close the temporary file: %w", err)
	}
	if err := o.step(ctx, stageMetaClose); err != nil {
		return err
	}
	if err := o.step(ctx, stageMetaRename); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	renamed = true

	// The new meta.json is in place; only its durability is open.
	if o.fail != nil {
		if ferr := o.fail(stageMetaFsyncDir); ferr != nil {
			return ferr
		}
	}
	return syncDir(dir)
}

// findWork returns the best working copy for path (see FindWork). The caller holds workMu.
func (o *Ops) findWork(ctx context.Context, root, path string) (best WorkCopy, found bool, err error) {
	dirs, err := workFolders(root)
	if err != nil {
		return WorkCopy{}, false, err
	}
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return WorkCopy{}, false, fmt.Errorf("docsafe: find work %q: %w", path, err)
		}
		meta, err := readWorkMeta(dir)
		if err != nil {
			// Not an error for the caller: the folder may hold the only copy of
			// somebody's text, so it is left alone and only reported.
			slog.Warn("docsafe: skipping a working copy with damaged metadata", "dir", filepath.Base(dir), "err", err)
			continue
		}
		if filepath.Clean(meta.Origin) != path && (meta.Target == "" || filepath.Clean(meta.Target) != path) {
			continue
		}
		// An unsaved copy beats a clean one; among equals the newest wins.
		// ReadDir sorts by name, so a full tie keeps the first, deterministically.
		better := !found || (meta.Dirty && !best.Meta.Dirty) ||
			(meta.Dirty == best.Meta.Dirty && meta.Opened.After(best.Meta.Opened))
		if better {
			best, found = WorkCopy{Dir: dir, Meta: meta}, true
		}
	}
	return best, found, nil
}

// FindWork returns the working copy whose Origin or Target is path, if there is one.
// Folders with damaged metadata are skipped (and logged), never deleted. When
// several copies match, an unsaved (Dirty) one wins, then the newest by Opened.
func (o *Ops) FindWork(ctx context.Context, path string) (WorkCopy, bool, error) {
	root, err := o.workRoot()
	if err != nil {
		return WorkCopy{}, false, err
	}
	if !filepath.IsAbs(path) {
		return WorkCopy{}, false, fmt.Errorf("docsafe: find work: %q is not an absolute path", path)
	}
	o.workMu.Lock()
	defer o.workMu.Unlock()
	return o.findWork(ctx, root, filepath.Clean(path))
}

// CreateWork makes a new, empty working folder for origin and writes its meta.json.
func (o *Ops) CreateWork(ctx context.Context, meta WorkMeta) (WorkCopy, error) {
	root, err := o.workRoot()
	if err != nil {
		return WorkCopy{}, err
	}
	if err := ctx.Err(); err != nil {
		return WorkCopy{}, fmt.Errorf("docsafe: create work %q: %w", meta.Origin, err)
	}
	meta.Version = workMetaVersion
	if meta.Opened.IsZero() {
		meta.Opened = o.now()
	}
	data, err := encodeWorkMeta(meta)
	if err != nil {
		return WorkCopy{}, fmt.Errorf("docsafe: create work %q: %w", meta.Origin, err)
	}

	o.workMu.Lock()
	defer o.workMu.Unlock()
	// FindWork matches Origin and Target, and so does this: a second copy for a
	// path that already has one would make FindWork's answer ambiguous.
	if existing, ok, err := o.findWork(ctx, root, filepath.Clean(meta.Origin)); err != nil {
		return WorkCopy{}, fmt.Errorf("docsafe: create work %q: %w", meta.Origin, err)
	} else if ok {
		return WorkCopy{}, fmt.Errorf("%w: %q already has the working copy %s", ErrExists, meta.Origin, filepath.Base(existing.Dir))
	}

	if err := os.MkdirAll(root, 0o700); err != nil {
		return WorkCopy{}, fmt.Errorf("docsafe: create work %q: %w", meta.Origin, err)
	}
	dir, err := makeWorkFolder(root)
	if err != nil {
		return WorkCopy{}, fmt.Errorf("docsafe: create work %q: %w", meta.Origin, err)
	}
	if err := o.writeWorkMeta(ctx, dir, data); err != nil {
		err = fmt.Errorf("docsafe: create work %q: %w", meta.Origin, err)
		// The folder is brand new and holds nothing but a possible temp file.
		if rerr := o.removeWork(dir); rerr != nil {
			err = errors.Join(err, rerr)
		}
		return WorkCopy{}, err
	}
	return WorkCopy{Dir: dir, Meta: meta}, nil
}

// makeWorkFolder creates a folder with a random, unused name in root.
func makeWorkFolder(root string) (string, error) {
	for range 10 {
		var rnd [workIDBytes]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return "", fmt.Errorf("random name: %w", err)
		}
		dir := filepath.Join(root, hex.EncodeToString(rnd[:]))
		err := os.Mkdir(dir, 0o700)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create the working folder: %w", err)
		}
		return dir, nil
	}
	return "", errors.New("create the working folder: no free name found")
}

// SaveWorkMeta writes w.Meta to w.Dir/meta.json atomically.
func (o *Ops) SaveWorkMeta(ctx context.Context, w WorkCopy) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("docsafe: save work metadata %q: %w", w.Dir, err)
	}
	data, err := encodeWorkMeta(w.Meta)
	if err != nil {
		return fmt.Errorf("docsafe: save work metadata %q: %w", w.Dir, err)
	}
	o.workMu.Lock()
	defer o.workMu.Unlock()
	dir, err := o.workChild(w.Dir)
	if err != nil {
		return fmt.Errorf("docsafe: save work metadata: %w", err)
	}
	if err := o.writeWorkMeta(ctx, dir, data); err != nil {
		return fmt.Errorf("docsafe: save work metadata %q: %w", w.Dir, err)
	}
	return nil
}

// removeWork deletes one working folder with everything in it. The caller holds workMu.
//
// os.RemoveAll is fine here although Pisar never deletes the user's files that
// way: these are Pisar's own folders, created by CreateWork, holding only its
// own copies. workChild makes sure nothing else can be reached: the folder must
// be a real (non-link) child of WorkDir with a generated name. A swap for a link
// after the check is harmless too, because RemoveAll removes a link, not its target.
func (o *Ops) removeWork(dir string) error {
	abs, err := o.workChild(dir)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("docsafe: remove the working folder %q: %w", dir, err)
	}
	return nil
}

// DiscardWork removes the working folder.
func (o *Ops) DiscardWork(ctx context.Context, w WorkCopy) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("docsafe: discard work %q: %w", w.Dir, err)
	}
	o.workMu.Lock()
	defer o.workMu.Unlock()
	return o.removeWork(w.Dir)
}

// SweepWork removes the working copies that hold nothing unsaved and reports the rest.
// A folder whose meta.json is missing or damaged is neither removed nor reported
// in kept: it may hold the only copy of somebody's text, so it is only logged.
func (o *Ops) SweepWork(ctx context.Context) (kept []WorkCopy, err error) {
	root, err := o.workRoot()
	if err != nil {
		return nil, err
	}
	o.workMu.Lock()
	defer o.workMu.Unlock()
	dirs, err := workFolders(root)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return kept, errors.Join(append(errs, fmt.Errorf("docsafe: sweep work: %w", err))...)
		}
		meta, err := readWorkMeta(dir)
		if err != nil {
			slog.Warn("docsafe: leaving a working copy with damaged metadata alone", "dir", filepath.Base(dir), "err", err)
			continue
		}
		if meta.Dirty {
			kept = append(kept, WorkCopy{Dir: dir, Meta: meta})
			continue
		}
		if err := o.removeWork(dir); err != nil {
			errs = append(errs, err)
		}
	}
	return kept, errors.Join(errs...)
}
