package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"Pisar/internal/config"
	"Pisar/internal/convert"
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
	application.RegisterEvent[string](eventMenuCopy)

	// Editor events. The two document events carry a structure: the registered
	// type must be exactly the one emitted (a value, not a pointer).
	application.RegisterEvent[string](eventPtyData)
	application.RegisterEvent[string](eventNvimExit)
	application.RegisterEvent[DocumentActive](eventDocumentActive)
	application.RegisterEvent[DocumentState](eventDocumentState)
	application.RegisterEvent[DocumentExport](eventDocumentExport)
	application.RegisterEvent[string](eventFocusTree)
}

// initialFile is the document named on the command line, "" if none. An
// argument that starts with "-" is a flag (macOS used to add -psn_...), not a file.
func initialFile(args []string) string {
	if len(args) < 2 || strings.HasPrefix(args[1], "-") {
		return ""
	}
	return args[1]
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if os.Getenv(config.EnvLog) == "debug" {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}
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

	docs, err := docsafe.New(docsafe.Options{
		TrashBin: store.TrashBin(), Timeout: limits.CommandTimeout,
		BackupsDir: store.BackupsDir(), MaxBackups: limits.MaxBackups, WorkDir: store.WorkDir(),
	})
	if err != nil {
		return err
	}
	docxLimits := convert.InspectLimits{MaxEntries: limits.MaxDocxEntries, MaxPartBytes: limits.MaxDocxPartBytes}
	converter, err := convert.New(convert.Options{
		PandocBin: store.PandocBin(), TextutilBin: store.TextutilBin(),
		Timeout: limits.ConvertTimeout, Limits: docxLimits,
	})
	if err != nil {
		return err
	}

	// The emitters, the clipboard and the dialogs need the application, the application needs
	// the service: they read app after it has been created, and nothing emits
	// or asks before Run.
	var app *application.App
	emit := func(name, data string) { app.Event.Emit(name, data) }
	dlg := &wailsDialogs{text: text}
	words, err := newWordDocs(wordDocsDeps{
		Convert: converter,
		Examine: func(ctx context.Context, path string) (convert.Report, error) {
			return convert.Examine(ctx, path, docxLimits)
		},
		Files: docs, Dialogs: dlg, Text: text, ExportAttempts: limits.ExportAttempts,
	})
	if err != nil {
		return err
	}

	// The editor comes first: the project service asks it which documents are
	// open. Deferred calls run in reverse, so the project service stops before
	// the editor does, and no rename or Trash reaches an editor that is gone.
	editor, err := newEditorService(editorServiceDeps{
		Store:       store,
		Emit:        func(name string, data any) { app.Event.Emit(name, data) },
		Clipboard:   func(text string) bool { return app.Clipboard.SetText(text) },
		Limits:      limits,
		InitialFile: initialFile(os.Args),
		Words:       words,
	})
	if err != nil {
		return err
	}
	defer editor.shutdown()

	svc, err := newProjectService(projectServiceDeps{
		Store:   store,
		Docs:    docs,
		Dialogs: dlg,
		Editor:  editor,
		Source:  project.FSEventsSource{},
		Emit:    emit,
		Limits:  limits,
	})
	if err != nil {
		return err
	}
	// On macOS Run does not return when the application quits, so cleanup runs
	// from OnShutdown; the deferred calls cover a failed start. shutdown is idempotent.
	defer svc.shutdown()

	// Cmd+Q, the Quit menu item and the red close button all ask the gate
	// (see quitGate). quit is App.Quit; Wails allows it from any goroutine
	// (it runs the native call on the main thread).
	gate := &quitGate{
		editor:  editor,
		dialogs: dlg,
		text:    text,
		timeout: limits.CommandTimeout,
		names:   limits.QuitDialogNames,
		quit:    func() { app.Quit() },
	}

	app = application.New(application.Options{
		Name: text.AppName,
		Services: []application.Service{
			application.NewServiceWithOptions(svc, application.ServiceOptions{MarshalError: marshalError}),
			application.NewServiceWithOptions(editor, application.ServiceOptions{MarshalError: marshalError}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		// Called on the main thread by AppKit; request never blocks.
		ShouldQuit: gate.request,
		OnShutdown: func() {
			svc.shutdown()
			editor.shutdown()
		},
	})
	dlg.app = app

	svc.syncWatchers() // watch every project whose folder exists

	actions := &menuActions{editor: editor, emit: emit, timeout: limits.CommandTimeout}
	app.Menu.Set(newMenu(app, text, actions,
		func() { app.Event.Emit(eventMenuNewDocument, "") },
		func() {
			if _, err := svc.AddProject(context.Background()); err != nil {
				slog.Error("menu: add project", "err", err)
			}
		},
	))

	// BackgroundColour is white and lives outside tokens.ts on purpose: omitting it leaves RGBA{}
	// (alpha 0), so the NSWindow background is clear and may flash see-through before the webview paints.
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            text.AppName,
		Width:            config.WindowWidth,
		Height:           config.WindowHeight,
		MinWidth:         config.WindowMinWidth,
		MinHeight:        config.WindowMinHeight,
		BackgroundColour: application.NewRGB(255, 255, 255),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarDefault,
		},
		URL: "/",
	})

	// The red close button asks like Cmd+Q does. Cancel keeps the window; when
	// the gate lets the request through, the window closes and the application
	// ends because the last window was closed.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !gate.request() {
			e.Cancel()
		}
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
