import { describe, expect, it } from 'vitest'
import { applyTheme, cssVariables, fontStack } from './cssVars'
import { colors, fontWeights, sizes, spacing, typography } from './tokens'

describe('cssVariables', () => {
  const vars = cssVariables()

  it.each([
    ['bg.app', '--bg-app'],
    ['border', '--border'],
    ['border.strong', '--border-strong'],
    ['accent.text', '--accent-text'],
    ['syntax.code', '--syntax-code'],
  ] as const)('colour %s becomes %s', (token, name) => {
    expect(vars[name]).toBe(colors[token])
  })

  it('gives every colour a variable', () => {
    for (const [token, value] of Object.entries(colors)) {
      expect(vars[`--${token.replace(/\./g, '-')}`]).toBe(value)
    }
  })

  it('gives sizes and spacing a px unit', () => {
    expect(vars['--size-toolbar-height']).toBe('48px')
    expect(vars['--size-tree-width']).toBe('240px')
    expect(vars['--space-sm']).toBe('8px')
    for (const [name, value] of Object.entries(vars)) {
      if (name.startsWith('--size-') || name.startsWith('--space-')) expect(value).toMatch(/^\d+px$/)
    }
  })

  it('has fonts and typography', () => {
    expect(vars['--font-ui']).toBe('"Inter", system-ui, sans-serif')
    expect(vars['--font-mono']).toBe('"JetBrains Mono", monospace')
    expect(vars['--font-size-editor']).toBe('13px')
    expect(vars['--font-line-height-editor']).toBe('21px')
    expect(vars['--font-letter-spacing-section-title']).toBe('0.06em')
    expect(vars['--font-weight-semibold']).toBe('600')
    expect(vars['--font-weight-bold']).toBe('700')
  })

  it('fontStack is the value of the font variable', () => {
    expect(fontStack('mono')).toBe(vars['--font-mono'])
    expect(fontStack('ui')).toBe(vars['--font-ui'])
  })

  it('has one variable per token and no duplicate names', () => {
    // Fonts: 2 stacks + one per weight; the rest map one to one.
    const expected =
      Object.keys(colors).length + 2 + Object.keys(fontWeights).length + Object.keys(typography).length + Object.keys(sizes).length + Object.keys(spacing).length
    expect(Object.keys(vars).length).toBe(expected)
  })
})

describe('applyTheme', () => {
  it('sets exactly what cssVariables returns', () => {
    const set: Record<string, string> = {}
    applyTheme({ style: { setProperty: (n, v) => void (set[n] = v) } })
    expect(set).toEqual(cssVariables())
  })
})
