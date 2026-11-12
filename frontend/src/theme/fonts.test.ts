import { describe, expect, it } from 'vitest'
import { fontFaceSpecs, previewFontFaceSpecs } from './fonts'
import { fontFamilyNames, fontWeights } from './tokens'

// Binary files read as text are good enough to prove they exist and are not empty.
const files = import.meta.glob('../assets/fonts/*', { query: '?raw', eager: true, import: 'default' }) as Record<string, string>
const sizeOf = (name: string) => files[`../assets/fonts/${name}`]?.length ?? 0

describe('fontFaceSpecs', () => {
  const specs = fontFaceSpecs()

  it('lists Inter 400/600 and JetBrains Mono 400/700, derived from the tokens', () => {
    expect(specs.map((s) => [s.family, s.weight, s.url.split('/').pop()])).toEqual([
      [fontFamilyNames.ui, fontWeights.regular, 'Inter-Regular.woff2'],
      [fontFamilyNames.ui, fontWeights.semibold, 'Inter-SemiBold.woff2'],
      [fontFamilyNames.mono, fontWeights.regular, 'JetBrainsMono-Regular.woff2'],
      [fontFamilyNames.mono, fontWeights.bold, 'JetBrainsMono-Bold.woff2'],
    ])
  })

  it('has no duplicate family and weight pair', () => {
    const keys = specs.map((s) => `${s.family}/${s.weight}`)
    expect(new Set(keys).size).toBe(keys.length)
  })
})

describe('previewFontFaceSpecs', () => {
  const specs = previewFontFaceSpecs()

  it('lists Source Serif 4 regular, italic, bold and bold italic, derived from the tokens', () => {
    expect(specs.map((s) => [s.family, s.weight, s.style, s.url.split('/').pop()])).toEqual([
      [fontFamilyNames.preview, fontWeights.regular, 'normal', 'SourceSerif4-Regular.woff2'],
      [fontFamilyNames.preview, fontWeights.regular, 'italic', 'SourceSerif4-Italic.woff2'],
      [fontFamilyNames.preview, fontWeights.bold, 'normal', 'SourceSerif4-Bold.woff2'],
      [fontFamilyNames.preview, fontWeights.bold, 'italic', 'SourceSerif4-BoldItalic.woff2'],
    ])
  })

  it('is not part of the faces the application loads at start-up (the preview frame declares them)', () => {
    expect(fontFaceSpecs().some((s) => s.family === fontFamilyNames.preview)).toBe(false)
  })
})

describe('font files', () => {
  it.each([
    'Inter-Regular.woff2',
    'Inter-SemiBold.woff2',
    'JetBrainsMono-Regular.woff2',
    'JetBrainsMono-Bold.woff2',
    'SourceSerif4-Regular.woff2',
    'SourceSerif4-Italic.woff2',
    'SourceSerif4-Bold.woff2',
    'SourceSerif4-BoldItalic.woff2',
  ])('%s exists and is not empty', (f) => {
    expect(sizeOf(f)).toBeGreaterThan(0)
  })
  it.each(['Inter-OFL.txt', 'JetBrainsMono-OFL.txt', 'SourceSerif4-OFL.txt'])('licence %s exists', (f) => {
    expect(sizeOf(f)).toBeGreaterThan(0)
  })
})
