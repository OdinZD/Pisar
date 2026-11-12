// The document of the preview frame, as a pure function. No Svelte, no DOM.
//
// The preview page is an <iframe> whose `srcdoc` is set ONCE, to the shell built here: a CSP, the
// styles derived from the tokens, and an empty container. The HTML of the user's document is never
// part of it; Preview.svelte writes that into the container, inside the frame's own document, so the
// frame keeps its scroll position. Security rests on three layers that do not depend on each other:
//   1. the HTML was already cleaned in Go (no scripts, links or addresses);
//   2. the frame's sandbox does not permit scripts, so nothing in it runs;
//   3. this CSP forbids everything except inline styles, `data:` images and the application's fonts,
//      so even a script that slipped through could not load or send anything.
import { cssVariables } from '../theme/cssVars'

/** The `sandbox` attribute of the frame: exactly this one permission (scripts, forms and popups stay off). */
export const previewSandbox = 'allow-same-origin'

/** The `id` of the empty container the page content is written into. */
export const previewPageId = 'page'

export interface FrameFace {
  family: string
  weight: number
  style: 'normal' | 'italic'
  /** absolute address of the woff2 file, on the application's origin */
  url: string
}

export interface FrameOptions {
  /**
   * the application's origin, e.g. `wails://wails` or `http://127.0.0.1:9245`: the only source of fonts.
   * null when it is not known (see appOrigin): then the frame loads no font at all and the fallback
   * family from the tokens is used.
   */
  origin: string | null
  faces: readonly FrameFace[]
}

// scheme://host[:port] and nothing else: no path, no wildcard, no quote or space that could add a source
// to the CSP or leave the attribute.
const originPattern = /^[A-Za-z][A-Za-z0-9+.-]*:\/\/[A-Za-z0-9.-]+(?::\d{1,5})?$/

