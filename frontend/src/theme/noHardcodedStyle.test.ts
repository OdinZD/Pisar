import { describe, expect, it } from 'vitest'
import { fontFamilyNames } from './tokens'

// import.meta.glob instead of node:fs: the project has no @types/node.
// Test files, theme/tokens.ts and generated bindings are the only exemptions.
const sources = import.meta.glob(
  ['/src/**/*.{ts,svelte,css}', '!/src/**/*.test.ts', '!/src/theme/tokens.ts', '!/src/bindings/**'],
  { query: '?raw', eager: true, import: 'default' },
) as Record<string, string>
const files = Object.keys(sources)

const colourPatterns = [/#[0-9a-fA-F]{3,8}\b/, /\brgba?\(/i, /\bhsla?\(/i]

// Neither side of the name may be part of a longer identifier or a file name (Inter-Regular.woff2).
const familyPattern = (name: string) => new RegExp(`(?<![\\w/-])${name}(?![\\w-])`)

describe('source guard', () => {
  it('scans some files', () => {
    expect(files).toContain('/src/theme/cssVars.ts')
    expect(files).toContain('/src/app.css')
    expect(files).not.toContain('/src/theme/tokens.ts')
  })

  it('has no colour literals outside theme/tokens.ts', () => {
    const hits = files.flatMap((f) => colourPatterns.filter((p) => p.test(sources[f])).map((p) => `${f}: ${p}`))
    expect(hits).toEqual([])
  })

  it('has no font family names outside theme/tokens.ts', () => {
    const hits = files.flatMap((f) =>
      Object.values(fontFamilyNames)
        .filter((name) => familyPattern(name).test(sources[f]))
        .map((name) => `${f}: ${name}`),
    )
    expect(hits).toEqual([])
  })

  it('the guard patterns do catch violations', () => {
    expect(colourPatterns.some((p) => p.test('color: #fff;'))).toBe(true)
    expect(colourPatterns.some((p) => p.test('background: rgba(0,0,0,.5)'))).toBe(true)
    expect(familyPattern('Inter').test("font-family: 'Inter', sans-serif")).toBe(true)
    expect(familyPattern('Inter').test('../assets/fonts/Inter-Regular.woff2')).toBe(false)
  })
})
