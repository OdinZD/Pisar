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
-- class of Vim's word characters, which follows 'iskeyword': letters of every
-- script, digits and "_" (left out: it is Markdown's own emphasis mark). Emoji
-- and punctuation are other classes, so "riječ," and "🙂riječ" end where the
-- word ends. Returns the byte range [from, to) of the word at byte column col,
-- or nil when the cursor is not on a word character.
local function is_word_char(ch)
  return ch ~= "_" and vim.fn.charclass(ch) == 2
end

local function word_at(line, col)
  local starts = vim.str_utf_pos(line)
  local n = #starts
  local function stop(i)
    return (starts[i + 1] or #line + 1) - 1
  end
  local function piece(i)
    return line:sub(starts[i], stop(i))
  end
  local at
  for i = 1, n do
    if starts[i] - 1 <= col and col < stop(i) then
      at = i
      break
    end
  end
  if not at or not is_word_char(piece(at)) then
    return nil
  end
  local first, last = at, at
  while first > 1 and is_word_char(piece(first - 1)) do
    first = first - 1
  end
  while last < n and is_word_char(piece(last + 1)) do
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
-- of the last character (inclusive).
local function selection()
  local a, b = vim.fn.getpos("v"), vim.fn.getpos(".")
  if a[2] > b[2] or (a[2] == b[2] and a[3] > b[3]) then
    a, b = b, a
  end
  return a[2] - 1, a[3] - 1, b[2] - 1, b[3] - 1
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

-- The pairs belong to one stay in insert mode.
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
    local runs = star_runs(get_line(row), from, get_line(row), to, true)
    local shift = #MARKERS[kind]
    if is_wrapped(kind, runs) then
      shift = -shift
    end
    toggle_marks(kind, row, from, row, to)
    set_cursor(row, col + shift)
    return
  end

  if mode == "block" then
    return
  end

  local sr, sc, er, ec = selection()
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

  -- "v": the end column of the selection is that of its last character.
  local last_line = get_line(er)
  ec = char_after(last_line, math.min(ec, #last_line))
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
function _G.pisar_format(kind)
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

-- Normal and visual mode only: in insert mode a space and a letter are text.
-- A failure is shown cut to a few columns, so that it cannot open a hit-enter
-- prompt.
for key, kind in pairs({ b = "bold", i = "italic", h = "heading", l = "list" }) do
  vim.keymap.set({ "n", "x" }, "<leader>" .. key, function()
    local err = _G.pisar_format(kind)
    if err ~= "" then
      api.nvim_echo({ { "Pisar: " .. vim.fn.strcharpart(err, 0, 40), "ErrorMsg" } }, true, {})
    end
  end, { silent = true, desc = "Pisar: format " .. kind })
end

-- Notifications on one channel are ordered, so "pisar_bye" reaches Go after
-- every event sent before it. That lets Go close its event stream without
-- losing the save that came right before `:wq`.
vim.api.nvim_create_autocmd("VimLeavePre", {
  group = group,
  callback = function()
    notify("pisar_bye")
  end,
})
