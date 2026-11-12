package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"Pisar/internal/config"
	"Pisar/internal/convert"
	"Pisar/internal/docsafe"
	"Pisar/internal/nvim"
	"Pisar/internal/preview"
	"Pisar/internal/project"
	"Pisar/internal/term"
)

// Test names are short on purpose: t.TempDir() embeds the test name and the RPC
// socket path inside it has to fit sun_path (103 bytes on macOS).

const edWait = 15 * time.Second

type recorded struct {
	name string
	data any
}

// recorder is the fake emit: it keeps every event in order.
type recorder struct {
	mu  sync.Mutex
	evs []recorded
}

func (r *recorder) emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, recorded{name, data})
}

// mark returns a position to wait from, so earlier events are not mistaken for new ones.
func (r *recorder) mark() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.evs)
}

func (r *recorder) since(from int) []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.evs[from:]...)
}

// wait blocks until an event after position from satisfies match.
func (r *recorder) wait(t *testing.T, from int, what string, match func(recorded) bool) recorded {
	t.Helper()
	deadline := time.Now().Add(edWait)
	for {
		for _, ev := range r.since(from) {
			if match(ev) {
				return ev
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s; got %v", what, r.since(from))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (r *recorder) waitName(t *testing.T, from int, name string) recorded {
	t.Helper()
	return r.wait(t, from, name, func(ev recorded) bool { return ev.name == name })
}

// waitActive waits for a document:active that satisfies match.
func (r *recorder) waitActive(t *testing.T, from int, what string, match func(DocumentActive) bool) DocumentActive {
	t.Helper()
	ev := r.wait(t, from, "document:active "+what, func(ev recorded) bool {
		d, ok := ev.data.(DocumentActive)
		return ev.name == eventDocumentActive && ok && match(d)
	})
	return ev.data.(DocumentActive)
}

func (r *recorder) waitState(t *testing.T, from int, file string, modified bool) {
	t.Helper()
	r.wait(t, from, fmt.Sprintf("document:state %s %v", file, modified), func(ev recorded) bool {
		d, ok := ev.data.(DocumentState)
		return ev.name == eventDocumentState && ok && d.Modified == modified && sameFile(d.Path, file)
	})
}

func sameFile(a, b string) bool {
	return realPath(a) == realPath(b)
}

// realPath resolves links of the folder part, as Neovim reports paths (/tmp is
// /private/tmp on macOS).
func realPath(p string) string {
	if p == "" {
		return ""
	}
	return resolveDir(p)
}

type fakeClip struct {
	mu    sync.Mutex
	texts []string
	ok    bool
}

func (c *fakeClip) set(text string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.texts = append(c.texts, text)
	return c.ok
}

func (c *fakeClip) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.texts...)
}

// edEnv is an EditorService on a real config.Store, with everything it and
// Neovim write confined to one t.TempDir().
type edEnv struct {
	t        *testing.T
	root     string // t.TempDir()
	home     string // the Pisar home (config.Store)
	projects string // folders of the test projects
	fakeHome string // $HOME of Neovim; must stay empty
	store    *config.Store
	rec      *recorder
	clip     *fakeClip
	svc      *EditorService
	evals    int
}

// newEd builds the service without starting Neovim. mutate may change what it is built from.
func newEd(t *testing.T, mutate ...func(*editorServiceDeps)) *edEnv {
	t.Helper()
	// $TMPDIR is about 50 bytes long, which leaves no room for the socket folder
	// and the socket under a test's own folder, so the test folder is made under
	// /tmp. Neovim then gets a $TMPDIR inside it (it makes /tmp/nvim.<user> there
	// otherwise) and a $HOME of its own, so nothing lands outside the test folder.
	t.Setenv("TMPDIR", "/tmp")
	root := t.TempDir()
	mk := func(name string) string {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	e := &edEnv{t: t, root: root, rec: &recorder{}, clip: &fakeClip{ok: true}}
	e.home, e.projects, e.fakeHome = mk("pisar"), mk("p"), mk("h")
	t.Setenv("TMPDIR", mk("t"))
	t.Setenv("HOME", e.fakeHome)

	store, err := config.Open(e.home)
	if err != nil {
		t.Fatal(err)
	}
	e.store = store
	limits := store.Limits()
	limits.NvimStartTimeout = edWait
	limits.NvimStopTimeout = 5 * time.Second
	deps := editorServiceDeps{
		Store:     store,
		Emit:      e.rec.emit,
		Clipboard: e.clip.set,
		Limits:    limits,
		RunDir:    mk("r"),
	}
	for _, m := range mutate {
		m(&deps)
	}
	svc, err := newEditorService(deps)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	t.Cleanup(func() {
		svc.shutdown()
		if entries, err := os.ReadDir(e.fakeHome); err != nil || len(entries) > 0 {
			t.Errorf("Neovim wrote into $HOME: %v %v", entries, err)
		}
	})
	return e
}

func needNvim(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not found in PATH")
	}
}

// sample is the generator's real output, kept current by a vitest snapshot.
func sample(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "colorscheme", "pisar.lua"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// startEd builds the service and starts Neovim with the sample colorscheme.
func startEd(t *testing.T, mutate ...func(*editorServiceDeps)) *edEnv {
	t.Helper()
	needNvim(t)
	e := newEd(t, mutate...)
	if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return e
}

// addProject registers a new folder under e.projects and returns it.
func (e *edEnv) addProject(name string) (config.Project, string) {
	e.t.Helper()
	dir := filepath.Join(e.projects, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	p, err := e.store.AddProject(dir)
	if err != nil {
		e.t.Fatal(err)
	}
	return p, dir
}

// eval has Neovim write the items of a Vim list expression to a file, one per
// line, and returns them: the only way to read Neovim's state through the PTY.
func (e *edEnv) eval(list string) []string {
	e.t.Helper()
	e.evals++
	out := filepath.Join(e.root, "o"+strconv.Itoa(e.evals))
	if err := e.svc.Write(":call writefile(" + list + ", '" + out + "')\r"); err != nil {
		e.t.Fatal(err)
	}
	deadline := time.Now().Add(edWait)
	for {
		if b, err := os.ReadFile(out); err == nil && strings.HasSuffix(string(b), "\n") {
			return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("timeout waiting for Neovim to answer %s", list)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func readString(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tokenColor reads a colour of tokens.ts, so the test does not repeat the value.
func tokenColor(t *testing.T, token string) string {
	t.Helper()
	src := readString(t, filepath.Join("frontend", "src", "theme", "tokens.ts"))
	re := regexp.MustCompile(`(?m)^\s*'?` + regexp.QuoteMeta(token) + `'?:\s*'(#[0-9A-Fa-f]{6})'`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("token %q not found in tokens.ts", token)
	}
	return m[1]
}

func TestEdStart(t *testing.T) {
	e := startEd(t)
	e.rec.waitName(t, 0, eventPtyData)
	cfg := filepath.Join(e.home, "nvim", "config", "pisar")
	if got := readString(t, filepath.Join(cfg, "init.lua")); got != readString(t, filepath.Join("nvim", "init.lua")) {
		t.Error("init.lua is not the one the application ships")
	}
	if got := readString(t, filepath.Join(cfg, "colors", "pisar.lua")); got != sample(t) {
		t.Error("colorscheme file differs from what Start was given")
	}
	for _, p := range []string{
		filepath.Join(cfg, "init.lua"), filepath.Join(cfg, "colors", "pisar.lua"),
	} {
		if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", p, info, err)
		}
	}
	for _, p := range []string{cfg, filepath.Join(e.home, "nvim", "data"), filepath.Join(e.home, "nvim", "state"), filepath.Join(e.home, "nvim", "cache")} {
		if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", p, info, err)
		}
	}

	got := e.eval(`[stdpath('config'), stdpath('data'), stdpath('state'), stdpath('cache'), g:colors_name]`)
	base := realPath(filepath.Join(e.home, "nvim"))
	for i, name := range []string{"config", "data", "state", "cache"} {
		if !strings.HasPrefix(realPath(got[i]), base+string(filepath.Separator)) {
			t.Errorf("stdpath(%s) = %q, want under %q", name, got[i], base)
		}
	}
	if got[4] != "pisar" {
		t.Errorf("g:colors_name = %q", got[4])
	}
}

func TestEdStartSpell(t *testing.T) {
	e := startEd(t)
	e.rec.waitName(t, 0, eventPtyData)
	paths := e.paths()
	if got := readString(t, paths.HrDictionary); got != string(hrDictionary) {
		t.Error("the dictionary in the spell folder is not the shipped one")
	}
	// The dictionary is found through the runtimepath, the words file through the
	// environment, and Croatian words are accepted without any download.
	got := e.eval(`[&spell, &spelllang, &spellfile, spellbadword('sjećanja')[0], spellbadword('rijč')[0], spellbadword('document')[0]]`)
	want := []string{"1", "hr,en", paths.UserWords, "", "rijč", ""}
	if !slices.Equal(got, want) {
		t.Errorf("spell state = %q, want %q", got, want)
	}
	// Neovim has not created the words file: nothing was added.
	if _, err := os.Lstat(paths.UserWords); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the words file exists without a zg: %v", err)
	}
}

func TestEdWriteConfigDictionary(t *testing.T) {
	e := newEd(t)
	paths := e.paths()
	write := func() {
		t.Helper()
		if err := e.svc.writeConfig(sample(t)); err != nil {
			t.Fatal(err)
		}
	}
	// An old modification time makes a rewrite visible: a new file would have "now".
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	setOld := func() {
		t.Helper()
		if err := os.Chtimes(paths.HrDictionary, old, old); err != nil {
			t.Fatal(err)
		}
	}
	mtime := func() time.Time {
		t.Helper()
		info, err := os.Stat(paths.HrDictionary)
		if err != nil {
			t.Fatal(err)
		}
		return info.ModTime()
	}

	write()
	if got := readString(t, paths.HrDictionary); got != string(hrDictionary) {
		t.Fatal("the dictionary is not byte-identical to the embedded one")
	}
	if info, err := os.Stat(paths.HrDictionary); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("dictionary: %v %v", info, err)
	}
	if info, err := os.Stat(paths.SpellDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("spell folder: %v %v", info, err)
	}

	setOld()
	write()
	if !mtime().Equal(old) {
		t.Error("a dictionary that is already right was written again")
	}

	for _, tc := range []struct {
		name   string
		damage func() []byte
	}{
		{"cut off", func() []byte { return hrDictionary[:len(hrDictionary)/2] }},
		{"empty", func() []byte { return nil }},
		{"same size, other bytes", func() []byte {
			b := append([]byte(nil), hrDictionary...)
			b[len(b)/2] ^= 0xff
			return b
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(paths.HrDictionary, tc.damage(), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := e.svc.writeConfig(sample(t)); err != nil {
				t.Fatal(err)
			}
			if got := readString(t, paths.HrDictionary); got != string(hrDictionary) {
				t.Error("a damaged dictionary was not replaced")
			}
		})
	}
	// No temporary file is left in the folder.
	entries, err := os.ReadDir(paths.SpellDir)
	if err != nil || len(entries) != 1 {
		t.Errorf("spell folder holds %v (%v), want only the dictionary", entries, err)
	}
}

// The words the user added are never written by the application: writeConfig
// makes their folder and leaves the file, and its compiled list, as they are.
func TestEdWriteConfigKeepsUserWords(t *testing.T) {
	e := newEd(t)
	paths := e.paths()
	nvimDir := filepath.Join(e.home, "nvim") + string(filepath.Separator)
	if strings.HasPrefix(paths.UserWords, nvimDir) {
		t.Fatalf("the words file %s is under %s, which is rewritten at every start", paths.UserWords, nvimDir)
	}

	if err := e.svc.writeConfig(sample(t)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(paths.UserWords)
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("words folder: %v %v", info, err)
	}
	if _, err := os.Lstat(paths.UserWords); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("writeConfig created the words file: %v", err)
	}

	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	files := map[string]string{paths.UserWords: "blorptastic\n", paths.UserWords + ".spl": "compiled"}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.svc.writeConfig(sample(t)); err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := readString(t, path); got != content || !info.ModTime().Equal(old) {
			t.Errorf("%s was touched: %q, %v", path, got, info.ModTime())
		}
	}
}

// The path of the user's own Neovim file reaches init.lua through the
// environment, and the file is never written by the application: not by
// writeConfig, not by Start. The file is the user's, so a test that wrote it
// would be one that could destroy it.
func TestEdUserPath(t *testing.T) {
	e := newEd(t)
	paths := e.paths()
	if paths.UserLua != filepath.Join(e.home, "user.lua") {
		t.Fatalf("UserLua = %q, want <home>/user.lua", paths.UserLua)
	}
	// No file: writeConfig makes none, and neither does Start.
	if err := e.svc.writeConfig(sample(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(paths.UserLua); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("writeConfig created user.lua: %v", err)
	}
	needNvim(t)
	if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := os.Lstat(paths.UserLua); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Start created user.lua: %v", err)
	}
	got := e.eval(`[$PISAR_USER_LUA, get(g:, 'user_ran', 0)]`)
	if want := []string{paths.UserLua, "0"}; !slices.Equal(got, want) {
		t.Errorf("PISAR_USER_LUA and user_ran = %q, want %q", got, want)
	}
}

// putUserLua writes the user's file into the home, the way the user would, with
// an old modification time and a mode of their own choosing.
func (e *edEnv) putUserLua(content string) (os.FileInfo, string) {
	e.t.Helper()
	path := e.paths().UserLua
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		e.t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		e.t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		e.t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		e.t.Fatal(err)
	}
	return info, path
}

func TestEdUserLua(t *testing.T) {
	needNvim(t)
	e := newEd(t)
	const src = "vim.g.user_ran = 1\nvim.o.number = true\n-- a comment of the user's\n"
	before, path := e.putUserLua(src)
	if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := e.eval(`[get(g:, 'user_ran', 0), &number, &hidden, &laststatus, &cmdheight, get(g:, 'pisar_user_error', '')]`)
	if want := []string{"1", "1", "1", "2", "1", ""}; !slices.Equal(got, want) {
		t.Errorf("state after the user's file = %q, want %q", got, want)
	}

	// Restarting the page runs Start again, and a second writeConfig: the file is
	// still the user's, byte for byte, with its own mode and time.
	if err := e.svc.Start(context.Background(), 90, 25, sample(t)); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.writeConfig(sample(t)); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if readString(t, path) != src || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("user.lua was touched: %q, %v, %v", readString(t, path), after.Mode(), after.ModTime())
	}
	entries, err := os.ReadDir(e.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if strings.Contains(ent.Name(), "user.lua") && ent.Name() != "user.lua" {
			t.Errorf("a temporary or backup copy of user.lua was left: %s", ent.Name())
		}
	}
}

