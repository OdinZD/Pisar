package main

import (
	"context"
	"log/slog"
	"os/exec"
)

// The stable names of the tools the frontend is told about. They are not the
// binary names from the configuration: those may be overridden, and the
// frontend picks its install instruction by this name.
const (
	toolNvim   = "nvim"
	toolPandoc = "pandoc"
)

// ToolStatus says whether an external tool the application needs was found.
type ToolStatus struct {
	Name  string `json:"name"` // "nvim" | "pandoc"
	Found bool   `json:"found"`
}

// Tools looks both tools up again on every call and always returns both, nvim
// first. Nothing is remembered: the user may install a tool while the
// application runs and ask again. The lookup does not start the tool.
func (e *EditorService) Tools(_ context.Context) []ToolStatus {
	out := []ToolStatus{
		{Name: toolNvim, Found: onPath(e.paths.Bin)},
		{Name: toolPandoc, Found: onPath(e.store.PandocBin())},
	}
	slog.Debug("editor: Tools", toolNvim, out[0].Found, toolPandoc, out[1].Found)
	return out
}

// onPath reports whether exec.LookPath resolves the name. A file without the
// execute bit and a folder are not found, which is what Start would hit too.
func onPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}
