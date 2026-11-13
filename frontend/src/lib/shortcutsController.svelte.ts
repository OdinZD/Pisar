// Reactive state of the shortcut overview and the service calls behind it. All decisions are made by
// the pure functions in shortcutsState.ts / shortcutsKeys.ts; this class holds the state, subscribes to
// the two events and calls the services.
import { Events } from '@wailsio/runtime'
import { MenuShortcuts, UiOpenShortcuts } from '../../bindings/events'
import * as editorService from '../../bindings/Pisar/editorservice'
import * as settingsService from '../../bindings/Pisar/settingsservice'
import { errorCode } from './apiError'
import type { Phase } from './editorState'
import { buildSections, type Section } from './shortcuts'
import type { ShortcutsAction } from './shortcutsKeys'
import {
  dismissed,
  initialShortcutsState,
  keymapsArrived,
  keymapsFailed,
  menuArrived,
  shortcutsView,
  toggled,
  type ShortcutsEffect,
  type ShortcutsState,
  type Transition,
} from './shortcutsState'

/** The part of the editor the panel needs; EditorController satisfies it. */
export interface ShortcutsEditorPort {
  readonly phase: Phase
  focusTerminal(): void
}

/** The part of the tree the panel needs; TreeController satisfies it. */
export interface ShortcutsTreePort {
  focusTree(): void
}

export class ShortcutsController {
  #editor: ShortcutsEditorPort
  #tree: ShortcutsTreePort
  #state = $state.raw<ShortcutsState>(initialShortcutsState)
  #stop: (() => void) | null = null

  open = $derived<boolean>(this.#state.open)
  sections = $derived<Section[]>(buildSections(shortcutsView(this.#state, this.#nvimRunning())))

  /** Set by the panel: gives the list the keyboard (a no-op while the panel is not in the page). */
  focusPanel: () => void = () => {}
  /** Set by the panel: scrolls the list by one line up or down. */
  scrollPanel: (delta: 1 | -1) => void = () => {}

  constructor(editor: ShortcutsEditorPort, tree: ShortcutsTreePort) {
    this.#editor = editor
    this.#tree = tree
  }

  #nvimRunning(): boolean {
    return this.#editor.phase === 'running'
  }

  /** Subscribes to ⌘/ (menu:shortcuts) and ␣? (ui:open-shortcuts); returns the cleanup that removes them. */
  start(): () => void {
    this.stop()
    const offs = [Events.On(MenuShortcuts, () => this.toggle()), Events.On(UiOpenShortcuts, () => this.toggle())]
    const cleanup = () => {
      for (const off of offs) off()
      if (this.#stop === cleanup) this.#stop = null
    }
    this.#stop = cleanup
    return cleanup
  }

  stop(): void {
    this.#stop?.()
  }

  /** Opens the overview, or closes it when it is open. */
  toggle(): void {
    this.#run(toggled(this.#state, this.#nvimRunning()))
  }

  /** A key of the list that the panel acts on (see shortcutsKeys.ts). */
  handleAction(action: ShortcutsAction): void {
    switch (action.type) {
      case 'close':
        this.dismiss(true)
        break
      case 'scroll':
        this.scrollPanel(action.delta)
        break
    }
  }

  /** Closes the panel; `refocus` is false when the user clicked somewhere that takes the keyboard itself. */
  dismiss(refocus: boolean): void {
    this.#run(dismissed(this.#state, refocus))
  }

  #run(t: Transition): void {
    this.#state = t.state
    for (const effect of t.effects) this.#perform(effect)
  }

  #perform(effect: ShortcutsEffect): void {
    switch (effect.type) {
      case 'load-menu':
        void this.#loadMenu(effect.token)
        break
      case 'load-keymaps':
        void this.#loadKeymaps(effect.token)
        break
      case 'focus-panel':
        this.focusPanel()
        break
      case 'focus-terminal':
        // Without a running Neovim there is no terminal; the tree takes the keyboard, so it does not
        // fall on the page body (where no key does anything).
        if (this.#nvimRunning()) this.#editor.focusTerminal()
        else this.#tree.focusTree()
        break
    }
  }

  async #loadMenu(token: number): Promise<void> {
    try {
      const list = await settingsService.MenuShortcuts()
      this.#run(menuArrived(this.#state, token, list))
    } catch (err) {
      // The app section stays empty; the other sections do not depend on it.
      if (errorCode(err) !== 'canceled') console.error('loading the menu shortcuts failed', err)
    }
  }

  async #loadKeymaps(token: number): Promise<void> {
    try {
      const keymaps = await editorService.Keymaps()
      this.#run(keymapsArrived(this.#state, token, keymaps))
    } catch (err) {
      if (errorCode(err) !== 'canceled') console.error('loading the Neovim shortcuts failed', err)
      this.#run(keymapsFailed(this.#state, token))
    }
  }
}
