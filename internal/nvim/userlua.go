package nvim

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	nvimrpc "github.com/neovim/go-client/nvim"
)

// Names shared with the "user.lua" part at the end of nvim/init.lua.
const (
	userErrorVar = "pisar_user_error"
	userNoteVar  = "pisar_user_note"
)

// frame puts value into the one placeholder of a text of Options.Text, in one
// pass: a value that itself holds "{reason}" is left alone. A text that is
// missing gives the value alone.
func frame(text, placeholder, value string) string {
	if text == "" {
		return value
	}
	return strings.NewReplacer("{"+placeholder+"}", value).Replace(text)
}

// UserConfig is what became of the user's own user.lua while Neovim started.
// All fields are "" when all went well, and when there is no such file.
type UserConfig struct {
	// Error is the full text of the error the file raised (a syntax error, or one
	// at run time, with the traceback). The rest of the file did not run, what it
	// did before the error is in effect. It is the user's code that is meant, not
	// the content of any document, so it may be logged.
	Error string
	// What init.lua undid after the file and the user should hear of. They are
	// data, not sentences: the sentence is a text of Options.Text.
	Spell   string // 'spelllang' entries without a dictionary, comma separated
	Events  string // what was removed from 'eventignore', comma separated
	Restore string // why restoring the settings failed (an error of init.lua itself)
}

// FirstLine returns the first line of the error: what the message line and the
// log get.
func (u UserConfig) FirstLine() string {
	first, _, _ := strings.Cut(u.Error, "\n")
	return strings.TrimSpace(first)
}

// notices are the messages the user sees in Neovim, one each, with the one that
// matters most last: it is the one left on the screen, the others stay in
// :messages. Empty when there is nothing to tell. m is Options.Text.
func (u UserConfig) notices(m map[string]string) []string {
	var out []string
	if u.Spell != "" {
		out = append(out, frame(m["nvimSpellMissing"], "languages", u.Spell))
	}
	if u.Events != "" {
		out = append(out, frame(m["nvimEventsRestored"], "events", u.Events))
	}
	if u.Restore != "" {
		out = append(out, frame(m["nvimRestoreFailed"], "reason", u.Restore))
	}
	if first := u.FirstLine(); first != "" {
		out = append(out, frame(m["nvimUserLuaError"], "reason", first))
	}
	return out
}

// UserConfig returns what is known about user.lua since Start. Reading the two
// variables is done once, in Start, after the RPC channel exists: the file runs
// before Go is connected, so nothing could have been sent to it from there.
func (p *Process) UserConfig() UserConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.user
}

// readUserConfig registers the RPC channel in g:pisar_channel and reads what
// init.lua leaves about user.lua, in one call: init.lua runs its last restoring
// pass (pisar_restore) in it, so what the user's file put off until the end of
// Neovim's start is dealt with and in the notes, and a pass that comes later
// sees the channel and tells the user itself instead of leaving it to us. It
// remembers what it read and, when there is something to tell, puts it on the
// message line, cut to fit like every other message of this package (see
// messageLua): the full text stays in the variable. A hit-enter prompt at this
// point would freeze the start.
func (p *Process) readUserConfig(ctx context.Context) error {
	// Only text counts. The file runs before Go is connected and can put anything
	// into any variable; a table or a number there is not a report of init.lua,
	// and decoding it into a string would end the whole start. The note is a
	// table of init.lua's making, with text in it. pisar_restore is checked: the
	// file can have replaced it.
	var vars []string
	err := p.call(ctx, "user.lua result", func(nv *nvimrpc.Nvim) error {
		return nv.ExecLua(`
			vim.g.`+channelVar+` = (...)
			if type(pisar_restore) == "function" then
				pcall(pisar_restore)
			end
			local function text(v)
				return type(v) == "string" and v or ""
			end
			local note = vim.g.`+userNoteVar+`
			if type(note) ~= "table" then
				note = {}
			end
			return { text(vim.g.`+userErrorVar+`), text(note.spell), text(note.events), text(note.restore) }`,
			&vars, nv.ChannelID())
	})
	if err != nil {
		return err
	}
	if len(vars) != 4 {
		return nil
	}
	u := UserConfig{Error: vars[0], Spell: vars[1], Events: vars[2], Restore: vars[3]}
	p.mu.Lock()
	p.user = u
	p.mu.Unlock()
	if msgs := u.notices(p.texts); len(msgs) > 0 {
		return p.call(ctx, "show user.lua result", func(nv *nvimrpc.Nvim) error {
			return nv.ExecLua(messageLua+"\n\tfor _, m in ipairs((...)) do show(m) end", nil, msgs)
		})
	}
	return nil
}

