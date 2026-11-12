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

// UserConfig is what became of the user's own user.lua while Neovim started.
// Both fields are "" when all went well, and when there is no such file.
type UserConfig struct {
	// Error is the full text of the error the file raised (a syntax error, or one
	// at run time, with the traceback). The rest of the file did not run, what it
	// did before the error is in effect. It is the user's code that is meant, not
	// the content of any document, so it may be logged.
	Error string
	// Note is something init.lua undid after the file and the user should hear
	// of: a 'spelllang' entry without a dictionary, an event taken out of
	// 'eventignore'. Several are joined with "; ".
	Note string
}

// FirstLine returns the first line of the error: what the message line and the
// log get.
func (u UserConfig) FirstLine() string {
	first, _, _ := strings.Cut(u.Error, "\n")
	return strings.TrimSpace(first)
}

// notice is the one line the user sees in Neovim: the error and the note, in
// that order. "" when there is neither.
func (u UserConfig) notice() string {
	var parts []string
	if first := u.FirstLine(); first != "" {
		if !strings.HasPrefix(first, "user.lua") {
			first = "user.lua: " + first
		}
		parts = append(parts, first)
	}
	if u.Note != "" {
		parts = append(parts, u.Note)
	}
	return strings.Join(parts, "; ")
}

// UserConfig returns what is known about user.lua since Start. Reading the two
// variables is done once, in Start, after the RPC channel exists: the file runs
// before Go is connected, so nothing could have been sent to it from there.
func (p *Process) UserConfig() UserConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.user
}

// readUserConfig reads the variables init.lua leaves, remembers them and, when
// there is something to tell, puts it on the message line, cut to fit like every
// other message of this package (see messageLua): the full text stays in the
// variable. A hit-enter prompt at this point would freeze the start.
func (p *Process) readUserConfig(ctx context.Context) error {
	// Only text counts. The file runs before Go is connected and can put anything
	// into any variable; a table or a number there is not a report of init.lua,
	// and decoding it into a string would end the whole start.
	var vars []string
	err := p.execLua(ctx, "user.lua result", &vars, `
		local function text(v)
			return type(v) == "string" and v or ""
		end
		return { text(vim.g.`+userErrorVar+`), text(vim.g.`+userNoteVar+`) }`)
	if err != nil {
		return err
	}
	if len(vars) != 2 {
		return nil
	}
	u := UserConfig{Error: vars[0], Note: vars[1]}
	p.mu.Lock()
	p.user = u
	p.mu.Unlock()
	if msg := u.notice(); msg != "" {
		return p.call(ctx, "show user.lua result", func(nv *nvimrpc.Nvim) error {
			return nv.ExecLua(messageLua+"\n\tshow((...))", nil, msg)
		})
	}
	return nil
}
