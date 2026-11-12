package nvim

import (
	"context"
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
