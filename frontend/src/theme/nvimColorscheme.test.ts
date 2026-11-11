import { describe, expect, it } from 'vitest'
import { contrastRatio } from './contrast'
import { highlights, nvimColorscheme, type Highlight } from './nvimColorscheme'
import { colors, type ColorToken } from './tokens'

const text = nvimColorscheme()
const entries = Object.entries(highlights)

describe('nvimColorscheme text', () => {
  it('is deterministic', () => {
    expect(nvimColorscheme()).toBe(text)
  })

  it('names itself pisar and starts from a clean slate', () => {
    expect(text).toContain('vim.g.colors_name = "pisar"')
    expect(text).toContain('vim.o.background = "light"')
    expect(text).toContain('vim.cmd("highlight clear")')
    // The clear must come before any group is defined, or it would wipe them.
    expect(text.indexOf('highlight clear')).toBeLessThan(text.indexOf('nvim_set_hl'))
  })

  it('contains only colours that are token values', () => {
    const allowed = new Set(Object.values(colors).map((c) => c.toUpperCase()))
    const found = text.match(/#[0-9a-fA-F]{3,8}\b/g) ?? []
    expect(found.length).toBeGreaterThan(0)
    expect(found.filter((c) => !allowed.has(c.toUpperCase()))).toEqual([])
  })

  it('defines every group exactly once', () => {
    for (const [name] of entries) {
      const calls = text.split(`nvim_set_hl(0, "${name}", `).length - 1
      expect(calls, name).toBe(1)
    }
    expect(text.match(/nvim_set_hl\(/g)).toHaveLength(entries.length)
  })

  it('writes a link alone', () => {
    for (const [name, h] of entries) {
      if (h.link === undefined) continue
      expect(Object.keys(h), name).toEqual(['link'])
      expect(text, name).toContain(`nvim_set_hl(0, "${name}", { link = "${h.link}" })`)
    }
  })

  it('links only to groups defined here', () => {
    const names = new Set(Object.keys(highlights))
    for (const [name, h] of entries) {
      if (h.link !== undefined) expect(names.has(h.link), `${name} -> ${h.link}`).toBe(true)
    }
  })
})

describe('shared sample for the Go tests', () => {
  // The Go side validates this text before Neovim executes it (colorscheme.go). Its tests read this
  // file instead of running Node, so a generator change fails here until the sample is renewed with
  // `npx vitest run -u src/theme/nvimColorscheme.test.ts`. In CI vitest never writes a snapshot: a
  // missing or different file is a failure. The path is relative to this test file (vitest resolves
  // it with path.resolve(dirname(testFile), path)): up to the repository root.
  it('matches testdata/colorscheme/pisar.lua', async () => {
    await expect(text).toMatchFileSnapshot('../../../testdata/colorscheme/pisar.lua')
  })
})

describe('required groups', () => {
  // Owner decisions: group -> [fg, bg] tokens (undefined = not set).
  const required: [string, ColorToken | undefined, ColorToken | undefined][] = [
    ['Normal', 'text', 'bg.editor'],
    ['CursorLine', undefined, 'bg.cursorline'],
    ['LineNr', 'text.linenr', 'bg.editor'],
    ['CursorLineNr', 'text', 'bg.cursorline'],
    ['StatusLine', 'text.status', 'bg.desk'],
    ['StatusLineNC', 'text.muted', 'bg.desk'],
    ['ModeMsg', 'accent', undefined],
    ['Visual', 'text', 'bg.selected'],
    ['CurSearch', 'accent.text', 'accent'],
    ['ErrorMsg', 'danger', undefined],
    ['WarningMsg', 'syntax.list', undefined],
    ['MoreMsg', 'accent', undefined],
    ['Question', 'accent', undefined],
    ['Title', 'accent', undefined],
    ['WinSeparator', 'border.strong', undefined],
    ['SignColumn', undefined, 'bg.editor'],
    ['FoldColumn', undefined, 'bg.editor'],
    ['Folded', 'text.muted', 'bg.desk'],
    ['Pmenu', 'text', 'bg.chrome'],
    ['PmenuSel', 'accent.text', 'accent'],
    ['Directory', 'accent', undefined],
    ['MatchParen', undefined, 'bg.selected'],
    ['PisarList', 'syntax.list', undefined],
    ['PisarCode', 'syntax.code', undefined],
  ]
  it.each(required)('%s uses the agreed tokens', (name, fg, bg) => {
    const h = highlights[name]
    expect(h, name).toBeDefined()
    if (fg !== undefined) expect(h.fg).toBe(fg)
    if (bg !== undefined) expect(h.bg).toBe(bg)
  })

  it('makes the mode, line number of the cursor line and title bold', () => {
    expect(highlights.ModeMsg.bold).toBe(true)
    expect(highlights.CursorLineNr.bold).toBe(true)
    expect(highlights.Title.bold).toBe(true)
    expect(highlights.MatchParen.bold).toBe(true)
  })

  it('draws a wavy red line for SpellBad without touching the text colour', () => {
    expect(highlights.SpellBad).toEqual({ sp: 'danger', undercurl: true })
  })

  it('covers headings, list markers and code in both Markdown highlighters', () => {
    const resolve = (name: string): Highlight => {
      let h = highlights[name]
      for (let i = 0; h?.link !== undefined && i < 10; i++) h = highlights[h.link]
      return h
    }
    for (const name of ['markdownH1', 'markdownH6', '@markup.heading', '@markup.heading.1', '@markup.heading.6']) {
      expect(resolve(name), name).toEqual(highlights.Title)
    }
    for (const name of ['markdownListMarker', 'markdownOrderedListMarker', '@markup.list']) {
      expect(resolve(name).fg, name).toBe('syntax.list')
    }
    for (const name of ['markdownCode', 'markdownCodeBlock', '@markup.raw', '@markup.raw.block']) {
      expect(resolve(name).fg, name).toBe('syntax.code')
    }
    expect(resolve('markdownBold').bold).toBe(true)
    expect(resolve('@markup.strong').bold).toBe(true)
    expect(resolve('markdownItalic').italic).toBe(true)
    expect(resolve('@markup.italic').italic).toBe(true)
  })
})

describe('colorscheme contrast (computed from highlights)', () => {
  const normalBg = highlights.Normal.bg as ColorToken
  const cursorline: ColorToken = 'bg.cursorline'
  const ratio = (a: ColorToken, b: ColorToken) => contrastRatio(colors[a], colors[b])

  // Only decoration: a separator or float border is a line, not text to read.
  const decorative: Record<string, string> = {
    WinSeparator: 'a thin line between windows, not text',
  }

  const resolved = entries.filter(([, h]) => h.link === undefined)

  it('only exempts groups that exist and are not linked', () => {
    for (const name of Object.keys(decorative)) {
      expect(highlights[name], name).toBeDefined()
      expect(highlights[name].link, name).toBeUndefined()
    }
  })

  for (const [name, h] of resolved) {
    if (h.fg === undefined || name in decorative) continue
    const fg = h.fg
    const own = h.bg
    it(`${name}: ${fg} on ${own ?? `Normal (${normalBg})`} is at least 4.5`, () => {
      expect(ratio(fg, own ?? normalBg)).toBeGreaterThanOrEqual(4.5)
    })
    if (own === undefined) {
      it(`${name}: ${fg} on the cursor line is at least 4.5`, () => {
        expect(ratio(fg, cursorline)).toBeGreaterThanOrEqual(4.5)
      })
    }
  }
})
