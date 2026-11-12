package nvim

import (
	"context"
	"fmt"

	nvimrpc "github.com/neovim/go-client/nvim"
)

// SetPreview tells Neovim whether the preview is on. While it is on, Neovim
// sends EventChanged whenever the text of the current buffer changes or another
// buffer becomes current (see init.lua); while it is off nothing is sent, so
// typing costs no RPC traffic.
//
// Switching it on also queues one EventChanged from here, so the consumer's
// single "something changed, ask for the text" path gives the first render
// without waiting for the first edit. It is queued by Go rather than sent by
// Neovim because that needs no extra Lua and cannot be lost between the
// variable being set and the first autocmd. A change made at that very moment
// may give a second event, which a debouncing consumer folds into one.
func (p *Process) SetPreview(ctx context.Context, on bool) error {
	err := p.call(ctx, "set preview", func(nv *nvimrpc.Nvim) error {
		return nv.SetVar(previewVar, on)
	})
	if err != nil {
		return err
	}
	if on {
		p.emit(Event{Kind: EventChanged})
	}
	return nil
}

// BufferText returns the text of the current buffer as it is on screen (unsaved
// changes included) together with its description, both taken in one Lua call:
// Neovim serves one request at a time, so the text and the path always belong
// to the same buffer, even if the user switches buffers right after.
//
// The text is the lines joined with "\n", with a final "\n" exactly when :w
// would write one: always with the default 'fixeol', and not for a buffer that
// is 'noeol' and 'nofixeol'. An empty buffer gives "", and a buffer whose only
// line is empty gives "\n". 'fileformat' does not matter, lines never carry a
// "\r". For a buffer that is not an ordinary file (help, terminal...) the
// description is Current's (empty Path) and the text is still returned.
//
// When the text is over maxBytes the error satisfies
// errors.Is(err, ErrBufferTooLarge) and no text was transferred: the size is
// taken from nvim_buf_get_offset(), which counts the bytes of the text without
// reading it (:help nvim_buf_get_offset(); the offset after the last line is
// the size :w would write), and the lines are read only when it fits.
func (p *Process) BufferText(ctx context.Context, maxBytes int) (text []byte, buf Buffer, err error) {
	var res struct {
		Path     string
		Modified bool
		Size     int
		TooLarge bool
		Text     []byte
	}
	err = p.execLua(ctx, "buffer text", &res, bufferLua+`
		local max = ...
		local b = vim.api.nvim_get_current_buf()
		local res = { Path = pathOf(b), Modified = vim.bo[b].modified, Size = 0, TooLarge = false, Text = "" }

		-- An empty buffer still has one (empty) line, and the offset counts its
		-- end of line; line2byte(1) is -1 for it, and the file would be 0 bytes.
		if vim.fn.line2byte(1) == -1 then
			return res
		end

		local count = vim.api.nvim_buf_line_count(b)
		local size = vim.api.nvim_buf_get_offset(b, count)
		res.Size = size
		if size > max then
			res.TooLarge = true
			return res
		end

		local text = table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), "\n")
		-- The offset includes the final end of line only if :w would write it.
		if size == #text + 1 then
			text = text .. "\n"
		end
		res.Text = text
		return res`, maxBytes)
	if err != nil {
		return nil, Buffer{}, err
	}
	if res.TooLarge {
		return nil, Buffer{}, fmt.Errorf("%w: %d bytes, limit %d", ErrBufferTooLarge, res.Size, maxBytes)
	}
	return res.Text, Buffer{Path: res.Path, Modified: res.Modified}, nil
}
