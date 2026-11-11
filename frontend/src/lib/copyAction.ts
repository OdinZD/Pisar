/** Where the keyboard focus is when the user chooses Copy, as far as Copy cares. */
export type CopyFocus =
  | { kind: 'terminal'; selection: string } // selection: xterm.js' own selection, '' if none
  | { kind: 'field'; selection: string } // an input field; selection: its highlighted text, '' if none
  | { kind: 'other' }

/** The two service calls Copy can end in (EditorService.Copy and EditorService.CopyText). */
export interface CopyPorts {
  /** Yanks Neovim's visual selection; false when Neovim is not in a visual mode. */
  copy(): Promise<boolean>
  copyText(text: string): Promise<void>
}

/**
 * What the Copy menu item does. In the terminal the visual selection of Neovim wins; only when there
 * is none, the terminal's own selection (made with the mouse) is copied. If copy() fails the error
 * propagates and nothing else is copied: Neovim's state is unknown, and a stale mouse selection
 * would put the wrong text on the clipboard.
 */
export async function performCopy(focus: CopyFocus, ports: CopyPorts): Promise<void> {
  switch (focus.kind) {
    case 'terminal':
      if (await ports.copy()) return
      if (focus.selection !== '') await ports.copyText(focus.selection)
      return
    case 'field':
      if (focus.selection !== '') await ports.copyText(focus.selection)
      return
    case 'other':
      return
  }
}
