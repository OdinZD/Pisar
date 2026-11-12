import { describe, expect, it } from 'vitest'
import { cssVariables } from '../theme/cssVars'
import { previewFontFaceSpecs } from '../theme/fonts'
import { sizes, themeIds, themes, type Palette } from '../theme/tokens'
import { appOrigin, previewCsp, previewFrameDocument, previewPageId, previewSandbox, type FrameFace } from './previewFrame'

const origin = 'http://127.0.0.1:9245'
const faces: FrameFace[] = previewFontFaceSpecs().map((f) => ({ ...f, url: `${origin}${f.url.startsWith('/') ? '' : '/'}${f.url}` }))
// The builder for one theme's palette; every block that builds a frame runs once per theme.
const builderFor =
  (palette: Palette) =>
  (o: Partial<{ origin: string | null; faces: FrameFace[] }> = {}) =>
    previewFrameDocument({ origin, faces, palette, ...o })

function cspOf(doc: string): Map<string, string> {
  const m = /<meta http-equiv="Content-Security-Policy" content="([^"]*)">/.exec(doc)
  expect(m, 'the CSP meta tag').not.toBeNull()
  const directives = new Map<string, string>()
  for (const part of (m?.[1] ?? '').split('; ')) {
    const [name, ...rest] = part.split(' ')
    directives.set(name, rest.join(' '))
  }
  return directives
}

describe('previewSandbox', () => {
  it('is exactly allow-same-origin', () => {
    expect(previewSandbox).toBe('allow-same-origin')
  })
})

describe.each(themeIds)('previewFrameDocument: CSP, theme %s', (id) => {
  const build = builderFor(themes[id].colors)
  it('allows nothing but inline styles, data: images and fonts from the origin', () => {
    expect(Object.fromEntries(cspOf(build()))).toEqual({
      'default-src': "'none'",
      'img-src': 'data:',
      'style-src': "'unsafe-inline'",
      'font-src': origin,
      'form-action': "'none'",
      'base-uri': "'none'",
    })
  })

  it('names no script, connect or frame source', () => {
    const names = [...cspOf(build()).keys()]
    for (const bad of ['script-src', 'connect-src', 'frame-src', 'object-src', 'media-src', 'child-src']) expect(names).not.toContain(bad)
  })

  it('takes the origin as given (https and another port)', () => {
    expect(cspOf(build({ origin: 'https://wails.localhost' })).get('font-src')).toBe('https://wails.localhost')
    expect(cspOf(build({ origin: 'wails://wails' })).get('font-src')).toBe('wails://wails')
  })

  it('allows no font at all without an origin', () => {
    const doc = build({ origin: null })
    expect(cspOf(doc).get('font-src')).toBe("'none'")
    expect(doc).not.toContain('@font-face')
  })

  it.each([
    ['a quote that would close the attribute', 'https://a.test"; script-src *'],
    ['a second source', 'https://a.test https://b.test'],
    ['a directive', 'https://a.test; script-src *'],
    ['a path', 'https://a.test/fonts'],
    ['a wildcard host', 'https://*.a.test'],
    ['a keyword', "'self'"],
    ['a bare wildcard', '*'],
    ['an empty string', ''],
    ['no scheme', 'a.test'],
    ['a markup character', 'https://a.test<'],
  ])('refuses an origin with %s', (_what, bad) => {
    expect(() => previewCsp(bad)).toThrow()
    expect(() => build({ origin: bad })).toThrow()
  })
})

describe('appOrigin', () => {
  it.each([
    [{ protocol: 'wails:', host: 'wails' }, 'wails://wails'],
    [{ protocol: 'http:', host: '127.0.0.1:9245' }, 'http://127.0.0.1:9245'],
    [{ protocol: 'https:', host: 'wails.localhost' }, 'https://wails.localhost'],
    [{ protocol: 'file:', host: '' }, null],
    [{ protocol: 'about:', host: '' }, null],
    [{ protocol: 'https:', host: 'a b' }, null],
  ])('%j gives %j', (loc, want) => {
    expect(appOrigin(loc)).toBe(want)
  })
})

