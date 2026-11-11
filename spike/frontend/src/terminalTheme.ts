import type { ITheme } from '@xterm/xterm'

// The single place that defines how the terminal looks (spike; the real app
// takes these from theme/tokens.ts later).
export const terminalFont = {
  family: 'JetBrains Mono',
  sizePx: 13,
  lineHeightPx: 21,
  weight: 400,
  weightBold: 700,
} as const

export const terminalTheme: ITheme = {
  background: '#FFFFFF',
  foreground: '#1D2125',
  cursor: '#1D2125',
  selectionBackground: '#DCE7FA',
}
