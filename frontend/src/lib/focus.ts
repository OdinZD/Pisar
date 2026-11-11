import type { CopyFocus } from './copyAction'

/** The part of a DOM element the focus decisions read; HTMLInputElement and HTMLTextAreaElement fit. */
export interface ElementLike {
  tagName: string
  value?: string
  selectionStart?: number | null
  selectionEnd?: number | null
}

/** What Terminal.svelte reports about itself. */
export interface TerminalFocus {
  focused: boolean
  selection: string
}

export const noTerminalFocus: TerminalFocus = { focused: false, selection: '' }

function isTextField(el: ElementLike): boolean {
  return el.tagName === 'INPUT' || el.tagName === 'TEXTAREA'
}

/**
 * Classifies the focus for Copy. The terminal is asked first: xterm.js keeps its keyboard input in a
 * hidden textarea, which would otherwise look like an input field.
 */
export function describeFocus(active: ElementLike | null, terminal: TerminalFocus): CopyFocus {
  if (terminal.focused) return { kind: 'terminal', selection: terminal.selection }
  if (active && isTextField(active)) {
    const { value = '', selectionStart, selectionEnd } = active
    // selectionStart is null for input types without a selection (checkbox, ...).
    const selection = selectionStart != null && selectionEnd != null ? value.slice(selectionStart, selectionEnd) : ''
    return { kind: 'field', selection }
  }
  return { kind: 'other' }
}

/**
 * After Neovim has ended the terminal disappears and the keyboard would be left on nothing. The tree
 * takes the focus, unless the user is typing a name into one of its fields: that must not be cut off.
 */
export function shouldFocusTreeAfterExit(active: ElementLike | null, terminal: TerminalFocus): boolean {
  // The event can arrive before the terminal is unmounted; its textarea is not the user's field.
  return terminal.focused || !(active && isTextField(active))
}
