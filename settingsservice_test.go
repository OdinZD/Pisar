package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"Pisar/internal/config"
)

// fakeThemeStore records its calls into the shared log, so that a test can see
// the order of store, window and event.
type fakeThemeStore struct {
	theme  string
	setErr error
	log    *[]string
}

func (f *fakeThemeStore) Theme() string { return f.theme }

func (f *fakeThemeStore) SetTheme(id string) error {
	*f.log = append(*f.log, "store:"+id)
	if f.setErr != nil {
		return f.setErr
	}
	f.theme = id
	return nil
}

func newSettings(store themeStore, log *[]string) *SettingsService {
	return newSettingsService(settingsServiceDeps{
		Store: store,
		Emit:  func(name, data string) { *log = append(*log, "emit:"+name+":"+data) },
		SetBackground: func(r, g, b uint8) {
			*log = append(*log, fmt.Sprintf("background:%d,%d,%d", r, g, b))
		},
	})
}

func background(id string) string {
	r, g, b := config.ThemeWindowBackground(id)
	return fmt.Sprintf("background:%d,%d,%d", r, g, b)
}

func TestSettingsTheme(t *testing.T) {
	var log []string
	store := &fakeThemeStore{theme: config.DefaultTheme, log: &log}
	s := newSettings(store, &log)
	if got := s.Theme(context.Background()); got != config.DefaultTheme {
		t.Errorf("Theme = %q, want %q", got, config.DefaultTheme)
	}
	if err := s.setTheme(config.ThemeDracula); err != nil {
		t.Fatal(err)
	}
	if got := s.Theme(context.Background()); got != config.ThemeDracula {
		t.Errorf("Theme after a change = %q, want %q", got, config.ThemeDracula)
	}
}

func TestSettingsSetTheme(t *testing.T) {
	for _, id := range config.Themes() {
		t.Run(id, func(t *testing.T) {
			var log []string
			s := newSettings(&fakeThemeStore{theme: "other", log: &log}, &log)
			if err := s.setTheme(id); err != nil {
				t.Fatal(err)
			}
			want := []string{"store:" + id, background(id), "emit:" + eventThemeChanged + ":" + id}
			if !slices.Equal(log, want) {
				t.Errorf("calls = %v, want %v", log, want)
			}
		})
	}
}

func TestSettingsSetThemeStoreError(t *testing.T) {
	var log []string
	boom := errors.New("disk full")
	s := newSettings(&fakeThemeStore{theme: config.DefaultTheme, setErr: boom, log: &log}, &log)
	err := s.setTheme(config.ThemeDracula)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}
	if want := []string{"store:" + config.ThemeDracula}; !slices.Equal(log, want) {
		t.Errorf("calls = %v, want only %v", log, want)
	}
}

func TestSettingsSetThemeUnknown(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var log []string
	s := newSettings(store, &log)
	before := s.Theme(context.Background())
	if err := s.setTheme("no-such-theme"); !errors.Is(err, config.ErrUnknownTheme) {
		t.Fatalf("err = %v, want ErrUnknownTheme", err)
	}
	if len(log) != 0 {
		t.Errorf("window or event touched for an unknown theme: %v", log)
	}
	if got := s.Theme(context.Background()); got != before {
		t.Errorf("Theme = %q, want it unchanged (%q)", got, before)
	}
}

func TestSettingsSetThemeSame(t *testing.T) {
	var log []string
	s := newSettings(&fakeThemeStore{theme: config.ThemeDracula, log: &log}, &log)
	if err := s.setTheme(config.ThemeDracula); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"store:" + config.ThemeDracula, background(config.ThemeDracula),
		"emit:" + eventThemeChanged + ":" + config.ThemeDracula,
	}
	if !slices.Equal(log, want) {
		t.Errorf("calls = %v, want %v", log, want)
	}
}

// A real store: the choice survives a reopen, and a theme chosen twice is
// still announced twice.
func TestSettingsSetThemeRealStore(t *testing.T) {
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	var log []string
	s := newSettings(store, &log)
	for range 2 {
		if err := s.setTheme(config.ThemeCatppuccinMocha); err != nil {
			t.Fatal(err)
		}
	}
	event := "emit:" + eventThemeChanged + ":" + config.ThemeCatppuccinMocha
	want := []string{background(config.ThemeCatppuccinMocha), event, background(config.ThemeCatppuccinMocha), event}
	if !slices.Equal(log, want) {
		t.Errorf("calls = %v, want %v", log, want)
	}
	reopened, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Theme(); got != config.ThemeCatppuccinMocha {
		t.Errorf("Theme after reopen = %q", got)
	}
}

// paletteBlocks maps a theme ID to its palette in tokens.ts (a key of
// themeBlocks). A theme without an entry fails TestSettingsTokens.
var paletteBlocks = map[string]string{
	config.ThemePisar:           "pisar",
	config.ThemeCatppuccinMocha: "catppuccin-mocha",
	config.ThemeDracula:         "dracula",
}

// TestSettingsTokens keeps what Go repeats from tokens.ts in step with it: the
// list of theme IDs and the bg.app colour the native window gets.
func TestSettingsTokens(t *testing.T) {
	src := readString(t, filepath.Join("frontend", "src", "theme", "tokens.ts"))
	m := regexp.MustCompile(`(?m)^export const themeIds = \[([^\]]*)\] as const`).FindStringSubmatch(src)
	if m == nil {
		t.Fatal("themeIds not found in tokens.ts")
	}
	var ids []string
	for _, q := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(m[1], -1) {
		ids = append(ids, q[1])
	}
	if got := config.Themes(); !slices.Equal(got, ids) {
		t.Errorf("config.Themes() = %v, tokens.ts themeIds = %v", got, ids)
	}

	for _, id := range config.Themes() {
		block, ok := paletteBlocks[id]
		if !ok {
			t.Errorf("no palette name for theme %q in paletteBlocks", id)
			continue
		}
		want := tokenColorIn(t, block, "bg.app")
		r, g, b := config.ThemeWindowBackground(id)
		if got := fmt.Sprintf("#%02X%02X%02X", r, g, b); !strings.EqualFold(got, want) {
			t.Errorf("%s: ThemeWindowBackground = %s, bg.app in tokens.ts = %s", id, got, want)
		}
	}
}

// The window URL carries the theme in the query, and the asset server of Wails
// still answers it with the page (it looks only at the path).
func TestSettingsWindowURL(t *testing.T) {
	for _, id := range config.Themes() {
		if got, want := windowURL(id), "/?theme="+id; got != want {
			t.Errorf("windowURL(%q) = %q, want %q", id, got, want)
		}
	}
	h := application.AssetFileServerFS(assets)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, windowURL(config.ThemeDracula), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("GET %s: status %d, body %.80q", windowURL(config.ThemeDracula), rec.Code, rec.Body.String())
	}
}
