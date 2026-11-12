import { describe, expect, it } from 'vitest'
// ?raw instead of node:fs: the project has no @types/node, and svelte-check type-checks test files.
import design from '../../../docs/design/DESIGN.md?raw'
import tokensSource from './tokens.ts?raw'
import { defaultTheme, themeIds, themes } from './tokens'

// A theme's section starts at "### Tema: Name (`id`, light|dark)" and runs to the next "###" or "##" heading.
const sectionHeading = /^### Tema: .*\(`([a-z-]+)`, [^)]*\)\s*$/gm

function themeSection(id: string): string | undefined {
  for (const m of design.matchAll(sectionHeading)) {
    if (m[1] !== id) continue
    const start = m.index + m[0].length
    const rest = design.slice(start)
    const end = rest.search(/^##+ /m)
    return end === -1 ? rest : rest.slice(0, end)
  }
  return undefined
}

// Rows look like: | `bg.app` | `#F5F6F7` | where |
function designColors(section: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const m of section.matchAll(/^\|\s*`([a-z.]+)`\s*\|\s*`(#[0-9A-Fa-f]{6})`\s*\|/gm)) {
    out[m[1]] = m[2].toUpperCase()
  }
  return out
}

describe('colour tokens in DESIGN.md', () => {
  it('has one "### Tema:" section per theme id, in the order of themeIds', () => {
    const ids = [...design.matchAll(sectionHeading)].map((m) => m[1])
    expect(ids).toEqual([...themeIds])
    expect([...design.matchAll(/^### Tema:/gm)]).toHaveLength(themeIds.length)
  })

  describe.each(themeIds)('theme %s', (id) => {
    it('matches the table in DESIGN.md (names and values)', () => {
      const section = themeSection(id)
      expect(section, 'a section in DESIGN.md').toBeDefined()
      const fromDesign = designColors(section ?? '')
      expect(Object.keys(fromDesign).length).toBe(17)
      const fromCode = Object.fromEntries(Object.entries(themes[id].colors).map(([k, v]) => [k, v.toUpperCase()]))
      expect(fromCode).toEqual(fromDesign)
    })
  })
})

describe('theme registry', () => {
  const pisarKeys = Object.keys(themes.pisar.colors).sort()

  it('starts with pisar, which is the default', () => {
    expect(themeIds[0]).toBe('pisar')
    expect(defaultTheme).toBe('pisar')
  })

  it('has a theme for every id and no other', () => {
    expect(Object.keys(themes).sort()).toEqual([...themeIds].sort())
  })

  describe.each(themeIds)('theme %s', (id) => {
    const theme = themes[id]

    it('has exactly the 17 colour tokens of pisar', () => {
      expect(Object.keys(theme.colors).sort()).toEqual(pisarKeys)
      expect(pisarKeys).toHaveLength(17)
    })

    it('writes every colour as #RRGGBB in capitals', () => {
      for (const [token, value] of Object.entries(theme.colors)) expect(value, token).toMatch(/^#[0-9A-F]{6}$/)
    })

    it('is light or dark', () => {
      expect(['light', 'dark']).toContain(theme.background)
    })
  })
})

describe('tokens.ts as source text', () => {
  // editorservice_test.go and scripts/smoke.sh take the first `'bg.editor': '#...'` match of this file.
  it('has the pisar palette first', () => {
    const m = /^\s*'?bg\.editor'?:\s*'(#[0-9A-Fa-f]{6})'/m.exec(tokensSource)
    expect(m, 'a bg.editor line').not.toBeNull()
    expect(m?.[1]).toBe(themes.pisar.colors['bg.editor'])
  })
})
