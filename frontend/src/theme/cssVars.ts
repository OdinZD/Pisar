import { colors, fontFallbacks, fontFamilyNames, fontWeights, sizes, spacing, typography } from './tokens'

// Naming rule: `--<group>-<token>` in kebab-case.
//   colours      --bg-app, --border-strong, --accent-text   (dots become dashes)
//   font stacks  --font-ui, --font-mono, --font-preview
//   weights      --font-weight-regular (unitless)
//   typography   --font-size-ui, --font-line-height-editor, --font-letter-spacing-section-title
//   sizes        --size-toolbar-height   (px)
//   spacing      --space-sm              (px)

const kebab = (s: string) => s.replace(/\./g, '-').replace(/([a-z0-9])([A-Z])/g, '$1-$2').toLowerCase()
const px = (n: number) => `${n}px`

/** CSS font-family value of a role; also what xterm.js gets as its fontFamily option. */
export function fontStack(role: keyof typeof fontFamilyNames): string {
  // Quoted so family names with spaces stay valid.
  return `"${fontFamilyNames[role]}", ${fontFallbacks[role]}`
}

/** Pure: every token as a CSS custom property, e.g. { '--bg-app': <bg.app value>, '--size-toolbar-height': '48px', … } */
export function cssVariables(): Record<string, string> {
  const vars: Record<string, string> = {}
  const add = (name: string, value: string) => {
    if (name in vars) throw new Error(`duplicate CSS variable ${name}`)
    vars[name] = value
  }

  for (const [k, v] of Object.entries(colors)) add(`--${kebab(k)}`, v)
  add('--font-ui', fontStack('ui'))
  add('--font-mono', fontStack('mono'))
  add('--font-preview', fontStack('preview'))
  for (const [k, v] of Object.entries(fontWeights)) add(`--font-weight-${kebab(k)}`, String(v))

  const typeName: Record<keyof typeof typography, string> = {
    uiSize: '--font-size-ui',
    uiLineHeight: '--font-line-height-ui',
    sectionTitleSize: '--font-size-section-title',
    sectionTitleLetterSpacing: '--font-letter-spacing-section-title',
    editorSize: '--font-size-editor',
    editorLineHeight: '--font-line-height-editor',
    docKindSize: '--font-size-doc-kind',
    previewSize: '--font-size-preview',
    previewLineHeight: '--font-line-height-preview',
    previewH1Size: '--font-size-preview-h1',
    previewH1LineHeight: '--font-line-height-preview-h1',
    previewH2Size: '--font-size-preview-h2',
    previewH2LineHeight: '--font-line-height-preview-h2',
  }
  for (const [k, v] of Object.entries(typography) as [keyof typeof typography, number | string][]) {
    add(typeName[k], typeof v === 'number' ? px(v) : v)
  }

  for (const [k, v] of Object.entries(sizes)) add(`--size-${kebab(k)}`, px(v))
  for (const [k, v] of Object.entries(spacing)) add(`--space-${kebab(k)}`, px(v))
  return vars
}

/** Sets them on the given element (the document root in the app). */
export function applyTheme(root: { style: { setProperty(name: string, value: string): void } }): void {
  for (const [name, value] of Object.entries(cssVariables())) root.style.setProperty(name, value)
}
