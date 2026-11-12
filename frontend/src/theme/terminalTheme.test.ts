import { describe, expect, it } from 'vitest'
import { contrastRatio } from './contrast'
import { terminalTheme, terminalThemeTokens, type TerminalTheme } from './terminalTheme'
import { themeIds, themes } from './tokens'

const keys = Object.keys(terminalThemeTokens) as (keyof TerminalTheme)[]

describe.each(themeIds)('terminalTheme, theme %s', (id) => {
  const colors = themes[id].colors
  const theme = terminalTheme(colors)

  it('has the 22 keys xterm.js needs here: 6 base keys and 16 ANSI colours', () => {
    expect(keys).toHaveLength(22)
    expect(Object.keys(theme).sort()).toEqual([...keys].sort())
  })

  it('takes every value from the token named in terminalThemeTokens', () => {
    for (const key of keys) expect(theme[key], key).toBe(colors[terminalThemeTokens[key]])
  })

  it('uses nothing but token values', () => {
    const allowed = new Set<string>(Object.values(colors))
    for (const key of keys) expect(allowed.has(theme[key]), key).toBe(true)
  })

  it('follows the mapping fixed in the design', () => {
    expect(theme.background).toBe(colors['bg.editor'])
    expect(theme.red).toBe(colors.danger)
    expect(theme.brightBlack).toBe(colors['text.muted'])
    expect(theme.magenta).toBe(colors.accent)
    expect(theme.white).toBe(colors['bg.desk'])
  })
})

describe.each(themeIds)('terminalTheme contrast, theme %s', (id) => {
  const theme = terminalTheme(themes[id].colors)
  const ratio = (a: keyof TerminalTheme, b: keyof TerminalTheme) => contrastRatio(theme[a], theme[b])

  it('text on background', () => expect(ratio('foreground', 'background')).toBeGreaterThanOrEqual(4.5))
  it('selected text on selection', () =>
    expect(ratio('selectionForeground', 'selectionBackground')).toBeGreaterThanOrEqual(4.5))
  it('cursor accent on cursor', () => expect(ratio('cursorAccent', 'cursor')).toBeGreaterThanOrEqual(4.5))

  // white and brightWhite are light fills for programs that paint backgrounds, not text colours.
  const textColours = keys.filter((k) => /^(black|red|green|yellow|blue|magenta|cyan|bright(?!White))/.test(k))
  it('covers the 14 text colours', () => expect(textColours).toHaveLength(14))
  for (const key of textColours) {
    it(`${key} on background is at least 4.5`, () => {
      expect(ratio(key, 'background')).toBeGreaterThanOrEqual(4.5)
    })
  }
})
