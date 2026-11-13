package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"Pisar/internal/config"
	"Pisar/internal/convert"
	"Pisar/internal/docsafe"
	"Pisar/internal/preview"
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
	application.RegisterEvent[string](eventMenuFormat)
	application.RegisterEvent[string](eventThemeChanged)

	// Editor events. The two document events carry a structure: the registered
	// type must be exactly the one emitted (a value, not a pointer).
	application.RegisterEvent[string](eventPtyData)
	application.RegisterEvent[string](eventNvimExit)
	application.RegisterEvent[DocumentActive](eventDocumentActive)
	application.RegisterEvent[DocumentState](eventDocumentState)
	application.RegisterEvent[DocumentExport](eventDocumentExport)
	application.RegisterEvent[string](eventFocusTree)

	// The preview and the search: one structure, the rest are cues without a payload.
	application.RegisterEvent[PreviewContent](eventPreviewContent)
	application.RegisterEvent[string](eventTogglePreview)
	application.RegisterEvent[string](eventOpenSearch)
	application.RegisterEvent[string](eventMenuSearch)
	application.RegisterEvent[string](eventOpenShortcuts)
	application.RegisterEvent[string](eventMenuShortcuts)
	application.RegisterEvent[string](eventMenuEditUserLua)
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
	// First of all: a Finder launch has a minimal PATH and no locale, and every
	// later LookPath and child process (Neovim, pbcopy) inherits what is set here.
	// Its failure is reported below, once the texts for the dialog are loaded.
	envErr := config.PrepareEnvironment()
	if os.Getenv(config.EnvLog) == "debug" {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}
	text, err := loadUIText()
	if err != nil {
		// No texts, so no dialog.
		return errors.Join(envErr, err)
	}
	if envErr != nil {
		return reportStartFailure(text, envErr)
	}
	app, stop, err := build(text)
	if err != nil {
		// A Finder launch has no stderr: say it in a dialog instead of exiting silently.
		return reportStartFailure(text, err)
	}
	defer stop()
	// An error from Run is not a start failure: the application has been running.
	return app.Run()
}

// startFailureDialog picks the title and the message the user sees for an
// error that stopped the application before it ran.
func startFailureDialog(text uiText, err error) (title, message string) {
	if errors.Is(err, config.ErrNewerVersion) {
		// The file is left exactly as it is; only tell the user.
		return text.ConfigNewerTitle, text.ConfigNewer
	}
	return text.StartFailedTitle, fill(text.StartFailed, "reason", err.Error())
}

// reportStartFailure shows the one dialog for a failed start and returns err.
func reportStartFailure(text uiText, err error) error {
	title, message := startFailureDialog(text, err)
	return showFatal(text, title, message, err)
}

// build creates everything the application needs up to, but not including,
// Run. stop releases what it started; it is idempotent and also runs here when
// the build fails halfway, so the caller has nothing to clean up on error.
func build(text uiText) (app *application.App, stop func(), err error) {
	// Deferred calls of the original sequence, in the order they were
	// registered; they run in reverse.
	var cleanups []func()
	runCleanups := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	defer func() {
		if err != nil {
			runCleanups()
			app, stop = nil, nil
		}
	}()

	home, err := config.DefaultHome()
	if err != nil {
		return nil, nil, err
	}
	store, err := config.Open(home)
	if err != nil {
		return nil, nil, err
	}
	limits := store.Limits()

	docs, err := docsafe.New(docsafe.Options{
		TrashBin: store.TrashBin(), Timeout: limits.CommandTimeout,
		BackupsDir: store.BackupsDir(), MaxBackups: limits.MaxBackups, WorkDir: store.WorkDir(),
	})
	if err != nil {
		return nil, nil, err
	}
	docxLimits := convert.InspectLimits{MaxEntries: limits.MaxDocxEntries, MaxPartBytes: limits.MaxDocxPartBytes}
	converter, err := convert.New(convert.Options{
		PandocBin: store.PandocBin(), TextutilBin: store.TextutilBin(),
		Timeout: limits.ConvertTimeout, Limits: docxLimits,
	})
	if err != nil {
		return nil, nil, err
	}

	renderer, err := preview.New(preview.Options{
		PandocBin: store.PandocBin(), Dir: store.PreviewDir(), Timeout: limits.PreviewTimeout,
		MaxSource: limits.MaxPreviewSource, MaxHTML: limits.MaxPreviewHTML,
	})
	if err != nil {
		return nil, nil, err
	}

	// The emitters, the clipboard and the dialogs need the application, the application needs
	// the service: they read app after it has been created, and nothing emits
	// or asks before Run.
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
		return nil, nil, err
	}

	// The settings service is built before the application and the window, like the
	// emitters above: setBackground reads window once it has been created, and no
	// theme is chosen before Run.
	var window *application.WebviewWindow
	settings := newSettingsService(settingsServiceDeps{
		Store: store,
		Emit:  emit,
		Text:  text,
		SetBackground: func(r, g, b uint8) {
			if window != nil {
				window.SetBackgroundColour(application.NewRGB(r, g, b))
			}
		},
	})

	// The editor comes first: the project service asks it which documents are
	// open. Cleanups run in reverse, so the project service stops before
	// the editor does, and no rename or Trash reaches an editor that is gone.
	editor, err := newEditorService(editorServiceDeps{
		Store:       store,
		Emit:        func(name string, data any) { app.Event.Emit(name, data) },
		Clipboard:   func(text string) bool { return app.Clipboard.SetText(text) },
		Limits:      limits,
		InitialFile: initialFile(os.Args),
		Words:       words,
		Preview:     renderer,
		Text:        nvimText(text),

		UserLuaTemplate: text.UserLuaTemplate,
	})
	if err != nil {
		return nil, nil, err
	}
	cleanups = append(cleanups, editor.shutdown)

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
		return nil, nil, err
	}
	// On macOS Run does not return when the application quits, so cleanup runs
	// from OnShutdown too; stop covers a failed start and the end of run.
	// shutdown is idempotent.
	cleanups = append(cleanups, svc.shutdown)

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
			application.NewServiceWithOptions(settings, application.ServiceOptions{MarshalError: marshalError}),
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
	theme := store.Theme()
	app.Menu.Set(newMenu(app, text, actions, theme, settings.setTheme,
		func() { app.Event.Emit(eventMenuNewDocument, "") },
		func() {
			if _, err := svc.AddProject(context.Background()); err != nil {
				slog.Error("menu: add project", "err", err)
			}
		},
	))

	// BackgroundColour is the bg.app colour of the remembered theme. It lives outside tokens.ts
	// on purpose (the window needs it before the webview exists); a test keeps the two in step.
	// Omitting it leaves RGBA{} (alpha 0), so the NSWindow background is clear and may flash
	// see-through before the webview paints. The URL carries the theme so that the page can
	// start in it without asking Go first.
	r, g, b := config.ThemeWindowBackground(theme)
	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            text.AppName,
		Width:            config.WindowWidth,
		Height:           config.WindowHeight,
		MinWidth:         config.WindowMinWidth,
		MinHeight:        config.WindowMinHeight,
		BackgroundColour: application.NewRGB(r, g, b),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarDefault,
		},
		URL: windowURL(theme),
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

	return app, runCleanups, nil
}

// windowURL is the page the window opens: the root, with the theme in the query.
func windowURL(theme string) string {
	return "/?" + url.Values{"theme": {theme}}.Encode()
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
