package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"Pisar/internal/config"
)

// The stable names of the tools the frontend is told about. They are not the
// binary names from the configuration: those may be overridden, and the
// frontend picks its install instruction by this name.
const (
	toolNvim   = "nvim"
	toolPandoc = "pandoc"
)

// ToolStatus says whether an external tool the application needs is usable.
type ToolStatus struct {
	Name    string `json:"name"` // "nvim" | "pandoc"
	Found   bool   `json:"found"`
	Version string `json:"version"` // "0.12.6"; "" when not found or when it could not be read
	Minimum string `json:"minimum"` // the oldest version Pisar works with, always set
	TooOld  bool   `json:"tooOld"`  // found, version read, and it is below Minimum
}

// toolState is what one look at a tool found. It is the unexported counterpart
// of ToolStatus, with the path and the parsed numbers the next step needs.
type toolState struct {
	name    string
	path    string // as found in PATH; "" when not found
	found   bool
	version toolVersion // nil when unknown: not found, or the tool could not be asked
	minimum toolVersion // nil only when the configured minimum does not parse
	tooOld  bool
	readErr error // why the version is unknown although the tool was found
}

// toolsProbe is the part of EditorService that remembers things between Tools
// calls. The zero value is ready to use.
type toolsProbe struct {
	versions versionCache

	logMu  sync.Mutex
	logged string // key of the state that was last written to the log
}

// toolMinimum is the configured oldest version of a tool, as written in the
// configuration, and what it parses to (nil when it does not).
func toolMinimum(name string) (string, toolVersion) {
	var raw string
	switch name {
	case toolNvim:
		raw = config.MinNvimVersion
	case toolPandoc:
		raw = config.MinPandocVersion
	}
	v, ok := parseVersionNumber(raw)
	if !ok {
		// A constant of the program that does not parse is a bug, not something
		// the user can fix, so it must not turn into a verdict about their tool.
		slog.Error("editor: the minimum version of a tool does not parse", "tool", name, "minimum", raw)
		return raw, nil
	}
	return raw, v
}

// toolState looks one tool up and asks its version. A tool whose version cannot
// be read is still found and counts as usable: a change in what it prints must
// not lock the application. The caller bounds ctx.
func (e *EditorService) toolState(ctx context.Context, name string) toolState {
	st := toolState{name: name}
	_, st.minimum = toolMinimum(name)

	var bin string
	var env []string
	switch name {
	case toolNvim:
		bin, env = e.paths.Bin, nvimProbeEnv(e.paths)
	case toolPandoc:
		bin, env = e.store.PandocBin(), os.Environ()
	default:
		slog.Error("editor: unknown tool", "tool", name)
		return st
	}

	path, v, err := e.tools.versions.get(ctx, bin, env)
	if path == "" {
		// Not installed (or not executable): nothing to ask.
		return st
	}
	st.path, st.found = path, true
	if err != nil {
		// Logged by logTools, once per change, not on every call.
		st.readErr = err
		return st
	}
	st.version = v
	st.tooOld = st.minimum != nil && v.less(st.minimum)
	return st
}

// toolTooOldError: an external tool is installed but older than Pisar works with.
type toolTooOldError struct{ tool, version, minimum string }

func (e *toolTooOldError) Error() string {
	return fmt.Sprintf("%s %s is older than the minimum %s", e.tool, e.version, e.minimum)
}

// code is the stable error code of the tool; codeInternal for a tool the
// frontend has no text for.
func (e *toolTooOldError) code() string {
	switch e.tool {
	case toolNvim:
		return codeNvimTooOld
	case toolPandoc:
		return codePandocTooOld
	}
	return codeInternal
}

// refuseIfTooOld is nil for a tool that is usable, and for one that is not
// found or whose version cannot be read: those are not "too old", and the
// existing path reports a missing tool in its own way.
func (s toolState) refuseIfTooOld() error {
	if !s.found || !s.tooOld {
		return nil
	}
	minimum, _ := toolMinimum(s.name)
	return &toolTooOldError{tool: s.name, version: s.version.String(), minimum: minimum}
}

// pandocReady is nil when pandoc may be run, and a *toolTooOldError when it is
// installed but too old. It is the one place the pandoc minimum is enforced
// (see pandocGuard), and it asks the version of an unchanged binary only once.
func (e *EditorService) pandocReady(ctx context.Context) error {
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	return e.toolState(ctx, toolPandoc).refuseIfTooOld()
}

// status is the form the frontend gets.
func (s toolState) status() ToolStatus {
	raw, _ := toolMinimum(s.name)
	out := ToolStatus{Name: s.name, Found: s.found, Minimum: raw, TooOld: s.tooOld}
	if s.version != nil {
		out.Version = s.version.String()
	}
	return out
}

// Tools looks both tools up again on every call and always returns both, nvim
// first. The lookup is repeated, so a tool the user installs or upgrades while
// the application runs is seen by the next call; only the answer of an
// unchanged binary is remembered (versionCache). Neither lookup starts the
// editor. The two tools are asked at once, each against the same deadline, so
// a hanging one does not cost the other its answer.
func (e *EditorService) Tools(ctx context.Context) []ToolStatus {
	ctx, cancel := e.bounded(ctx)
	defer cancel()

	names := [...]string{toolNvim, toolPandoc}
	var states [len(names)]toolState
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			states[i] = e.toolState(ctx, name)
		}()
	}
	wg.Wait()

	out := make([]ToolStatus, len(states))
	for i, s := range states {
		out[i] = s.status()
	}
	e.logTools(states[:])
	slog.Debug("editor: Tools", toolNvim, out[0].Found, toolPandoc, out[1].Found)
	return out
}

// logTools writes the state of the tools at Info level (and a version that could
// not be read at Warn) the first time and whenever it differs from the last one
// written, so asking again does not repeat the same warning. The frontend asks at start-up,
// which is what brings the versions into the log file.
func (e *EditorService) logTools(states []toolState) {
	var key strings.Builder
	for _, s := range states {
		fmt.Fprintf(&key, "%s|%s|%t|%v|%v|%t|%v\n", s.name, s.path, s.found, s.version, s.minimum, s.tooOld, s.readErr)
	}
	e.tools.logMu.Lock()
	changed := e.tools.logged != key.String()
	e.tools.logged = key.String()
	e.tools.logMu.Unlock()
	if !changed {
		return
	}
	for _, s := range states {
		raw, _ := toolMinimum(s.name)
		slog.Info("editor: tool", "tool", s.name, "found", s.found, "path", s.path,
			"version", s.version.String(), "minimum", raw, "tooOld", s.tooOld)
		if s.readErr != nil {
			slog.Warn("editor: could not read the version of a tool", "tool", s.name, "err", s.readErr)
		}
	}
}
