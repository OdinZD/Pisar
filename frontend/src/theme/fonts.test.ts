import { describe, expect, it } from 'vitest'
import { fontFaceSpecs } from './fonts'
import { fontFamilyNames, fontWeights } from './tokens'

// Binary files read as text are good enough to prove they exist and are not empty.
const files = import.meta.glob('../assets/fonts/*', { query: '?raw', eager: true, import: 'default' }) as Record<string, string>
const sizeOf = (name: string) => files[`../assets/fonts/${name}`]?.length ?? 0

describe('fontFaceSpecs', () => {
  const specs = fontFaceSpecs()

  it('lists Inter 400/600 and the mono regular face, derived from the tokens', () => {
    expect(specs.map((s) => [s.family, s.weight, s.url.split('/').pop()])).toEqual([
      [fontFamilyNames.ui, fontWeights.regular, 'Inter-Regular.woff2'],
      [fontFamilyNames.ui, fontWeights.semibold, 'Inter-SemiBold.woff2'],
      [fontFamilyNames.mono, fontWeights.regular, 'JetBrainsMono-Regular.woff2'],
    ])
  })

  it('has no duplicate family and weight pair', () => {
    const keys = specs.map((s) => `${s.family}/${s.weight}`)
    expect(new Set(keys).size).toBe(keys.length)
  })
})

describe('font files', () => {
  it.each(['Inter-Regular.woff2', 'Inter-SemiBold.woff2', 'JetBrainsMono-Regular.woff2'])('%s exists and is not empty', (f) => {
    expect(sizeOf(f)).toBeGreaterThan(0)
  })
  it.each(['Inter-OFL.txt', 'JetBrainsMono-OFL.txt'])('licence %s exists', (f) => {
    expect(sizeOf(f)).toBeGreaterThan(0)
  })
})
