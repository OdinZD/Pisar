package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"Pisar/internal/config"
)

// The colorscheme text comes from the frontend and Neovim EXECUTES it as Lua
// (colors/pisar.lua). A compromised webview must not be able to slip code into
// it, so Go accepts only the shape frontend/src/theme/nvimColorscheme.ts really
// produces and refuses everything else. The test data testdata/colorscheme/pisar.lua
// is that generator's real output (kept current by a vitest file snapshot), and
// colorscheme_test.go runs the check on it.
//
// Every non-empty line has to match, in full, one of:
//   - a header line, byte for byte (headerLines);
//   - one nvim_set_hl call (groupLine): a quoted group name from a closed
//     character set and a one-line table of known attributes, whose values are
//     "#RRGGBB", true, or a quoted group name. Nothing in those values can end
//     the string or the table, so no other Lua can ride along.
//
// Empty lines are allowed. A line that holds only blanks is not empty: it is not
// something the generator writes.

// errInvalidColorscheme is returned for text that is not generator output.
var errInvalidColorscheme = errors.New("invalid colorscheme")

// headerLines are the fixed lines the generator writes before the groups,
// character for character (a copy of nvimColorscheme() in nvimColorscheme.ts).
var headerLines = map[string]struct{}{
	"-- Generated from frontend/src/theme/tokens.ts by Pisar. Do not edit: the file is rewritten at start.": {},
	"-- Drop a previous colorscheme first: changing 'background' while g:colors_name is set":                {},
	"-- would reload that scheme.":                         {},
	"vim.g.colors_name = nil":                              {},
	`vim.o.background = "light"`:                           {},
	`vim.cmd("highlight clear")`:                           {},
	`vim.g.colors_name = "` + config.NvimColorscheme + `"`: {},
}

const (
	// groupName is the character set of luaGroupName() in the generator.
	groupName = `[A-Za-z0-9_.@]+`
	// attribute is one `key = value` pair; each key has exactly one kind of value.
	attribute = `(?:(?:fg|bg|sp) = "#[0-9A-Fa-f]{6}"` +
		`|(?:bold|italic|underline|undercurl) = true` +
		`|link = "` + groupName + `")`
)

// groupLine is anchored with \A and \z: the regexp package's ^ and $ match at
// the text boundaries unless the m flag is set, but the explicit forms leave no
// doubt, and each line is matched on its own (no newline can be inside it).
var groupLine = regexp.MustCompile(
	`\Avim\.api\.nvim_set_hl\(0, "` + groupName + `", \{ ` + attribute + `(?:, ` + attribute + `)* \}\)\z`)

// checkColorscheme returns nil if text is acceptable to write as colors/pisar.lua.
// The error never contains the offending text, only where it is and why.
func checkColorscheme(text string, maxBytes int) error {
	switch {
	case len(text) > maxBytes:
		return fmt.Errorf("%w: %d bytes, limit %d", errInvalidColorscheme, len(text), maxBytes)
	case !utf8.ValidString(text):
		return fmt.Errorf("%w: not valid UTF-8", errInvalidColorscheme)
	case strings.IndexByte(text, 0) >= 0:
		return fmt.Errorf("%w: contains a NUL byte", errInvalidColorscheme)
	}
	groups := 0
	for i, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if _, ok := headerLines[line]; ok {
			continue
		}
		if groupLine.MatchString(line) {
			groups++
			continue
		}
		return fmt.Errorf("%w: line %d is not a line the generator writes", errInvalidColorscheme, i+1)
	}
	if groups == 0 {
		return fmt.Errorf("%w: no highlight group", errInvalidColorscheme)
	}
	return nil
}