/** Escapes a string for an HTML attribute value in double quotes. */
function escapeAttr(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

/**
 * A CSS string literal. Every character that could end the string, the rule or the <style> element is
 * written as a hexadecimal escape, so the result can only ever be one string.
 */
function cssString(value: string): string {
  let out = ''
  for (const ch of value) {
    const code = ch.codePointAt(0) ?? 0
    out += /[A-Za-z0-9 _.,:/+=()-]/.test(ch) ? ch : `\\${code.toString(16)} `
  }
  return `"${out}"`
}

/**
 * The origin of the application's page as a CSP source, or null when it has none that a CSP can name
 * (a file: page, an opaque origin). Built from protocol and host rather than `location.origin`,
 * which is the string "null" for a custom scheme in some engines.
 */
export function appOrigin(loc: { protocol: string; host: string }): string | null {
  const origin = `${loc.protocol}//${loc.host}`
  return originPattern.test(origin) ? origin : null
}

/**
 * The Content-Security-Policy of the frame. Directives that fall back to default-src are not repeated.
 * Throws if the origin is not a plain `scheme://host[:port]`.
 */
export function previewCsp(origin: string | null): string {
  if (origin !== null && !originPattern.test(origin)) throw new Error(`invalid origin for the preview CSP: ${JSON.stringify(origin)}`)
  // form-action and base-uri do not fall back to default-src, so they are named.
  return `default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src ${origin ?? "'none'"}; form-action 'none'; base-uri 'none'`
}

function rootVariables(): string {
  const lines = Object.entries(cssVariables()).map(([name, value]) => {
    if (!/^--[a-z0-9-]+$/.test(name) || /[<>{};]/.test(value)) throw new Error(`variable ${name} cannot be written into the frame`)
    return `  ${name}: ${value};`
  })
  return `:root {\n${lines.join('\n')}\n}`
}

function fontFaces(faces: readonly FrameFace[]): string {
  return faces
    .map((f) => {
      if (!Number.isInteger(f.weight) || f.weight < 1 || f.weight > 1000) throw new Error(`invalid font weight ${f.weight}`)
      if (f.style !== 'normal' && f.style !== 'italic') throw new Error(`invalid font style ${String(f.style)}`)
      return [
        '@font-face {',
        `  font-family: ${cssString(f.family)};`,
        `  font-weight: ${f.weight};`,
        `  font-style: ${f.style};`,
        '  font-display: block;',
        `  src: url(${cssString(f.url)}) format("woff2");`,
        '}',
      ].join('\n')
    })
    .join('\n')
}

// Every colour, size and font is a variable from the tokens. The few literals left are 0, 1 px hairlines
// and unitless ratios, which are not design values.
const pageCss = `
*, *::before, *::after { box-sizing: border-box; }
html { background: var(--bg-desk); }
body {
  margin: 0;
  padding: var(--space-lg);
  background: var(--bg-desk);
  color: var(--text);
  font-family: var(--font-preview);
  font-size: var(--font-size-preview);
  line-height: var(--font-line-height-preview);
  font-weight: var(--font-weight-regular);
}
#${previewPageId} {
  max-width: var(--size-preview-page-max-width);
  min-height: calc(100vh - 2 * var(--space-lg));
  margin: 0 auto;
  padding: var(--size-preview-page-padding);
  background: var(--bg-editor);
  box-shadow: 0 0 0 1px var(--border), 0 1px 3px var(--border-strong);
  overflow-wrap: break-word;
}
#${previewPageId} > :first-child { margin-top: 0; }
#${previewPageId} > :last-child { margin-bottom: 0; }
h1, h2, h3, h4, h5, h6 {
  margin: var(--space-xl) 0 var(--space-sm);
  font-weight: var(--font-weight-bold);
  font-size: var(--font-size-preview);
  line-height: var(--font-line-height-preview);
}
h1 { font-size: var(--font-size-preview-h1); line-height: var(--font-line-height-preview-h1); }
h2 { font-size: var(--font-size-preview-h2); line-height: var(--font-line-height-preview-h2); }
p { margin: 0 0 var(--space-md); }
ul, ol { margin: 0 0 var(--space-md); padding-left: var(--space-xl); }
li > p { margin-bottom: var(--space-xs); }
li:has(> .task-box:first-child) { list-style: none; }
blockquote {
  margin: 0 0 var(--space-md);
  padding-left: var(--space-md);
  border-left: 3px solid var(--border-strong);
  color: var(--text-muted);
}
code, pre { font-family: var(--font-mono); font-size: var(--font-size-editor); }
code { padding: 0 var(--space-xs); border-radius: var(--size-radius); background: var(--bg-app); }
pre {
  margin: 0 0 var(--space-md);
  padding: var(--space-md);
  overflow-x: auto;
  border-radius: var(--size-radius);
  background: var(--bg-app);
  line-height: var(--font-line-height-editor);
}
pre code { padding: 0; background: none; }
table { display: block; max-width: 100%; margin: 0 0 var(--space-md); overflow-x: auto; border-collapse: collapse; }
th, td { padding: var(--space-xs) var(--space-sm); border: 1px solid var(--border-strong); text-align: left; vertical-align: top; }
th { background: var(--bg-app); font-weight: var(--font-weight-bold); }
hr { margin: var(--space-xl) 0; border: 0; border-top: 1px solid var(--border-strong); }
img { display: block; max-width: 100%; height: auto; }
sup, sub { line-height: 0; }
del { color: var(--text-muted); }
/* A link in the document looks like one but leads nowhere: Go turns every link into this span. */
.link { color: var(--accent); text-decoration: underline; }
/* The description of a picture that cannot be shown (the picture itself is not loaded). */
.missing-image { color: var(--text-muted); font-style: italic; }
.task-box { margin-right: var(--space-xs); color: var(--text-muted); }
a { color: var(--accent); }
.footnote-ref, .footnote-back { text-decoration: none; }
.footnotes {
  margin-top: var(--space-xl);
  color: var(--text-muted);
  font-size: var(--font-size-ui);
  line-height: var(--font-line-height-ui);
}
.footnotes hr { margin: 0 0 var(--space-md); }
.footnotes ol { margin-bottom: 0; }
`.trim()

/**
 * The complete document of the preview frame. It contains no content of the user's document: only an
 * empty container (`#page`) for it. Throws if the origin is not a plain `scheme://host[:port]`
 * (and not null).
 */
export function previewFrameDocument(opts: FrameOptions): string {
  const csp = escapeAttr(previewCsp(opts.origin))
  return [
    '<!doctype html>',
    '<html lang="hr">',
    '<head>',
    '<meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${csp}">`,
    '<style>',
    rootVariables(),
    fontFaces(opts.origin === null ? [] : opts.faces),
    pageCss,
    '</style>',
    '</head>',
    '<body>',
    `<main id="${previewPageId}"></main>`,
    '</body>',
    '</html>',
  ].join('\n')
}
