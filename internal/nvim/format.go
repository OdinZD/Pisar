package nvim

import (
	"context"
	"errors"
	"fmt"

	nvimrpc "github.com/neovim/go-client/nvim"
)

// ErrUnknownFormat is returned by Format for a kind it does not know. Nothing
// is sent to Neovim then.
var ErrUnknownFormat = errors.New("nvim: unknown format")

// formatKinds are the actions of pisar_format() in init.lua; the names must
// match. Checked here as well so that a typo from the caller is an error of its
// own and not a Lua one.
var formatKinds = map[string]bool{"bold": true, "italic": true, "heading": true, "list": true}

// Format runs one Markdown formatting action on the current buffer, as the
// mapping of the same name would in the mode Neovim is in now: kind is "bold",
// "italic", "heading" or "list" (see "Markdown formatting" in init.lua for what
// each does in normal, insert and visual mode). The whole action is one change
// for undo and does not touch registers, the search pattern or the message
// line. Marks do move: the change sets the mark '. as any change does (measured
// with Neovim 0.12: nvim_buf_set_text leaves '[ and '] alone), and a visual-mode
// action sets '< and '> on purpose, to the text it formatted, so that gv
// selects it again.
//
// It does nothing, and returns nil, when the action does not apply: Neovim is
// waiting for more input (an operator, the command line, a prompt), is in a
// mode formatting has no meaning in (replace, select, terminal), or the buffer
// cannot be changed ('nomodifiable', which is how a read-only Word document is
// open). Neither is a failure the user should be told about: the keys they
// pressed simply do not mean anything there.
//
// A Lua call is not answered while Neovim waits in a hit-enter prompt or
// another blocking read (measured with Neovim 0.12: it comes back only when a
// key arrives), so the mode is asked first with nvim_get_mode, which is
// "api-fast" and always answered; "blocking" in its reply is true in exactly
// those states. The command line and operator-pending mode are not blocking in
// that sense: init.lua sees their mode and does nothing.
func (p *Process) Format(ctx context.Context, kind string) error {
	if !formatKinds[kind] {
		return fmt.Errorf("%w: %q", ErrUnknownFormat, kind)
	}
	var failure string
	err := p.call(ctx, "format", func(nv *nvimrpc.Nvim) error {
		m, err := nv.Mode()
		if err != nil {
			return err
		}
		if m.Blocking {
			return nil
		}
		return nv.ExecLua(`return pisar_format((...))`, &failure, kind)
	})
	if err != nil {
		return err
	}
	if failure != "" {
		return fmt.Errorf("nvim: format %s: %s", kind, failure)
	}
	return nil
}
