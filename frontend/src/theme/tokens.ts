// The only place in the frontend where colours, sizes, spacing and font families are written down.
// Components read them as CSS variables (see cssVars.ts). Values mirror docs/design/DESIGN.md;
// tokens.test.ts fails if the colour table there and the `pisar` palette here drift apart.

// A palette is one object per theme with the same keys, the colour name being the key. Adding a theme
// means a new palette below, a new entry in `themes` and in `themeIds`.
//
// The `pisar` palette must stay the FIRST palette in this file with one `'name': '#RRGGBB',` per line:
// editorservice_test.go (tokenColor) and scripts/smoke.sh (token) read tokens.ts with a regular
// expression and take the first match. tokens.test.ts checks the order.
const pisar = {
  'bg.app': '#F5F6F7',
  'bg.chrome': '#ECEEF0',
  'bg.editor': '#FFFFFF',
  'bg.desk': '#E3E6EA',
  'bg.cursorline': '#EEF3FC',
  'bg.selected': '#DCE7FA',
  border: '#D5D9DE',
  'border.strong': '#C3C8CE',
  text: '#1D2125',
  'text.muted': '#5B6470',
  'text.status': '#3C444D',
  'text.linenr': '#6B7480',
  accent: '#1F5FD1',
  'accent.text': '#FFFFFF',
  'syntax.list': '#B45309',
  'syntax.code': '#0F6B3C',
  danger: '#B42318',
} as const
export type ColorToken = keyof typeof pisar
export type Palette = Readonly<Record<ColorToken, string>>

export const themeIds = ['pisar'] as const
export type ThemeId = (typeof themeIds)[number]

export interface Theme {
  /** What Neovim's 'background' is set to; the colorscheme and some plugins decide on it. */
  readonly background: 'light' | 'dark'
  readonly colors: Palette
}

export const themes: Readonly<Record<ThemeId, Theme>> = {
  pisar: { background: 'light', colors: pisar },
}
export const defaultTheme: ThemeId = 'pisar'

// Family names are used by fonts.ts to register the faces and must not appear anywhere else.
export const fontFamilyNames = {
  ui: 'Inter',
  mono: 'JetBrains Mono',
  // The preview page only; it lives in the preview frame, which has its own @font-face rules.
  preview: 'Source Serif 4',
} as const
export type FontRole = keyof typeof fontFamilyNames

// Fallbacks apply when a face fails to load; the app still has to render.
export const fontFallbacks = {
  ui: 'system-ui, sans-serif',
  mono: 'monospace',
  preview: 'ui-serif, Georgia, serif',
} as const

export const fontWeights = {
  regular: 400,
  semibold: 600,
  // The editor colorscheme uses bold (headings, the line number of the cursor line).
  bold: 700,
} as const
export type FontWeightToken = keyof typeof fontWeights

// Sizes and line heights in px; letter spacing is relative to the font size, hence a string.
export const typography = {
  uiSize: 13,
  uiLineHeight: 20,
  sectionTitleSize: 11,
  sectionTitleLetterSpacing: '0.06em',
  editorSize: 13,
  editorLineHeight: 21,
  docKindSize: 10,
  previewSize: 14,
  previewLineHeight: 22,
  previewH1Size: 26,
  previewH1LineHeight: 32,
  previewH2Size: 18,
  previewH2LineHeight: 24,
} as const
export type TypographyToken = keyof typeof typography

// Layout sizes in px (DESIGN.md "Raspored" and "Komponente").
export const sizes = {
  toolbarHeight: 48,
  treeWidth: 240,
  rowHeight: 28,
  buttonHeight: 32,
  radius: 6,
  // 2 px stays visible next to the 1 px borders and is the common minimum for a focus indicator.
  focusRingWidth: 2,
  // Preview pane (DESIGN.md "Raspored" and "Pregled"). The pane is fixed; the editor takes the rest and
  // never gets narrower than editorMinWidth. The preview closes itself below previewCollapseWidth
  // (= treeWidth + previewWidth + editorMinWidth).
  previewWidth: 400,
  previewPageMaxWidth: 420,
  previewPagePadding: 40,
  // The page's soft shadow (offset down and blur) and the rule beside a quotation. Beyond the
  // 1 px hairlines these are the only fixed lengths the preview page needs.
  previewPageShadowOffset: 1,
  previewPageShadowBlur: 3,
  previewQuoteRuleWidth: 3,
  previewCollapseWidth: 1100,
  editorMinWidth: 460,
} as const
export type SizeToken = keyof typeof sizes

export const spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
} as const
export type SpacingToken = keyof typeof spacing

// Behaviour of the search panel (DESIGN.md "Tražilica"). Not CSS variables: the row count reaches the
// stylesheet as a component-local variable, the rest is read by code.
export const search = {
  // Results shown without scrolling.
  visibleRows: 8,
  // Quiet time after the last keystroke before the service is called.
  debounceMs: 80,
  // Rows at the bottom of the terminal that Neovim keeps for itself: the status line and the command
  // line (laststatus=2 and cmdheight=1 are Neovim's defaults and init.lua does not change them).
  reservedRows: 2,
} as const
