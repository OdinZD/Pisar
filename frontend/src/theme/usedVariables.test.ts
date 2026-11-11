import { describe, expect, it } from 'vitest'
import { cssVariables } from './cssVars'

// Catches a misspelled variable name, which otherwise only shows up as a missing style on screen.
// import.meta.glob instead of node:fs: the project has no @types/node.
const sources = import.meta.glob(['/src/**/*.{svelte,css}'], { query: '?raw', eager: true, import: 'default' }) as Record<
  string,
  string
>

const usedPattern = /var\(\s*(--[\w-]+)/g
// Component-local custom properties (e.g. a row's depth) are declared in the same files:
// `--depth: 1;` in CSS, `style:--depth={…}` in Svelte markup.
const declaredPattern = /(?:^|[\s;{"'])(--[\w-]+)\s*:|style:(--[\w-]+)/g

function names(text: string, pattern: RegExp): string[] {
  return [...text.matchAll(pattern)].map((m) => m[1] ?? m[2])
}

describe('CSS variable guard', () => {
  const known = new Set(Object.keys(cssVariables()))
  const files = Object.keys(sources)

  it('scans the stylesheet and the components', () => {
    expect(files).toContain('/src/app.css')
    expect(files.some((f) => f.endsWith('.svelte'))).toBe(true)
  })

  it('every var(--name) is a token variable or declared next to its use', () => {
    const declared = new Set(files.flatMap((f) => names(sources[f], declaredPattern)))
    const unknown = files.flatMap((f) =>
      names(sources[f], usedPattern)
        .filter((n) => !known.has(n) && !declared.has(n))
        .map((n) => `${f}: ${n}`),
    )
    expect(unknown).toEqual([])
  })

  it('the guard does catch an unknown name', () => {
    expect(names('color: var(--bg-chrom);', usedPattern)).toEqual(['--bg-chrom'])
    expect(known.has('--bg-chrom')).toBe(false)
    expect(known.has('--bg-chrome')).toBe(true)
    expect(names('style:--depth={d}', declaredPattern)).toEqual(['--depth'])
  })
})
