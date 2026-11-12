// Reactive state of the search panel and the service calls behind it. All decisions are made by the
// pure functions in searchState.ts / searchKeys.ts; this class holds the state, subscribes to the two
// events, waits out the quiet time and calls the services.
import { Events } from '@wailsio/runtime'
import { MenuSearch, UiOpenSearch } from '../../bindings/events'
import * as service from '../../bindings/Pisar/projectservice'
import { errorCode } from './apiError'
import type { ActiveDocument, Phase } from './editorState'
import type { SearchAction } from './searchKeys'
import {
  activateSelected,
  dismissed,
  initialSearchState,
  opened,
  openSettled,
  queryChanged,
  rowSelected,
  scopeTarget,
  scopeToggled,
  searchArrived,
  searchFailed,
  searchView,
  selectionMoved,
  type SearchEffect,
  type SearchState,
  type SearchView,
  type Transition,
} from './searchState'
import { search as searchTokens } from '../theme/tokens'

/** The part of the editor the panel needs; EditorController satisfies it. */
export interface SearchEditorPort {
  readonly phase: Phase
  readonly active: ActiveDocument | null
  open(projectId: string, rel: string): Promise<void>
  focusTerminal(): void
}

/** The part of the tree the panel needs; TreeController satisfies it. */
export interface SearchTreePort {
  readonly projects: ReadonlyArray<{ id: string; name: string }>
  focusTree(): void
}

export class SearchController {
  #state = $state.raw<SearchState>(initialSearchState)

  open = $derived<boolean>(this.#state.open)
  query = $derived<string>(this.#state.query)
  view = $derived<SearchView>(searchView(this.#state))
  /** the panel is waiting for an answer (or for the quiet time before the call) */
  loading = $derived<boolean>(this.#state.pending !== null)

  /** Set by the panel: gives the search field the keyboard (a no-op while the panel is not in the page). */
  focusInput: () => void = () => {}

  #editor: SearchEditorPort
  #tree: SearchTreePort
  #timer: ReturnType<typeof setTimeout> | null = null
  #stop: (() => void) | null = null

  constructor(editor: SearchEditorPort, tree: SearchTreePort) {
    this.#editor = editor
    this.#tree = tree
  }

  /** Subscribes to ␣f (ui:open-search) and ⌘P (menu:search); returns the cleanup that removes them. */
  start(): () => void {
    this.stop()
    const offs = [Events.On(UiOpenSearch, () => this.show()), Events.On(MenuSearch, () => this.show())]
    const cleanup = () => {
      for (const off of offs) off()
      this.#clearTimer()
      if (this.#stop === cleanup) this.#stop = null
    }
    this.#stop = cleanup
    return cleanup
  }

  stop(): void {
    this.#stop?.()
  }

  /** Opens the panel, or, when it is open, only takes the keyboard back into the field. */
  show(): void {
    this.#run(opened(this.#state))
  }

  /** The field's text changed. */
  setQuery(query: string): void {
    this.#run(queryChanged(this.#state, query))
  }

  /** A key of the field that the panel acts on (see searchKeys.ts). */
  handleAction(action: SearchAction): void {
    switch (action.type) {
      case 'move':
        this.#run(selectionMoved(this.#state, action.delta))
        break
      case 'open':
        this.#run(activateSelected(this.#state))
        break
      case 'close':
        this.dismiss(true)
        break
      case 'toggle-scope':
        this.#run(scopeToggled(this.#state, scopeTarget(this.#editor.active?.projectId, this.#tree.projects)))
        break
    }
  }

  /** A click on a row does what ↵ does on it. */
  clickRow(index: number): void {
    this.#state = rowSelected(this.#state, index)
    this.#run(activateSelected(this.#state))
  }

  /** Closes the panel; `refocus` is false when the user clicked somewhere that takes the keyboard itself. */
  dismiss(refocus: boolean): void {
    this.#run(dismissed(this.#state, refocus))
  }

  #run(t: Transition): void {
    this.#state = t.state
    // Nothing is awaited any more (closed, query emptied): the quiet time that is still running is moot.
    if (t.state.pending === null) this.#clearTimer()
    for (const effect of t.effects) this.#perform(effect)
  }

  #perform(effect: SearchEffect): void {
    switch (effect.type) {
      case 'search':
        // Only the newest request is waited for: a newer one replaces the timer, and a call that is
        // already out is dropped on arrival by its token.
        this.#clearTimer()
        this.#timer = setTimeout(() => {
          this.#timer = null
          void this.#search(effect.token, effect.query, effect.projectId)
        }, searchTokens.debounceMs)
        break
      case 'open':
        void this.#open(effect.token, effect.projectId, effect.rel)
        break
      case 'focus-terminal':
        // Without a running Neovim there is no terminal; the tree takes the keyboard, so it does not
        // fall on the page body (where no key does anything).
        if (this.#editor.phase === 'running') this.#editor.focusTerminal()
        else this.#tree.focusTree()
        break
      case 'focus-input':
        this.focusInput()
        break
    }
  }

  #clearTimer(): void {
    if (this.#timer !== null) clearTimeout(this.#timer)
    this.#timer = null
  }

  async #search(token: number, query: string, projectId: string): Promise<void> {
    // The request may have been replaced while the quiet time ran out.
    if (this.#state.pending?.token !== token) return
    try {
      const hits = await service.SearchDocuments(query, projectId)
      this.#run(searchArrived(this.#state, token, hits))
    } catch (err) {
      const code = errorCode(err)
      if (code !== 'canceled') console.error('search failed', err)
      this.#run(searchFailed(this.#state, token, code))
    }
  }

  async #open(token: number, projectId: string, rel: string): Promise<void> {
    let code: ReturnType<typeof errorCode> | null = null
    try {
      await this.#editor.open(projectId, rel)
    } catch (err) {
      code = errorCode(err)
      if (code !== 'canceled') console.error('opening from the search failed', err)
    }
    this.#run(openSettled(this.#state, token, code))
  }
}
