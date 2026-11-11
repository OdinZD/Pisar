// Package docsafe is the only place in Pisar that writes to the user's files.
// This step covers creating, renaming and trashing documents; every action
// refuses paths outside the project and never overwrites or deletes anything.
package docsafe

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"Pisar/internal/project"
)

// Runner runs an external command and returns its combined output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner is the Runner used in production (exec.CommandContext).
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Options configures Ops.
type Options struct {
	TrashBin string        // path or name of the trash tool; required
	Timeout  time.Duration // limit for one external command; required, > 0
	Run      Runner        // nil means ExecRunner
}

var (
	ErrExists          = errors.New("docsafe: a file with that name already exists")
	ErrInvalidName     = errors.New("docsafe: invalid file name")
	ErrUnsupportedKind = errors.New("docsafe: unsupported document type")
	ErrNotDocument     = errors.New("docsafe: not a document")
)

// maxNameBytes is the file name limit of APFS/HFS+ (bytes, extension included).
const maxNameBytes = 255

// Ops performs the safe document actions.
type Ops struct {
	trashBin string
	timeout  time.Duration
	run      Runner
}

// New validates opts and returns Ops.
func New(opts Options) (*Ops, error) {
	if opts.TrashBin == "" {
		return nil, errors.New("docsafe: TrashBin is required")
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("docsafe: Timeout must be positive, got %v", opts.Timeout)
	}
	run := opts.Run
	if run == nil {
		run = ExecRunner
	}
	return &Ops{trashBin: opts.TrashBin, timeout: opts.Timeout, run: run}, nil
}

// validName checks the rules shared by every user-supplied file name.
func validName(name string) error {
	switch {
	case name == "" || name == "." || name == "..":
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	case strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$"):
		return fmt.Errorf("%w: %q must not start with \".\" or \"~$\"", ErrInvalidName, name)
	case strings.HasPrefix(name, " ") || strings.HasSuffix(name, " "):
		return fmt.Errorf("%w: %q starts or ends with a space", ErrInvalidName, name)
	case len(name) > maxNameBytes:
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidName, maxNameBytes)
	}
	for _, r := range name {
		// ":" is how Finder displays "/"; NUL is a control character.
		if r == '/' || r == ':' || unicode.IsControl(r) {
			return fmt.Errorf("%w: %q contains %q", ErrInvalidName, name, r)
		}
	}
	return nil
}

func joinRel(dirRel, name string) string {
	if dirRel == "" {
		return name
	}
	return dirRel + "/" + name
}

// CreateFile creates an empty document in the folder dirRel ("" is the project
// root) and returns its path relative to root.
func (o *Ops) CreateFile(ctx context.Context, root, dirRel, name string) (rel string, err error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("docsafe: create %q: %w", joinRel(dirRel, name), err)
	}
	if err := validName(name); err != nil {
		return "", err
	}
	final := name
	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".md", ".txt":
	case ".docx", ".doc":
		return "", fmt.Errorf("%w: %q (new %s files are not supported yet)", ErrUnsupportedKind, name, ext)
	default:
		final += ".md"
	}
	if err := validName(final); err != nil { // the added extension may exceed the byte limit
		return "", err
	}
	rel = joinRel(dirRel, final)

	dirAbs, err := project.Resolve(root, dirRel)
	if err != nil {
		return "", fmt.Errorf("docsafe: create %q: %w", rel, err)
	}
	// Lstat so that a link to a folder is refused rather than followed.
	info, err := os.Lstat(dirAbs)
	if err != nil {
		return "", fmt.Errorf("docsafe: create %q: folder %q: %w", rel, dirRel, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("docsafe: create %q: %q is not a folder", rel, dirRel)
	}

	// O_EXCL makes "does it exist" and "create it" one atomic step; on a
	// case-insensitive volume it also catches names differing only in case.
	f, err := os.OpenFile(filepath.Join(dirAbs, final), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("%w: %q", ErrExists, rel)
		}
		return "", fmt.Errorf("docsafe: create %q: %w", rel, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("docsafe: create %q: close: %w", rel, err)
	}
	return rel, nil
}

// requireDocument resolves rel and checks that it is an existing regular file
// of a supported type. A missing file is reported as ErrNotDocument wrapping
// fs.ErrNotExist, so callers can use either errors.Is check.
func requireDocument(root, rel, action string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("%w: %s: the project root", ErrNotDocument, action)
	}
	abs, err := project.Resolve(root, rel)
	if err != nil {
		return "", fmt.Errorf("docsafe: %s %q: %w", action, rel, err)
	}
	if _, ok := project.KindOf(path.Base(rel)); !ok {
		return "", fmt.Errorf("%w: %s %q has no supported extension", ErrNotDocument, action, rel)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%w: %s %q: %w", ErrNotDocument, action, rel, fs.ErrNotExist)
		}
		return "", fmt.Errorf("docsafe: %s %q: %w", action, rel, err)
	}
	if !info.Mode().IsRegular() { // links, folders, devices
		return "", fmt.Errorf("%w: %s %q is not a regular file", ErrNotDocument, action, rel)
	}
	return abs, nil
}

