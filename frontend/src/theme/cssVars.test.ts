import { describe, expect, it } from 'vitest'
import { applyTheme, cssVariables, fontStack } from './cssVars'
import { colors, fontFamilyNames, fontWeights, sizes, spacing, typography } from './tokens'

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
    expect(vars['--size-preview-width']).toBe('400px')
    expect(vars['--size-preview-page-max-width']).toBe('420px')
    expect(vars['--size-preview-page-padding']).toBe('40px')
    expect(vars['--size-preview-collapse-width']).toBe('1100px')
    expect(vars['--size-editor-min-width']).toBe('460px')
    expect(vars['--size-preview-page-shadow-offset']).toBe('1px')
    expect(vars['--size-preview-page-shadow-blur']).toBe('3px')
    expect(vars['--size-preview-quote-rule-width']).toBe('3px')
    for (const [name, value] of Object.entries(vars)) {
      if (name.startsWith('--size-') || name.startsWith('--space-')) expect(value).toMatch(/^\d+px$/)
    }
  })

  it('has fonts and typography', () => {
    expect(vars['--font-ui']).toBe('"Inter", system-ui, sans-serif')
    expect(vars['--font-mono']).toBe('"JetBrains Mono", monospace')
    expect(vars['--font-preview']).toBe('"Source Serif 4", ui-serif, Georgia, serif')
    expect(vars['--font-size-preview']).toBe('14px')
    expect(vars['--font-line-height-preview']).toBe('22px')
    expect(vars['--font-size-preview-h1']).toBe('26px')
    expect(vars['--font-line-height-preview-h1']).toBe('32px')
    expect(vars['--font-size-preview-h2']).toBe('18px')
    expect(vars['--font-line-height-preview-h2']).toBe('24px')
    expect(vars['--font-size-editor']).toBe('13px')
    expect(vars['--font-line-height-editor']).toBe('21px')
    expect(vars['--font-letter-spacing-section-title']).toBe('0.06em')
    expect(vars['--font-weight-semibold']).toBe('600')
    expect(vars['--font-weight-bold']).toBe('700')
  })

  it('fontStack is the value of the font variable', () => {
    expect(fontStack('mono')).toBe(vars['--font-mono'])
    expect(fontStack('ui')).toBe(vars['--font-ui'])
    expect(fontStack('preview')).toBe(vars['--font-preview'])
  })

  it('has one variable per token and no duplicate names', () => {
    // Fonts: one stack per family role + one per weight; the rest map one to one.
    const expected =
      Object.keys(colors).length + Object.keys(fontFamilyNames).length + Object.keys(fontWeights).length + Object.keys(typography).length + Object.keys(sizes).length + Object.keys(spacing).length
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
