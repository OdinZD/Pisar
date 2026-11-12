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
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// EnvHome overrides the directory that holds everything Pisar owns.
const EnvHome = "PISAR_HOME"

// Window size in points. The minimum comes from docs/design/DESIGN.md.
const (
	WindowWidth     = 1280
	WindowHeight    = 800
	WindowMinWidth  = 1000
	WindowMinHeight = 600
)

// EnvNvimSpellFile is the environment variable that carries the path of the
// file with the user's own words (NvimPaths.UserWords) to nvim/init.lua, which
// must not guess paths outside its own folder. The two names must match.
const EnvNvimSpellFile = "PISAR_SPELLFILE"

// EnvNvimUserLua is the environment variable that carries the path of the
// user's own Neovim file (NvimPaths.UserLua) to nvim/init.lua. The two names
// must match.
const EnvNvimUserLua = "PISAR_USER_LUA"

// EnvLog set to "debug" makes the application log every service call to
// stderr. It is a diagnostic for development and for scripts/smoke.sh.
const EnvLog = "PISAR_LOG"

const (
	fileName   = "config.json"
	appDirName = "Pisar"
	// fileVersion is the version Pisar writes. Version 1 (no theme) is still read;
	// the first write after reading it upgrades the file.
	fileVersion   = 2
	fileVersionV1 = 1

	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
)

// Built-in limits. They are not written to the config file.
const (
	DefaultWatchDebounce  = 200 * time.Millisecond
	DefaultWatchMaxDelay  = 2 * time.Second
	DefaultMaxTreeNodes   = 20000
	DefaultCommandTimeout = 10 * time.Second
	// DefaultConvertTimeout bounds one call of pandoc or textutil. It is longer
	// than DefaultCommandTimeout because a large document with many images takes
	// far longer to convert than trash takes to move a file.
	DefaultConvertTimeout = 60 * time.Second
	// DefaultMaxBackups is how many backups of one document are kept; older ones
	// are rotated out.
	DefaultMaxBackups = 10
	// DefaultExportAttempts bounds how often one export converts the Markdown
	// again because the editor wrote it while pandoc was reading it. A real save
	// takes milliseconds, so more than one repeat means something keeps writing;
	// 4 allows a few unlucky overlaps and still ends the export with an error
	// instead of looping.
	DefaultExportAttempts = 4
	// The two .docx numbers bound the inspection of one archive that may come
	// from anywhere (see internal/convert). Both are far above anything a real
	// document needs and only exist to stop a hostile archive:
	//   - DefaultMaxDocxEntries: a .docx has a few dozen entries; one with many
	//     images has a few hundred. 10 000 leaves room and still caps the
	//     central directory that has to be walked.
	//   - DefaultMaxDocxPartBytes: word/document.xml of a whole book is tens of
	//     MiB uncompressed. 256 MiB is several times that and still finishes in
	//     seconds, so a decompression bomb is refused instead of filling memory
	//     or the disk.
	DefaultMaxDocxEntries   = 10000
	DefaultMaxDocxPartBytes = 256 << 20
	// DefaultQuitDialogNames is how many document names the quit dialog lists
	// before it says "…and N more".
	DefaultQuitDialogNames = 5

	// DefaultNvimStartTimeout bounds the wait until Neovim has started and
	// run init.lua. DefaultNvimStopTimeout applies to each stage of its
	// shutdown (RPC quit, SIGTERM, SIGKILL), not to the whole of it.
	DefaultNvimStartTimeout = 10 * time.Second
	DefaultNvimStopTimeout  = 3 * time.Second

	// The two terminal numbers trade the latency of a typed key's echo against
	// the number of events the webview has to handle while the screen scrolls.
	// Both were measured in the phase 0 spike:
	//   - DefaultTermFlushInterval: 8 ms is half a 60 Hz frame, so an echo waits
	//     at most about one frame in the worst case and normally far less, while
	//     a full-screen scroll (Neovim emits output continuously) costs at most
	//     125 events per second instead of one per PTY read.
	//   - DefaultTermMaxChunk: 16 KiB is a few full 200x60 screens of escape
	//     sequences. A burst bigger than this is cut and sent at once rather
	//     than waiting for the timer, so one event (JSON-encoded, then parsed by
	//     xterm.js in one write call) stays small enough not to stall the UI.
	DefaultTermFlushInterval = 8 * time.Millisecond
	DefaultTermMaxChunk      = 16 << 10

	// The preview numbers (see internal/preview):
	//   - DefaultPreviewDebounce: quiet time after the last keystroke before the
	//     preview is rendered again. 300 ms is the design's value: short enough to
	//     feel live, long enough that a burst of typing costs one pandoc run
	//     (about 125 ms for a small document) and not one per key.
	//   - DefaultPreviewTimeout: one pandoc run. A small document takes 125 ms, so
	//     10 s is far above any real page and still ends a run that hangs on a
	//     pathological input before the next refresh is due.
	//   - DefaultMaxPreviewSource: the text of one buffer that is converted. 8 MiB
	//     is a long book in Markdown; a larger text is not a page anyone previews,
	//     and every refresh would re-convert all of it.
	//   - DefaultMaxPreviewHTML: the output of one conversion. Pictures are
	//     embedded as base64, which is a third bigger than the file, so a document
	//     with a few photographs needs far more than its source; 64 MiB is
	//     generous for that and still bounds what the app keeps and sends to the
	//     webview.
	//   - DefaultMaxSearchResults: the hits one search returns; the list is read
	//     by eye, and the first 50 of an ambiguous query are all that fit anyway.
	DefaultPreviewDebounce  = 300 * time.Millisecond
	DefaultPreviewTimeout   = 10 * time.Second
	DefaultMaxPreviewSource = 8 << 20
	DefaultMaxPreviewHTML   = 64 << 20
	DefaultMaxSearchResults = 50
)

