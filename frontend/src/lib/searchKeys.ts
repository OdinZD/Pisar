export type SearchAction = { type: 'move'; delta: 1 | -1 } | { type: 'open' } | { type: 'close' } | { type: 'toggle-scope' }

export interface SearchKey {
  key: string
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
  /** an IME is composing: Enter and Esc belong to it */
  isComposing?: boolean
}

/**
 * Maps a key pressed in the search field to a panel action; null means the field handles it (typing,
 * caret movement, ⌘-shortcuts). Only exact combinations count: ⌘ is never intercepted (⌘P reaches
 * the app as a menu event), and ⌥/⇧ variants of the arrow keys and Enter keep their meaning in the
 * field. Ctrl+P is the one Ctrl shortcut; Ctrl+N / Ctrl+J are not used.
 */
export function keyToSearchAction(ev: SearchKey): SearchAction | null {
  if (ev.isComposing || ev.metaKey) return null
  if (ev.ctrlKey) {
    // Letter keys report 'p' or 'P' depending on Caps Lock; Shift would make it another shortcut.
    return !ev.altKey && !ev.shiftKey && ev.key.toLowerCase() === 'p' ? { type: 'toggle-scope' } : null
  }
  if (ev.altKey || ev.shiftKey) return null
  switch (ev.key) {
    case 'ArrowDown':
      return { type: 'move', delta: 1 }
    case 'ArrowUp':
      return { type: 'move', delta: -1 }
    case 'Enter':
      return { type: 'open' }
    case 'Escape':
      return { type: 'close' }
    default:
      return null
  }
}
