export type ShortcutsAction = { type: 'close' } | { type: 'scroll'; delta: 1 | -1 }

export interface ShortcutsKey {
  key: string
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
  /** an IME is composing: Esc belongs to it */
  isComposing?: boolean
}

/**
 * Maps a key pressed in the overview to an action; null means the browser handles it. The list is a
 * focused container that scrolls by itself (arrows, PageUp/PageDown, Space), so only Esc and j/k
 * are taken. Only exact combinations count, and ⌘ is never intercepted: ⌘/ reaches the app as a
 * menu event.
 */
export function keyToShortcutsAction(ev: ShortcutsKey): ShortcutsAction | null {
  if (ev.isComposing || ev.metaKey || ev.ctrlKey || ev.altKey || ev.shiftKey) return null
  switch (ev.key) {
    case 'Escape':
      return { type: 'close' }
    case 'j':
      return { type: 'scroll', delta: 1 }
    case 'k':
      return { type: 'scroll', delta: -1 }
    default:
      return null
  }
}
