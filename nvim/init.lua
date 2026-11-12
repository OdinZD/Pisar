-- Minimal configuration shipped with Pisar. Only what the Go side relies on:
-- no plugins, no network, no prose settings (those come later).
vim.g.mapleader = " "

-- The application is light. Set it explicitly instead of relying on Neovim's
-- background detection, which asks the terminal and can guess wrong.
vim.o.background = "light"

-- A modified buffer must not block opening another file: it stays loaded in
-- the background. Neovim 0.12 defaults to this (:help 'hidden'), but Go
-- depends on it, so it is stated here instead of relied upon.
vim.o.hidden = true

-- Go calls :write and :edit inside RPC calls. Their messages ("name" 3L, 20B
-- written) are cut to the message line only with these two flags: "t" for file
-- messages, "T" for other messages (:help 'shortmess'). A message that does not
-- fit opens a hit-enter prompt, which freezes Neovim inside the call. Both are
-- in Neovim's default today; stated here so that a changed default or a later
-- edit of this file cannot silently remove them.
vim.opt.shortmess:append("t")
vim.opt.shortmess:append("T")

-- The colorscheme uses true colours, and the TUI runs inside xterm.js.
vim.o.termguicolors = true

-- The colorscheme "pisar" arrives in a later step. Look for it first: a failed
-- :colorscheme would leave E185 in v:errmsg even under pcall.
if #vim.api.nvim_get_runtime_file("colors/pisar.*", false) > 0 then
  pcall(vim.cmd.colorscheme, "pisar")
end

-- The Go side stores its RPC channel id in g:pisar_channel after connecting.
-- Notifying that channel directly (instead of rpcnotify(0, ...)) needs no
-- nvim_subscribe() call, which is not part of the documented API in 0.12.
-- Until the variable is set nothing is sent, so early events cannot error.
-- The group name and method names must match internal/nvim.
local group = vim.api.nvim_create_augroup("PisarNotify", { clear = true })

local function notify(method, ...)
  local chan = vim.g.pisar_channel
  if chan then
    vim.rpcnotify(chan, method, ...)
  end
end

-- The file of a buffer; "" for an unnamed one and for anything that is not an
-- ordinary file (help, quickfix, terminal...). nvim_buf_get_name() is always
-- the absolute path Neovim itself uses.
local function path_of(buf)
  if vim.bo[buf].buftype ~= "" then
    return ""
  end
  return vim.api.nvim_buf_get_name(buf)
end

-- Go sets g:pisar_preview while the preview is on. The notification carries no
-- text (a buffer can be large): Go asks for it with BufferText when it needs it.
-- With the preview off nothing is sent, so typing costs no RPC traffic.
local function changed()
  if vim.g.pisar_preview then
    notify("pisar_changed")
  end
end

-- Last 'modified' state sent to Go per buffer (absent = unmodified). BufModifiedSet
-- does not fire when a buffer that is not current is written (:wall, or nvim_buf_call
-- from SaveAll; measured with Neovim 0.12), so BufWritePost reports the new state
-- as well; this table keeps the two sources from sending it twice.
local reported = {}
local function modified_changed(buf)
  local modified = vim.bo[buf].modified
  if (reported[buf] or false) == modified then
    return
  end
  reported[buf] = modified
  notify("pisar_modified", path_of(buf), modified)
end

-- BufWritePost runs after 'modified' was reset (checked in 0.12), so the saved
-- event comes first and the state change right after it.
vim.api.nvim_create_autocmd("BufWritePost", {
  group = group,
  callback = function(args)
    notify("pisar_saved", vim.api.nvim_buf_get_name(args.buf))
    modified_changed(args.buf)
  end,
})

-- BufEnter also fires when a window is entered or the buffer is revisited
-- without a change, so only a different buffer (or a different name for the
-- same one, which BufFilePost reports: :saveas, :file) is announced. Only the
-- current buffer counts: :saveas also fires BufEnter for the buffer that keeps
-- the old name, which the user never enters.
local current = { buf = nil, path = nil }
local function entered(buf)
  -- Go's Reload runs :edit! inside nvim_buf_call, which makes a background
  -- buffer "current" for a moment and fires BufEnter for it. The user never
  -- entered it, and announcing it would also leave `current` on the wrong
  -- buffer, hiding the next real switch. Reload sets this variable around the
  -- call and clears it again, whether or not :edit! succeeded (internal/nvim).
  if vim.g.pisar_reloading then
    return
  end
  local path = path_of(buf)
  if current.buf == buf and current.path == path then
    return
  end
  current.buf, current.path = buf, path
  notify("pisar_entered", path, vim.bo[buf].modified)
  -- The text the preview shows is that of the current buffer, so a different
  -- one is a change too.
  changed()
end

vim.api.nvim_create_autocmd({ "BufEnter", "BufFilePost" }, {
  group = group,
  callback = function(args)
    if args.buf == vim.api.nvim_get_current_buf() then
      entered(args.buf)
    end
  end,
})

-- The text of the current buffer changed: TextChanged in normal mode,
-- TextChangedI in insert mode, TextChangedP in insert mode while the completion
-- menu is open (:help TextChanged).
vim.api.nvim_create_autocmd({ "TextChanged", "TextChangedI", "TextChangedP" }, {
  group = group,
  callback = changed,
})

-- Loading the current buffer again from disk (:edit!, a reload after the file
-- changed outside) replaces its text without TextChanged (measured with
-- Neovim 0.12). Only the buffer already announced as current counts: a buffer
-- that is just being opened is announced by BufEnter, through entered().
vim.api.nvim_create_autocmd("BufReadPost", {
  group = group,
  callback = function(args)
    if args.buf == current.buf then
      changed()
    end
  end,
})

-- Fires for any buffer, not just the current one.
vim.api.nvim_create_autocmd("BufModifiedSet", {
  group = group,
  callback = function(args)
    modified_changed(args.buf)
  end,
})

vim.api.nvim_create_autocmd("BufWipeout", {
  group = group,
  callback = function(args)
    reported[args.buf] = nil
  end,
})

-- Normal mode only; in insert mode a space and "e" are just typed. The
-- mapping does nothing but notify.
vim.keymap.set("n", "<leader>e", function()
  notify("pisar_focus")
end, { silent = true, desc = "Pisar: focus the project tree" })

-- Normal mode only, like <leader>e: in insert mode these are plain text.
vim.keymap.set("n", "<leader>p", function()
  notify("pisar_preview")
end, { silent = true, desc = "Pisar: toggle the preview" })

vim.keymap.set("n", "<leader>f", function()
  notify("pisar_search")
end, { silent = true, desc = "Pisar: open the document search" })

-- Notifications on one channel are ordered, so "pisar_bye" reaches Go after
-- every event sent before it. That lets Go close its event stream without
-- losing the save that came right before `:wq`.
vim.api.nvim_create_autocmd("VimLeavePre", {
  group = group,
  callback = function()
    notify("pisar_bye")
  end,
})