// logSink collects what slog writes while a test runs.
type logSink struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logSink) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// A mistake in user.lua does not stop the editor, is logged with the path and
// the first line of the error, and is the user's code only: not its text.
func TestEdUserLuaLog(t *testing.T) {
	needNvim(t)
	sink := &logSink{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	e := newEd(t)
	const secret = "a_line_of_the_users_own_code"
	_, path := e.putUserLua("vim.g.user_ran = 1\nlocal " + secret + " = 1\nerror('boom\\nsecond line')\n")
	if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := e.eval(`[get(g:, 'user_ran', 0), &hidden, get(g:, 'pisar_user_error', '') =~ 'second line']`)
	if want := []string{"1", "1", "1"}; !slices.Equal(got, want) {
		t.Errorf("state = %q, want %q: the editor must work, with the full error kept in Neovim", got, want)
	}
	log := sink.String()
	for _, want := range []string{"editor: user.lua", "file=" + path, "user.lua:3: boom"} {
		if !strings.Contains(log, want) {
			t.Errorf("log = %q, want it to contain %q", log, want)
		}
	}
	for _, not := range []string{"second line", "stack traceback", secret} {
		if strings.Contains(log, not) {
			t.Errorf("log = %q, must not contain %q", log, not)
		}
	}
}

func TestEdStartTwice(t *testing.T) {
	e := startEd(t)
	first := readString(t, e.paths().ColorschemeFile)
	other := sample(t) + "vim.api.nvim_set_hl(0, \"Extra\", { fg = \"#000000\" })\n"
	from := e.rec.mark()
	if err := e.svc.Start(context.Background(), 90, 25, other); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	e.rec.waitName(t, from, eventPtyData) // the repaint
	if readString(t, e.paths().ColorschemeFile) != first {
		t.Error("the colorscheme changed while Neovim was running")
	}
	if got := e.eval(`[&columns, &lines]`); got[0] != "90" || got[1] != "25" {
		t.Errorf("size after second Start = %v", got)
	}
	// Invalid text is refused also when Neovim already runs.
	if err := e.svc.Start(context.Background(), 90, 25, other+"os.execute('x')\n"); err == nil {
		t.Error("invalid colorscheme accepted by a second Start")
	}
}

func (e *edEnv) paths() config.NvimPaths { return e.store.Nvim() }

func TestEdBadScheme(t *testing.T) {
	good := ""
	{
		b, err := os.ReadFile(filepath.Join("testdata", "colorscheme", "pisar.lua"))
		if err != nil {
			t.Fatal(err)
		}
		good = string(b)
	}
	const group = `vim.api.nvim_set_hl(0, "X", `
	cases := []struct{ name, text string }{
		{"extra code line", good + "os.execute(\"x\")\n"},
		{"require", good + "require(\"x\")\n"},
		{"function", good + "local function f() end\n"},
		{"comment", good + "-- hello\n"},
		{"unknown key", good + group + "{ foo = true })\n"},
		{"unknown key next to known", good + group + "{ fg = \"#000000\", reverse = true })\n"},
		{"colour is a word", good + group + "{ fg = \"red\" })\n"},
		{"short colour", good + group + "{ fg = \"#FFF\" })\n"},
		{"colour is an expression", good + group + "{ fg = os.getenv(\"HOME\") })\n"},
		{"flag is false", good + group + "{ bold = false })\n"},
		{"flag is a string", good + group + "{ bold = \"true\" })\n"},
		{"link to a colour", good + group + "{ link = \"#000000\" })\n"},
		{"link name with a quote", good + group + "{ link = \"A\\\")os.execute(\\\"x\\\")--\" })\n"},
		{"group name with a quote", good + "vim.api.nvim_set_hl(0, \"A\\\")os.execute(\\\"x\\\")--\", { bold = true })\n"},
		{"code after the call", good + group + "{ bold = true }) os.execute(\"x\")\n"},
		{"code after the call with ;", good + group + "{ bold = true });os.execute(\"x\")\n"},
		{"trailing blank", good + group + "{ bold = true }) \n"},
		{"leading blank", good + " " + group + "{ bold = true })\n"},
		{"carriage return", good + group + "{ bold = true })\r\n"},
		{"empty table", good + group + "{  })\n"},
		{"multi-line expression", good + group + "{\n  fg = \"#000000\" })\n"},
		{"multi-line header trick", good + "vim.o.background = \"light\"\nos.execute(\"x\")\n"},
		{"two calls on a line", good + group + "{ bold = true }) " + group + "{ bold = true })\n"},
		{"NUL", good + "\x00"},
		{"invalid UTF-8", good + "\xff\xfe\n"},
		{"too large", good + strings.Repeat("\n", config.DefaultMaxColorscheme)},
		{"no group at all", "vim.g.colors_name = \"pisar\"\n"},
		{"empty", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEd(t) // not started: Start must refuse before it launches anything
			err := e.svc.Start(context.Background(), 80, 24, c.text)
			if !errors.Is(err, errInvalidColorscheme) {
				t.Fatalf("Start error = %v, want errInvalidColorscheme", err)
			}
			if e.svc.session.Running() {
				t.Error("Neovim was started")
			}
			if exists(filepath.Join(e.home, "nvim")) {
				t.Error("something was written under <home>/nvim")
			}
			if got := e.rec.mark(); got != 0 {
				t.Errorf("%d events emitted", got)
			}
		})
	}
}