// ErrNotRegularFile: the path exists but is not a regular file (a folder, a FIFO...).
var ErrNotRegularFile = errors.New("nvim: not a regular file")

// OpenUserLua edits the user's own user.lua at path in the current window. It
// never creates or writes the file: when there is none, the buffer is a new one
// that holds the lines of template and is 'modified', and the file comes into
// being only when the user saves it. A file that is there is opened as it is.
// path must be absolute. template may be empty (nothing is put into the buffer).
//
// It refuses with ErrPromptOpen while a prompt is up, and with ErrNotRegularFile
// (after one line on the message line) when path is a folder, a FIFO or the
// like: such a path is not opened at all, a FIFO would block Neovim on read.
func (p *Process) OpenUserLua(ctx context.Context, path, template string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("nvim: open user.lua: path is not absolute: %q", path)
	}
	lines := []string{}
	if template != "" {
		lines = strings.Split(strings.TrimSuffix(template, "\n"), "\n")
	}
	// One chunk, so that nothing can happen between looking at the file and
	// opening it. The kind of the file is decided by vim.uv.fs_stat, which follows
	// links like user_file() in init.lua does: a dangling link has no type and
	// counts as "no file". The failure of :edit travels like in OpenFile (see
	// there for the structured argument and the :redraw).
	//
	// Two guards for a prompt. In a hit-enter prompt or another blocking read a
	// Lua call is not answered at all (measured with Neovim 0.12), so the mode is
	// asked first, as Keymaps does. In the question about a swap file the call is
	// answered, with a buffer that is only half loaded: refuseDuringPrompt of the
	// chunk sees that.
	var res string
	err := p.call(ctx, "open user.lua", func(nv *nvimrpc.Nvim) error {
		if m, err := nv.Mode(); err != nil {
			return err
		} else if m.Blocking {
			return ErrPromptOpen
		}
		return nv.ExecLua(openUserLuaChunk, &res, path, lines, p.texts["nvimUserLuaNotFile"])
	})
	if err != nil {
		return err
	}
	if res == "notfile" {
		return fmt.Errorf("nvim: open user.lua: %w: %s", ErrNotRegularFile, path)
	}
	return nil
}

// openUserLuaChunk is the Lua of OpenUserLua. Arguments: the path, the lines of
// the template and the text for "not a regular file". It returns "notfile" when
// it opened nothing for that reason, "" otherwise.
const openUserLuaChunk = messageLua + promptGuardLua + bufferLua + findLua + `
		refuseDuringPrompt()
		local path, lines, notfile = ...
		local st = vim.uv.fs_stat(path)
		if st and st.type ~= "file" then
			if notfile ~= "" then show(notfile) end
			return "notfile"
		end

		-- :edit of the file that is already current fails with E37 when it has
		-- unsaved changes, which is the normal state of a new user.lua that is
		-- being written. There is nothing to open then.
		local here = false
		for _, b in ipairs(buffersOf(path)) do
			if b == vim.api.nvim_get_current_buf() then here = true end
		end
		if not here then
			local ok, err = pcall(vim.cmd, { cmd = "edit", args = { path }, magic = { file = false, bar = false } })
			vim.cmd.redraw()
			if not ok then
				error(err, 0)
			end
		end

		-- The template goes only into an empty buffer for a file that is not there:
		-- a buffer that already has text (a second open) keeps the user's own.
		if not st and #lines > 0 then
			local buf = vim.api.nvim_get_current_buf()
			local text = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
			if not vim.bo[buf].modified and #text == 1 and text[1] == "" then
				vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
				vim.api.nvim_win_set_cursor(0, { 1, 0 })
			end
		end
		return ""`
