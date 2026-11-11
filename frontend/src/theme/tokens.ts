// The only place in the frontend where colours, sizes, spacing and font families are written down.
// Components read them as CSS variables (see cssVars.ts). Values mirror docs/design/DESIGN.md;
// tokens.test.ts fails if the colour table there and `colors` here drift apart.

// Colour name is the key so a dark theme can later be a second object with the same keys.
export const colors = {
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
} as const
export type ColorToken = keyof typeof colors

// Family names are used by fonts.ts to register the faces and must not appear anywhere else.
export const fontFamilyNames = {
  ui: 'Inter',
  mono: 'JetBrains Mono',
} as const
export type FontRole = keyof typeof fontFamilyNames

// Fallbacks apply when a face fails to load; the app still has to render.
export const fontFallbacks = {
  ui: 'system-ui, sans-serif',
  mono: 'monospace',
} as const

export const fontWeights = {
  regular: 400,
  semibold: 600,
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