// Neovim as Pisar runs it. These are the only places the names are written down
// (nvim/init.lua refers to the colorscheme by the same name).
const (
	// NvimHrDictionary is the Croatian dictionary the application ships; Neovim
	// looks for "spell/<lang>.utf-8.spl" on the runtimepath (:help spell-load).
	NvimHrDictionary = "hr.utf-8.spl"
	// nvimUserWordsName is the file of the user's own words. 'spellfile' must end
	// in ".utf-8.add" (:help 'spellfile'); Neovim keeps the compiled list next to
	// it as "<name>.spl" and uses it for every language in 'spelllang'.
	nvimUserWordsName = "pisar.utf-8.add"
	// nvimUserLuaName is the user's own Neovim file in the home folder.
	nvimUserLuaName = "user.lua"

	NvimBin         = "nvim"
	NvimAppName     = "pisar" // NVIM_APPNAME: isolates Pisar's Neovim from the user's own
	NvimColorscheme = "pisar"

	// nvimDirName is the folder under the home that holds everything Neovim
	// stores for Pisar, one subfolder per XDG base directory.
	nvimDirName = "nvim"

	// NvimRunDirPrefix starts the name of the short-lived temporary folder that
	// holds the RPC socket. The name continues with the PID of the application
	// (see editorservice.go), so a folder left by a crash can be told from a live one.
	NvimRunDirPrefix = "pisar-run-"

	// DefaultMaxColorscheme bounds the colorscheme text the frontend may send.
	// The generated one is about 12 KiB; 256 KiB leaves room to grow while still
	// refusing anything that is not that generator's output.
	DefaultMaxColorscheme = 256 << 10
)

// DefaultTrashBin is the macOS tool that moves a file to the Trash. This is
// the only place its path is written down.
const DefaultTrashBin = "/usr/bin/trash"

// External converters. As with NvimBin and DefaultTrashBin, these are the only
// places the names are written down.
const (
	// PandocBin is looked up in PATH (exec.LookPath), like NvimBin.
	PandocBin = "pandoc"
	// DefaultTextutilBin is part of macOS, so its absolute path is fixed.
	DefaultTextutilBin = "/usr/bin/textutil"
)

