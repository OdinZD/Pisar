package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"Pisar/spike/internal/nvim"
	"Pisar/spike/internal/term"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

// The Neovim configuration the application ships. It is written to a private
// temporary directory at startup because Neovim reads it from a file path.
//
//go:embed nvim/init.lua
var initLua []byte

// shutdownTimeout bounds stopping Neovim when the application quits.
const shutdownTimeout = 5 * time.Second

func init() {
	// Typed events: the binding generator emits a TypeScript type for each payload.
	application.RegisterEvent[string](term.EventData)
	application.RegisterEvent[string](term.EventSaved)
	application.RegisterEvent[string](term.EventExit)
	application.RegisterEvent[string](term.EventCopy)
	application.RegisterEvent[string](eventRenderer)
	// Only the bench mode emits these; registering lets the binding generator type them.
	application.RegisterEvent[string](eventBenchStart)
	application.RegisterEvent[string](eventBenchStop)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == benchTableFlag {
		os.Exit(benchTableMain(os.Args[2:], os.Stdout, os.Stderr))
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// The directory holds init.lua and Neovim's RPC socket. It is ours alone
	// (MkdirTemp creates it 0700), which is what nvim.Options.SocketDir asks for.
	dir, err := os.MkdirTemp("", "pisar-spike-")
	if err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}
	initPath := filepath.Join(dir, "init.lua")
	if err := os.WriteFile(initPath, initLua, 0o600); err != nil {
		return errors.Join(fmt.Errorf("write init.lua: %w", err), os.RemoveAll(dir))
	}

	// The emitter needs the application, the application needs the service:
	// the closure reads wails after it has been created, and nothing emits
	// before the frontend calls Start.
	var wails *application.App
	emit := func(name, data string) { wails.Event.Emit(name, data) }
	// Bench mode only: the recorder sees every pty:data event on its way out.
	benchPath := os.Getenv(envBench)
	var rec *emitRecorder
	sessionEmit := term.Emitter(emit)
	if benchPath != "" {
		rec = &emitRecorder{}
		sessionEmit = rec.wrap(emit)
	}
	session := term.NewSession(term.Config{
		Nvim: nvim.Options{
			Bin:       "nvim",
			InitLua:   initPath,
			AppName:   "pisar-spike",
			SocketDir: dir,
		},
	}, sessionEmit)

	tr, err := newTracer(os.Getenv(envTrace))
	if err != nil {
		// Diagnostics are optional; the application runs without them.
		log.Printf("tracing disabled: %v", err)
	}

	// A first command-line argument is a file to open once Neovim runs.
	var initialFile string
	if len(os.Args) > 1 {
		initialFile = os.Args[1]
	}
	service := NewTerminalService(session, initialFile, os.Getenv(envRenderer), tr)
	if benchPath != "" {
		service.useBench(newBenchRun(benchConfig{
			Session:  session,
			Recorder: rec,
			Emit:     emit,
			Quit:     func() { wails.Quit() },
			Path:     benchPath,
			File:     initialFile,
			NvimBin:  "nvim",
		}))
	}

	// On macOS Run does not return when the application quits, so cleanup
	// runs from OnShutdown; the deferred call covers a failed start. Once
	// makes the second call a no-op.
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if err := session.Close(ctx); err != nil {
				log.Printf("close session: %v", err)
			}
			// After Close, so the counters in the last trace line are final.
			if err := service.closeTrace(); err != nil {
				log.Printf("%v", err)
			}
			if err := os.RemoveAll(dir); err != nil {
				log.Printf("remove %s: %v", dir, err)
			}
		})
	}
	defer cleanup()

	wails = application.New(application.Options{
		Name:        "Pisar spike",
		Description: "Neovim in a terminal, driven from Go",
		Services: []application.Service{
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: cleanup,
	})

	service.useClipboard(wails.Clipboard.SetText)
	actions := newMenuActions(session, emit, tr)
	wails.Menu.Set(newMenu(wails, actions))

	wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Pisar spike",
		Width:            1100,
		Height:           700,
		BackgroundColour: application.NewRGB(255, 255, 255),
		URL:              "/",
	})

	return wails.Run()
}
