// Package project reads the document tree of a project folder and safely maps
// frontend-supplied relative paths to paths on disk. It never writes.
package project

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// Kind is the type of a tree node.
type Kind string

const (
	KindDir  Kind = "dir"
	KindMD   Kind = "md"
	KindTXT  Kind = "txt"
	KindDOCX Kind = "docx"
	KindDOC  Kind = "doc"
)

// Node is one row of the tree. It is sent to the frontend as JSON.
type Node struct {
	Name     string `json:"name"` // name on disk, with extension, exactly as the filesystem reports it
	Rel      string `json:"rel"`  // path relative to the project root, always with "/" separators
	Kind     Kind   `json:"kind"`
	Children []Node `json:"children,omitempty"` // only for KindDir
}

// Tree is the result of a scan.
type Tree struct {
	Nodes   []Node   // top level of the project
	Skipped []string // Rel of directories that could not be read (permission denied and the like)
}

var (
	ErrTooLarge    = errors.New("project: tree has too many entries")
	ErrInvalidPath = errors.New("project: invalid relative path")
	ErrOutsideRoot = errors.New("project: path is outside the project")
)

// KindOf reports the document kind of a file name by its extension
// (case-insensitive). ok is false for anything that is not a supported document.
func KindOf(name string) (kind Kind, ok bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md":
		return KindMD, true
	case ".txt":
		return KindTXT, true
	case ".docx":
		return KindDOCX, true
	case ".doc":
		return KindDOC, true
	}
	return "", false
}

type scanner struct {
	maxNodes int
	count    int
	skipped  []string
}

// Scan reads the tree under root.
func Scan(ctx context.Context, root string, maxNodes int) (Tree, error) {
	if maxNodes <= 0 {
		return Tree{}, fmt.Errorf("project: maxNodes must be positive, got %d", maxNodes)
	}
	info, err := os.Stat(root) // follows a symlinked root on purpose
	if err != nil {
		return Tree{}, fmt.Errorf("project: scan root: %w", err)
	}
	if !info.IsDir() {
		return Tree{}, fmt.Errorf("project: root %q is not a directory", root)
	}
	// Resolve once so every later ReadDir runs on a real directory; Rel stays
	// relative to root either way.
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Tree{}, fmt.Errorf("project: resolve root: %w", err)
	}

	s := &scanner{maxNodes: maxNodes}
	nodes, err := s.walk(ctx, real, "")
	if err != nil {
		return Tree{}, err
	}
	return Tree{Nodes: nodes, Skipped: s.skipped}, nil
}

// add counts one node and enforces the limit while scanning, so a huge tree
// is abandoned early instead of being built in full.
func (s *scanner) add() error {
	s.count++
	if s.count > s.maxNodes {
		return fmt.Errorf("%w: more than %d", ErrTooLarge, s.maxNodes)
	}
	return nil
}

// walk lists dir. rel is the "/"-separated path of dir below the root ("" for the root).
func (s *scanner) walk(ctx context.Context, dir, rel string) ([]Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("project: scan canceled: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if rel == "" {
			return nil, fmt.Errorf("project: read root: %w", err)
		}
		// One unreadable folder must not hide the rest of the project.
		s.skipped = append(s.skipped, rel)
		return nil, nil
	}

	var nodes []Node
	for _, e := range entries { // os.ReadDir returns entries sorted by name
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") {
			continue
		}
		childRel := name
		if rel != "" {
			childRel = path.Join(rel, name)
		}
		// Type() comes from the directory read and does not follow links, so
		// symlinks show up as ModeSymlink and are skipped (no loops, no escapes).
		typ := e.Type()
		switch {
		case typ&fs.ModeSymlink != 0:
			continue
		case typ.IsDir():
			children, err := s.walk(ctx, filepath.Join(dir, name), childRel)
			if err != nil {
				return nil, err
			}
			if len(children) == 0 {
				continue
			}
			if err := s.add(); err != nil {
				return nil, err
			}
			nodes = append(nodes, Node{Name: name, Rel: childRel, Kind: KindDir, Children: children})
		case typ.IsRegular():
			kind, ok := KindOf(name)
			if !ok {
				continue
			}
			if err := s.add(); err != nil {
				return nil, err
			}
			nodes = append(nodes, Node{Name: name, Rel: childRel, Kind: kind})
		}
	}
	return nodes, nil
}

// Resolve turns rel (as sent by the frontend) into an absolute path inside root.
func Resolve(root, rel string) (string, error) {
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("%w: contains NUL", ErrInvalidPath)
	}
	if rel != "" {
		// Splitting on "/" catches leading/trailing/double slashes (empty parts).
		// Backslash is a valid name character on macOS, so it is not special.
		for _, part := range strings.Split(rel, "/") {
			if part == "" || part == "." || part == ".." {
				return "", fmt.Errorf("%w: %q", ErrInvalidPath, rel)
			}
		}
	}
	target := filepath.Join(root, rel)

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("project: resolve root: %w", err)
	}

	// The target may not exist yet, so check its deepest existing ancestor.
	p := target
	var realAnc string
	for {
		realAnc, err = filepath.EvalSymlinks(p)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return "", fmt.Errorf("project: resolve %q: %w", rel, err)
		}
		if _, lerr := os.Lstat(p); lerr == nil {
			// The entry exists but cannot be resolved: a dangling symlink.
			// Its destination cannot be verified, so refuse it.
			return "", fmt.Errorf("%w: dangling link %q", ErrOutsideRoot, rel)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", fmt.Errorf("project: resolve %q: %w", rel, err)
		}
		p = parent
	}

	// filepath.Rel is lexical but compares whole components, so "/a/proj"
	// is not mistaken for being inside "/a/pro".
	r, err := filepath.Rel(realRoot, realAnc)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrOutsideRoot, rel)
	}
	return target, nil
}
