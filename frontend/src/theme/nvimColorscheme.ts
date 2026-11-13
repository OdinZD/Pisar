import { type ColorToken, type Palette, type Theme } from './tokens'

export interface Highlight {
  fg?: ColorToken
  bg?: ColorToken
  sp?: ColorToken
  bold?: boolean
  italic?: boolean
  underline?: boolean
  undercurl?: boolean
  link?: string
}

// Design notes that explain the choices below (the ratios are measured on the pisar palette; the
// contrast tests repeat the checks for every theme):
// - Every text colour is one of text, text.muted, accent, syntax.list, syntax.code, danger, which all
//   reach 4.5:1 on bg.editor AND on bg.cursorline (the cursor line lies under any text), so a group
//   without its own background never needs a contrast exception. text.linenr is only 4.25 on
//   bg.cursorline, hence it is used for LineNr (own background) and nowhere else.
// - syntax.list is only 4.03 on bg.selected, so every group painted on bg.selected sets its text
//   colour explicitly (Visual, Search, MatchParen...) instead of inheriting syntax colours.
// - nvim_set_hl replaces a group completely, so attributes Neovim's defaults would add (reverse,
//   bold, its own palette) are gone for every group listed here.
// - Groups missing here keep Neovim's defaults (its own grey/colour palette): LSP and diagnostic
//   groups, which Pisar does not use.

