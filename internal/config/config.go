// Package config holds the application configuration: the list of registered
// projects, persisted as one JSON file, and every built-in limit.
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// EnvHome overrides the directory that holds everything Pisar owns.
const EnvHome = "PISAR_HOME"

// EnvLog set to "debug" makes the application log every service call to
// stderr. It is a diagnostic for development and for scripts/smoke.sh.
const EnvLog = "PISAR_LOG"

const (
	fileName    = "config.json"
	appDirName  = "Pisar"
	fileVersion = 1

	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
)

// Built-in limits. They are not written to the config file.
const (
	DefaultWatchDebounce  = 200 * time.Millisecond
	DefaultWatchMaxDelay  = 2 * time.Second
	DefaultMaxTreeNodes   = 20000
	DefaultCommandTimeout = 10 * time.Second
)

// DefaultTrashBin is the macOS tool that moves a file to the Trash. This is
// the only place its path is written down.
const DefaultTrashBin = "/usr/bin/trash"

// Project is one registered folder.
type Project struct {
	ID   string `json:"id"`   // stable, random; independent of the path
	Name string `json:"name"` // base name of the folder, for display
	Path string `json:"path"` // absolute, cleaned
}

// Limits are the built-in limits of the application. They are defined in this
// package only; other packages receive them, they do not invent their own.
type Limits struct {
	WatchDebounce  time.Duration // quiet time after a disk change before the tree is refreshed
	WatchMaxDelay  time.Duration // longest a refresh may be postponed while changes keep coming
	MaxTreeNodes   int           // a project tree larger than this is refused
	CommandTimeout time.Duration // external commands (trash, later pandoc/textutil)
}

var (
	ErrNotFound      = errors.New("config: project not found")
	ErrNotADirectory = errors.New("config: not a directory")
	ErrNewerVersion  = errors.New("config: file was written by a newer version of Pisar")
)

// errDamaged marks content that cannot be used; Open moves such a file aside.
var errDamaged = errors.New("config: damaged file")

type fileFormat struct {
	Version  int       `json:"version"`
	Projects []Project `json:"projects"`
}

// Store is the loaded configuration. It is safe for concurrent use.
type Store struct {
	home      string
	recovered string

	mu       sync.Mutex
	projects []Project
}

// DefaultHome returns $PISAR_HOME if set (must be absolute), otherwise
// os.UserConfigDir()/Pisar. It creates nothing.
func DefaultHome() (string, error) {
	if v := os.Getenv(EnvHome); v != "" {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("config: %s must be an absolute path, got %q", EnvHome, v)
		}
		return filepath.Clean(v), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: locate user config dir: %w", err)
	}
	return filepath.Join(base, appDirName), nil
}

// Open loads the configuration from home, creating the directory (0700) if needed.
func Open(home string) (*Store, error) {
	if err := os.MkdirAll(home, dirPerm); err != nil {
		return nil, fmt.Errorf("config: create home: %w", err)
	}
	s := &Store{home: home}
	path := filepath.Join(home, fileName)

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil // nothing is written until the first change
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	projects, err := parse(data)
	switch {
	case err == nil:
		s.projects = projects
	case errors.Is(err, ErrNewerVersion):
		// Never touch a file written by a newer version.
		return nil, err
	default:
		moved, mvErr := moveAside(home, path)
		if mvErr != nil {
			return nil, fmt.Errorf("config: %w; could not set the file aside: %w", err, mvErr)
		}
		s.recovered = moved
	}
	return s, nil
}

// parse validates the file content. Unknown fields are rejected on purpose:
// for the same version they mean we do not understand the file, and saving
// would silently drop them; treating the file as damaged keeps a copy instead.
func parse(data []byte) ([]Project, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("%w: %w", errDamaged, err)
	}
	if head.Version > fileVersion {
		return nil, ErrNewerVersion
	}
	if head.Version != fileVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", errDamaged, head.Version)
	}

	var f fileFormat
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: %w", errDamaged, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing data", errDamaged)
	}

	seen := make(map[string]bool, len(f.Projects))
	for _, p := range f.Projects {
		if p.ID == "" || p.Path == "" || !filepath.IsAbs(p.Path) || seen[p.ID] {
			return nil, fmt.Errorf("%w: invalid project entry %q", errDamaged, p.ID)
		}
		seen[p.ID] = true
	}
	return f.Projects, nil
}