// Folders under the home. Both hold data Pisar owns, never the user's documents.
const (
	// workDirName holds one working copy per open Word document.
	workDirName = "work"
	// backupsDirName holds the backups taken before a .docx is overwritten.
	backupsDirName = "backups"
	// previewDirName holds the files the preview needs next to pandoc (its filter).
	previewDirName = "preview"
	// spellDirName holds the user's own spelling words. Pisar never rewrites it,
	// unlike the Neovim configuration folder, which is replaced at every start.
	spellDirName = "spell"
)

// Theme IDs. The order of Themes is the order in the menu.
const (
	ThemePisar           = "pisar"
	ThemeCatppuccinMocha = "catppuccin-mocha"
	ThemeDracula         = "dracula"
	DefaultTheme         = ThemePisar
)

// Themes returns every known theme ID in menu order. It is a new slice on every
// call, so a caller cannot corrupt the list.
func Themes() []string {
	return []string{ThemePisar, ThemeCatppuccinMocha, ThemeDracula}
}

// Project is one registered folder.
type Project struct {
	ID   string `json:"id"`   // stable, random; independent of the path
	Name string `json:"name"` // base name of the folder, for display
	Path string `json:"path"` // absolute, cleaned
}

// Limits are the built-in limits of the application. They are defined in this
// package only; other packages receive them, they do not invent their own.
type Limits struct {
	WatchDebounce    time.Duration // quiet time after a disk change before the tree is refreshed
	WatchMaxDelay    time.Duration // longest a refresh may be postponed while changes keep coming
	MaxTreeNodes     int           // a project tree larger than this is refused
	CommandTimeout   time.Duration // short external commands (trash)
	ConvertTimeout   time.Duration // one call of pandoc or textutil; longer than CommandTimeout, see DefaultConvertTimeout
	MaxBackups       int           // backups kept per document
	ExportAttempts   int           // conversions of one export, repeated while the editor writes the Markdown
	MaxDocxEntries   int           // archive entries a .docx may have before it is refused
	MaxDocxPartBytes int64         // uncompressed bytes read from one XML part of a .docx before it is refused
	QuitDialogNames  int           // document names listed in the quit dialog

	NvimStartTimeout time.Duration // longest wait until Neovim is up and init.lua has run
	NvimStopTimeout  time.Duration // longest wait per shutdown stage of Neovim

	TermFlushInterval time.Duration // longest a pending byte of terminal output waits before it is sent
	TermMaxChunk      int           // pending terminal output bytes that force an immediate send

	MaxColorscheme int // largest colorscheme text, in bytes, the editor accepts from the frontend

	PreviewDebounce  time.Duration // quiet time after an edit before the preview is rendered again
	PreviewTimeout   time.Duration // one conversion of the preview
	MaxPreviewSource int           // largest Markdown text, in bytes, that is converted for the preview
	MaxPreviewHTML   int           // largest HTML, in bytes, one conversion may produce
	MaxSearchResults int           // hits a search by name returns
}

var (
	ErrNotFound      = errors.New("config: project not found")
	ErrNotADirectory = errors.New("config: not a directory")
	ErrNewerVersion  = errors.New("config: file was written by a newer version of Pisar")
	ErrUnknownTheme  = errors.New("config: unknown theme")
)

// errDamaged marks content that cannot be used; Open moves such a file aside.
var errDamaged = errors.New("config: damaged file")

// fileFormatV1 is kept apart from fileFormat so that a version 1 file with a
// "theme" field is still rejected by DisallowUnknownFields: such a file is not
// one this program or an older one wrote.
type fileFormatV1 struct {
	Version  int       `json:"version"`
	Projects []Project `json:"projects"`
}

type fileFormat struct {
	Version  int       `json:"version"`
	Theme    string    `json:"theme"`
	Projects []Project `json:"projects"`
}

// state is what a config file holds, whatever its version.
type state struct {
	projects []Project
	theme    string
}

