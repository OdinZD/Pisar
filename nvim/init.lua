-- Configuration shipped with Pisar: what the Go side relies on, and the
-- defaults for writing prose. No plugins, no network.

-- First, before anything can load a runtime plugin: Neovim's own spellfile
-- plugin (runtime/plugin/spellfile.lua, guarded by this variable) asks the user
-- whether to download a dictionary that is missing when 'spelllang' is set. Go
-- talks to Neovim over RPC and cannot answer, so that question would freeze the
-- application. The dictionary Pisar needs is shipped with it, never downloaded.
vim.g.loaded_spellfile_plugin = 1

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

-- The two bottom rows: the status line and a one-row command line. The search
-- panel of the application is laid out with exactly these two rows. Both are
-- Neovim's defaults; stated here so that a later edit of this file cannot
-- silently move them.
vim.o.laststatus = 2
vim.o.cmdheight = 1

-- Prose. A paragraph is one long line, so it has to wrap on screen:
--   wrap + linebreak: wrap between words, not inside them (the default breaks
--   at the last column that fits).
--   breakindent: a wrapped row of a list item or quote lines up under its text.
-- Line numbers are off: they count lines of the file, which for prose are
-- paragraphs. <leader>n brings them back. 'display' already has "lastline" in
-- Neovim's default, so a paragraph taller than the window is cut with "@@@"
-- instead of being replaced by "@" rows; 'list' is off by default and stays so.
-- These are window-local options. :set (vim.o) changes the global value as
-- well, which is what a window gives to a buffer it has not shown before, so
-- the toggles below carry over to documents opened later.
vim.o.wrap = true
vim.o.linebreak = true
vim.o.breakindent = true
vim.o.number = false
vim.o.relativenumber = false

-- Moving by display rows: a paragraph of ten wrapped rows would otherwise be
-- crossed in one keystroke. With a count (5j) the keys keep their meaning of
-- real lines, which is what a count is used for. Only normal and visual mode:
-- an operator (dj) must stay linewise, and gj is not (:help gj). With
-- remap = false the "j" returned for a count is the built-in one.
local function by_rows(key, rows_key)
  return function()
    return vim.v.count == 0 and rows_key or key
  end
end
for _, m in ipairs({
  { "j", "gj" },
  { "k", "gk" },
  { "<Down>", "gj" },
  { "<Up>", "gk" },
}) do
  vim.keymap.set({ "n", "x" }, m[1], by_rows(m[1], m[2]), {
    expr = true,
    silent = true,
    desc = "Pisar: move by display rows",
  })
end

-- Insert mode: <C-o> runs one command and returns to insert mode, so the mode
-- does not change. While the completion menu is open the keys must keep
-- choosing its items, hence pumvisible(). A count cannot be typed here.
local function insert_by_rows(key, rows_key)
  return function()
    if vim.fn.pumvisible() == 1 then
      return key
    end
    return "<C-o>" .. rows_key
  end
end
vim.keymap.set("i", "<Down>", insert_by_rows("<Down>", "gj"), {
  expr = true,
  silent = true,
  desc = "Pisar: move by display rows",
})
vim.keymap.set("i", "<Up>", insert_by_rows("<Up>", "gk"), {
  expr = true,
  silent = true,
  desc = "Pisar: move by display rows",
})

-- Spelling, Croatian and English together. Neovim ships only English; the
-- application writes hr.utf-8.spl into the "spell" folder of this configuration
-- (stdpath("config") is on the runtimepath, where Neovim looks for it) before
-- it starts Neovim.
--
-- The words the user adds with zg go to the file Go names in PISAR_SPELLFILE, a
-- folder the application never rewrites. It has to be set before 'spell' and
-- 'spelllang', which read it when they load the dictionaries. Without the
-- variable Neovim picks its own place (stdpath("data")); that is only so that
-- a Neovim started by hand with this file still works.
local spellfile = vim.env.PISAR_SPELLFILE
if spellfile and spellfile ~= "" then
  -- The option is a comma-separated list, so a comma in the path would split it
  -- (E474, which also ends the rest of this file); "\," is a literal comma.
  vim.o.spellfile = (spellfile:gsub(",", "\\,"))
end
-- The default pattern flags a lower-case word after "." as a missing capital.
-- In Croatian prose that is mostly wrong ("npr. riječ", "itd. sutra", "tj. što")
-- and in Markdown it hits every numbered list item ("1. kupiti"). Misspelled
-- words are still marked; only the capital-letter check is off.
vim.o.spellcapcheck = ""
-- Only languages whose dictionary file exists are asked for. Measured with
-- Neovim 0.12: when 'spelllang' names a language without a file while Neovim is
-- still starting, the core registers a VimEnter autocmd that calls
-- require("nvim.spellfile").get(), whatever g:loaded_spellfile_plugin says. That
-- asks "Download? [y/N]" (a prompt Go cannot answer, and the start never
-- finishes) and would fetch the file from the network. The application writes
-- hr.utf-8.spl before it starts Neovim, so this should not come up; if the file
-- is missing anyway, English alone is better than a frozen window.
local langs = {}
for _, lang in ipairs({ "hr", "en" }) do
  if #vim.api.nvim_get_runtime_file("spell/" .. lang .. ".utf-8.spl", false) > 0 then
    table.insert(langs, lang)
  end
end
-- A file that exists but is damaged is loaded when these are set, and Neovim
-- reports it with messages that end in a hit-enter prompt (measured with a cut
-- file). "silent!" hides them; the options are set either way and English
-- still works.
if #langs > 0 then
  vim.cmd("silent! set spelllang=" .. table.concat(langs, ","))
  vim.cmd("silent! set spell")
end

-- Switches for the two defaults above. Normal mode only, like the other
-- mappings: in insert mode a space and a letter are text. They use vim.o so the
-- global value follows (see the prose options above): a document opened later
-- gets the last choice, not the old default.
vim.keymap.set("n", "<leader>s", function()
  vim.o.spell = not vim.o.spell
end, { silent = true, desc = "Pisar: toggle spell checking" })

vim.keymap.set("n", "<leader>n", function()
  vim.o.number = not vim.o.number
end, { silent = true, desc = "Pisar: toggle line numbers" })

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
