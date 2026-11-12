import { describe, expect, it } from 'vitest'

// The two properties of the preview that no unit test of a function can show: the HTML of a document
// never enters the application's DOM, and the frame never gets more than allow-same-origin. These
// scan the sources for the ways either could be broken.
// import.meta.glob instead of node:fs: the project has no @types/node.
const sources = import.meta.glob(['/src/**/*.{ts,svelte}', '!/src/**/*.test.ts'], { query: '?raw', eager: true, import: 'default' }) as Record<
  string,
  string
>
const files = Object.keys(sources)
// The one file that writes into the frame's container.
const writer = '/src/lib/previewWrite.ts'

describe('preview source guard', () => {
  it('scans the preview files', () => {
    expect(files).toContain(writer)
    expect(files).toContain('/src/components/Preview.svelte')
  })

  it('no {@html} anywhere', () => {
    expect(files.filter((f) => /\{@html\b/.test(sources[f]))).toEqual([])
  })

  it('innerHTML and the like appear only in the frame writer', () => {
    const hits = files.filter((f) => f !== writer && /\b(innerHTML|outerHTML|insertAdjacentHTML|createContextualFragment|DOMParser)\b|document\.write/.test(sources[f]))
    expect(hits).toEqual([])
    expect(sources[writer]).toMatch(/\.innerHTML = html/)
  })

  it('no sandbox permission but allow-same-origin is ever written down', () => {
    const hits = files.filter((f) => /allow-(scripts|forms|popups|modals|top-navigation|downloads|pointer-lock|presentation|orientation-lock)/.test(sources[f]))
    expect(hits).toEqual([])
  })

  it('the frame element takes its sandbox from the constant and its srcdoc from the shell function', () => {
    const svelte = sources['/src/components/Preview.svelte']
    expect(svelte).toContain('sandbox={previewSandbox}')
    expect(svelte).toContain('{srcdoc}')
    expect(svelte).toMatch(/const srcdoc = previewFrameDocument\(/)
  })

  it('the guard patterns do catch violations', () => {
    expect(/\{@html\b/.test('{@html x}')).toBe(true)
    expect(/allow-(scripts|forms)/.test('sandbox="allow-same-origin allow-scripts"')).toBe(true)
  })
})