/** Every highlight group the colorscheme defines, by token name. The single place the mapping lives. */
export const highlights: Record<string, Highlight> = {
  // Surfaces and the cursor line.
  Normal: { fg: 'text', bg: 'bg.editor' },
  NormalNC: { link: 'Normal' },
  CursorLine: { bg: 'bg.cursorline' },
  CursorColumn: { bg: 'bg.cursorline' },
  ColorColumn: { bg: 'bg.chrome' },
  Cursor: { fg: 'bg.editor', bg: 'text' },
  lCursor: { link: 'Cursor' },
  CursorIM: { link: 'Cursor' },
  TermCursor: { fg: 'bg.editor', bg: 'text' },

  // Gutter.
  LineNr: { fg: 'text.linenr', bg: 'bg.editor' },
  CursorLineNr: { fg: 'text', bg: 'bg.cursorline', bold: true },
  SignColumn: { fg: 'text.muted', bg: 'bg.editor' },
  FoldColumn: { fg: 'text.muted', bg: 'bg.editor' },
  CursorLineSign: { bg: 'bg.cursorline' },
  CursorLineFold: { bg: 'bg.cursorline' },
  Folded: { fg: 'text.muted', bg: 'bg.desk' },

  // Window chrome drawn by Neovim.
  StatusLine: { fg: 'text.status', bg: 'bg.desk' },
  StatusLineNC: { fg: 'text.muted', bg: 'bg.desk' },
  StatusLineTerm: { link: 'StatusLine' },
  StatusLineTermNC: { link: 'StatusLineNC' },
  WinBar: { link: 'StatusLine' },
  WinBarNC: { link: 'StatusLineNC' },
  WinSeparator: { fg: 'border.strong' },
  VertSplit: { link: 'WinSeparator' },
  TabLine: { link: 'StatusLineNC' },
  TabLineFill: { bg: 'bg.desk' },
  TabLineSel: { fg: 'text', bg: 'bg.editor', bold: true },

  // Messages and the mode indicator.
  ModeMsg: { fg: 'accent', bold: true },
  MoreMsg: { fg: 'accent' },
  Question: { fg: 'accent' },
  ErrorMsg: { fg: 'danger' },
  WarningMsg: { fg: 'syntax.list' },
  OkMsg: { fg: 'syntax.code' },

  // Selection and search.
  Visual: { fg: 'text', bg: 'bg.selected' },
  Search: { fg: 'text', bg: 'bg.selected', underline: true },
  IncSearch: { fg: 'accent.text', bg: 'accent' },
  CurSearch: { fg: 'accent.text', bg: 'accent', bold: true },
  MatchParen: { fg: 'text', bg: 'bg.selected', bold: true },
  QuickFixLine: { fg: 'text', bg: 'bg.selected', bold: true },

  // Popups and floating windows.
  Pmenu: { fg: 'text', bg: 'bg.chrome' },
  PmenuSel: { fg: 'accent.text', bg: 'accent' },
  PmenuSbar: { bg: 'bg.desk' },
  PmenuThumb: { bg: 'border.strong' },
  NormalFloat: { fg: 'text', bg: 'bg.chrome' },
  FloatBorder: { fg: 'text.muted', bg: 'bg.chrome' },
  FloatTitle: { fg: 'accent', bg: 'bg.chrome', bold: true },
  // The which-key window in nvim/init.lua: key column and group names. Both set bg themselves so the
  // contrast test measures them on the float's real background, not on Normal. syntax.list would be
  // the natural pick for group names, but it is only 4.32 on bg.chrome in the pisar theme.
  PisarKey: { fg: 'accent', bg: 'bg.chrome', bold: true },
  PisarKeyGroup: { fg: 'syntax.code', bg: 'bg.chrome' },

  // Quiet text.
  Comment: { fg: 'text.muted' },
  NonText: { fg: 'text.muted' },
  EndOfBuffer: { fg: 'text.muted' },
  Conceal: { fg: 'text.muted' },
  Delimiter: { fg: 'text.muted' },

  // Spelling: only a wavy line, the text keeps its colour.
  SpellBad: { sp: 'danger', undercurl: true },
  SpellCap: { sp: 'syntax.list', undercurl: true },
  SpellRare: { sp: 'accent', undercurl: true },
  SpellLocal: { sp: 'syntax.code', undercurl: true },

  // Diff.
  DiffAdd: { fg: 'syntax.code', bg: 'bg.chrome' },
  DiffChange: { fg: 'accent', bg: 'bg.chrome' },
  DiffDelete: { fg: 'danger', bg: 'bg.chrome' },
  DiffText: { fg: 'text', bg: 'bg.selected', bold: true },
  Added: { fg: 'syntax.code' },
  Changed: { fg: 'accent' },
  Removed: { fg: 'danger' },

  // Headings and accents.
  Title: { fg: 'accent', bold: true },
  Directory: { fg: 'accent' },

  // Pisar's own base groups: the Markdown groups below link here, so the token is written once.
  PisarList: { fg: 'syntax.list' },
  PisarCode: { fg: 'syntax.code' },

  // Generic syntax (code inside Markdown fences, other file types).
  Identifier: { fg: 'text' },
  Function: { fg: 'text' },
  Operator: { fg: 'text' },
  Statement: { fg: 'accent' },
  Type: { fg: 'accent' },
  Constant: { fg: 'syntax.code' },
  String: { fg: 'syntax.code' },
  Special: { fg: 'syntax.list' },
  PreProc: { fg: 'syntax.list' },
  Underlined: { fg: 'accent', underline: true },
  Todo: { fg: 'text', bg: 'bg.selected', bold: true },
  Error: { fg: 'danger', bold: true },
  '@variable': { fg: 'text' },

  // Markdown, classic syntax (syntax/markdown.vim, active for filetype=markdown).
  markdownH1: { link: 'Title' },
  markdownH2: { link: 'Title' },
  markdownH3: { link: 'Title' },
  markdownH4: { link: 'Title' },
  markdownH5: { link: 'Title' },
  markdownH6: { link: 'Title' },
  markdownHeadingDelimiter: { link: 'Title' },
  markdownHeadingRule: { link: 'Delimiter' },
  markdownRule: { link: 'Delimiter' },
  markdownListMarker: { link: 'PisarList' },
  markdownOrderedListMarker: { link: 'PisarList' },
  markdownCode: { link: 'PisarCode' },
  markdownCodeBlock: { link: 'PisarCode' },
  markdownCodeDelimiter: { link: 'PisarCode' },
  markdownBold: { bold: true },
  markdownBoldDelimiter: { link: 'markdownBold' },
  markdownItalic: { italic: true },
  markdownItalicDelimiter: { link: 'markdownItalic' },
  markdownBoldItalic: { bold: true, italic: true },
  markdownBoldItalicDelimiter: { link: 'markdownBoldItalic' },
  markdownBlockquote: { link: 'Comment' },
  markdownLinkText: { fg: 'accent', underline: true },
  markdownUrl: { fg: 'text.muted', underline: true },
  markdownAutomaticLink: { link: 'markdownUrl' },

  // Markdown, treesitter (queries/markdown*, started by ftplugin/markdown.lua). Heading levels are
  // listed one by one: the lookup falls back from @markup.heading.1 to @markup.heading only
  // when the longer name is undefined, and a link here is clearer than relying on that.
  '@markup': { fg: 'text' },
  '@markup.heading': { link: 'Title' },
  '@markup.heading.1': { link: 'Title' },
  '@markup.heading.2': { link: 'Title' },
  '@markup.heading.3': { link: 'Title' },
  '@markup.heading.4': { link: 'Title' },
  '@markup.heading.5': { link: 'Title' },
  '@markup.heading.6': { link: 'Title' },
  '@markup.list': { link: 'PisarList' },
  '@markup.list.checked': { link: 'PisarCode' },
  '@markup.list.unchecked': { link: 'PisarList' },
  '@markup.raw': { link: 'PisarCode' },
  '@markup.raw.block': { link: 'PisarCode' },
  '@markup.strong': { bold: true },
  '@markup.italic': { italic: true },
  '@markup.quote': { link: 'Comment' },
  '@markup.link': { link: 'Underlined' },
  '@markup.link.label': { fg: 'accent', underline: true },
  '@markup.link.url': { link: 'markdownUrl' },
  '@punctuation.special': { link: 'Delimiter' },
}