// Store is the loaded configuration. It is safe for concurrent use.
type Store struct {
	home      string
	recovered string

	mu       sync.Mutex
	projects []Project
	theme    string // never empty
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
	s := &Store{home: home, theme: DefaultTheme}
	path := filepath.Join(home, fileName)

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil // nothing is written until the first change
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	st, err := parse(data)
	switch {
	case err == nil:
		s.projects = st.projects
		s.theme = st.theme
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
// A version 1 file has no theme, so it gets DefaultTheme; it is not rewritten
// until the next change.
func parse(data []byte) (state, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return state{}, fmt.Errorf("%w: %w", errDamaged, err)
	}
	if head.Version > fileVersion {
		return state{}, ErrNewerVersion
	}

	var st state
	var projects []Project
	switch head.Version {
	case fileVersionV1:
		var f fileFormatV1
		if err := decodeStrict(data, &f); err != nil {
			return state{}, err
		}
		projects = f.Projects
		st.theme = DefaultTheme
	case fileVersion:
		var f fileFormat
		if err := decodeStrict(data, &f); err != nil {
			return state{}, err
		}
		projects = f.Projects
		st.theme = f.Theme
		// An unknown theme (for example from a build that had more of them) is
		// not damage: the file keeps its bytes and only the shown theme falls back.
		// An empty value is the same as an omitted field and needs no warning.
		if !slices.Contains(Themes(), st.theme) {
			if st.theme != "" {
				slog.Warn("config: unknown theme in config file, using the default", "theme", st.theme, "default", DefaultTheme)
			}
			st.theme = DefaultTheme
		}
	default:
		return state{}, fmt.Errorf("%w: unsupported version %d", errDamaged, head.Version)
	}

	seen := make(map[string]bool, len(projects))
	for _, p := range projects {
		if p.ID == "" || p.Path == "" || !filepath.IsAbs(p.Path) || seen[p.ID] {
			return state{}, fmt.Errorf("%w: invalid project entry %q", errDamaged, p.ID)
		}
		seen[p.ID] = true
	}
	st.projects = projects
	return st, nil
}

// decodeStrict decodes exactly one JSON value into v, refusing unknown fields
// and trailing data.
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", errDamaged, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing data", errDamaged)
	}
	return nil
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
		WatchDebounce:    DefaultWatchDebounce,
		WatchMaxDelay:    DefaultWatchMaxDelay,
		MaxTreeNodes:     DefaultMaxTreeNodes,
		CommandTimeout:   DefaultCommandTimeout,
		ConvertTimeout:   DefaultConvertTimeout,
		MaxBackups:       DefaultMaxBackups,
		ExportAttempts:   DefaultExportAttempts,
		MaxDocxEntries:   DefaultMaxDocxEntries,
		MaxDocxPartBytes: DefaultMaxDocxPartBytes,
		QuitDialogNames:  DefaultQuitDialogNames,

		NvimStartTimeout: DefaultNvimStartTimeout,
		NvimStopTimeout:  DefaultNvimStopTimeout,

		TermFlushInterval: DefaultTermFlushInterval,
		TermMaxChunk:      DefaultTermMaxChunk,

		MaxColorscheme: DefaultMaxColorscheme,

		PreviewDebounce:  DefaultPreviewDebounce,
		PreviewTimeout:   DefaultPreviewTimeout,
		MaxPreviewSource: DefaultMaxPreviewSource,
		MaxPreviewHTML:   DefaultMaxPreviewHTML,
		MaxSearchResults: DefaultMaxSearchResults,
	}
}

// NvimPaths are the names and locations Pisar's Neovim runs with. They are a
// value of their own, not Limits fields, because they are names and paths.
type NvimPaths struct {
	Bin         string // binary name, resolved with exec.LookPath
	AppName     string // NVIM_APPNAME
	Colorscheme string // colorscheme name, also the file name in colors/

	// The four XDG base directories Neovim's stdpath() reads. Each lies under
	// <home>/nvim, so nothing Neovim writes ends up in the user's ~/.config,
	// ~/.local or ~/.cache.
	ConfigHome, DataHome, StateHome, CacheHome string

	InitLua         string // <ConfigHome>/<AppName>/init.lua, passed to `nvim -u`
	SpellDir        string // <ConfigHome>/<AppName>/spell: stdpath("config") is on the runtimepath, so Neovim finds the shipped dictionaries here
	HrDictionary    string // <SpellDir>/hr.utf-8.spl
	ColorschemeFile string // <ConfigHome>/<AppName>/colors/<Colorscheme>.lua; stdpath("config") is on the runtimepath

	// UserWords is the file for the words added with zg, <home>/spell/pisar.utf-8.add,
	// handed to Neovim as 'spellfile' (see EnvNvimSpellFile). It lies outside
	// <home>/nvim on purpose: the application rewrites that folder at every
	// start, and these words are the user's. The folder is created by the
	// application, the file only by Neovim, and Neovim writes "<UserWords>.spl"
	// beside it.
	UserWords string

	// UserLua is <home>/user.lua, the user's own Neovim configuration, which
	// init.lua loads after its own when the file exists (see EnvNvimUserLua). It
	// is the user's file: the application never creates, changes or deletes it,
	// and it lies outside <home>/nvim, which is rewritten at every start.
	UserLua string

	RunDirPrefix string // see NvimRunDirPrefix
}

