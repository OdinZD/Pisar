import { type ColorToken, type Palette } from './tokens'

/** Theme object for xterm.js, built from tokens only. Keys follow xterm.js ITheme. */
export interface TerminalTheme {
  background: string
  foreground: string
  cursor: string
  cursorAccent: string
  selectionBackground: string
  selectionForeground: string
  black: string
  red: string
  green: string
  yellow: string
  blue: string
  magenta: string
  cyan: string
  white: string
  brightBlack: string
  brightRed: string
  brightGreen: string
  brightYellow: string
  brightBlue: string
  brightMagenta: string
  brightCyan: string
  brightWhite: string
}

// Neovim runs with true colours (the colorscheme is generated from the same tokens), so it rarely
// touches the ANSI palette. The palette serves programs started inside :terminal. Every colour meant
// for text must stay readable on bg.editor whatever the theme (the tests check it for each theme);
// the design has no purple token, hence magenta shares the accent, and cyan shares the code green.
// "white" is a surface colour (bg.desk) for backgrounds drawn by programs, not for text.
/** Which token each key uses; the single place the mapping is written down. */
export const terminalThemeTokens: Record<keyof TerminalTheme, ColorToken> = {
  background: 'bg.editor',
  foreground: 'text',
  cursor: 'text',
  cursorAccent: 'bg.editor',
  selectionBackground: 'bg.selected',
  selectionForeground: 'text',
  black: 'text',
  red: 'danger',
  green: 'syntax.code',
  yellow: 'syntax.list',
  blue: 'accent',
  magenta: 'accent',
  cyan: 'syntax.code',
  white: 'bg.desk',
  brightBlack: 'text.muted',
  brightRed: 'danger',
  brightGreen: 'syntax.code',
  brightYellow: 'syntax.list',
  brightBlue: 'accent',
  brightMagenta: 'accent',
  brightCyan: 'syntax.code',
  brightWhite: 'bg.editor',
}

export function terminalTheme(palette: Palette): TerminalTheme {
  const theme = {} as TerminalTheme
  for (const key of Object.keys(terminalThemeTokens) as (keyof TerminalTheme)[]) {
    theme[key] = palette[terminalThemeTokens[key]]
  }
  return theme
}
