import { describe, expect, it } from 'vitest'
import { contrastRatio } from './contrast'
import { themeIds, themes, type ColorToken } from './tokens'

describe('contrastRatio', () => {
  it('knows the extremes', () => {
    expect(contrastRatio('#000000', '#FFFFFF')).toBeCloseTo(21, 5)
    expect(contrastRatio('#1F5FD1', '#1f5fd1')).toBe(1)
  })
  it('is symmetric', () => {
    expect(contrastRatio('#1D2125', '#F5F6F7')).toBe(contrastRatio('#F5F6F7', '#1D2125'))
  })
  it('accepts three-digit hex', () => {
    expect(contrastRatio('#000', '#fff')).toBeCloseTo(21, 5)
  })
  it('rejects unsupported formats', () => {
    expect(() => contrastRatio('red', '#fff')).toThrow(/unsupported colour/)
    expect(() => contrastRatio('rgb(0,0,0)', '#fff')).toThrow(/unsupported colour/)
  })
})

// Pairs actually used for text. text.linenr on bg.cursorline (4.25) is left out on purpose: phase 2 decides it.
const pairs: [ColorToken, ColorToken[]][] = [
  ['text', ['bg.app', 'bg.chrome', 'bg.editor', 'bg.desk', 'bg.cursorline', 'bg.selected']],
  ['text.muted', ['bg.app', 'bg.chrome', 'bg.editor', 'bg.desk', 'bg.selected', 'bg.cursorline']],
  ['text.status', ['bg.desk']],
  ['text.linenr', ['bg.editor']],
  ['accent.text', ['accent']],
  ['accent', ['bg.editor', 'bg.app', 'bg.chrome', 'bg.cursorline', 'bg.selected']],
  ['syntax.list', ['bg.editor', 'bg.cursorline']],
  ['syntax.code', ['bg.editor', 'bg.cursorline']],
  ['danger', ['bg.editor', 'bg.chrome', 'bg.app', 'bg.cursorline']],
]

describe.each(themeIds)('text contrast of the colour tokens, theme %s', (id) => {
  const colors = themes[id].colors
  for (const [fg, bgs] of pairs) {
    for (const bg of bgs) {
      it(`${fg} on ${bg} is at least 4.5`, () => {
        expect(contrastRatio(colors[fg], colors[bg])).toBeGreaterThanOrEqual(4.5)
      })
    }
  }
})