// Nvim returns where Neovim's files live under the home. It creates nothing.
func (s *Store) Nvim() NvimPaths {
	base := filepath.Join(s.home, nvimDirName)
	cfg := filepath.Join(base, "config")
	appCfg := filepath.Join(cfg, NvimAppName)
	return NvimPaths{
		Bin:         NvimBin,
		AppName:     NvimAppName,
		Colorscheme: NvimColorscheme,

		ConfigHome: cfg,
		DataHome:   filepath.Join(base, "data"),
		StateHome:  filepath.Join(base, "state"),
		CacheHome:  filepath.Join(base, "cache"),

		InitLua:         filepath.Join(appCfg, "init.lua"),
		SpellDir:        filepath.Join(appCfg, "spell"),
		HrDictionary:    filepath.Join(appCfg, "spell", NvimHrDictionary),
		ColorschemeFile: filepath.Join(appCfg, "colors", NvimColorscheme+".lua"),

		UserWords: filepath.Join(s.home, spellDirName, nvimUserWordsName),
		UserLua:   filepath.Join(s.home, nvimUserLuaName),

		RunDirPrefix: NvimRunDirPrefix,
	}
}

// TrashBin returns the path of the tool that moves files to the Trash. It is a
// method of its own rather than a Limits field because it is a path, not a limit.
func (s *Store) TrashBin() string { return DefaultTrashBin }

// PandocBin returns the pandoc binary name, resolved with exec.LookPath.
func (s *Store) PandocBin() string { return PandocBin }

// TextutilBin returns the path of macOS textutil, which converts .doc to .docx.
func (s *Store) TextutilBin() string { return DefaultTextutilBin }

// WorkDir returns the folder that holds the working copies. It creates nothing.
func (s *Store) WorkDir() string { return filepath.Join(s.home, workDirName) }

// BackupsDir returns the folder that holds the backups. It creates nothing.
func (s *Store) BackupsDir() string { return filepath.Join(s.home, backupsDirName) }

// PreviewDir returns the folder for the files the preview keeps next to pandoc.
// It is Pisar's own; the user's documents are never there. It creates nothing.
func (s *Store) PreviewDir() string { return filepath.Join(s.home, previewDirName) }

// Recovered returns the path the damaged file was moved to by Open, or "".
func (s *Store) Recovered() string { return s.recovered }

// Theme returns the ID of the chosen theme; DefaultTheme until one is chosen.
func (s *Store) Theme() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.theme
}

// SetTheme chooses a theme and writes the file. An unknown ID is refused with
// ErrUnknownTheme and nothing is written; choosing the current theme again
// writes nothing either.
func (s *Store) SetTheme(id string) error {
	if !slices.Contains(Themes(), id) {
		return fmt.Errorf("%w: %q", ErrUnknownTheme, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == s.theme {
		return nil
	}
	if err := save(s.home, s.projects, id); err != nil {
		return err
	}
	s.theme = id
	return nil
}

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
	if err := save(s.home, next, s.theme); err != nil {
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
	if err := save(s.home, next, s.theme); err != nil {
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
func save(home string, projects []Project, theme string) (err error) {
	if projects == nil {
		projects = []Project{}
	}
	data, err := json.MarshalIndent(fileFormat{Version: fileVersion, Theme: theme, Projects: projects}, "", "  ")
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
