package nvim

import (
	"context"

	"Pisar/internal/config"
)

// Colorscheme makes the running Neovim load colors/pisar.lua again and repaint
// the whole screen, for when that file has just been rewritten (another theme).
// The file always has the same name, whatever the theme, so ":colorscheme" is
// given the same name again; measured with Neovim 0.12: it sources the file
// anew each time, also when g:colors_name already holds that name, and the
// file itself sets 'background' (it clears g:colors_name first, so that this
// does not load the scheme a second time).
//
// The repaint is the one Redraw does (":mode"), because the highlight groups
// changed under text that is already drawn and Neovim would otherwise repaint
// only what it has marked as changed. Side effect, measured with Neovim 0.12:
// the text on the message line is wiped (":colorscheme" alone leaves it), as it
// is by Redraw.
//
// There is no check for a hit-enter prompt or another blocking read first, on
// purpose, unlike Format. A Lua call is not answered while Neovim waits for a
// key (measured with Neovim 0.12), but it is not lost either: it stays queued
// in Neovim and runs when the prompt is closed. So while a prompt is open this
// returns the error of ctx (context.DeadlineExceeded or Canceled) when ctx
// ends, and the colors change when the user closes the prompt. A caller that
// has the new file on disk can treat that error as "applied later", which beats
// dropping the change.
//
// A missing or broken file is an error from Neovim (E185 for a missing one);
// Neovim keeps running with the colors it had.
func (p *Process) Colorscheme(ctx context.Context) error {
	// The name is an argument and not part of the Lua text, so that no
	// quoting is needed whatever the name holds.
	return p.execLua(ctx, "colorscheme", nil, `
		vim.cmd.colorscheme((...))
		vim.cmd.mode()`, config.NvimColorscheme)
}