// moveAside renames the damaged file so the original bytes are kept.
func moveAside(home, path string) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for i := 0; ; i++ {
		name := fmt.Sprintf("config.bad-%s.json", stamp)
		if i > 0 {
			name = fmt.Sprintf("config.bad-%s-%d.json", stamp, i)
		}
		dst := filepath.Join(home, name)
		// Rename would overwrite an existing file, so check first.
		if _, err := os.Lstat(dst); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if err := os.Rename(path, dst); err != nil {
			return "", err
		}
		return dst, nil
	}
}

// Home returns the directory that holds the configuration.
func (s *Store) Home() string { return s.home }

// Limits returns the built-in limits.
func (s *Store) Limits() Limits {
	return Limits{
		WatchDebounce:  DefaultWatchDebounce,
		WatchMaxDelay:  DefaultWatchMaxDelay,
		MaxTreeNodes:   DefaultMaxTreeNodes,
		CommandTimeout: DefaultCommandTimeout,
	}
}

// TrashBin returns the path of the tool that moves files to the Trash. It is a
// method of its own rather than a Limits field because it is a path, not a limit.
func (s *Store) TrashBin() string { return DefaultTrashBin }

// Recovered returns the path the damaged file was moved to by Open, or "".
func (s *Store) Recovered() string { return s.recovered }

// Projects returns a copy of the projects in the order they were added.
func (s *Store) Projects() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.projects)
}

// Project returns the project with the given ID.
func (s *Store) Project(id string) (Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.projects {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

// AddProject registers a folder. If the same folder (after resolving
// symlinks) is already registered, the existing project is returned.
func (s *Store) AddProject(path string) (Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Project{}, fmt.Errorf("config: resolve %q: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Project{}, fmt.Errorf("config: add project: %w", err)
	}
	if !info.IsDir() {
		return Project{}, fmt.Errorf("%w: %s", ErrNotADirectory, abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Project{}, fmt.Errorf("config: resolve symlinks of %q: %w", abs, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.projects {
		// A registered folder may be gone (unmounted disk); fall back to its stored path.
		other, err := filepath.EvalSymlinks(p.Path)
		if err != nil {
			other = p.Path
		}
		if other == resolved {
			return p, nil
		}
	}

	id, err := newID()
	if err != nil {
		return Project{}, err
	}
	p := Project{ID: id, Name: filepath.Base(abs), Path: abs}
	next := append(slices.Clone(s.projects), p)
	if err := save(s.home, next); err != nil {
		return Project{}, err
	}
	s.projects = next
	return p, nil
}

// RemoveProject forgets a project. The folder on disk is not touched.
func (s *Store) RemoveProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := slices.IndexFunc(s.projects, func(p Project) bool { return p.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	next := slices.Delete(slices.Clone(s.projects), i, i+1)
	if err := save(s.home, next); err != nil {
		return err
	}
	s.projects = next
	return nil
}

func newID() (string, error) {
	var b [16]byte
	// rand.Read never returns an error (see go doc crypto/rand.Read).
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("config: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// save writes the file atomically: temp file in the same directory (so the
// rename stays on one filesystem), fsync, rename.
func save(home string, projects []Project) (err error) {
	if projects == nil {
		projects = []Project{}
	}
	data, err := json.MarshalIndent(fileFormat{Version: fileVersion, Projects: projects}, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(home, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(filePerm); err != nil {
		return fmt.Errorf("config: chmod temp file: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("config: write temp file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("config: sync temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("config: close temp file: %w", err)
	}
	if err = os.Rename(tmp.Name(), filepath.Join(home, fileName)); err != nil {
		return fmt.Errorf("config: replace config file: %w", err)
	}
	return nil
}
