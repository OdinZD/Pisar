package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"Pisar/internal/config"
)

// toolVersion is a dotted version number: 0.12.6 is {0, 12, 6}, 3.1.11.1 is {3, 1, 11, 1}.
type toolVersion []int

func (v toolVersion) String() string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// less compares numerically, part by part (3.9 is below 3.13); a missing part
// counts as 0, so 3.13 and 3.13.0 are equal.
func (v toolVersion) less(other toolVersion) bool {
	for i := 0; i < max(len(v), len(other)); i++ {
		a, b := v.part(i), other.part(i)
		if a != b {
			return a < b
		}
	}
	return false
}

func (v toolVersion) part(i int) int {
	if i < len(v) {
		return v[i]
	}
	return 0
}

// versionInLine finds the first dotted number that starts the line or follows
// white space, with an optional "v" before it. That covers "NVIM v0.12.6" and
// "pandoc 3.13", and skips digits glued to a name or a path.
var versionInLine = regexp.MustCompile(`(?:^|\s)v?(\d+(?:\.\d+)*)`)

// parseToolVersion reads the version from the first line of "<tool> --version":
// "NVIM v0.12.6", "NVIM v0.13.0-dev-12+gabc" (0.13.0), "pandoc 3.13", "pandoc 3.1.11.1".
// Whatever follows the number is dropped, so a pre-release such as
// "v0.12.0-rc1" reads as 0.12.0: a release candidate has the behaviour the
// minimum asks for, and refusing it would lock out people who test Neovim.
// ok is false when the line holds no version. Only the first line is read.
func parseToolVersion(firstLine string) (v toolVersion, ok bool) {
	line, _, _ := strings.Cut(firstLine, "\n")
	m := versionInLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if m == nil {
		return nil, false
	}
	return parseVersionNumber(m[1])
}

// parseVersionNumber reads a plain dotted number ("0.12.0"), for the minimums of config.
func parseVersionNumber(s string) (toolVersion, bool) {
	if s == "" {
		return nil, false
	}
	parts := strings.Split(s, ".")
	v := make(toolVersion, len(parts))
	for i, p := range parts {
		// Atoi rejects an empty part, a sign and a number too large for int.
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p[0] == '+' {
			return nil, false
		}
		v[i] = n
	}
	return v, true
}

// versionWaitDelay bounds how long Wait lingers on the pipes after the process
// was killed. Not a user-facing limit: the caller's context is the real deadline.
const versionWaitDelay = time.Second

// probeVersion runs "<path> --version" (path is absolute, already resolved) with env and returns the
// version of its first line. It is bounded by ctx. An exit code other than 0, no output, an
// unreadable first line or a deadline is an error.
func probeVersion(ctx context.Context, path string, env []string) (toolVersion, error) {
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = env
	// A nil Stdin is /dev/null and a nil Stderr is discarded.
	// The tool gets its own process group, so that a script that started a
	// child (sh -c 'sleep 30') does not leave it behind when the deadline kills
	// the tool; internal/nvim does the same for the editor.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = versionWaitDelay
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("probe %s: %w", filepath.Base(path), err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("probe %s: %w", filepath.Base(path), err)
	}
	data, readErr := io.ReadAll(io.LimitReader(out, config.MaxVersionOutput+1))
	tooLong := len(data) > config.MaxVersionOutput
	if tooLong {
		// The tool is still writing; Wait would never return on its own.
		_ = killGroup(cmd)
	}
	waitErr := cmd.Wait()
	switch {
	case ctx.Err() != nil:
		return nil, fmt.Errorf("probe %s: %w", filepath.Base(path), ctx.Err())
	case tooLong:
		return nil, fmt.Errorf("probe %s: more than %d bytes of output", filepath.Base(path), config.MaxVersionOutput)
	case readErr != nil:
		return nil, fmt.Errorf("probe %s: read output: %w", filepath.Base(path), readErr)
	case waitErr != nil:
		return nil, fmt.Errorf("probe %s: %w", filepath.Base(path), waitErr)
	}
	v, ok := parseToolVersion(string(data))
	if !ok {
		first, _, _ := strings.Cut(string(data), "\n")
		return nil, fmt.Errorf("probe %s: no version in %q", filepath.Base(path), truncate(first, 80))
	}
	return v, nil
}

// killGroup kills the tool and everything it started. A group that is already
// gone is not an error.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// nvimXDGEnv is the redirection of Neovim's own folders into the application's
// home. Everything that starts Neovim, a version probe included, needs it:
// "nvim --version" already writes <XDG_STATE_HOME>/<NVIM_APPNAME>/nvim.log.
func nvimXDGEnv(paths config.NvimPaths) []string {
	return []string{
		"XDG_CONFIG_HOME=" + paths.ConfigHome,
		"XDG_DATA_HOME=" + paths.DataHome,
		"XDG_STATE_HOME=" + paths.StateHome,
		"XDG_CACHE_HOME=" + paths.CacheHome,
	}
}

// nvimProbeEnv is the whole environment for asking Neovim its version: that of
// the process, with Pisar's NVIM_APPNAME and XDG folders on top (the last value
// of a key wins). Never ask Neovim with the bare process environment.
func nvimProbeEnv(paths config.NvimPaths) []string {
	env := append(os.Environ(), "NVIM_APPNAME="+paths.AppName)
	return append(env, nvimXDGEnv(paths)...)
}

// versionKey identifies one build of a binary: a tool that was upgraded in
// place changes its size or modification time, one that was replaced behind a
// symbolic link changes the resolved path.
type versionKey struct {
	path    string
	size    int64
	modTime int64 // UnixNano
}

// versionCache remembers probeVersion's answer per binary: key is the resolved path plus the size
// and modification time of the file it points to, so a tool that was upgraded is asked again.
// The zero value is ready to use.
type versionCache struct {
	mu sync.Mutex
	m  map[versionKey]toolVersion
}

// get looks bin up in PATH and returns the path it found with its version. A
// tool that is not installed gives an error wrapping exec.ErrNotFound; any
// other error means it is there but could not be asked. Errors are not
// remembered. The lock is not held while the tool runs, so two calls that miss
// at the same time may both run it.
func (c *versionCache) get(ctx context.Context, bin string, env []string) (path string, v toolVersion, err error) {
	path, err = exec.LookPath(bin)
	if err != nil {
		return "", nil, fmt.Errorf("find %s: %w", bin, err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path, nil, fmt.Errorf("resolve %s: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return path, nil, fmt.Errorf("stat %s: %w", resolved, err)
	}
	key := versionKey{path: resolved, size: info.Size(), modTime: info.ModTime().UnixNano()}

	c.mu.Lock()
	cached, ok := c.m[key]
	c.mu.Unlock()
	if ok {
		return path, slices.Clone(cached), nil
	}

	v, err = probeVersion(ctx, path, env)
	if err != nil {
		return path, nil, err
	}
	c.mu.Lock()
	if c.m == nil {
		c.m = make(map[versionKey]toolVersion)
	}
	c.m[key] = slices.Clone(v)
	c.mu.Unlock()
	return path, v, nil
}
