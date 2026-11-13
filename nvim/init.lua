-- Configuration shipped with Pisar: what the Go side relies on, and the
-- defaults for writing prose. No plugins, no network.

-- First, before anything can load a runtime plugin: Neovim's own spellfile
-- plugin (runtime/plugin/spellfile.lua, guarded by this variable) asks the user
-- whether to download a dictionary that is missing when 'spelllang' is set. Go
-- talks to Neovim over RPC and cannot answer, so that question would freeze the
-- application. The dictionary Pisar needs is shipped with it, never downloaded.
vim.g.loaded_spellfile_plugin = 1
-- The same question comes through a second door, which that variable does not
-- close. While Neovim is still starting, setting 'spelllang' to a language
-- without a dictionary makes the core itself add the autocmd
--   VimEnter * call v:lua.require'nvim.spellfile'.get('xx')|set spell
-- (measured with Neovim 0.12: nvim_get_autocmds lists it). That is how a
-- 'spelllang' in the user's own file would ask "Download? [y/N]" after init.lua
-- has corrected it. The module it calls is replaced by one that does nothing;
-- the "set spell" half of the autocmd is undone where user.lua is handled.
package.loaded["nvim.spellfile"] = setmetatable({}, {
  __index = function()
    return function() end
  end,
})

vim.g.mapleader = " "

-- How long Neovim waits for the next key of a mapping. It is also the time after
-- which the which-key window below opens (<Space> alone is a mapping, and Neovim
-- fires it only once the wait is over). Set here, before user.lua, and not
-- restored after it: the user may change it.
vim.o.timeoutlen = 500

-- The texts the user may see from here (the descriptions of the mappings, the
-- message about a failed formatting action) are Croatian and live in
-- frontend/src/strings.json, the one place for the interface's text. Go hands
-- them over as JSON in PISAR_TEXT, because they are needed now, while this file
-- loads, before Go is connected. Without the variable (Neovim started by hand
-- with this file) or with JSON that does not decode, there are none: a mapping
-- has no description and a message only the facts, and everything works. There
-- is no sentence of Pisar's in this file.
local texts = {}
do
  local raw = vim.env.PISAR_TEXT
  if raw and raw ~= "" then
    local ok, decoded = pcall(vim.json.decode, raw)
    if ok and type(decoded) == "table" then
      texts = decoded
    end
  end
end

-- The text under key; nil when there is none (also for a value that is no text).
local function text(key)
  local value = texts[key]
  return type(value) == "string" and value or nil
end