func TestEdSampleAccepted(t *testing.T) {
	if err := checkColorscheme(sample(t), config.DefaultMaxColorscheme); err != nil {
		t.Fatalf("the generator's own output was refused: %v", err)
	}
	// Blank lines are fine, the generator writes them.
	if err := checkColorscheme("\n\n"+sample(t)+"\n\n", config.DefaultMaxColorscheme); err != nil {
		t.Fatalf("blank lines refused: %v", err)
	}
}

func TestEdScheme(t *testing.T) {
	e := startEd(t)
	got := e.eval(`[string(nvim_get_hl(0, {'name': 'Normal'}).fg), string(nvim_get_hl(0, {'name': 'Normal'}).bg), v:errmsg]`)
	hex := func(c string) string {
		n, err := strconv.ParseUint(strings.TrimPrefix(c, "#"), 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		return strconv.FormatUint(n, 10)
	}
	if want := hex(tokenColor(t, "text")); got[0] != want {
		t.Errorf("Normal fg = %s, want %s (token text)", got[0], want)
	}
	if want := hex(tokenColor(t, "bg.editor")); got[1] != want {
		t.Errorf("Normal bg = %s, want %s (token bg.editor)", got[1], want)
	}
	if got[2] != "" {
		t.Errorf("v:errmsg = %q after loading the colorscheme", got[2])
	}
}

// docFixture is a project with one file of each kind.
func docFixture(t *testing.T, e *edEnv) (config.Project, string) {
	t.Helper()
	p, dir := e.addProject("proj")
	write(t, filepath.Join(dir, "a.md"), "# a\n")
	write(t, filepath.Join(dir, "b.txt"), "b\n")
	write(t, filepath.Join(dir, "sub", "c.md"), "c\n")
	write(t, filepath.Join(dir, "d.docx"), "not really a docx")
	write(t, filepath.Join(dir, "e.doc"), "not really a doc")
	write(t, filepath.Join(dir, "slika.png"), "png")
	if err := os.MkdirAll(filepath.Join(dir, "folder.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p, dir
}

func TestEdOpen(t *testing.T) {
	e := startEd(t)
	p, dir := docFixture(t, e)
	for _, c := range []struct{ rel, name string }{
		{"a.md", "a.md"}, {"b.txt", "b.txt"}, {"sub/c.md", "c.md"},
	} {
		from := e.rec.mark()
		if err := e.svc.OpenDocument(context.Background(), p.ID, c.rel); err != nil {
			t.Fatalf("OpenDocument(%s): %v", c.rel, err)
		}
		d := e.rec.waitActive(t, from, c.rel, func(d DocumentActive) bool { return d.Rel == c.rel })
		if d.ProjectID != p.ID || d.Name != c.name || !sameFile(d.Path, filepath.Join(dir, filepath.FromSlash(c.rel))) {
			t.Errorf("%s: got %+v", c.rel, d)
		}
	}
}

func TestEdOpenErrors(t *testing.T) {
	e := startEd(t)
	p, dir := docFixture(t, e)
	gone, goneDir := e.addProject("gone")
	if err := os.RemoveAll(goneDir); err != nil {
		t.Fatal(err)
	}
	from := e.rec.mark()
	for _, c := range []struct {
		name, id, rel, code string
	}{
		{"docx", p.ID, "d.docx", codeUnsupportedKind},
		{"doc", p.ID, "e.doc", codeUnsupportedKind},
		{"image", p.ID, "slika.png", codeNotDocument},
		{"folder", p.ID, "folder.md", codeNotDocument},
		{"plain folder", p.ID, "sub", codeNotDocument},
		{"missing file", p.ID, "nope.md", codeNotDocument},
		{"project root", p.ID, "", codeNotDocument},
		{"parent", p.ID, "../x", codeInvalidPath},
		{"parent md", p.ID, "../x.md", codeInvalidPath},
		{"absolute", p.ID, "/etc/passwd", codeInvalidPath},
		{"dot", p.ID, "./a.md", codeInvalidPath},
		{"NUL", p.ID, "a\x00.md", codeInvalidPath},
		{"unknown project", "nonexistent", "a.md", codeNotFound},
		{"project folder deleted", gone.ID, "a.md", codeMissing},
	} {
		t.Run(c.name, func(t *testing.T) {
			wantCode(t, e.svc.OpenDocument(context.Background(), c.id, c.rel), c.code)
		})
	}
	// A symbolic link that leaves the project is refused, and so is any link.
	outside := filepath.Join(e.root, "outside.md")
	write(t, outside, "x")
	if err := os.Symlink(outside, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.svc.OpenDocument(context.Background(), p.ID, "link.md"), codeInvalidPath)
	if got := e.rec.since(from); len(got) != 0 {
		for _, ev := range got {
			if ev.name == eventDocumentActive {
				t.Errorf("a refused document became active: %v", ev)
			}
		}
	}
}

func TestEdOpenNotRunning(t *testing.T) {
	e := newEd(t)
	p, _ := docFixture(t, e)
	wantCode(t, e.svc.OpenDocument(context.Background(), p.ID, "a.md"), codeNotRunning)
	wantCode(t, e.svc.Write("x"), codeNotRunning)
	wantCode(t, e.svc.Resize(80, 24), codeNotRunning)
	wantCode(t, e.svc.Save(context.Background()), codeNotRunning)
	_, err := e.svc.Copy(context.Background())
	wantCode(t, err, codeNotRunning)
}

func TestEdSwitch(t *testing.T) {
	e := startEd(t)
	p, dir := docFixture(t, e)
	if err := e.svc.OpenDocument(context.Background(), p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}

	// From inside Neovim, to another file of the project.
	from := e.rec.mark()
	if err := e.svc.Write(":edit " + filepath.Join(dir, "b.txt") + "\r"); err != nil {
		t.Fatal(err)
	}
	d := e.rec.waitActive(t, from, "b.txt", func(d DocumentActive) bool { return d.Name == "b.txt" })
	if d.ProjectID != p.ID || d.Rel != "b.txt" {
		t.Errorf("got %+v", d)
	}

	// To a file that belongs to no project.
	other := filepath.Join(e.root, "loose.md")
	write(t, other, "x")
	from = e.rec.mark()
	if err := e.svc.Write(":edit " + other + "\r"); err != nil {
		t.Fatal(err)
	}
	d = e.rec.waitActive(t, from, "loose.md", func(d DocumentActive) bool { return d.Name == "loose.md" })
	if d.ProjectID != "" || d.Rel != "" || !sameFile(d.Path, other) {
		t.Errorf("a file outside every project: got %+v", d)
	}

	// To a buffer without a file.
	from = e.rec.mark()
	if err := e.svc.Write(":enew\r"); err != nil {
		t.Fatal(err)
	}
	d = e.rec.waitActive(t, from, "empty", func(d DocumentActive) bool { return d.Path == "" })
	// DocumentActive holds a slice now, so it is no longer comparable with !=.
	if !reflect.DeepEqual(d, DocumentActive{ReadOnly: []string{}, Simplified: []string{}}) {
		t.Errorf("a buffer without a file: got %+v", d)
	}
}

func TestEdNestedOpen(t *testing.T) {
	e := startEd(t)
	outer, dir := e.addProject("outer")
	write(t, filepath.Join(dir, "x.md"), "x")
	write(t, filepath.Join(dir, "in", "y.md"), "y")
	inner, err := e.store.AddProject(filepath.Join(dir, "in"))
	if err != nil {
		t.Fatal(err)
	}
	from := e.rec.mark()
	if err := e.svc.OpenDocument(context.Background(), outer.ID, "in/y.md"); err != nil {
		t.Fatal(err)
	}
	d := e.rec.waitActive(t, from, "y.md", func(d DocumentActive) bool { return d.Name == "y.md" })
	if d.ProjectID != inner.ID || d.Rel != "y.md" {
		t.Errorf("the deepest project must win: got %+v, inner is %s", d, inner.ID)
	}
}

func TestEdLocate(t *testing.T) {
	e := newEd(t)
	outer, outerDir := e.addProject("outer")
	inner, innerDir := e.addProject("outer/in")
	sibling, siblingDir := e.addProject("outer-x") // shares a name prefix with outer
	write(t, filepath.Join(outerDir, "a.md"), "")
	write(t, filepath.Join(innerDir, "b.md"), "")
	write(t, filepath.Join(innerDir, "deep", "c.md"), "")
	write(t, filepath.Join(siblingDir, "d.md"), "")

	// A project given through a symbolic link, and a file that is a link to a file inside it.
	realDir := filepath.Join(e.root, "real")
	write(t, filepath.Join(realDir, "f.md"), "")
	link := filepath.Join(e.root, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	viaLink, err := e.store.AddProject(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(realDir, "f.md"), filepath.Join(realDir, "g.md")); err != nil {
		t.Fatal(err)
	}
	// A project outside the symlinked /tmp, so the "/var" kind of mismatch is tested for real too.
	elsewhere := filepath.Join(e.root, "elsewhere")
	write(t, filepath.Join(elsewhere, "h.md"), "")

	rp := func(p string) string { // the path as Neovim would report it
		return resolveDir(p)
	}
	for _, c := range []struct {
		name, file, id, rel string
	}{
		{"outer file", rp(filepath.Join(outerDir, "a.md")), outer.ID, "a.md"},
		{"outer file, path as stored", filepath.Join(outerDir, "a.md"), outer.ID, "a.md"},
		{"nested: deepest wins", rp(filepath.Join(innerDir, "b.md")), inner.ID, "b.md"},
		{"nested: deeper folder", rp(filepath.Join(innerDir, "deep", "c.md")), inner.ID, "deep/c.md"},
		{"name prefix is not a parent", rp(filepath.Join(siblingDir, "d.md")), sibling.ID, "d.md"},
		{"project through a link", rp(filepath.Join(realDir, "f.md")), viaLink.ID, "f.md"},
		{"path through the link", filepath.Join(link, "f.md"), viaLink.ID, "f.md"},
		{"file link keeps its name", rp(filepath.Join(realDir, "g.md")), viaLink.ID, "g.md"},
		{"outside every project", rp(filepath.Join(elsewhere, "h.md")), "", ""},
		{"the project folder itself", rp(outerDir), "", ""},
		{"the parent of a project", rp(e.projects), "", ""},
		{"nonexistent file in a project", rp(filepath.Join(outerDir, "no", "x.md")), outer.ID, "no/x.md"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := e.svc.describe(c.file)
			if d.ProjectID != c.id || d.Rel != c.rel || d.Path != c.file || d.Name != filepath.Base(c.file) {
				t.Errorf("describe(%s) = %+v, want project %q rel %q", c.file, d, c.id, c.rel)
			}
		})
	}

	// The list of projects is read every time.
	if err := e.store.RemoveProject(inner.ID); err != nil {
		t.Fatal(err)
	}
	if d := e.svc.describe(rp(filepath.Join(innerDir, "b.md"))); d.ProjectID != outer.ID || d.Rel != "in/b.md" {
		t.Errorf("after removing the inner project: %+v", d)
	}
	if err := e.store.RemoveProject(outer.ID); err != nil {
		t.Fatal(err)
	}
	if d := e.svc.describe(rp(filepath.Join(outerDir, "a.md"))); d.ProjectID != "" || d.Rel != "" {
		t.Errorf("after removing the outer project: %+v", d)
	}
	if d := e.svc.describe(""); !reflect.DeepEqual(d, DocumentActive{ReadOnly: []string{}, Simplified: []string{}}) {
		t.Errorf("describe(\"\") = %+v", d)
	}
}

func TestEdSave(t *testing.T) {
	e := startEd(t)
	p, dir := docFixture(t, e)
	file := filepath.Join(dir, "a.md")
	from := e.rec.mark()
	if err := e.svc.OpenDocument(context.Background(), p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitActive(t, from, "a.md", func(d DocumentActive) bool { return d.Rel == "a.md" })

	from = e.rec.mark()
	if err := e.svc.Write("GoHello"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitState(t, from, file, true)
	if err := e.svc.Write("\x1b"); err != nil { // sent alone: Esc followed by a key would be read as Alt+key
		t.Fatal(err)
	}
	if got := readString(t, file); got != "# a\n" {
		t.Fatalf("the file changed before Save: %q", got)
	}

	from = e.rec.mark()
	if err := e.svc.Save(context.Background()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	e.rec.waitState(t, from, file, false)
	if got := readString(t, file); !strings.Contains(got, "Hello") {
		t.Errorf("file after Save = %q", got)
	}
}

func TestEdUnnamed(t *testing.T) {
	e := startEd(t)
	from := e.rec.mark()
	if err := e.svc.Write(":enew\rix"); err != nil {
		t.Fatal(err)
	}
	e.rec.wait(t, from, "modified unnamed buffer", func(ev recorded) bool {
		d, ok := ev.data.(DocumentState)
		return ev.name == eventDocumentState && ok && d.Path == "" && d.Modified
	})
	bufs, err := e.svc.modified(context.Background())
	if err != nil || len(bufs) != 1 || bufs[0] != (nvim.Buffer{Path: "", Modified: true}) {
		t.Fatalf("modified = %v, %v", bufs, err)
	}
	wantCode(t, e.svc.saveAll(context.Background()), codeUnnamedBuffer)
}

func TestEdModifiedAll(t *testing.T) {
	e := startEd(t)
	p, dir := docFixture(t, e)
	from := e.rec.mark()
	if err := e.svc.OpenDocument(context.Background(), p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitActive(t, from, "a.md", func(d DocumentActive) bool { return d.Rel == "a.md" })
	from = e.rec.mark()
	if err := e.svc.Write("GoX"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitState(t, from, filepath.Join(dir, "a.md"), true)
	bufs, err := e.svc.modified(context.Background())
	if err != nil || len(bufs) != 1 || !sameFile(bufs[0].Path, filepath.Join(dir, "a.md")) {
		t.Fatalf("modified = %v, %v", bufs, err)
	}
	if err := e.svc.saveAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, filepath.Join(dir, "a.md")); !strings.Contains(got, "X") {
		t.Errorf("file after saveAll = %q", got)
	}
}

func TestEdFocusTree(t *testing.T) {
	e := startEd(t)
	from := e.rec.mark()
	if err := e.svc.Write(" e"); err != nil {
		t.Fatal(err)
	}
	ev := e.rec.waitName(t, from, eventFocusTree)
	if s, ok := ev.data.(string); !ok || s != "" {
		t.Errorf("payload %#v", ev.data)
	}
}

// Switching to a buffer that already has unsaved changes must report that
// state, or the toolbar would show the state of the buffer that was left.
func TestEdActiveState(t *testing.T) {
	e := startEd(t)
	p, dir := docFixture(t, e)
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.txt")
	ctx := context.Background()
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	from := e.rec.mark()
	if err := e.svc.Write("GoHello"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitState(t, from, a, true)
	if err := e.svc.Write("\x1b"); err != nil {
		t.Fatal(err)
	}

	from = e.rec.mark()
	if err := e.svc.OpenDocument(ctx, p.ID, "b.txt"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitActive(t, from, "b.txt", func(d DocumentActive) bool { return d.Rel == "b.txt" })
	e.rec.waitState(t, from, b, false)

	from = e.rec.mark()
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitActive(t, from, "a.md", func(d DocumentActive) bool { return d.Rel == "a.md" })
	e.rec.waitState(t, from, a, true)
}

// The system may clear the socket folder while Neovim is not running; the
// next Start has to make it again.
func TestEdRunDirGone(t *testing.T) {
	e := startEd(t)
	from := e.rec.mark()
	if err := e.svc.Write(":qa!\r"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitName(t, from, eventNvimExit)
	if err := os.RemoveAll(e.svc.runDir); err != nil {
		t.Fatal(err)
	}
	from = e.rec.mark()
	if err := e.svc.Start(context.Background(), 80, 24, sample(t)); err != nil {
		t.Fatalf("Start after the socket folder vanished: %v", err)
	}
	e.rec.waitName(t, from, eventPtyData)
}

// A reloaded page calls Start again while Neovim runs; it must learn which
// document is active and its state without waiting for the next change.
func TestEdRestartReports(t *testing.T) {
	e := startEd(t)
	p, dir := docFixture(t, e)
	a := filepath.Join(dir, "a.md")
	ctx := context.Background()
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	from := e.rec.mark()
	if err := e.svc.Write("GoHello"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitState(t, from, a, true)
	if err := e.svc.Write("\x1b"); err != nil {
		t.Fatal(err)
	}

	from = e.rec.mark()
	if err := e.svc.Start(ctx, 90, 28, sample(t)); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	e.rec.waitActive(t, from, "a.md after the second Start", func(d DocumentActive) bool {
		return d.ProjectID == p.ID && d.Rel == "a.md"
	})
	e.rec.waitState(t, from, a, true)
}

func TestEdExit(t *testing.T) {
	e := startEd(t)
	p, _ := docFixture(t, e)
	from := e.rec.mark()
	if err := e.svc.Write(":qa!\r"); err != nil {
		t.Fatal(err)
	}
	ev := e.rec.waitName(t, from, eventNvimExit)
	if s, ok := ev.data.(string); !ok || s != "" {
		t.Errorf("payload %#v", ev.data)
	}
	wantCode(t, e.svc.OpenDocument(context.Background(), p.ID, "a.md"), codeNotRunning)
	wantCode(t, e.svc.Write("x"), codeNotRunning)

	// Starting again works, with the same colorscheme text, and output arrives.
	from = e.rec.mark()
	if err := e.svc.Start(context.Background(), 80, 24, sample(t)); err != nil {
		t.Fatalf("Start after exit: %v", err)
	}
	e.rec.waitName(t, from, eventPtyData)
	from = e.rec.mark()
	if err := e.svc.OpenDocument(context.Background(), p.ID, "a.md"); err != nil {
		t.Fatalf("OpenDocument after restart: %v", err)
	}
	e.rec.waitActive(t, from, "a.md", func(d DocumentActive) bool { return d.Rel == "a.md" })
}

func TestEdCopy(t *testing.T) {
	e := startEd(t)
	// Only the negative case: a selection would be yanked into the real system clipboard.
	copied, err := e.svc.Copy(context.Background())
	if err != nil || copied {
		t.Fatalf("Copy in normal mode = %v, %v", copied, err)
	}
	if calls := e.clip.calls(); len(calls) != 0 {
		t.Errorf("Copy used the fake clipboard: %v", calls)
	}
}

// openA opens a.md of the document fixture and waits until it is the active one.
func (e *edEnv) openA() {
	e.t.Helper()
	p, _ := docFixture(e.t, e)
	from := e.rec.mark()
	if err := e.svc.OpenDocument(context.Background(), p.ID, "a.md"); err != nil {
		e.t.Fatal(err)
	}
	e.rec.waitActive(e.t, from, "a.md", func(d DocumentActive) bool { return d.Rel == "a.md" })
}

// lines reads the buffer back through eval; it needs normal mode.
func (e *edEnv) lines() string {
	e.t.Helper()
	return strings.Join(e.eval(`[join(getline(1, '$'), '|')]`), "|")
}

func TestEdFmt(t *testing.T) {
	e := startEd(t)
	e.openA()
	ctx := context.Background()

	// Normal mode: the word under the cursor is wrapped.
	if err := e.svc.Write("Gotext"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Write("\x1b"); err != nil { // alone: Esc followed by a key would be read as Alt+key
		t.Fatal(err)
	}
	if got := e.lines(); got != "# a|text" {
		t.Fatalf("typed text: %q", got)
	}
	if err := e.svc.Format(ctx, "bold"); err != nil {
		t.Fatalf("Format bold: %v", err)
	}
	if got := e.lines(); got != "# a|**text**" {
		t.Errorf("bold in normal mode: %q", got)
	}
	// The same action again takes the marks off.
	if err := e.svc.Format(ctx, "bold"); err != nil {
		t.Fatal(err)
	}
	if got := e.lines(); got != "# a|text" {
		t.Errorf("bold twice: %q", got)
	}
	if err := e.svc.Format(ctx, "list"); err != nil {
		t.Fatal(err)
	}
	if got := e.lines(); got != "# a|- text" {
		t.Errorf("list: %q", got)
	}
}

// Keys typed just before a format must be in the text before it: the frontend
// sends both through one ordered queue, and Neovim has to see them in that
// order although the keys travel through the PTY and Format through the socket.
func TestEdFmtOrder(t *testing.T) {
	e := startEd(t)
	e.openA()
	ctx := context.Background()
	var want []string
	want = append(want, "# a")
	for i := range 12 {
		for _, keys := range []string{"o", "text"} {
			if err := e.svc.Write(keys); err != nil {
				t.Fatal(err)
			}
		}
		// Neovim is in insert mode (or is about to be): the pair of marks is inserted
		// after "text", and "more" goes between them.
		if err := e.svc.Format(ctx, "bold"); err != nil {
			t.Fatalf("round %d: Format: %v", i, err)
		}
		if err := e.svc.Write("more"); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Write("\x1b"); err != nil {
			t.Fatal(err)
		}
		want = append(want, "text**more**")
		e.eval(`[1]`) // all keys of the round are handled before the next one starts
	}
	if got := e.lines(); got != strings.Join(want, "|") {
		t.Errorf("lines = %q, want %q", got, strings.Join(want, "|"))
	}
}

func TestEdFmtErr(t *testing.T) {
	e := startEd(t)
	e.openA()
	before := e.lines()
	for _, kind := range []string{"", "bogus", "BOLD", "bold ", "../x"} {
		wantCode(t, e.svc.Format(context.Background(), kind), codeInvalidFormat)
	}
	if got := e.lines(); got != before {
		t.Errorf("an unknown action changed the text: %q", got)
	}
	// An expired context is the caller's cancellation, not an invalid action.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.svc.Format(ctx, "bold"); err == nil {
		t.Error("Format with a canceled context succeeded")
	}
	// Still alive and answering.
	if got := e.eval(`['ok']`); got[0] != "ok" {
		t.Errorf("eval = %v", got)
	}
}

func TestEdFmtStopped(t *testing.T) {
	e := newEd(t)
	wantCode(t, e.svc.Format(context.Background(), "bold"), codeNotRunning)
}

func TestEdCopyText(t *testing.T) {
	e := newEd(t)
	if err := e.svc.CopyText(""); err != nil || len(e.clip.calls()) != 0 {
		t.Fatalf("CopyText(\"\") = %v, clipboard calls %v", err, e.clip.calls())
	}
	if err := e.svc.CopyText("x"); err != nil {
		t.Fatal(err)
	}
	if got := e.clip.calls(); len(got) != 1 || got[0] != "x" {
		t.Fatalf("clipboard calls %v", got)
	}
	e.clip.mu.Lock()
	e.clip.ok = false
	e.clip.mu.Unlock()
	err := e.svc.CopyText("y")
	if err == nil {
		t.Fatal("a refused clipboard must be an error")
	}
	wantCode(t, err, codeInternal)

	none := newEd(t, func(d *editorServiceDeps) { d.Clipboard = nil })
	if err := none.svc.CopyText("x"); err == nil {
		t.Fatal("no clipboard must be an error")
	}
	if err := none.svc.CopyText(""); err != nil {
		t.Fatalf("empty text needs no clipboard: %v", err)
	}
}

// initialIn makes newEd build the service with a document of the project folder "proj" on its command line.
func initialIn(rel string) func(*editorServiceDeps) {
	return func(d *editorServiceDeps) {
		d.InitialFile = filepath.Join(filepath.Dir(d.Store.Home()), "p", "proj", filepath.FromSlash(rel))
	}
}

func TestEdInitial(t *testing.T) {
	needNvim(t)
	e := newEd(t, initialIn("sub/c.md"))
	p, dir := docFixture(t, e)
	file := filepath.Join(dir, "sub", "c.md")
	if !e.svc.HasInitialDocument() {
		t.Fatal("HasInitialDocument = false before Start")
	}
	if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
		t.Fatal(err)
	}
	if e.svc.HasInitialDocument() {
		t.Error("HasInitialDocument = true after Start")
	}
	d := e.rec.waitActive(t, 0, "initial", func(d DocumentActive) bool { return d.Name == "c.md" })
	if d.ProjectID != p.ID || d.Rel != "sub/c.md" || !sameFile(d.Path, file) {
		t.Errorf("got %+v", d)
	}

	// A second Start does not open it again.
	from := e.rec.mark()
	if err := e.svc.Write(":edit " + filepath.Join(dir, "b.txt") + "\r"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitActive(t, from, "b.txt", func(d DocumentActive) bool { return d.Name == "b.txt" })
	from = e.rec.mark()
	if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
		t.Fatal(err)
	}
	e.rec.waitName(t, from, eventPtyData)
	time.Sleep(300 * time.Millisecond)
	// The second Start reports the document that is current now (b.txt, for a
	// reloaded page); it must never bring the initial document back.
	for _, ev := range e.rec.since(from) {
		if d, ok := ev.data.(DocumentActive); ev.name == eventDocumentActive && (!ok || d.Name != "b.txt") {
			t.Errorf("second Start changed the active document: %v", ev)
		}
	}
}

// The document on the command line may be anywhere, not only in a project.
func TestEdInitialOutside(t *testing.T) {
	needNvim(t)
	e := newEd(t, func(d *editorServiceDeps) {
		d.InitialFile = filepath.Join(filepath.Dir(d.Store.Home()), "loose.md")
	})
	file := e.svc.initial
	write(t, file, "x")
	if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
		t.Fatal(err)
	}
	d := e.rec.waitActive(t, 0, "loose.md", func(d DocumentActive) bool { return d.Name == "loose.md" })
	if d.ProjectID != "" || d.Rel != "" || !sameFile(d.Path, file) {
		t.Errorf("got %+v", d)
	}
}

// initialLoose makes newEd build the service with the file name in the folder
// of the test projects, which exists but is not a project itself.
func initialLoose(name string) func(*editorServiceDeps) {
	return func(d *editorServiceDeps) {
		d.InitialFile = filepath.Join(filepath.Dir(d.Store.Home()), "p", name)
	}
}

// A link named like a document may point at a .docx; Neovim's :w would write
// through it, outside docsafe. The command-line document must be a real file.
func TestEdInitialSymlink(t *testing.T) {
	needNvim(t)
	var target string
	e := newEd(t, initialLoose("link.md"), func(d *editorServiceDeps) {
		target = filepath.Join(filepath.Dir(d.InitialFile), "real.docx")
		write(t, target, "docx bytes")
		if err := os.Symlink(target, d.InitialFile); err != nil {
			t.Fatal(err)
		}
	})
	if e.svc.HasInitialDocument() {
		t.Fatal("HasInitialDocument = true for a link")
	}
	if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	for _, ev := range e.rec.since(0) {
		if d, ok := ev.data.(DocumentActive); ev.name == eventDocumentActive && (!ok || d.Path != "") {
			t.Errorf("a document became active: %v", ev)
		}
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "docx bytes" {
		t.Errorf("the link's target changed: %q, %v", got, err)
	}
}

// What is not a .md or .txt file must never reach Neovim from the command line:
// its bytes would be written back by :w outside internal/docsafe.
func TestEdInitialRefused(t *testing.T) {
	needNvim(t)
	for _, name := range []string{"x.docx", "x.DOC", "slika.png", "README", "dir", "dir.md"} {
		t.Run(name, func(t *testing.T) {
			isFolder := strings.HasPrefix(name, "dir")
			e := newEd(t, initialLoose(name), func(d *editorServiceDeps) {
				// The folder must exist when the service is made: that is
				// when the command-line document is judged.
				if isFolder {
					if err := os.Mkdir(d.InitialFile, 0o755); err != nil {
						t.Fatal(err)
					}
				}
			})
			file := filepath.Join(e.projects, name)
			if !isFolder {
				write(t, file, "not for Neovim")
			}
			if e.svc.HasInitialDocument() {
				t.Fatal("HasInitialDocument = true right after creating the service")
			}
			if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
				t.Fatal(err)
			}
			// Nothing may arrive late either: give an open call time to show up.
			time.Sleep(300 * time.Millisecond)
			for _, ev := range e.rec.since(0) {
				if d, ok := ev.data.(DocumentActive); ev.name == eventDocumentActive && (!ok || d.Path != "") {
					t.Errorf("a document became active: %v", ev)
				}
			}
			cur, err := e.svc.session.Current(context.Background())
			if err != nil || cur != (nvim.Buffer{}) {
				t.Errorf("Current = %+v, %v; want an unnamed buffer", cur, err)
			}
			if !isFolder {
				if got, err := os.ReadFile(file); err != nil || string(got) != "not for Neovim" {
					t.Errorf("the file changed: %q, %v", got, err)
				}
			}
		})
	}
}

// A .md or .txt name is accepted in any case, also for a file that does not exist yet.
func TestEdInitialAccepted(t *testing.T) {
	needNvim(t)
	for _, c := range []struct {
		name   string
		create bool
	}{{"biljeske.md", true}, {"Popis.TXT", true}, {"nova.md", false}} {
		t.Run(c.name, func(t *testing.T) {
			e := newEd(t, initialLoose(c.name))
			file := filepath.Join(e.projects, c.name)
			if c.create {
				write(t, file, "x")
			}
			if !e.svc.HasInitialDocument() {
				t.Fatal("HasInitialDocument = false before Start")
			}
			if err := e.svc.Start(context.Background(), 100, 30, sample(t)); err != nil {
				t.Fatal(err)
			}
			e.rec.waitActive(t, 0, c.name, func(d DocumentActive) bool { return d.Name == c.name && sameFile(d.Path, file) })
		})
	}
}

func TestEdInitialNone(t *testing.T) {
	e := newEd(t)
	if e.svc.HasInitialDocument() {
		t.Error("HasInitialDocument = true without a document")
	}
}

// A relative path from the command line is made absolute against the working directory.
func TestEdInitialRelative(t *testing.T) {
	e := newEd(t, func(d *editorServiceDeps) { d.InitialFile = filepath.Join("some", "a.md") })
	want := filepath.Join(mustGetwd(t), "some", "a.md")
	if e.svc.initial != want {
		t.Errorf("initial = %q, want %q", e.svc.initial, want)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func TestEdNoNvim(t *testing.T) {
	e := newEd(t, func(d *editorServiceDeps) { d.NvimBin = "pisar-no-such-nvim-binary" })
	err := e.svc.Start(context.Background(), 80, 24, sample(t))
	wantCode(t, err, codeNvimMissing)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("the cause is lost: %v", err)
	}
	if e.svc.session.Running() {
		t.Error("a process is running")
	}
	if got := e.rec.mark(); got != 0 {
		t.Errorf("%d events", got)
	}
}

func TestEdStale(t *testing.T) {
	parent := t.TempDir()
	prefix := config.NvimRunDirPrefix

	// A PID that belonged to a process that has ended and been reaped.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot run true: %v", err)
	}
	dead := cmd.Process.Pid
	if pidAlive(dead) {
		t.Skip("the PID of the finished process is in use again")
	}
	self := os.Getpid()

	mkdir := func(name string) string {
		p := filepath.Join(parent, name)
		write(t, filepath.Join(p, "nvim-1.sock"), "x")
		return p
	}
	staleDir := mkdir(fmt.Sprintf("%s%d-123456", prefix, dead))
	staleDir2 := mkdir(fmt.Sprintf("%s%d-9", prefix, dead))
	liveDir := mkdir(fmt.Sprintf("%s%d-123456", prefix, self))
	otherName := mkdir(fmt.Sprintf("other-%d-123456", dead))
	noPID := mkdir(prefix + "abc-123")
	noTail := mkdir(fmt.Sprintf("%s%d-", prefix, dead))
	noDash := mkdir(fmt.Sprintf("%s%d", prefix, dead))
	zero := mkdir(prefix + "0-1")
	huge := mkdir(prefix + "99999999999999999999-1")
	negative := mkdir(prefix + "-5-1")
	file := filepath.Join(parent, fmt.Sprintf("%s%d-file", prefix, dead))
	write(t, file, "x")
	target := mkdir("target")
	link := filepath.Join(parent, fmt.Sprintf("%s%d-link", prefix, dead))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	removed := removeStaleRunDirs(parent, prefix, pidAlive)
	if len(removed) != 2 {
		t.Errorf("removed %v", removed)
	}
	for _, p := range []string{staleDir, staleDir2} {
		if exists(p) {
			t.Errorf("%s was not removed", p)
		}
	}
	for _, p := range []string{liveDir, otherName, noPID, noTail, noDash, zero, huge, negative, file, link, target, filepath.Join(target, "nvim-1.sock")} {
		if !exists(p) {
			t.Errorf("%s was removed", p)
		}
	}
	// A parent that does not exist is not an error.
	if got := removeStaleRunDirs(filepath.Join(parent, "nope"), prefix, pidAlive); len(got) != 0 {
		t.Errorf("removed %v", got)
	}
}

func TestEdRunDir(t *testing.T) {
	// Built through the service: stale folders go at start, the live one is its own and goes at shutdown.
	parent := t.TempDir()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot run true: %v", err)
	}
	if pidAlive(cmd.Process.Pid) {
		t.Skip("the PID of the finished process is in use again")
	}
	stale := filepath.Join(parent, fmt.Sprintf("%s%d-1", config.NvimRunDirPrefix, cmd.Process.Pid))
	write(t, filepath.Join(stale, "x"), "x")
	e := newEd(t, func(d *editorServiceDeps) { d.RunDir = parent })
	if exists(stale) {
		t.Error("the stale folder was not removed at start")
	}
	if !strings.HasPrefix(filepath.Base(e.svc.runDir), config.NvimRunDirPrefix+strconv.Itoa(os.Getpid())+"-") || !isDir(e.svc.runDir) {
		t.Errorf("run folder %q", e.svc.runDir)
	}
	if info, err := os.Stat(e.svc.runDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("run folder mode: %v %v", info, err)
	}
	e.svc.shutdown()
	if exists(e.svc.runDir) {
		t.Error("the run folder was not removed by shutdown")
	}
}

// pgrepFor reports whether a process has this text on its command line
// (only looks, never signals).
func pgrepFor(t *testing.T, text string) bool {
	t.Helper()
	err := exec.Command("pgrep", "-f", text).Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return true
	case errors.As(err, &ee) && ee.ExitCode() == 1:
		return false
	default:
		t.Skipf("pgrep unavailable: %v", err)
		return false
	}
}

func TestEdShutdown(t *testing.T) {
	e := startEd(t)
	e.rec.waitName(t, 0, eventPtyData)
	if !pgrepFor(t, e.svc.runDir) {
		t.Fatal("no Neovim process found for this service")
	}
	// Live output: Neovim redraws a long loop, so data keeps arriving.
	from := e.rec.mark()
	if err := e.svc.Write(":for i in range(3000) | call setline(1, string(i)) | redraw | endfor\r"); err != nil {
		t.Fatal(err)
	}
	seen := 0
	e.rec.wait(t, from, "output of the loop", func(ev recorded) bool {
		if ev.name == eventPtyData {
			seen++
		}
		return seen >= 5
	})

	e.svc.shutdown()
	n := e.rec.mark()
	time.Sleep(300 * time.Millisecond)
	if after := e.rec.mark(); after != n {
		t.Errorf("%d events after shutdown returned: %v", after-n, e.rec.since(n))
	}
	if pgrepFor(t, e.svc.runDir) {
		t.Error("the Neovim process is still there")
	}
	if e.svc.session.Running() {
		t.Error("session still running")
	}
	if exists(e.svc.runDir) {
		t.Error("the run folder was not removed")
	}
	wantCode(t, e.svc.Write("x"), codeNotRunning)
	wantCode(t, e.svc.Start(context.Background(), 80, 24, sample(t)), codeNotRunning)

	done := make(chan struct{})
	go func() { e.svc.shutdown(); close(done) }() // second call: nothing to do
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the second shutdown did not return")
	}
	if got := e.rec.mark(); got != n {
		t.Errorf("events after the second shutdown: %d", got-n)
	}
}

func TestEdShutdownIdle(t *testing.T) {
	e := newEd(t) // never started
	e.svc.shutdown()
	e.svc.shutdown()
	if exists(e.svc.runDir) {
		t.Error("the run folder was not removed")
	}
}

func TestEdConcurrent(t *testing.T) {
	e := startEd(t)
	p, _ := docFixture(t, e)
	// Save needs a buffer with a file (an unnamed one is Neovim's E32), so one is open first.
	from := e.rec.mark()
	if err := e.svc.OpenDocument(context.Background(), p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitActive(t, from, "a.md", func(d DocumentActive) bool { return d.Rel == "a.md" })
	var wg sync.WaitGroup
	errs := make(chan error, 1000)
	run := func(f func(i int) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 15 {
				if err := f(i); err != nil {
					errs <- err
				}
			}
		}()
	}
	for range 2 {
		run(func(int) error { return e.svc.Write("\x1b") })
		run(func(i int) error { return e.svc.Resize(80+i, 24+i%3) })
		run(func(i int) error {
			return e.svc.OpenDocument(context.Background(), p.ID, []string{"a.md", "b.txt", "sub/c.md"}[i%3])
		})
		run(func(int) error { return e.svc.Save(context.Background()) })
		run(func(i int) error { return e.svc.CopyText("x") })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	// Still alive and answering.
	if got := e.eval(`['ok']`); got[0] != "ok" {
		t.Errorf("eval = %v", got)
	}
}

func TestEdCodes(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", err)) }
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"not found", config.ErrNotFound, codeNotFound},
		{"missing", errMissing, codeMissing},
		{"too large", project.ErrTooLarge, codeTooLarge},
		{"invalid path", project.ErrInvalidPath, codeInvalidPath},
		{"outside root", project.ErrOutsideRoot, codeInvalidPath},
		{"exists", docsafe.ErrExists, codeExists},
		{"invalid name", docsafe.ErrInvalidName, codeInvalidName},
		{"unsupported kind", docsafe.ErrUnsupportedKind, codeUnsupportedKind},
		{"not document", docsafe.ErrNotDocument, codeNotDocument},
		{"not a directory", config.ErrNotADirectory, codeNotADirectory},
		{"canceled", context.Canceled, codeCanceled},
		{"deadline", context.DeadlineExceeded, codeCanceled},
		{"not running", nvim.ErrNotRunning, codeNotRunning},
		{"session closed", term.ErrClosed, codeNotRunning},
		{"binary not found", exec.ErrNotFound, codeNvimMissing},
		{"binary not found (exec.Error)", &exec.Error{Name: "nvim", Err: exec.ErrNotFound}, codeNvimMissing},
		{"unnamed buffer", nvim.ErrUnnamedBuffer, codeUnnamedBuffer},
		{"unsaved", errUnsaved, codeUnsavedChanges},
		{"pandoc missing", fmt.Errorf("%w: pandoc: %w", convert.ErrToolMissing, exec.ErrNotFound), codePandocMissing},
		{"convert failed", &convert.ToolError{Tool: "pandoc", Err: errors.New("exit status 1")}, codeConvertFailed},
		{"not a Word document", convert.ErrNotDocx, codeNotWord},
		{"document too large", convert.ErrTooLarge, codeDocumentTooLarge},
		{"read only", &docsafe.ReadOnlyError{Path: "/x", Reason: docsafe.ReadOnlyLocked}, codeReadOnly},
		{"conflict", docsafe.ErrConflict, codeConflict},
		{"damaged work", docsafe.ErrDamagedWork, codeDamagedWork},
		{"buffer modified", nvim.ErrBufferModified, codeUnsavedChanges},
		{"preview too large", preview.ErrTooLarge, codePreviewTooLarge},
		{"preview source too large", nvim.ErrBufferTooLarge, codePreviewTooLarge},
		{"preview without pandoc", fmt.Errorf("%w: %w", preview.ErrToolMissing, exec.ErrNotFound), codePandocMissing},
		{"preview tool failed", &preview.ToolError{Tool: "pandoc", Err: errors.New("exit status 1")}, codeConvertFailed},
		{"unknown format", nvim.ErrUnknownFormat, codeInvalidFormat},
		{"already coded", &apiError{code: codeNotRunning}, codeNotRunning},
		{"specific beats canceled", errors.Join(nvim.ErrNotRunning, context.Canceled), codeNotRunning},
		{"unknown", errors.New("whatever"), codeInternal},
		{"invalid colorscheme", errInvalidColorscheme, codeInternal},
		{"socket path", nvim.ErrSocketPathTooLong, codeInternal},
	}
	covered := map[string]bool{}
	for _, c := range cases {
		covered[c.want] = true
		t.Run(c.name, func(t *testing.T) {
			if got := codeOf(c.err); got != c.want {
				t.Errorf("codeOf = %q, want %q", got, c.want)
			}
			if got := codeOf(wrap(c.err)); got != c.want {
				t.Errorf("wrapped: codeOf = %q, want %q", got, c.want)
			}
			if got := string(marshalError(failEditor("test", c.err))); got != `{"code":"`+c.want+`"}` {
				t.Errorf("marshalError = %s", got)
			}
		})
	}
	// Every code the service can send is in the table above and has a description.
	for code, text := range codeText {
		if text == "" || !covered[code] {
			t.Errorf("code %q: text %q, covered by the table: %v", code, text, covered[code])
		}
	}
	for _, code := range []string{codeNotRunning, codeNvimMissing, codeUnnamedBuffer} {
		if codeText[code] == "" {
			t.Errorf("no text for %q", code)
		}
	}
}

// The frontend reads the payload of a typed event as a plain object with these keys.
func TestEdEventJSON(t *testing.T) {
	keys := func(data any) map[string]any {
		ev := &application.CustomEvent{Name: "x", Data: data}
		var out struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(ev.ToJSON()), &out); err != nil {
			t.Fatalf("%s: %v", ev.ToJSON(), err)
		}
		return out.Data
	}
	got := keys(DocumentActive{ProjectID: "p", Rel: "a/b.md", Path: "/x/a/b.md", Name: "b.md", ReadOnly: []string{}, Simplified: []string{}})
	want := map[string]any{
		"projectId": "p", "rel": "a/b.md", "path": "/x/a/b.md", "name": "b.md",
		"readOnly": []any{}, "imported": false, "unexported": false, "simplified": []any{},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("DocumentActive JSON = %v, want %v", got, want)
	}
	got = keys(DocumentState{Path: "/x", Modified: true})
	want = map[string]any{"path": "/x", "modified": true}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("DocumentState JSON = %v, want %v", got, want)
	}
}

func TestEdInitialFileArg(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"pisar"}, ""},
		{[]string{"pisar", "/x/a.md"}, "/x/a.md"},
		{[]string{"pisar", "a.md", "b.md"}, "a.md"},
		{[]string{"pisar", "-psn_0_12345"}, ""},
	} {
		if got := initialFile(c.args); got != c.want {
			t.Errorf("initialFile(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestEdNew(t *testing.T) {
	if _, err := newEditorService(editorServiceDeps{}); err == nil {
		t.Error("no store and no emit must be an error")
	}
}

func TestEdUnsaved(t *testing.T) {
	ctx := context.Background()
	t.Run("no editor", func(t *testing.T) {
		e := newEd(t) // Neovim is never started
		if got, err := e.svc.unsaved(ctx, "/x/a.md"); err != nil || got {
			t.Fatalf("unsaved = %v, %v; want false, nil", got, err)
		}
		if err := e.svc.renamed(ctx, "/x/a.md", "/x/b.md"); err != nil {
			t.Fatalf("renamed = %v", err)
		}
		if err := e.svc.removed(ctx, "/x/a.md"); err != nil {
			t.Fatalf("removed = %v", err)
		}
	})

	e := startEd(t)
	p, dir := docFixture(t, e)
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.txt")
	check := func(file string, want bool) {
		t.Helper()
		if got, err := e.svc.unsaved(ctx, file); err != nil || got != want {
			t.Fatalf("unsaved(%s) = %v, %v; want %v", file, got, err, want)
		}
	}
	check(a, false) // not open

	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	check(a, false) // open, no changes
	from := e.rec.mark()
	if err := e.svc.Write("GoX"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitState(t, from, a, true)
	check(a, true)
	check(b, false)

	// The path may come through a link to the folder, as a project root can.
	link := filepath.Join(e.root, "ln")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	check(filepath.Join(link, "a.md"), true)
}

func TestEdRenamed(t *testing.T) {
	ctx := context.Background()
	t.Run("active", func(t *testing.T) {
		e := startEd(t)
		p, dir := docFixture(t, e)
		a, z := filepath.Join(dir, "a.md"), filepath.Join(dir, "z.md")
		if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
			t.Fatal(err)
		}
		e.rec.waitActive(t, 0, "a.md", func(d DocumentActive) bool { return d.Rel == "a.md" })
		if err := os.Rename(a, z); err != nil {
			t.Fatal(err)
		}
		from := e.rec.mark()
		if err := e.svc.renamed(ctx, a, z); err != nil {
			t.Fatal(err)
		}
		d := e.rec.waitActive(t, from, "z.md", func(d DocumentActive) bool { return d.Rel == "z.md" })
		if d.ProjectID != p.ID || d.Name != "z.md" || !sameFile(d.Path, z) {
			t.Fatalf("active = %+v", d)
		}
		bufs, err := e.svc.session.Buffers(ctx)
		if err != nil || len(bufs) != 1 || !sameFile(bufs[0].Path, z) {
			t.Fatalf("Buffers = %+v, %v; want only z.md", bufs, err)
		}
	})

	t.Run("background", func(t *testing.T) {
		e := startEd(t)
		p, dir := docFixture(t, e)
		a, z, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "z.md"), filepath.Join(dir, "b.txt")
		for _, rel := range []string{"a.md", "b.txt"} {
			if err := e.svc.OpenDocument(ctx, p.ID, rel); err != nil {
				t.Fatal(err)
			}
		}
		e.rec.waitActive(t, 0, "b.txt", func(d DocumentActive) bool { return d.Rel == "b.txt" })
		if err := os.Rename(a, z); err != nil {
			t.Fatal(err)
		}
		from := e.rec.mark()
		if err := e.svc.renamed(ctx, a, z); err != nil {
			t.Fatal(err)
		}
		// An edit gives a barrier: events come in order, so anything the
		// rename sent has arrived once this one has.
		if err := e.svc.Write("GoX"); err != nil {
			t.Fatal(err)
		}
		e.rec.waitState(t, from, b, true)
		for _, ev := range e.rec.since(from) {
			if ev.name == eventDocumentActive {
				t.Fatalf("the active document changed: %+v", ev)
			}
		}
		bufs, err := e.svc.session.Buffers(ctx)
		if err != nil || len(bufs) != 1 || !sameFile(bufs[0].Path, b) {
			t.Fatalf("Buffers = %+v, %v; want only b.txt", bufs, err)
		}
	})
}

func TestEdRemoved(t *testing.T) {
	ctx := context.Background()
	e := startEd(t)
	p, dir := docFixture(t, e)
	a := filepath.Join(dir, "a.md")
	if err := e.svc.OpenDocument(ctx, p.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	e.rec.waitActive(t, 0, "a.md", func(d DocumentActive) bool { return d.Rel == "a.md" })
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	from := e.rec.mark()
	if err := e.svc.removed(ctx, a); err != nil {
		t.Fatal(err)
	}
	d := e.rec.waitActive(t, from, "empty", func(d DocumentActive) bool { return d.Path == "" })
	if !reflect.DeepEqual(d, DocumentActive{ReadOnly: []string{}, Simplified: []string{}}) {
		t.Fatalf("active = %+v, want empty fields", d)
	}
	cur, err := e.svc.session.Current(ctx)
	if err != nil || cur != (nvim.Buffer{}) {
		t.Fatalf("Current = %+v, %v; want an unnamed buffer", cur, err)
	}
	if !e.svc.session.Running() {
		t.Fatal("Neovim quit")
	}
	// The editor still takes documents.
	if err := e.svc.OpenDocument(ctx, p.ID, "b.txt"); err != nil {
		t.Fatalf("OpenDocument after removed: %v", err)
	}
}

// bothEnv is a ProjectService and an EditorService over the same project, with
// no fake between them.
type bothEnv struct {
	*edEnv
	proj  *ProjectService
	dlg   *fakeDialogs
	info  config.Project
	dir   string
	trash int // calls of the fake trash tool
	mu    sync.Mutex
}

func newBoth(t *testing.T) *bothEnv {
	t.Helper()
	e := startEd(t)
	p, dir := docFixture(t, e)
	b := &bothEnv{edEnv: e, dlg: &fakeDialogs{}, info: p, dir: dir}
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		b.mu.Lock()
		b.trash++
		b.mu.Unlock()
		return nil, os.Remove(args[len(args)-1])
	}
	limits := e.store.Limits()
	docs, err := docsafe.New(docsafe.Options{TrashBin: e.store.TrashBin(), Timeout: limits.CommandTimeout, Run: run})
	if err != nil {
		t.Fatal(err)
	}
	b.proj, err = newProjectService(projectServiceDeps{
		Store: e.store, Docs: docs, Dialogs: b.dlg, Editor: e.svc, Source: &fakeSource{},
		Emit: func(string, string) {}, Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.proj.shutdown)
	return b
}

func TestBothRename(t *testing.T) {
	b := newBoth(t)
	ctx := context.Background()
	if err := b.svc.OpenDocument(ctx, b.info.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	b.rec.waitActive(t, 0, "a.md", func(d DocumentActive) bool { return d.Rel == "a.md" })

	from := b.rec.mark()
	node, err := b.proj.RenameDocument(ctx, b.info.ID, "a.md", "b")
	if err != nil {
		t.Fatal(err)
	}
	if node.Rel != "b.md" || !exists(filepath.Join(b.dir, "b.md")) || exists(filepath.Join(b.dir, "a.md")) {
		t.Fatalf("node = %+v; a.md exists %v", node, exists(filepath.Join(b.dir, "a.md")))
	}
	b.rec.waitActive(t, from, "b.md", func(d DocumentActive) bool { return d.Rel == "b.md" })

	// A save writes the renamed file; the old one must not come back.
	from = b.rec.mark()
	if err := b.svc.Write("GoNEW"); err != nil {
		t.Fatal(err)
	}
	b.rec.waitState(t, from, filepath.Join(b.dir, "b.md"), true)
	if err := b.svc.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, filepath.Join(b.dir, "b.md")); !strings.Contains(got, "NEW") {
		t.Fatalf("b.md = %q, want the saved text", got)
	}
	if exists(filepath.Join(b.dir, "a.md")) {
		t.Fatal("a.md came back")
	}
}

func TestBothUnsaved(t *testing.T) {
	b := newBoth(t)
	ctx := context.Background()
	a := filepath.Join(b.dir, "a.md")
	if err := b.svc.OpenDocument(ctx, b.info.ID, "a.md"); err != nil {
		t.Fatal(err)
	}
	from := b.rec.mark()
	if err := b.svc.Write("GoNEW"); err != nil {
		t.Fatal(err)
	}
	b.rec.waitState(t, from, a, true)

	_, err := b.proj.RenameDocument(ctx, b.info.ID, "a.md", "b")
	wantCode(t, err, codeUnsavedChanges)
	b.dlg.set(func(d *fakeDialogs) { d.confirm = true })
	wantCode(t, b.proj.TrashDocument(ctx, b.info.ID, "a.md"), codeUnsavedChanges)

	if !exists(a) || exists(filepath.Join(b.dir, "b.md")) {
		t.Fatal("the disk changed")
	}
	if len(b.dlg.confirmed) != 0 || b.trash != 0 {
		t.Fatalf("dialog %v, trash calls %d", b.dlg.confirmed, b.trash)
	}
	mod, err := b.svc.modified(ctx)
	if err != nil || len(mod) != 1 || !sameFile(mod[0].Path, a) {
		t.Fatalf("modified = %+v, %v; the changes must stay in the buffer", mod, err)
	}
}

func TestEdRunning(t *testing.T) {
	e := newEd(t) // never started
	if e.svc.running() {
		t.Error("running before Start")
	}
	e = startEd(t)
	if !e.svc.running() {
		t.Error("not running after Start")
	}
	e.svc.shutdown()
	if e.svc.running() {
		t.Error("running after shutdown")
	}
}
