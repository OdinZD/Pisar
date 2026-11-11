import { describe, expect, it } from 'vitest'
// ?raw instead of node:fs: the project has no @types/node, and svelte-check type-checks test files.
import design from '../../../docs/design/DESIGN.md?raw'
import { colors } from './tokens'

// Rows look like: | `bg.app` | `#F5F6F7` | where |
function designColors(): Record<string, string> {
  const out: Record<string, string> = {}
  for (const m of design.matchAll(/^\|\s*`([a-z.]+)`\s*\|\s*`(#[0-9A-Fa-f]{6})`\s*\|/gm)) {
    out[m[1]] = m[2].toUpperCase()
  }
  return out
}

describe('colour tokens', () => {
  it('match the table in DESIGN.md (names and values)', () => {
    const fromDesign = designColors()
    expect(Object.keys(fromDesign).length).toBe(16)
    const fromCode = Object.fromEntries(Object.entries(colors).map(([k, v]) => [k, v.toUpperCase()]))
    expect(fromCode).toEqual(fromDesign)
  })
})