-- The default before the colorscheme loads: light, set explicitly instead of
-- relying on Neovim's background detection, which asks the terminal and can
-- guess wrong. The real value (light or dark) is set by colors/pisar.lua, which
-- the application generates from the chosen theme; light stays the safe
-- fallback when there is no colorscheme.
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
    desc = text("nvimMapRows"),
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
  desc = text("nvimMapRows"),
})
vim.keymap.set("i", "<Up>", insert_by_rows("<Up>", "gk"), {
  expr = true,
  silent = true,
  desc = text("nvimMapRows"),
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
--
-- Whether a dictionary exists for a 'spelllang' entry: Neovim looks for
-- spell/<name>.<encoding>.spl, with an ASCII file as the fallback, and the
-- region ("_us" in "en_us") is not part of the file name (:help spell-load).
-- "cjk" is no language but a flag (:help 'spelllang'). A name outside the
-- characters the option documents (letters, digits, "-", "_") is refused too,
-- since it goes into a :set command.
local function has_dictionary(name)
  if name == "cjk" then
    return true
  end
  if not name:match("^[%w_%-]+$") then
    return false
  end
  local base = name:match("^(.-)_%l%l$") or name
  for _, enc in ipairs({ "utf-8", "ascii" }) do
    if #vim.api.nvim_get_runtime_file("spell/" .. base .. "." .. enc .. ".spl", false) > 0 then
      return true
    end
  end
  return false
end

local langs = {}
for _, lang in ipairs({ "hr", "en" }) do
  if has_dictionary(lang) then
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
end, { silent = true, desc = text("nvimMapSpell") })

vim.keymap.set("n", "<leader>n", function()
  vim.o.number = not vim.o.number
end, { silent = true, desc = text("nvimMapNumbers") })

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

-- Everything Go listens to. It is a function because it runs twice: here, and
-- once more after the user's own file (see "user.lua" at the end), which may
-- have removed the group or single autocmds of it. "clear = true" empties the
-- group first, so a second run leaves exactly one copy of each autocmd, never
-- two. The group name must match internal/nvim.
local function install_events()
  local group = vim.api.nvim_create_augroup("PisarNotify", { clear = true })

  -- BufWritePost runs after 'modified' was reset (checked in 0.12), so the saved
  -- event comes first and the state change right after it.
  vim.api.nvim_create_autocmd("BufWritePost", {
    group = group,
    callback = function(args)
      notify("pisar_saved", vim.api.nvim_buf_get_name(args.buf))
      modified_changed(args.buf)
    end,
  })

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

  -- Notifications on one channel are ordered, so "pisar_bye" reaches Go after
  -- every event sent before it. That lets Go close its event stream without
  -- losing the save that came right before `:wq`.
  vim.api.nvim_create_autocmd("VimLeavePre", {
    group = group,
    callback = function()
      notify("pisar_bye")
    end,
  })
end
install_events()
-- Normal mode only; in insert mode a space and "e" are just typed. The
-- mapping does nothing but notify.
vim.keymap.set("n", "<leader>e", function()
  notify("pisar_focus")
end, { silent = true, desc = text("nvimMapTree") })

-- Normal mode only, like <leader>e: in insert mode these are plain text.
vim.keymap.set("n", "<leader>p", function()
  notify("pisar_preview")
end, { silent = true, desc = text("nvimMapPreview") })

vim.keymap.set("n", "<leader>f", function()
  notify("pisar_search")
end, { silent = true, desc = text("nvimMapSearch") })

-- What Pisar puts on the message line, from here and from Go (internal/nvim
-- takes these functions as they are: messageLua, _G.pisar_msg). One copy, because
-- a message that is not cut to the line opens a hit-enter prompt (:help
-- hit-enter), and inside an RPC call that freezes Neovim until a key arrives: it
-- answers no further calls, and the call that caused it never returns. nvim_echo()
-- and emsg() do not shorten anything ('shortmess' T and t only shorten messages
-- Neovim builds itself). Measured with Neovim 0.12: an echo of N cells at 80
-- columns prompts as soon as N > 68, at 20 columns as soon as N > 8.
--
-- The usable width follows Neovim's comp_col(): the message line stops before
-- the area reserved for 'showcmd' and, when the last window has no status line,
-- for the 'ruler'. The numbers are the 10 cells of 'showcmd' and the 17 of the
-- ruler (:help 'rulerformat'; "%N(" changes it), plus one separating cell for
-- each, plus one cell that must stay free. They are not documented; they were
-- confirmed by measuring the hit-enter threshold for the combinations of
-- 'showcmd', 'ruler', 'laststatus' and 'showcmdloc' that TestWidth runs. The
-- default (showcmd on, ruler on, laststatus=2) reserves 11 cells + 1: width
-- columns - 12. 'cmdheight' > 1 would allow more; we ignore it and stay on one
-- line.
--
-- The texts themselves are Croatian and come from PISAR_TEXT (see text() at the
-- top): without them only the text of the error is shown, without Pisar's frame.
local msg = {}

function msg.width()
  local o = vim.o
  local wins = 0
  for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if vim.api.nvim_win_get_config(w).relative == "" then
      wins = wins + 1
    end
  end
  local status = o.laststatus >= 2 or (o.laststatus == 1 and wins > 1)
  local reserved = 0
  if o.ruler and not status then
    reserved = (tonumber(o.rulerformat:match("^%%%-?(%d+)%(")) or 17) + 1
  end
  if o.showcmd and o.showcmdloc == "last" then
    reserved = reserved + 10 + ((not o.ruler or status) and 1 or 0)
  end
  return math.max(o.columns - reserved - 1, 1)
end

-- Cuts text to at most max screen cells, ending in "…" when it had to cut.
-- Cells, not bytes or characters: č is two bytes, an emoji or CJK character is
-- two cells, and the cut never goes through a character. The cut is a binary
-- search because the width of a prefix never shrinks as the prefix grows.
function msg.fit(text, max)
  text = text:gsub("%s*[\r\n]+%s*", " "):gsub("\t", " ")
  if vim.fn.strdisplaywidth(text) <= max then
    return text
  end
  local ell = "…"
  local room = max - vim.fn.strdisplaywidth(ell)
  if room < 0 then
    ell, room = "", max
  end
  local lo, hi = 0, vim.fn.strchars(text)
  while lo < hi do
    local mid = math.ceil((lo + hi) / 2)
    if vim.fn.strdisplaywidth(vim.fn.strcharpart(text, 0, mid)) <= room then
      lo = mid
    else
      hi = mid - 1
    end
  end
  return vim.fn.strcharpart(text, 0, lo) .. ell
end

-- "Vim(write):" and "Vim:" are how Lua prefixes errors raised by commands.
local function plain(text)
  return (tostring(text):gsub("^Vim%(?%w*%)?:", ""))
end

-- Puts a message on the message line, cut to fit, and redraws. Only the screen
-- copy is cut; callers hand the full text to Go. The ErrorMsg echo (not err =
-- true) and the redraw also matter in insert and visual mode: Neovim otherwise
-- blocks the main loop, ignoring keys, to keep a message on screen (1 s for an
-- error, 3 s for the "written" message of a successful :write; measured with
-- Neovim 0.12).
function msg.show(text)
  vim.api.nvim_echo({ { msg.fit(plain(text), msg.width()), "ErrorMsg" } }, true, {})
  vim.cmd.redraw()
end

-- The template with every "{name}" replaced by value, in one pass: a value that
-- contains "{name}" itself is left as it is (the same rule as frame() in
-- internal/nvim). Without a template (Go has not given the texts) it is the
-- value alone.
function msg.framed(template, name, value)
  value = plain(value)
  if type(template) ~= "string" or template == "" then
    return value
  end
  -- A function as the replacement: its result is taken literally, "%" and all.
  return (template:gsub("{" .. name .. "}", function()
    return value
  end))
end
_G.pisar_msg = msg

-- Markdown formatting: bold, italic, heading, list.
--
-- Each action is a function that works in every mode the user can be typing in,
-- because two inputs reach it: the mappings below (normal and visual mode) and
-- Go, which calls pisar_format() when the application menu is used while the
-- user is typing in insert mode (internal/nvim, Process.Format).
--
-- Text is changed with nvim_buf_set_text(), never with normal-mode commands:
-- the API touches no register, no search pattern and no message, and counts
-- bytes, so a two-byte "č" cannot be cut in half. All of the changes of one
-- action are made inside one call, which Neovim keeps in one undo block.
local api = vim.api

local format_ns = api.nvim_create_namespace("pisar_format")

local MARKERS = { bold = "**", italic = "*" }

local function get_line(row)
  return api.nvim_buf_get_lines(0, row, row + 1, true)[1]
end

local function set_text(row, from, to, text)
  api.nvim_buf_set_text(0, row, from, row, to, { text })
end

-- The mode as far as formatting is concerned: "normal", "insert", "v", "V" or
-- "block". Everything else is nil and the action does nothing: operator-pending
-- ("no"), the command line ("c"), a prompt ("r..."), select mode, replace mode,
-- the completion sub-modes of insert mode ("ic", "ix") and the terminal. "ni..."
-- is normal mode entered from insert mode with CTRL-O.
local function format_mode()
  local mode = api.nvim_get_mode().mode
  if mode == "n" or mode:sub(1, 2) == "ni" then
    return "normal"
  elseif mode == "i" then
    return "insert"
  elseif mode == "v" or mode == "V" then
    return mode
  elseif mode == "\22" then
    return "block"
  end
  return nil
end

-- A word is a run of letters, digits and combining marks. charclass() 2 is the
-- class of Vim's word characters, which follows 'iskeyword': the letters of the
-- scripts that put spaces between words (Latin, Cyrillic, Greek, Hebrew, Arabic,
-- Thai...), digits and "_". Han, kana and Hangul have a class of their own each,
-- so a word in them is not found and "x 漢字 y" is left as it is. "_" is left out
-- of the class here: it is Markdown's own emphasis mark. Emoji and punctuation
-- are other classes, so "riječ," and "🙂riječ" end where the word ends.
local function is_word_char(ch)
  return ch ~= "_" and vim.fn.charclass(ch) == 2
end

-- An apostrophe, straight or typographic. Between two word characters it is part
-- of the word ("don't", "l’homme"); at the edge of a word it is a quotation mark
-- ("'riječ'") and is not.
local function is_apostrophe(ch)
  return ch == "'" or ch == "’"
end

-- Returns the byte range [from, to) of the word at byte column col, or nil when
-- the cursor is not on a word character.
local function word_at(line, col)
  local starts = vim.str_utf_pos(line)
  local n = #starts
  local function stop(i)
    return (starts[i + 1] or #line + 1) - 1
  end
  local function piece(i)
    return line:sub(starts[i], stop(i))
  end
  local function in_word(i)
    local ch = piece(i)
    if is_word_char(ch) then
      return true
    end
    return is_apostrophe(ch) and i > 1 and i < n and is_word_char(piece(i - 1)) and is_word_char(piece(i + 1))
  end
  local at
  for i = 1, n do
    if starts[i] - 1 <= col and col < stop(i) then
      at = i
      break
    end
  end
  if not at or not in_word(at) then
    return nil
  end
  local first, last = at, at
  while first > 1 and in_word(first - 1) do
    first = first - 1
  end
  while last < n and in_word(last + 1) do
    last = last + 1
  end
  return starts[first] - 1, stop(last)
end

-- How many "*" touch the range [sc, ec) of a line (or, for a range over several
-- lines, from (first line, sc) to (last line, ec)): a, b inside it at its two
-- ends, L, R just outside. A run of stars is one marker, whichever side of the
-- range's edge it is on, so n = min(left run, right run) is the number of stars
-- around the text: 1 is italic, 2 is bold, 3 is both. That is what keeps the "*"
-- of a "**" from being taken for an italic mark.
local function star_runs(first, sc, last, ec, same)
  local inside = same and first:sub(sc + 1, ec)
  local a = #(inside or first:sub(sc + 1)):match("^%**")
  local b = #(inside or last:sub(1, ec)):match("%**$")
  local left = #first:sub(1, sc):match("%**$")
  local right = #last:sub(ec + 1):match("^%**")
  return {
    a = a,
    b = b,
    L = left,
    R = right,
    n = math.min(left + a, b + right),
    -- A range made only of stars has no text to be wrapped.
    text = not same or ec - sc - a - b > 0,
  }
end

local function is_wrapped(kind, runs)
  if not runs.text then
    return false
  end
  if kind == "bold" then
    return runs.n >= 2
  end
  return runs.n % 2 == 1
end

-- Wraps the range or, when it is already wrapped in this kind of mark, unwraps
-- it. Returns the range as it is afterwards: start column on the first row, end
-- column (exclusive) on the last row. The end edit is made first so that on one
-- line the start edit does not move it.
local function toggle_marks(kind, sr, sc, er, ec)
  local mark = MARKERS[kind]
  local k = #mark
  local same = sr == er
  local runs = star_runs(get_line(sr), sc, get_line(er), ec, same)
  if is_wrapped(kind, runs) then
    set_text(er, ec + runs.R - k, ec + runs.R, "")
    set_text(sr, sc - runs.L, sc - runs.L + k, "")
    return sc - runs.L, ec + runs.R - k - (same and k or 0)
  end
  set_text(er, ec, ec, mark)
  set_text(sr, sc, sc, mark)
  -- The closing mark goes in front of the stars that were already there (a run
  -- is one thing), so the end of the processed text is after those.
  return sc, ec + runs.R + (same and 2 * k or k)
end

-- The pairs of "*" marks of a line, for a word that is inside a span that is
-- bold or italic without the marks touching it ("**one word**" with the cursor
-- on "word"). Runs of "*" are matched like brackets: a run closes the open one
-- before it when they are equally long and the text in front of it is not white
-- space, and opens one when the text after it is not white space. Runs with
-- white space on both sides ("2 * 3") and the bullet of a list item are plain
-- text. A run that opens nothing and closes nothing, or is never closed, means
-- the line is not understood: no spans at all, and the word is wrapped as if
-- there were none. Columns are bytes: os and cs are where the opening and the
-- closing run start, oe and ce where they end (exclusive), n is their length.
local function star_spans(line)
  local indent = #line:match("^[ \t]*")
  local spans, open = {}, {}
  local pos = 1
  while true do
    local s, e = line:find("%*+", pos)
    if not s then
      break
    end
    pos = e + 1
    local before, after = line:sub(s - 1, s - 1), line:sub(e + 1, e + 1)
    local bullet = e == s and s == indent + 1 and (after == "" or after:match("%s") ~= nil)
    local can_close = before ~= "" and not before:match("%s")
    local can_open = after ~= "" and not after:match("%s")
    if not bullet and (can_close or can_open) then
      local top = open[#open]
      if can_close and top and top.len == e - s + 1 then
        open[#open] = nil
        spans[#spans + 1] = { os = top.s - 1, oe = top.e, cs = s - 1, ce = e, n = e - s + 1 }
      elseif can_open then
        open[#open + 1] = { s = s, e = e, len = e - s + 1 }
      else
        return {}
      end
    end
  end
  if #open > 0 then
    return {}
  end
  return spans
end

-- The innermost span of this kind (bold: two stars or more, italic: an odd
-- number) that has the range [from, to) of the line inside it.
local function enclosing_span(kind, line, from, to)
  local best
  for _, sp in ipairs(star_spans(line)) do
    local applies = kind == "bold" and sp.n >= 2 or kind == "italic" and sp.n % 2 == 1
    if applies and sp.oe <= from and to <= sp.cs and (not best or sp.os > best.os) then
      best = sp
    end
  end
  return best
end

-- Takes one mark of this kind off each end of the span. From "***a b***" bold
-- leaves "*a b*" and italic "**a b**". The end goes first, so that the start
-- does not move it.
local function unwrap_span(kind, row, span)
  local k = #MARKERS[kind]
  set_text(row, span.ce - k, span.ce, "")
  set_text(row, span.os, span.os + k, "")
end

-- Start byte of the character that ends at byte offset stop (0 for none). The
-- pattern is the same one Neovim's cursor uses for a character, so it takes a
-- base letter with its combining marks as one.
local function char_before(line, stop)
  if stop <= 0 then
    return 0
  end
  local at = vim.fn.match(line, [[.\%]] .. (stop + 1) .. "c")
  return at < 0 and 0 or at
end

-- Byte offset after the character that starts at byte col (#line for none).
local function char_after(line, col)
  if col >= #line then
    return #line
  end
  local ch = vim.fn.matchstr(line, [[\%]] .. (col + 1) .. [[c.]])
  return math.min(col + math.max(#ch, 1), #line)
end

local function set_cursor(row, col)
  api.nvim_win_set_cursor(0, { row + 1, math.min(col, #get_line(row)) })
end

-- Leaves visual mode as <Esc> does, which also sets '< and '>.
local function leave_visual()
  vim.cmd.normal({ args = { "\27" }, bang = true })
end

-- The selection in order: first row and byte column, last row and byte column
-- of the later end, and whether that end is left out of the selection. It is
-- with 'selection' "exclusive" (:help 'selection'); "inclusive" and "old" both
-- include the character there ("old" only differs in how far the cursor may go
-- past the end of a line). One character selected is the exception: Neovim
-- treats it as inclusive then, as there is no empty selection.
local function selection()
  local a, b = vim.fn.getpos("v"), vim.fn.getpos(".")
  local single = a[2] == b[2] and a[3] == b[3]
  if a[2] > b[2] or (a[2] == b[2] and a[3] > b[3]) then
    a, b = b, a
  end
  return a[2] - 1, a[3] - 1, b[2] - 1, b[3] - 1, vim.o.selection == "exclusive" and not single
end

-- Marks must hug the text: "** word **" is not bold in Markdown. Moves the
-- range's edges in over spaces and tabs (and over empty lines at either end);
-- nil when nothing but white space is left.
local function trim_range(sr, sc, er, ec)
  while true do
    local line = get_line(sr)
    sc = sc + #line:match("^[ \t]*", sc + 1)
    if sc < (sr == er and ec or #line) then
      break
    end
    if sr == er then
      return nil
    end
    sr, sc = sr + 1, 0
  end
  while true do
    local line = get_line(er)
    local low = er == sr and sc or 0
    ec = ec - #line:sub(low + 1, ec):match("[ \t]*$")
    if ec > low then
      break
    end
    er = er - 1
    ec = #get_line(er)
  end
  return sr, sc, er, ec
end

-- Insert mode: the pair "**" "**" at the cursor, with the cursor between. The
-- closing mark gets an extmark, so that a second call, with the cursor right in
-- front of that mark, steps over it instead of inserting another pair: bold,
-- text, bold gives bold text and the typing goes on after it. The extmark has
-- right gravity: text typed at the cursor, in front of the mark, pushes it on.
-- The text is checked as well, so a mark the user deleted is not stepped over.
local open_pairs = {} -- buffer -> kind -> extmark id

local function insert_pair(kind)
  local mark = MARKERS[kind]
  local buf = api.nvim_get_current_buf()
  local pos = api.nvim_win_get_cursor(0)
  local row, col = pos[1] - 1, pos[2]
  local open = open_pairs[buf] or {}
  open_pairs[buf] = open

  local id = open[kind]
  if id then
    local at = api.nvim_buf_get_extmark_by_id(buf, format_ns, id, {})
    if at[1] == row and at[2] == col and get_line(row):sub(col + 1, col + #mark) == mark then
      api.nvim_buf_del_extmark(buf, format_ns, id)
      open[kind] = nil
      set_cursor(row, col + #mark)
      return
    end
    api.nvim_buf_del_extmark(buf, format_ns, id)
    open[kind] = nil
  end

  set_text(row, col, col, mark .. mark)
  open[kind] = api.nvim_buf_set_extmark(buf, format_ns, row, col + #mark, { right_gravity = true })
  set_cursor(row, col + #mark)
end

-- The pairs belong to one stay in insert mode. A function, like install_events,
-- because it is run again after the user's file.
local function install_format_events()
  local format_group = api.nvim_create_augroup("PisarFormat", { clear = true })
  api.nvim_create_autocmd("InsertLeave", {
    group = format_group,
    callback = function(args)
      local open = open_pairs[args.buf]
      open_pairs[args.buf] = nil
      for _, id in pairs(open or {}) do
        pcall(api.nvim_buf_del_extmark, args.buf, format_ns, id)
      end
    end,
  })
  api.nvim_create_autocmd("BufWipeout", {
    group = format_group,
    callback = function(args)
      open_pairs[args.buf] = nil
    end,
  })
end
install_format_events()

-- Bold and italic. Visual mode: "v" wraps the selection (or unwraps it), "V"
-- every line of it that has text, one by one, because emphasis does not go over
-- an empty line. A block selection is refused (nothing changes, visual mode
-- stays): its columns would have to be wrapped line by line, which is not what
-- anybody selecting a block of prose means.
local function emphasis(kind, mode)
  if mode == "insert" then
    return insert_pair(kind)
  end

  if mode == "normal" then
    local pos = api.nvim_win_get_cursor(0)
    local row, col = pos[1] - 1, pos[2]
    local from, to = word_at(get_line(row), col)
    if not from then
      return
    end
    local line = get_line(row)
    local runs = star_runs(line, from, line, to, true)
    local shift = #MARKERS[kind]
    if is_wrapped(kind, runs) then
      shift = -shift
    else
      -- The marks are not at the word, but the word may be inside a span that
      -- has them further away: that span is what the action takes off.
      local span = enclosing_span(kind, line, from, to)
      if span then
        unwrap_span(kind, row, span)
        set_cursor(row, col - shift)
        return
      end
    end
    toggle_marks(kind, row, from, row, to)
    set_cursor(row, col + shift)
    return
  end

  if mode == "block" then
    return
  end

  local sr, sc, er, ec, exclusive = selection()
  leave_visual()

  if mode == "V" then
    local rows = {}
    for row = sr, er do
      local line = get_line(row)
      local from = line:find("%S")
      if from then
        from = from - 1
        local to = #line - #line:match("%s*$")
        rows[#rows + 1] = {
          row = row,
          from = from,
          to = to,
          wrapped = is_wrapped(kind, star_runs(line, from, line, to, true)),
        }
      end
    end
    local all = #rows > 0
    for _, r in ipairs(rows) do
      all = all and r.wrapped
    end
    local last
    for _, r in ipairs(rows) do
      local to = r.to
      -- When some lines are wrapped already and others not, the wrapped ones stay
      -- as they are; when all are, all are unwrapped.
      if all or not r.wrapped then
        local _, new_to = toggle_marks(kind, r.row, r.from, r.row, r.to)
        to = new_to
      end
      last = { row = r.row, to = to }
    end
    if last then
      set_cursor(last.row, char_before(get_line(last.row), last.to))
    end
    return
  end

  -- "v": the end of the range is after the last character of the selection; with
  -- an exclusive selection that is where the later end starts.
  local last_line = get_line(er)
  if exclusive then
    ec = math.min(ec, #last_line)
  else
    ec = char_after(last_line, math.min(ec, #last_line))
  end
  sc = math.min(sc, #get_line(sr))
  sr, sc, er, ec = trim_range(sr, sc, er, ec)
  if not sr then
    return
  end
  local new_sc, new_ec = toggle_marks(kind, sr, sc, er, ec)
  local line = get_line(er)
  local at = char_before(line, new_ec)
  set_cursor(er, at)
  -- '< and '> follow the text, so that gv selects what was just formatted.
  vim.fn.setpos("'<", { 0, sr + 1, new_sc + 1, 0 })
  vim.fn.setpos("'>", { 0, er + 1, at + 1, 0 })
end

-- Heading. The marks of the line are taken apart: indent, then a heading mark
-- ("#" to "######" and its white space) or a bullet ("-", "*" or "+" and its
-- white space), then the text. A heading replaces a bullet, because "# " after
-- "- " is not a heading in the document's outline but a heading inside a list
-- item; a numbered item keeps its number ("# 1. Intro"), since the number may
-- be part of the title.
local function parts(line)
  local indent = line:match("^[ \t]*")
  return #indent, line:sub(#indent + 1)
end

local function heading_mark(rest)
  local hashes = rest:match("^(#+)[ \t]") or rest:match("^(#+)$")
  if hashes and #hashes <= 6 then
    return #hashes, #rest:match("^#+[ \t]*")
  end
  return nil
end

local function bullet_mark(rest)
  local mark = rest:match("^[-*+][ \t]+") or rest:match("^[-*+]$")
  return mark and #mark
end

local function numbered(rest)
  return rest:match("^%d+[.)][ \t]") ~= nil or rest:match("^%d+[.)]$") ~= nil
end

-- What to put in place of `old` bytes at `at`, as (at, old, new text).
local function heading_edit(line)
  local at, rest = parts(line)
  local level, len = heading_mark(rest)
  if level then
    if level >= 3 then
      return at, len, ""
    end
    return at, len, string.rep("#", level + 1) .. " "
  end
  return at, bullet_mark(rest) or 0, "# "
end

local function list_edit(line)
  local at, rest = parts(line)
  if rest == "" or heading_mark(rest) or numbered(rest) then
    return nil
  end
  local len = bullet_mark(rest)
  if len then
    return at, len, ""
  end
  return at, 0, "- "
end

-- The cursor stays on the same character: after the replaced bytes it moves by
-- the change in length; inside them it goes to the start of the text.
local function moved_col(col, at, old, new)
  if col < at then
    return col
  elseif col < at + old then
    return at + #new
  end
  return col + #new - old
end

-- Rows the line actions work on: the cursor's row, or every row of the
-- selection (which ends visual mode first). Returns the rows and the cursor.
local function line_rows(mode)
  if mode == "normal" or mode == "insert" then
    local pos = api.nvim_win_get_cursor(0)
    return { pos[1] - 1 }, pos[1] - 1, pos[2]
  end
  local sr, _, er = selection()
  local cursor = vim.fn.getpos(".")
  leave_visual()
  local rows = {}
  for row = sr, er do
    rows[#rows + 1] = row
  end
  return rows, cursor[2] - 1, cursor[3] - 1
end

local function apply_edits(edits, cursor_row, cursor_col)
  local col = cursor_col
  for _, e in ipairs(edits) do
    set_text(e.row, e.at, e.at + e.old, e.new)
    if e.row == cursor_row then
      col = moved_col(cursor_col, e.at, e.old, e.new)
    end
  end
  set_cursor(cursor_row, col)
end

local function heading(mode)
  local rows, crow, ccol = line_rows(mode)
  local edits = {}
  for _, row in ipairs(rows) do
    local line = get_line(row)
    -- Several lines: the empty ones between paragraphs are not headings. A
    -- single line, empty or not, is the one the user asked for.
    if #rows == 1 or line:find("%S") then
      local at, old, new = heading_edit(line)
      edits[#edits + 1] = { row = row, at = at, old = old, new = new }
    end
  end
  apply_edits(edits, crow, ccol)
end

-- List. Over several lines it is one decision: when every line that can be an
-- item already is a bullet the bullets come off, otherwise the lines that have
-- none get one. Empty lines, headings and numbered items are left alone.
local function list(mode)
  local rows, crow, ccol = line_rows(mode)
  local items, bullets = {}, 0
  for _, row in ipairs(rows) do
    local at, old, new = list_edit(get_line(row))
    if at then
      items[#items + 1] = { row = row, at = at, old = old, new = new }
      if old > 0 then
        bullets = bullets + 1
      end
    end
  end
  local edits = {}
  for _, e in ipairs(items) do
    if #items == bullets or e.old == 0 then
      edits[#edits + 1] = e
    end
  end
  apply_edits(edits, crow, ccol)
end

local actions = {
  bold = function(mode)
    emphasis("bold", mode)
  end,
  italic = function(mode)
    emphasis("italic", mode)
  end,
  heading = heading,
  list = list,
}

-- The one entry point, for the mappings and for Go. Returns "" when the action
-- was done or does not apply (a mode that waits for input, a buffer that cannot
-- be changed: both are normal, not failures), and the text of the error when
-- something went wrong, so that Go can return it; it never raises one.
local function pisar_format(kind)
  local action = actions[kind]
  if not action then
    return "unknown format: " .. tostring(kind)
  end
  local mode = format_mode()
  if not mode or not vim.bo.modifiable then
    return ""
  end
  local ok, err = pcall(action, mode)
  if not ok then
    return tostring(err)
  end
  return ""
end
-- Go calls it by this global name; it is set again after the user's file.
_G.pisar_format = pisar_format

-- Normal and visual mode only: in insert mode a space and a letter are text.
-- A failure is shown cut to the message line (msg.show), so that it cannot open
-- a hit-enter prompt.
local map_desc = { bold = "nvimMapBold", italic = "nvimMapItalic", heading = "nvimMapHeading", list = "nvimMapList" }
for key, kind in pairs({ b = "bold", i = "italic", h = "heading", l = "list" }) do
  vim.keymap.set({ "n", "x" }, "<leader>" .. key, function()
    local err = pisar_format(kind)
    if err ~= "" then
      msg.show(msg.framed(text("nvimFormatFailed"), "reason", err))
    end
  end, { silent = true, desc = text(map_desc[kind]) })
end

-- Which-key. After <Space> (the leader) and a wait of 'timeoutlen' a small
-- window without focus lists what can follow; the next key runs the mapping,
-- goes into a group or closes the window. No plugin: the window is a float with
-- a scratch buffer, and the keys are caught with vim.on_key().
--
-- Measured with Neovim 0.12.6, and the reason for the shape of this code:
--   * Why not getcharstr(): it blocks Neovim, so nvim_get_mode() says
--     "blocking" and Go's checks of the mode (Format, Save) would refuse. Why
--     not feeding the leader again: it would open the window again, for ever.
--   * <Space> alone is mapped, and longer mappings start with it, so Neovim
--     fires the bare mapping only after 'timeoutlen'. A quick "<Space>f" is the
--     mapping <leader>f, and never sees the window.
--   * The mapped keys come as several keys: j is mapped to gj, and on_key gets
--     key = g, typed = j, then key = j, typed = "" (not typed: the rest of the
--     same mapping). A catcher taken off after the first would let the second
--     run, and the cursor would move. That is the state "drain" below.
-- It is a function body in a block so that the helpers are not locals of the
-- whole file (Lua allows 200 of them).
local install_whichkey
do
  local PREFIX = " " -- <Space>
  local GROUP = "PisarWhichKey"

  local wk = {
    hl_ns = api.nvim_create_namespace("pisar_whichkey_hl"),
    key_ns = api.nvim_create_namespace("pisar_whichkey_keys"),
    state = nil, -- nil, "open" or "drain"
    drain = nil, -- a table that identifies the drain a scheduled call belongs to
    win = nil,
    group = nil, -- id of the autocmd group
    mode = "n", -- "n" or "x"
    count = 0, -- v:count when the window was opened
    prefix = PREFIX, -- raw keys typed so far
    path = "", -- the same without the leader, in keytrans form
    index = {}, -- keytrans of the next key -> its entry
  }

  -- One key per element of a raw string (lhsraw, typed): a key is not a byte.
  -- K_SPECIAL (0x80) starts a special key of three bytes (<Down> is 80 6b 64), and
  -- 80 fc <bits> is a modifier that belongs to the key after it (<C-g>, <M-x>).
  -- The rest is UTF-8, so č is one key of two bytes.
  local function split_keys(raw)
    local keys, i, n = {}, 1, #raw
    while i <= n do
      local from = i
      while raw:byte(i) == 0x80 and raw:byte(i + 1) == 0xfc and i + 3 <= n do
        i = i + 3
      end
      local b = raw:byte(i)
      local len = 1
      if b == 0x80 then
        len = 3
      elseif b >= 0xf0 then
        len = 4
      elseif b >= 0xe0 then
        len = 3
      elseif b >= 0xc0 then
        len = 2
      end
      i = math.min(i + len, n + 1)
      keys[#keys + 1] = raw:sub(from, i - 1)
    end
    return keys
  end

  -- The mappings of a mode: those of the buffer first, and a global one with the
  -- same keys is shadowed by them.
  local function mode_maps(mode)
    local seen, out = {}, {}
    for _, list in ipairs({ api.nvim_buf_get_keymap(0, mode), api.nvim_get_keymap(mode) }) do
      for _, m in ipairs(list) do
        if not seen[m.lhsraw] then
          seen[m.lhsraw] = true
          out[#out + 1] = m
        end
      end
    end
    return out
  end

  local function has_desc(m)
    return type(m.desc) == "string" and m.desc ~= ""
  end

  -- The name vim.g.pisar_groups gives to the group at path (the keys after the
  -- leader, as keytrans writes them: "g", "gh"); nil when it has none.
  local function group_name(path)
    local names = vim.g.pisar_groups
    local name = type(names) == "table" and names[path]
    return type(name) == "string" and name ~= "" and name or nil
  end

  -- What can follow the keys typed so far. One entry per next key:
  --   key      keytrans form of it ("g", "<Down>", "<lt>")
  --   raw      the key as the mappings spell it
  --   deeper   the mappings that go on after it
  --   exact    the mapping that ends with it, if there is one
  --   group    there are mappings that go on: it leads into a group. A key that
  --            is both (<Space>g and <Space>gs) counts as a group; the short
  --            mapping stays reachable by typing fast, without the window.
  --   shown    it is in the window: only mappings with a description are, and a
  --            group that has a name or a described mapping under it. Moving and
  --            running work for every mapping, described or not.
  local function continuations()
    local by_key, list = {}, {}
    for _, m in ipairs(mode_maps(wk.mode)) do
      local raw = m.lhsraw
      if raw and #raw > #wk.prefix and raw:sub(1, #wk.prefix) == wk.prefix then
        local rest = split_keys(raw:sub(#wk.prefix + 1))
        local key = vim.fn.keytrans(rest[1])
        local e = by_key[key]
        if not e then
          e = { key = key, raw = rest[1], deeper = {}, order = #list + 1 }
          by_key[key] = e
          list[#list + 1] = e
        end
        if #rest == 1 then
          e.exact = m
        else
          table.insert(e.deeper, m)
        end
      end
    end
    for _, e in ipairs(list) do
      e.group = #e.deeper > 0
      if e.group then
        local name = group_name(wk.path .. e.key)
        local described = false
        for _, m in ipairs(e.deeper) do
          described = described or has_desc(m)
        end
        e.label = "+" .. (name or #e.deeper)
        e.shown = name ~= nil or described
      else
        e.label = e.exact.desc
        e.shown = has_desc(e.exact)
      end
    end
    -- By the written key, upper and lower case together; the rest of the order
    -- is the one the mappings came in, so it does not change between calls.
    table.sort(list, function(a, b)
      local x, y = a.key:lower(), b.key:lower()
      if x ~= y then
        return x < y
      end
      if a.key ~= b.key then
        return a.key < b.key
      end
      return a.order < b.order
    end)
    return list, by_key
  end

  local function close_window()
    if wk.win and api.nvim_win_is_valid(wk.win) then
      api.nvim_win_close(wk.win, true)
    end
    wk.win = nil
  end

  local function clear_autocmds()
    if wk.group then
      -- The group may be gone already: user code can delete any group.
      pcall(api.nvim_del_augroup_by_id, wk.group)
      wk.group = nil
    end
  end

  local function remove_hook()
    vim.on_key(nil, wk.key_ns)
  end

  -- Everything back to nothing: no window, no autocmds, no catcher.
  local function teardown()
    close_window()
    clear_autocmds()
    remove_hook()
    wk.state, wk.drain = nil, nil
  end

  -- Shows the window for the keys typed so far. Returns false, with no window
  -- left, when there is nothing to show or no room for it.
  local function render()
    close_window()
    local list, by_key = continuations()
    wk.index = by_key
    local shown = vim.tbl_filter(function(e)
      return e.shown
    end, list)
    if #shown == 0 then
      return false
    end

    -- The window sits in the bottom right corner, above the status line. With a
    -- border the corner keeps one row and one column of margin (measured in the
    -- terminal with 24 rows and 80 columns: row 22 puts the bottom border on
    -- screen row 21, with the status line on row 23, and the right border on
    -- column 79). So the bottom border is on the row above `row`, the text has
    -- the rows above that border minus the top border, and a border takes two
    -- columns.
    local row = vim.o.lines - vim.o.cmdheight - 1
    local max_w, max_h = vim.o.columns - 2, row - 3
    if max_h < 1 or max_w < 10 or (#shown > max_h and max_h < 3) then
      return false
    end

    local key_w = 0
    for _, e in ipairs(shown) do
      key_w = math.max(key_w, vim.fn.strdisplaywidth(e.key))
    end
    local title
    if wk.path == "" then
      title = text("nvimKeysTitle")
    else
      title = group_name(wk.path) or wk.path
    end
    local rows = {}
    local width = title and vim.fn.strdisplaywidth(title) or 0
    for _, e in ipairs(shown) do
      local pad = string.rep(" ", key_w - vim.fn.strdisplaywidth(e.key))
      rows[#rows + 1] = { e = e, line = e.key .. pad .. "  " .. e.label, from = #e.key + #pad + 2 }
      width = math.max(width, vim.fn.strdisplaywidth(rows[#rows].line))
    end
    width = math.min(width, max_w)
    local more = #rows > max_h
    if more then
      rows = vim.list_slice(rows, 1, max_h - 1)
    end

    local lines = {}
    for i, r in ipairs(rows) do
      lines[i] = msg.fit(r.line, width)
    end
    if more then
      lines[#lines + 1] = "…"
    end

    local buf = api.nvim_create_buf(false, true)
    vim.bo[buf].bufhidden = "wipe"
    api.nvim_buf_set_lines(buf, 0, -1, false, lines)
    vim.bo[buf].modified = false
    vim.bo[buf].modifiable = false
    for i, r in ipairs(rows) do
      -- strict = false: a line that was cut can be shorter than its columns.
      api.nvim_buf_set_extmark(buf, wk.hl_ns, i - 1, 0, {
        end_col = #r.e.key,
        hl_group = "PisarKey",
        strict = false,
      })
      if r.e.group then
        api.nvim_buf_set_extmark(buf, wk.hl_ns, i - 1, r.from, {
          end_col = #lines[i],
          hl_group = "PisarKeyGroup",
          strict = false,
        })
      end
    end

    wk.win = api.nvim_open_win(buf, false, {
      relative = "editor",
      anchor = "SE",
      row = row,
      col = vim.o.columns,
      style = "minimal",
      border = "single",
      focusable = false,
      noautocmd = true,
      width = width,
      height = #lines,
      title = title,
    })
    return true
  end

  -- A failure of ours shows as one line on the message line (a message of
  -- several lines would open a hit-enter prompt) and leaves nothing behind.
  local function fail(err)
    teardown()
    local text_of_err = tostring(err)
    if not pcall(msg.show, text_of_err) then
      vim.schedule(function()
        msg.show(text_of_err)
      end)
    end
  end

  -- Closes the window, and what was typed is done as if it had been typed
  -- without the window: feed is that, or nil to do nothing. The keys are put in
  -- at once, not scheduled: the next key the user types must come after them.
  --
  -- The catcher stays for a moment as "drain". The key that closed the window is
  -- swallowed (on_key returns ""), but a mapped key is several keys, and the rest
  -- of them (typed = "") are swallowed too. Keys that are put in with feedkeys()
  -- come with typed ~= "", and the first of them ends the drain. The scheduled
  -- call is the safety net: it runs when what the mapping produced is used up,
  -- and takes the catcher off if no key came, which would otherwise swallow the
  -- next <Esc> (Go's pisar_format sends one to leave visual mode).
  local function finish(feed)
    close_window()
    clear_autocmds()
    local token = {}
    wk.state, wk.drain = "drain", token
    if feed then
      api.nvim_feedkeys(feed, "mt", false)
    end
    vim.schedule(function()
      if wk.drain == token then
        remove_hook()
        wk.state, wk.drain = nil, nil
      end
    end)
  end

  local function feed_with(typed)
    return (wk.count > 0 and tostring(wk.count) or "") .. wk.prefix .. typed
  end

  -- What the catcher does with a key. Returns "" to swallow it, nil to let it by.
  local function on_key(_, typed)
    if wk.state == "drain" then
      if typed == "" then
        return ""
      end
      remove_hook()
      wk.state, wk.drain = nil, nil
      return nil
    end
    if wk.state ~= "open" then
      return nil
    end
    if typed == "" then
      return nil -- not typed: it is not the user's answer
    end
    local keys = split_keys(typed)
    local e = #keys == 1 and wk.index[vim.fn.keytrans(typed)] or nil
    if not e then
      finish(nil) -- <Esc>, <Space>, an unknown key, a click, several keys
      return ""
    end
    if not e.group then
      finish(feed_with(typed))
      return ""
    end
    wk.prefix, wk.path = wk.prefix .. e.raw, wk.path .. e.key
    -- Nothing to show in this group, or no room: the catcher stays, without a
    -- window, and the next key is handled as it would be with one. The keys are
    -- never put back: the bare <Space> would fire again after 'timeoutlen' and
    -- open the window again, the group key would lead here again, and so on for
    -- ever (measured: Neovim stops answering). render() has already taken in the
    -- continuations, so the lookup above finds them.
    render()
    return ""
  end

  local function open(mode)
    teardown()
    wk.mode, wk.count, wk.prefix, wk.path = mode, vim.v.count, PREFIX, ""
    if not render() then
      return
    end
    wk.group = api.nvim_create_augroup(GROUP, { clear = true })
    -- Moving away from the window without a key of ours: it is closed and the
    -- key is not swallowed.
    api.nvim_create_autocmd(
      { "BufLeave", "WinLeave", "ModeChanged", "CmdlineEnter", "InsertEnter", "VimResized" },
      {
        group = wk.group,
        callback = function()
          local ok, err = pcall(teardown)
          if not ok then
            fail(err)
          end
        end,
      }
    )
    wk.state = "open"
    vim.on_key(function(key, typed)
      local ok, res = pcall(on_key, key, typed)
      if not ok then
        fail(res)
        return nil
      end
      return res
    end, wk.key_ns)
  end

  function install_whichkey()
    if vim.g.pisar_whichkey == false then
      return
    end
    for _, mode in ipairs({ "n", "x" }) do
      -- A <Space> of the user's own is theirs.
      if next(vim.fn.maparg("<Space>", mode, false, true)) == nil then
        vim.keymap.set(mode, "<Space>", function()
          local ok, err = pcall(open, mode)
          if not ok then
            fail(err)
          end
        end, { silent = true, desc = text("nvimMapKeys") })
      end
    end
  end
end

-- The user's own file, user.lua, runs last. Pisar never creates, changes or
-- deletes it: it is a file of the user's, in Pisar's home folder, and its path
-- arrives in PISAR_USER_LUA. No variable, no file, a folder or a file that is
-- not a regular one (a dangling link, a FIFO, which would block the read): it
-- is as if it did not exist, silently. A link to a regular file is loaded: the
-- user may keep user.lua in their own repository and link it here.
--
-- Two things must not happen. A mistake in the file must not stop the editor
-- from starting, so it is loaded and run under pcall, and the rest of it (after
-- the error) does not run, which is how Lua works; what the file did before the
-- error stays in effect. And the file must not break what Go relies on, so after
-- it that is set again. The user is told of the mistake: Go reads
-- g:pisar_user_error (the full text with the traceback, the path replaced by
-- "user.lua") after it connects, logs the first line and shows it on the message
-- line, cut to fit (internal/nvim, Process.UserError).
--
-- THIS COMES BACK AFTER user.lua (it cannot be changed from there):
--   'hidden' on (opening a document while another has unsaved changes);
--   the flags "t" and "T" in 'shortmess' (the file and other messages that Go's
--     :write and :edit produce stay one line); other flags are the user's;
--   'laststatus' 2 and 'cmdheight' 1 (the search panel is laid out with exactly
--     these two bottom rows);
--   the autocmd groups PisarNotify (what Go listens to: saved, entered,
--     modified, changed, bye) and PisarFormat, rebuilt from scratch: anything a
--     file added to those two groups is gone, and nothing is doubled;
--   'eventignore' and 'eventignorewin' (the global value and that of every
--     window): "all" and the events the two groups listen to are removed, with
--     a note. The other entries are the user's. An ignored BufWritePost would
--     leave the saved .docx old after a :w, with no message;
--   _G.pisar_format, which Go calls;
--   g:pisar_reloading, g:pisar_preview and g:pisar_channel, Pisar's own
--     variables, are unset: a true g:pisar_reloading would silence every buffer
--     switch, a true g:pisar_preview would send a change event for every key
--     while the preview is off, and a g:pisar_channel that is no channel would
--     make every notification an error. Go sets g:pisar_channel after it
--     connects, and g:pisar_preview when the preview is turned on;
--   'spelllang': names without a dictionary are removed, with a note (a
--     missing one would otherwise ask to download it, see above); when none is
--     left the languages Pisar chose are used.
--
-- It is done at the end of this file, again after the user's own VimEnter
-- autocmds and the calls it scheduled, and once more when Go has connected
-- (restore_again). That covers the loading of the file and the end of Neovim's
-- start; it does not cover later work: an autocmd of the user's that changes one
-- of these on a later event (BufWinEnter, say), vim.defer_fn() with a delay, or
-- a replaced function (vim.rpcnotify = ...) can still silence what Go listens
-- to. That is the user's code; the limit is known and not closed here.
--
-- THE USER MAY CHANGE everything else, among it all the <leader> mappings above
-- (<leader>e, <leader>p, <leader>f too: the application's actions stay available
-- from its buttons, the menu and the keyboard shortcuts of the window), options,
-- colours, their own mappings and autocmds (in groups of their own). A changed
-- mapleader applies to what the file maps afterwards; Pisar's mappings were
-- made with the space and stay there.
--
-- The which-key window (above) is set up after this file, and is the user's to
-- shape:
--   vim.o.timeoutlen = N             how long a <Space> waits before the window
--                                    opens (Pisar's default is 500);
--   vim.g.pisar_whichkey = false     no window: the bare <Space> is not mapped;
--   vim.g.pisar_groups = { g = "Git", gh = "Hunks" }
--                                    names for groups, by the keys after the
--                                    leader in the form keytrans() writes; a
--                                    group without a name shows "+N";
--   a mapping of the user's own on exactly <Space> (in normal or visual mode,
--     each on its own): the window is not made for that mode.
-- The window lists the mappings that have a description (desc); the others
-- work from it, but are not shown.
-- Known limits:
--   * A user.lua that replaces vim.on_key breaks the window.
--   * A key that begins a mapping of normal mode ("g", for the default gc, gx,
--     gr...) reaches the window only after 'timeoutlen', unless the key after it
--     is typed quickly.
--   * Two keys that are read in the same go (typed ahead) can run in the reverse
--     order: the mapping from the window is queued after what was already typed.
--
-- The messages the file prints while loading (print, vim.notify, :echo) are
-- caught instead of shown: any of them over one line would open a hit-enter
-- prompt, and nothing answers it before Go is connected. They are kept in
-- g:pisar_user_output. A file that asks for a key while loading (input(),
-- confirm(), getchar()) or never returns still stops the start; that cannot be
-- guarded against from inside Neovim, and Go ends the start with its timeout.
--
-- Note: a callback of the file that fails later (an autocmd, a timer) is
-- reported by Neovim in the usual way, and a message of several lines opens a
-- hit-enter prompt then. Go's calls that can be answered ask for that state
-- first (Format) or refuse (Save).

-- A user.lua that prints without end must not fill memory twice; what is kept
-- is the start of the output.
local USER_OUTPUT_MAX = 16 * 1024

local function user_file()
  local path = vim.env.PISAR_USER_LUA
  if not path or path == "" then
    return nil
  end
  -- fs_stat follows links: a dangling one has no type, a link to a folder is a
  -- directory.
  local st = vim.uv.fs_stat(path)
  if not st or st.type ~= "file" then
    return nil
  end
  return path
end

-- Lua cuts the name of a chunk that is long to its end, with "..." in front
-- (LUA_IDSIZE; measured: "... directory name/Library/Application Support/Pisar/
-- user.lua:2: boom"), and a path under ~/Library/Application Support/Pisar is
-- long. The message line has room for little, so the name is made plain.
local function plain_name(text, path)
  text = text:gsub(vim.pesc(path), "user.lua")
  local lines = vim.split(text, "\n", { plain = true })
  for i, line in ipairs(lines) do
    line = line:gsub("^(%s*)%.%.%.[^\n]-/user%.lua", "%1user.lua")
    -- An error of vim.cmd() comes with the way to it: "vim/_core/editor.lua:0:
    -- <path>..nvim_exec2() called at <path>:0:1, line 1: Vim:E492: ...".
    lines[i] = line:gsub("^.*nvim_exec2%(%) called at [^\n]-, line %d+: ", "")
  end
  return table.concat(lines, "\n")
end

-- Runs the file; returns the text of the error, or nil.
local function run_user_file(path)
  local chunk, err = loadfile(path)
  if not chunk then
    return err
  end
  local ok, failure = xpcall(chunk, function(e)
    local trace = debug.traceback(tostring(e), 2)
    -- The frames below the file are those of this one: of no use to its author.
    local below = trace:find("\n\t%[C%]: in function 'xpcall'")
    return below and trace:sub(1, below - 1) or trace
  end)
  if not ok then
    return failure
  end
  return nil
end

local function check_spelllang()
  local kept, dropped = {}, {}
  for name in vim.gsplit(vim.o.spelllang, ",", { plain = true }) do
    name = vim.trim(name)
    if name ~= "" then
      table.insert(has_dictionary(name) and kept or dropped, name)
    end
  end
  if #dropped == 0 then
    return nil
  end
  if #kept == 0 then
    kept = langs
  end
  vim.cmd("silent! set spelllang=" .. table.concat(kept, ","))
  return table.concat(dropped, ",")
end

-- Whether an item of 'eventignore' names an event that PisarNotify or PisarFormat
-- listens to. The two groups are the one list of them, so it cannot drift from
-- what is registered above. nvim_get_autocmds() takes an event in any case and
-- under its alias too ("BufRead" is BufReadPost), which is how the option
-- reads them; it raises for what is no event ("all", "-WinLeave"), hence pcall.
local function pisar_listens_to(event)
  for _, group in ipairs({ "PisarNotify", "PisarFormat" }) do
    local ok, found = pcall(api.nvim_get_autocmds, { group = group, event = event })
    if ok and #found > 0 then
      return true
    end
  end
  return false
end

-- The value of an event list ('eventignore', 'eventignorewin') without "all" and
-- without the events Pisar listens to, also in the form "-Event" (:help
-- 'eventignore': "all,-WinLeave" ignores all but WinLeave). The other items stay
-- as they are. Returns the new value and the items removed.
local function without_pisar_events(value)
  local kept, removed = {}, {}
  for item in vim.gsplit(value, ",", { plain = true }) do
    if item ~= "" then
      local event = item:gsub("^%-", "")
      if item:lower() == "all" or pisar_listens_to(event) then
        table.insert(removed, item)
      else
        table.insert(kept, item)
      end
    end
  end
  return table.concat(kept, ","), removed
end

-- Takes Pisar's events out of 'eventignore' and 'eventignorewin' (the global
-- value, which windows opened later start from, and that of each window now).
-- Only an option that changes is written. Returns a note, or nil.
local function restore_eventignore()
  local taken, seen = {}, {}
  local function clean(current, set)
    local value, removed = without_pisar_events(current)
    if #removed == 0 then
      return
    end
    set(value)
    for _, item in ipairs(removed) do
      if not seen[item:lower()] then
        seen[item:lower()] = true
        table.insert(taken, item)
      end
    end
  end

  clean(vim.go.eventignore, function(value)
    vim.go.eventignore = value
  end)
  clean(vim.go.eventignorewin, function(value)
    vim.go.eventignorewin = value
  end)
  for _, win in ipairs(api.nvim_list_wins()) do
    local opts = { scope = "local", win = win }
    clean(api.nvim_get_option_value("eventignorewin", opts), function(value)
      api.nvim_set_option_value("eventignorewin", value, opts)
    end)
  end
  if #taken == 0 then
    return nil
  end
  return table.concat(taken, ",")
end

-- What Go relies on (the list above). Returns what the user's file made that was
-- undone and that they should hear about, as data and not as text: a table with
-- "events" (what was removed from 'eventignore', comma separated) and "spell"
-- (the 'spelllang' entries without a dictionary), or nil. Go writes the
-- sentences, in Croatian, once it is connected (internal/nvim, UserConfig).
local restore_again
local function restore_required(late)
  vim.o.hidden = true
  vim.opt.shortmess:append("t")
  vim.opt.shortmess:append("T")
  vim.o.laststatus = 2
  vim.o.cmdheight = 1
  install_events()
  install_format_events()
  _G.pisar_format = pisar_format
  _G.pisar_msg = msg
  _G.pisar_restore = function()
    restore_again(false)
  end
  vim.g.pisar_reloading = nil
  -- Go sets these two once it is connected, which can be before a later pass
  -- (restore_again): that one must leave them as they are.
  if not late then
    vim.g.pisar_preview = nil
    vim.g.pisar_channel = nil
  end

  -- After install_events(): the event list is read from what it registered.
  local notes = {}
  notes.events = restore_eventignore()
  notes.spell = check_spelllang()
  if next(notes) == nil then
    return nil
  end
  return notes
end

-- The same restoring once more, for what the user's file put off until later in
-- the start: a VimEnter autocmd of its own, vim.schedule(), vim.defer_fn(). The
-- first pass runs at the end of this file, before any of that. These run in
-- order behind the user's: PisarStartup is registered after user.lua, so its
-- VimEnter autocmd comes after the user's, and what it schedules is queued
-- behind what the user's file and VimEnter autocmds scheduled (measured: the
-- user's VimEnter autocmd, then PisarStartup's; then the file's scheduled calls
-- and its timers of 0 and 20 ms; then what the user's VimEnter autocmd
-- scheduled, then PisarStartup's scheduled pass; calls scheduled from inside
-- those come after it, and Go connects after them all). Go asks for one more
-- pass, in the same call in which it sets g:pisar_channel and reads the notes, so
-- the state at the end of Neovim's start is checked (pisar_restore).
--
-- What is found is added to g:pisar_user_note, which Go reads when it connects.
-- A pass that comes after Go has read it (g:pisar_channel is set) says so itself,
-- from the texts it has: nothing else would tell the user. announce is that.
function restore_again(announce)
  local ok, notes = pcall(restore_required, true)
  if not ok then
    notes = { restore = tostring(notes) }
  end
  if not notes then
    return
  end
  local all = vim.g.pisar_user_note
  if type(all) ~= "table" then
    all = {}
  end
  for _, key in ipairs({ "events", "spell" }) do
    if notes[key] then
      -- A list of names: only what is not in it yet.
      local have, merged = {}, {}
      for name in vim.gsplit(all[key] or "", ",", { plain = true }) do
        if name ~= "" then
          have[name] = true
          table.insert(merged, name)
        end
      end
      for name in vim.gsplit(notes[key], ",", { plain = true }) do
        if not have[name] then
          table.insert(merged, name)
        end
      end
      all[key] = table.concat(merged, ",")
    end
  end
  if notes.restore then
    all.restore = all.restore and (all.restore .. "; " .. notes.restore) or notes.restore
  end
  vim.g.pisar_user_note = all

  if announce and vim.g.pisar_channel then
    for _, item in ipairs({
      { "spell", "nvimSpellMissing", "languages" },
      { "events", "nvimEventsRestored", "events" },
      { "restore", "nvimRestoreFailed", "reason" },
    }) do
      if notes[item[1]] then
        msg.show(msg.framed(text(item[2]), item[3], notes[item[1]]))
      end
    end
  end
end

-- 'messagesopt' without "hit-enter" and with "wait:0" (:help 'messagesopt'): a
-- message longer than the message line is then neither waited for nor asked
-- about. It is for the start only. What the file prints while it loads is caught
-- (below), but what it prints later still during the start (vim.schedule,
-- vim.defer_fn, a VimEnter autocmd of its own) would open a hit-enter prompt that
-- nothing answers (measured: Start does not return). Neovim's own setting is back
-- once VimEnter and the callbacks queued until then have run, so that the prompts
-- the rest of Pisar is built around work as before. Items other than these two
-- are kept as they are ("history:" must be there).
local function startup_messagesopt(current)
  local items = {}
  for item in vim.gsplit(current, ",", { plain = true }) do
    if item ~= "hit-enter" and not item:match("^wait:") then
      table.insert(items, item)
    end
  end
  table.insert(items, "wait:0")
  return table.concat(items, ",")
end

local function load_user_file()
  local path = user_file()
  if not path then
    return
  end

  local original = vim.o.messagesopt
  local guarded = startup_messagesopt(original)
  vim.o.messagesopt = guarded

  local failure
  -- The function is global only for the duration of the call: nvim_exec2 can
  -- run Ex commands, not a Lua closure.
  _G.pisar_run_user_file = function()
    failure = run_user_file(path)
  end
  local ok, res = pcall(vim.api.nvim_exec2, "lua pisar_run_user_file()", { output = true })
  _G.pisar_run_user_file = nil
  if ok then
    if res.output ~= "" then
      vim.g.pisar_user_output = res.output:sub(1, USER_OUTPUT_MAX)
    end
  elseif not failure then
    -- The file ran to its end, but wrote an error message (vim.notify with
    -- ERROR, nvim_err_writeln): :lua reports that when it ends, and the output
    -- caught so far is lost with it. "Vim(lua):" precedes the message.
    failure = tostring(res):match("Vim%(lua%):(.*)$") or tostring(res)
  end
  if failure then
    vim.g.pisar_user_error = plain_name(failure, path)
  end

  -- The file may have set 'messagesopt' itself: that is what comes back.
  local wanted = vim.o.messagesopt
  if wanted == guarded then
    wanted = original
  end
  vim.o.messagesopt = startup_messagesopt(wanted)
  vim.api.nvim_create_autocmd("VimEnter", {
    group = vim.api.nvim_create_augroup("PisarStartup", { clear = true }),
    once = true,
    callback = function()
      -- After the VimEnter autocmds of the file (this one is registered later).
      restore_again(true)
      -- Callbacks of the file queued with vim.schedule run after VimEnter
      -- (measured), and this one is queued behind them.
      vim.schedule(function()
        vim.o.messagesopt = wanted
        restore_again(true)
      end)
    end,
  })
end

-- The core's "VimEnter * ...get('xx')|set spell" (see the top of this file) does
-- nothing now but "set spell", which would switch spelling on against a file
-- that turned it off. It is undone after it: this autocmd is added later, so it
-- runs later.
local function keep_spell_choice()
  for _, au in ipairs(vim.api.nvim_get_autocmds({ event = "VimEnter" })) do
    if au.command and au.command:find("nvim.spellfile", 1, true) then
      local spell = vim.o.spell
      vim.api.nvim_create_autocmd("VimEnter", {
        group = vim.api.nvim_create_augroup("PisarSpell", { clear = true }),
        once = true,
        callback = function()
          vim.o.spell = spell
        end,
      })
      return
    end
  end
end

load_user_file()
local restore_ok, note = pcall(restore_required)
if not restore_ok then
  note = { restore = tostring(note) }
end
keep_spell_choice()
if note then
  vim.g.pisar_user_note = note
end

-- Last, once: after the user's file, so that it can turn the window off or map
-- its own <Space>. Not in restore_again: nothing there can undo it.
install_whichkey()
