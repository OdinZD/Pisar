package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// EnvToolDirs replaces the built-in list of extra tool directories. Entries are
// separated like PATH. Set but empty means "no extra directories".
const EnvToolDirs = "PISAR_TOOL_DIRS"

// DefaultLocale is the LC_CTYPE value given to the process when no locale variable is set.
const DefaultLocale = "en_US.UTF-8"

// builtinToolDirs is the only place that names the Homebrew prefixes: Apple
// silicon first, then Intel.
var builtinToolDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// ToolDirs returns the directories appended to PATH: the entries of $PISAR_TOOL_DIRS
// when the variable is set (even to ""), otherwise the built-in list
// ("/opt/homebrew/bin", "/usr/local/bin" — Homebrew on Apple silicon and on Intel).
// Entries that are empty or not absolute are dropped.
func ToolDirs() []string {
	entries := slices.Clone(builtinToolDirs)
	if v, ok := os.LookupEnv(EnvToolDirs); ok {
		entries = filepath.SplitList(v)
	}
	// A relative or empty entry would mean "the current directory" to LookPath.
	return slices.DeleteFunc(entries, func(d string) bool { return d == "" || !filepath.IsAbs(d) })
}

// ExtendPath returns path with every directory of dirs that it does not already
// contain appended, in order. Pure: it touches neither the environment nor the disk.
func ExtendPath(path string, dirs []string) string {
	seen := make(map[string]bool)
	for _, e := range filepath.SplitList(path) {
		seen[filepath.Clean(e)] = true
	}
	var added []string
	for _, d := range dirs {
		c := filepath.Clean(d)
		if seen[c] {
			continue
		}
		seen[c] = true
		added = append(added, c)
	}
	if len(added) == 0 {
		return path
	}
	tail := strings.Join(added, string(filepath.ListSeparator))
	if path == "" {
		// A leading separator would mean "the current directory".
		return tail
	}
	return path + string(filepath.ListSeparator) + tail
}

// PrepareEnvironment adjusts the environment of this process, once, at startup:
// PATH gets ToolDirs() appended, and LC_CTYPE is set to DefaultLocale when none of
// LC_ALL, LC_CTYPE and LANG has a non-empty value.
func PrepareEnvironment() error {
	if err := os.Setenv("PATH", ExtendPath(os.Getenv("PATH"), ToolDirs())); err != nil {
		return fmt.Errorf("config: set PATH: %w", err)
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if os.Getenv(k) != "" {
			return nil
		}
	}
	if err := os.Setenv("LC_CTYPE", DefaultLocale); err != nil {
		return fmt.Errorf("config: set LC_CTYPE: %w", err)
	}
	return nil
}
