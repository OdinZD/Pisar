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
	setBackground func(r, g, b uint8)
}

func newSettingsService(d settingsServiceDeps) *SettingsService {
	return &SettingsService{store: d.Store, emit: d.Emit, setBackground: d.SetBackground}
}

// Theme returns the ID of the theme that is in force.
func (s *SettingsService) Theme(context.Context) string {
	theme := s.store.Theme()
	slog.Debug("service call", "method", "Theme", "theme", theme)
	return theme
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