describe.each(themeIds)('previewFrameDocument: content, theme %s', (id) => {
  const palette = themes[id].colors
  const build = builderFor(palette)
  const doc = build()

  it('has no script and no javascript: address', () => {
    expect(doc.toLowerCase()).not.toContain('<script')
    expect(doc.toLowerCase()).not.toContain('javascript:')
    expect(doc).not.toMatch(/\son[a-z]+=/i)
  })

  it('mentions no address but the origin and the given fonts', () => {
    const allowed = [origin, ...faces.map((f) => f.url)]
    const found = doc.match(/https?:\/\/[^\s"')<;]+/g) ?? []
    expect(found.length).toBeGreaterThan(0)
    for (const url of found) expect(allowed.some((a) => url.startsWith(a)), url).toBe(true)
  })

  it('declares the four faces with the given addresses, weights and styles', () => {
    const blocks = doc.match(/@font-face \{[^}]*\}/g) ?? []
    expect(blocks).toHaveLength(4)
    expect(faces.map((f) => [f.weight, f.style])).toEqual([
      [400, 'normal'],
      [400, 'italic'],
      [700, 'normal'],
      [700, 'italic'],
    ])
    faces.forEach((f, i) => {
      expect(blocks[i]).toContain(`font-family: "${f.family}";`)
      expect(blocks[i]).toContain(`font-weight: ${f.weight};`)
      expect(blocks[i]).toContain(`font-style: ${f.style};`)
      expect(blocks[i]).toContain(`url("${f.url}")`)
    })
  })

  it('writes every token variable into :root', () => {
    const root = /:root \{([^}]*)\}/.exec(doc)?.[1] ?? ''
    for (const [name, value] of Object.entries(cssVariables(palette))) expect(root).toContain(`${name}: ${value};`)
  })

  it('uses only variables that :root defines', () => {
    const defined = new Set(Object.keys(cssVariables(palette)))
    const used = [...doc.matchAll(/var\((--[\w-]+)/g)].map((m) => m[1])
    expect(used.length).toBeGreaterThan(0)
    expect(used.filter((n) => !defined.has(n))).toEqual([])
  })

  it('follows the tokens: another token value gives another document', () => {
    const c = palette as Record<string, string>
    const s = sizes as Record<string, number>
    const [oldColor, oldPadding] = [c['bg.desk'], s.previewPagePadding]
    try {
      c['bg.desk'] = '#123456'
      s.previewPagePadding = 33
      const changed = build()
      expect(changed).toContain('--bg-desk: #123456;')
      expect(changed).toContain('--size-preview-page-padding: 33px;')
      expect(changed).not.toBe(doc)
    } finally {
      c['bg.desk'] = oldColor
      s.previewPagePadding = oldPadding
    }
    expect(build()).toBe(doc)
  })

  it('has the preview typography and page sizes from the tokens', () => {
    expect(doc).toContain('--font-size-preview: 14px;')
    expect(doc).toContain('--font-line-height-preview: 22px;')
    expect(doc).toContain('--font-size-preview-h1: 26px;')
    expect(doc).toContain('--font-line-height-preview-h1: 32px;')
    expect(doc).toContain('--font-size-preview-h2: 18px;')
    expect(doc).toContain('--font-line-height-preview-h2: 24px;')
    expect(doc).toContain('--size-preview-page-max-width: 420px;')
    expect(doc).toContain('--size-preview-page-padding: 40px;')
    expect(doc).toContain('--font-preview: "Source Serif 4", ui-serif, Georgia, serif;')
  })

  it('has one empty container for the page', () => {
    expect(previewPageId).toBe('page')
    expect(doc.match(/id="page"/g)).toHaveLength(1)
    expect(doc).toContain('<main id="page"></main>')
  })

  it('styles what the preview shows (links that are not links, missing pictures, task boxes, footnotes)', () => {
    for (const selector of ['.link', '.missing-image', '.task-box', '.footnotes', 'table', 'blockquote', 'pre', 'img']) {
      expect(doc, selector).toContain(selector)
    }
    expect(doc).toMatch(/img \{[^}]*max-width: 100%/)
  })

  it('styles a note as a number and a list: there is no link to style', () => {
    expect(doc).toContain('.footnote-ref')
    expect(doc).not.toContain('.footnote-back')
    expect(doc).not.toMatch(/(^|[\s,}])a \{/)
  })

  it('writes no length of its own beyond 0 and 1 px: the sizes are variables from the tokens', () => {
    // Everything after the :root block, which is where the token values are written down.
    const rules = doc.slice(doc.indexOf('\n}\n', doc.indexOf(':root {')))
    const literals = rules.match(/(?<![\w.-])\d*\.?\d+(px|rem|em|pt)\b/g) ?? []
    expect(literals.filter((l) => l !== '0' && l !== '1px' && !/^0(px|rem|em|pt)$/.test(l))).toEqual([])
    expect(doc).toContain('var(--size-preview-page-shadow-blur)')
    expect(doc).toContain('var(--size-preview-quote-rule-width)')
  })
})

describe.each(themeIds)('previewFrameDocument: hostile values, theme %s', (id) => {
  const build = builderFor(themes[id].colors)
  it('cannot be broken out of by a font address', () => {
    const evil: FrameFace = {
      family: 'A"; } </style><script>alert(1)</script>',
      weight: 400,
      style: 'normal',
      url: `${origin}/f.woff2"); } </style><script>alert(2)</script><style>x{`,
    }
    const doc = build({ faces: [evil] })
    expect(doc.toLowerCase()).not.toContain('<script')
    expect(doc.match(/<\/style>/g)).toHaveLength(1)
    expect(doc.match(/<style>/g)).toHaveLength(1)
    // Both strings stay one CSS string each.
    const block = /@font-face \{[^}]*\}/.exec(doc)?.[0] ?? ''
    expect(block.match(/(?<!\\)"/g)).toHaveLength(4 + 2) // family quotes, url quotes, format quotes
  })

  it('refuses a weight or style that is not one', () => {
    const ok = faces[0]
    expect(() => build({ faces: [{ ...ok, weight: 400.5 }] })).toThrow()
    expect(() => build({ faces: [{ ...ok, weight: 0 }] })).toThrow()
    expect(() => build({ faces: [{ ...ok, style: 'normal; src: url(x)' as 'normal' }] })).toThrow()
  })
})