// Rename gives the document at rel a new base name and returns its new path
// relative to root. The extension never changes.
func (o *Ops) Rename(ctx context.Context, root, rel, newName string) (newRel string, err error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("docsafe: rename %q: %w", rel, err)
	}
	abs, err := requireDocument(root, rel, "rename")
	if err != nil {
		return "", err
	}
	if err := validName(newName); err != nil {
		return "", err
	}

	base := path.Base(rel)
	oldExt := filepath.Ext(base)
	stem := newName
	if kind, ok := project.KindOf(newName); ok {
		oldKind, _ := project.KindOf(base)
		if kind != oldKind {
			// Not ErrInvalidName: the name is fine, the type change is what is
			// refused, and the user is told exactly that.
			return "", fmt.Errorf("%w: %q would change the document type", ErrUnsupportedKind, newName)
		}
		stem = strings.TrimSuffix(newName, filepath.Ext(newName)) // typed the same extension: do not double it
	}
	final := stem + oldExt // old extension kept exactly as it is
	if err := validName(final); err != nil {
		return "", err
	}
	if final == base {
		return rel, nil
	}
	dirRel := path.Dir(rel)
	if dirRel == "." {
		dirRel = ""
	}
	newRel = joinRel(dirRel, final)
	dst := filepath.Join(filepath.Dir(abs), final)

	// A hard link fails if dst exists, unlike os.Rename which silently
	// replaces it; the link shares the inode, so content, mode and mtime stay.
	linkErr := os.Link(abs, dst)
	switch {
	case linkErr == nil:
		if err := os.Remove(abs); err != nil {
			// Undo: dst is only the extra link we just made.
			if uerr := os.Remove(dst); uerr != nil {
				return "", fmt.Errorf("docsafe: rename %q: remove old name: %w; removing the new name %q failed too, both names exist now: %w", rel, err, newRel, uerr)
			}
			return "", fmt.Errorf("docsafe: rename %q: remove old name: %w", rel, err)
		}
		return newRel, nil
	case errors.Is(linkErr, fs.ErrExist):
		// On a case-insensitive volume a case-only change "exists" because it
		// is the same file; then os.Rename cannot overwrite anything else.
		src, serr := os.Lstat(abs)
		dstInfo, derr := os.Lstat(dst)
		if serr != nil || derr != nil || !os.SameFile(src, dstInfo) {
			return "", fmt.Errorf("%w: %q", ErrExists, newRel)
		}
		// The same inode is not enough: dst may be a second hard link with its
		// own directory entry. os.Rename would then do nothing and report
		// success. A case-only change has no entry spelled exactly like final.
		entries, err := os.ReadDir(filepath.Dir(abs))
		if err != nil {
			return "", fmt.Errorf("docsafe: rename %q to %q: %w", rel, newRel, err)
		}
		for _, e := range entries {
			if e.Name() == final {
				return "", fmt.Errorf("%w: %q", ErrExists, newRel)
			}
		}
		if err := os.Rename(abs, dst); err != nil {
			return "", fmt.Errorf("docsafe: rename %q to %q: %w", rel, newRel, err)
		}
		return newRel, nil
	default:
		// The volume may not support hard links. Fall back to check-then-rename;
		// a file created at dst between the check and the rename would be
		// replaced (small race window, only on such volumes).
		if _, err := os.Lstat(dst); err == nil {
			return "", fmt.Errorf("%w: %q", ErrExists, newRel)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("docsafe: rename %q to %q: %w", rel, newRel, err)
		}
		if err := os.Rename(abs, dst); err != nil {
			return "", fmt.Errorf("docsafe: rename %q to %q: %w (link: %w)", rel, newRel, err, linkErr)
		}
		return newRel, nil
	}
}

// Trash moves the document at rel to the user's trash.
func (o *Ops) Trash(ctx context.Context, root, rel string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("docsafe: trash %q: %w", rel, err)
	}
	abs, err := requireDocument(root, rel, "trash")
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	// Probed on a missing path: trash exits 5 with and without -s, so the exit
	// status already signals failure for one file. -s (--stopOnError) is kept as
	// a cheap guard in case the tool's default ever changes. The path is its
	// own argument and absolute, so a name starting with "-" cannot be read as
	// an option.
	out, err := o.run(runCtx, o.trashBin, "-s", abs)
	if err != nil {
		if cerr := runCtx.Err(); cerr != nil && !errors.Is(err, cerr) {
			err = fmt.Errorf("%w: %w", cerr, err) // a killed process only reports "signal: killed"
		}
		return fmt.Errorf("docsafe: trash %q: %w: %s", rel, err, strings.TrimSpace(string(out)))
	}
	// Do not trust the exit status alone: the file must really be gone.
	if _, err := os.Lstat(abs); err == nil {
		return fmt.Errorf("docsafe: trash %q: tool reported success but the file is still there", rel)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("docsafe: trash %q: verify: %w", rel, err)
	}
	return nil
}
