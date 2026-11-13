package main

import (
	"context"
	"fmt"
	"log/slog"

	"Pisar/internal/config"
)

// eventThemeChanged tells the frontend that the theme changed. Payload: the
// theme ID.
const eventThemeChanged = "theme:changed"

// themeStore is what the settings service needs from the configuration:
// *config.Store.
type themeStore interface {
	Theme() string
	SetTheme(id string) error
}

// settingsServiceDeps are the parts a SettingsService is built from.
type settingsServiceDeps struct {
	Store themeStore
	// Emit sends an event to the frontend.
	Emit func(name, data string)
	// Text gives the labels of MenuShortcuts.
	Text uiText
	// SetBackground colours the native window behind the webview. main.go
	// binds it to the window after the window exists, so it may be nil in a
	// service that is built before that; nil means there is no window yet.
	SetBackground func(r, g, b uint8)
}

// SettingsService is the Wails service behind the user's settings; for now the
// theme. Wails binds every exported method, so what only the menu calls is
// unexported.
type SettingsService struct {
	store         themeStore
	emit          func(name, data string)
	text          uiText
	setBackground func(r, g, b uint8)
}

func newSettingsService(d settingsServiceDeps) *SettingsService {
	return &SettingsService{store: d.Store, emit: d.Emit, text: d.Text, setBackground: d.SetBackground}
}

// Theme returns the ID of the theme that is in force.
func (s *SettingsService) Theme(context.Context) string {
	theme := s.store.Theme()
	slog.Debug("service call", "method", "Theme", "theme", theme)
	return theme
}

// MenuShortcut is one menu item that has a keyboard shortcut.
type MenuShortcut struct {
	Label string `json:"label"`
	Accel string `json:"accel"` // as Wails writes it: "CmdOrCtrl+s"
}

// MenuShortcuts lists the shortcuts of the menu items for the overview of
// shortcuts. It is menuShortcuts, the one list the menu and its tests share, so
// the overview can never disagree with the menu. The result is never nil, so it
// reaches the frontend as an array.
func (s *SettingsService) MenuShortcuts(context.Context) []MenuShortcut {
	items := menuShortcuts(s.text)
	out := make([]MenuShortcut, 0, len(items))
	for _, it := range items {
		out = append(out, MenuShortcut{Label: it.label, Accel: it.accel})
	}
	return out
}

// setTheme remembers the theme, colours the native window and tells the
// frontend, in this order: when the choice cannot be remembered, neither the
// window nor the page changes, so what the user sees never disagrees with what
// the next start will show. Choosing the theme that already holds is not
// skipped: the store writes nothing then, and resending the colour and the
// event repairs a frontend that fell behind.
func (s *SettingsService) setTheme(id string) error {
	slog.Debug("theme chosen", "theme", id)
	if err := s.store.SetTheme(id); err != nil {
		return fmt.Errorf("set theme: %w", err)
	}
	if s.setBackground != nil {
		r, g, b := config.ThemeWindowBackground(id)
		s.setBackground(r, g, b)
	}
	s.emit(eventThemeChanged, id)
	return nil
}
