import { activateAction, type ActivateAction } from './activeDocument'
import type { ItemRow } from './tree'

export type TreeAction =
  | { type: 'move'; delta: 1 | -1 }
  | ActivateAction
  | { type: 'new-document' }
  | { type: 'rename' }
  | { type: 'trash' }
  | { type: 'remove-project' }
  | { type: 'focus-editor' }

export interface KeyLike {
  key: string
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
}

/**
 * Maps a key press to a tree action for the currently selected row (null = nothing selected).
 * null means the tree does not handle the key. Modified keys are never handled: ⌘/Ctrl/⌥ belong
 * to the application and the system. Escape hands the keyboard to the editor (the controller
 * only does so when Neovim runs).
 */
export function keyToAction(ev: KeyLike, row: ItemRow | null, state: { confirming: boolean } = { confirming: false }): TreeAction | null {
  if (ev.metaKey || ev.ctrlKey || ev.altKey) return null
  switch (ev.key) {
    case 'Escape':
      // While the question is open Escape answers it (the controller handles that); it must not also leave the tree.
      return state.confirming ? null : { type: 'focus-editor' }
    case 'j':
    case 'ArrowDown':
      return { type: 'move', delta: 1 }
    case 'k':
    case 'ArrowUp':
      return { type: 'move', delta: -1 }
    case 'Enter':
      return row ? activateAction(row) : null
    case 'a':
      return { type: 'new-document' }
    case 'r':
      return row?.type === 'doc' ? { type: 'rename' } : null
    case 'd':
      if (row?.type === 'doc') return { type: 'trash' }
      if (row?.type === 'project') return { type: 'remove-project' }
      return null // folders cannot be deleted in this phase
    default:
      return null
  }
}
