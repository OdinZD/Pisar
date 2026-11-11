-- Minimal configuration shipped with Pisar. Only what the Go side relies on.
vim.g.mapleader = " "

-- The application is light. Set it explicitly instead of relying on Neovim's
-- background detection, which asks the terminal and can guess wrong.
vim.o.background = "light"

-- The Go side stores its RPC channel id in g:pisar_channel after connecting.
-- Notifying that channel directly (instead of rpcnotify(0, ...)) needs no
-- nvim_subscribe() call, which is not part of the documented API in 0.12.
-- Until the variable is set nothing is sent, so early writes cannot error.
-- The group name must match notifyGroup in internal/nvim.
local group = vim.api.nvim_create_augroup("PisarNotify", { clear = true })

vim.api.nvim_create_autocmd("BufWritePost", {
  group = group,
  callback = function(args)
    local chan = vim.g.pisar_channel
    if chan then
      -- nvim_buf_get_name() is always the absolute path Neovim itself uses.
      vim.rpcnotify(chan, "pisar_saved", vim.api.nvim_buf_get_name(args.buf))
    end
  end,
})

-- Notifications on one channel are ordered, so "pisar_bye" reaches Go after
-- every "pisar_saved" sent before it. That lets Go close its event stream
-- without losing the save that came right before `:wq`.
vim.api.nvim_create_autocmd("VimLeavePre", {
  group = group,
  callback = function()
    local chan = vim.g.pisar_channel
    if chan then
      vim.rpcnotify(chan, "pisar_bye")
    end
  end,
})
