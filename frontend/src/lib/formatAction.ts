import type { CopyFocus } from './copyAction'
import type { Phase } from './editorState'

/** The formatting actions the menu can choose (the payload of the menu:format event). */
export type FormatKind = 'bold' | 'italic'

/**
 * Whether a formatting action from the menu goes to Neovim. Only when the user is writing: the
 * terminal has the keyboard and Neovim runs. In an input field (search, new name, rename) the
 * shortcut must not change the document, and neither must it from the tree, a button, the preview
 * or with no focus at all (all of them 'other').
 */
export function shouldFormat(focus: CopyFocus, phase: Phase): boolean {
  return focus.kind === 'terminal' && phase === 'running'
}
