package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"Pisar/internal/config"
	"Pisar/internal/docsafe"
	"Pisar/internal/project"
)

// The frontend build output is embedded into the binary; `task build` and
// every Go check build the frontend first, so frontend/dist exists.
//
//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Typed events: the binding generator emits a TypeScript type for each payload.
	application.RegisterEvent[string](eventProjectsChanged)
	application.RegisterEvent[string](eventProjectChanged)
	application.RegisterEvent[string](eventMenuNewDocument)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	text, err := loadUIText()
	if err != nil {
		return err
	}
	home, err := config.DefaultHome()
	if err != nil {
		return err
	}
	store, err := config.Open(home)
	if errors.Is(err, config.ErrNewerVersion) {
		// The file is left exactly as it is; only tell the user.
		return showFatal(text, text.ConfigNewerTitle, text.ConfigNewer, err)
	}
	if err != nil {
		return err
	}
	limits := store.Limits()

	docs, err := docsafe.New(docsafe.Options{TrashBin: store.TrashBin(), Timeout: limits.CommandTimeout})
	if err != nil {
		return err
	}

	// The emitter and the dialogs need the application, the application needs
	// the service: they read app after it has been created, and nothing emits
	// or asks before Run.
	var app *application.App
	emit := func(name, data string) { app.Event.Emit(name, data) }
	dlg := &wailsDialogs{text: text}

	svc, err := newProjectService(projectServiceDeps{
		Store:   store,
		Docs:    docs,
		Dialogs: dlg,
		Source:  project.FSEventsSource{},
		Emit:    emit,
		Limits:  limits,
	})
	if err != nil {
		return err
	}
	// On macOS Run does not return when the application quits, so cleanup runs
	// from OnShutdown; the deferred call covers a failed start. shutdown is idempotent.
	defer svc.shutdown()

	app = application.New(application.Options{
		Name: text.AppName,
		Services: []application.Service{
			application.NewServiceWithOptions(svc, application.ServiceOptions{MarshalError: marshalError}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: svc.shutdown,
	})
	dlg.app = app

	svc.syncWatchers() // watch every project whose folder exists

	app.Menu.Set(newMenu(app, text,
		func() { app.Event.Emit(eventMenuNewDocument, "") },
		func() {
			if _, err := svc.AddProject(context.Background()); err != nil {
				slog.Error("menu: add project", "err", err)
			}
		},
	))

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            text.AppName,
		Width:            1280,
		Height:           800,
		MinWidth:         1000,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(255, 255, 255),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarDefault,
		},
		URL: "/",
	})

	if saved := store.Recovered(); saved != "" {
		// A dialog cannot be shown before Run, and it must not hold the window back
		// (Show returns at once).
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			app.Dialog.Warning().
				SetTitle(text.ConfigRecoveredTitle).
				SetMessage(fill(text.ConfigRecovered, "path", saved)).
				Show()
		})
	}

	return app.Run()
}

// showFatal shows a native error and quits when it is dismissed. It runs a
// bare application, because a dialog needs a running one. cause is returned
// once the application has stopped (on macOS Run normally never returns).
func showFatal(text uiText, title, message string, cause error) error {
	app := application.New(application.Options{Name: text.AppName})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		dlg := app.Dialog.Error().SetTitle(title).SetMessage(message)
		ok := dlg.AddButton(text.OK).OnClick(app.Quit)
		dlg.SetDefaultButton(ok).SetCancelButton(ok).Show()
	})
	if err := app.Run(); err != nil {
		return fmt.Errorf("%w (and the error dialog failed: %w)", cause, err)
	}
	return cause
}