// Group names go into Lua source, so anything outside this set is refused instead of escaped.
function luaGroupName(name: string): string {
  if (!/^[A-Za-z0-9_@.]+$/.test(name)) throw new Error(`unsupported highlight group name: ${name}`)
  return `"${name}"`
}

// `link` is written alone: Neovim ignores every other attribute next to it (:help nvim_set_hl()).
function luaDefinition(h: Highlight, palette: Palette): string {
  if (h.link !== undefined) return `{ link = ${luaGroupName(h.link)} }`
  const parts: string[] = []
  for (const key of ['fg', 'bg', 'sp'] as const) {
    const token = h[key]
    if (token !== undefined) parts.push(`${key} = "${palette[token]}"`)
  }
  for (const key of ['bold', 'italic', 'underline', 'undercurl'] as const) {
    if (h[key]) parts.push(`${key} = true`)
  }
  return `{ ${parts.join(', ')} }`
}

/** Lua source of colors/pisar.lua for the given theme; only the colours and 'background' differ between themes. */
export function nvimColorscheme(theme: Theme): string {
  const lines = [
    '-- Generated from frontend/src/theme/tokens.ts by Pisar. Do not edit: the file is rewritten at start.',
    '',
    '-- Drop a previous colorscheme first: changing \'background\' while g:colors_name is set',
    '-- would reload that scheme.',
    'vim.g.colors_name = nil',
    `vim.o.background = "${theme.background}"`,
    'vim.cmd("highlight clear")',
    'vim.g.colors_name = "pisar"',
    '',
  ]
  for (const [name, h] of Object.entries(highlights)) {
    lines.push(`vim.api.nvim_set_hl(0, ${luaGroupName(name)}, ${luaDefinition(h, theme.colors)})`)
  }
  return lines.join('\n') + '\n'
}
