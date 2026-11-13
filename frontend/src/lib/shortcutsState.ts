// State of the shortcut overview as pure functions. The controller (shortcutsController.svelte.ts)
// holds the state, subscribes to the events and calls the services; every transition here returns the
// new state and the effects the controller has to run. Nothing here touches Wails or the DOM.
import type { Keymaps, MenuShortcut } from '../../bindings/Pisar/models'
import { strings } from '../strings'
import type { SectionsInput } from './shortcuts'

/** The answers that are awaited; only an answer carrying the token is used. null = nothing awaited. */
export interface PendingLoads {
  menu: number | null
  keymaps: number | null
}

export interface ShortcutsState {
  open: boolean
  menu: readonly MenuShortcut[]
  keymaps: Keymaps | null
  /** the call for the Neovim shortcuts failed */
  nvimFailed: boolean
  pending: PendingLoads
  /** the last token handed out; survives closing, so an answer that arrives after the panel was
   *  closed and opened again can never match (the same as in searchState.ts) */
  lastToken: number
}

export const initialShortcutsState: ShortcutsState = {
  open: false,
  menu: [],
  keymaps: null,
  nvimFailed: false,
  pending: { menu: null, keymaps: null },
  lastToken: 0,
}

export type ShortcutsEffect =
  | { type: 'load-menu'; token: number }
  | { type: 'load-keymaps'; token: number }
  | { type: 'focus-panel' }
  /** the keyboard goes back to the editor (or, without a running Neovim, to the tree) */
  | { type: 'focus-terminal' }

export interface Transition {
  state: ShortcutsState
  effects: ShortcutsEffect[]
}

const unchanged = (state: ShortcutsState): Transition => ({ state, effects: [] })

// A closed panel holds nothing; the token counter is the one thing that carries over.
const fresh = (state: ShortcutsState): ShortcutsState => ({ ...initialShortcutsState, lastToken: state.lastToken })

/**
 * `menu:shortcuts` or `ui:open-shortcuts`: both toggle. Opening asks for the menu list and, when
 * Neovim runs, for its shortcuts; closing hands the keyboard back.
 */
export function toggled(state: ShortcutsState, nvimRunning: boolean): Transition {
  if (state.open) return { state: fresh(state), effects: [{ type: 'focus-terminal' }] }
  const menuToken = state.lastToken + 1
  const keymapsToken = nvimRunning ? menuToken + 1 : null
  const effects: ShortcutsEffect[] = [{ type: 'load-menu', token: menuToken }]
  if (keymapsToken !== null) effects.push({ type: 'load-keymaps', token: keymapsToken })
  effects.push({ type: 'focus-panel' })
  return {
    state: { ...fresh(state), open: true, pending: { menu: menuToken, keymaps: keymapsToken }, lastToken: keymapsToken ?? menuToken },
    effects,
  }
}

/**
 * Esc (`refocus`: the keyboard goes back to the editor) or a click or focus elsewhere (it does not:
 * it goes where the user put it).
 */
export function dismissed(state: ShortcutsState, refocus: boolean): Transition {
  if (!state.open) return unchanged(state)
  return { state: fresh(state), effects: refocus ? [{ type: 'focus-terminal' }] : [] }
}

/** The menu list arrived. The generated type allows null for an empty list. */
export function menuArrived(state: ShortcutsState, token: number, list: readonly MenuShortcut[] | null): Transition {
  if (!state.open || state.pending.menu !== token) return unchanged(state)
  return { state: { ...state, menu: list ?? [], pending: { ...state.pending, menu: null } }, effects: [] }
}

export function keymapsArrived(state: ShortcutsState, token: number, keymaps: Keymaps): Transition {
  if (!state.open || state.pending.keymaps !== token) return unchanged(state)
  return { state: { ...state, keymaps, nvimFailed: false, pending: { ...state.pending, keymaps: null } }, effects: [] }
}

export function keymapsFailed(state: ShortcutsState, token: number): Transition {
  if (!state.open || state.pending.keymaps !== token) return unchanged(state)
  return { state: { ...state, keymaps: null, nvimFailed: true, pending: { ...state.pending, keymaps: null } }, effects: [] }
}

/**
 * What the sections are built from. The Neovim section carries the note when Neovim is not running
 * or when it could not answer; while its answer is awaited it is simply empty, and the note would
 * only flash.
 */
export function shortcutsView(state: ShortcutsState, nvimRunning: boolean): SectionsInput {
  const unavailable: SectionsInput = { menu: state.menu, keymaps: null, nvimNote: strings.shortcutsNvimUnavailable }
  if (!nvimRunning || state.nvimFailed) return unavailable
  if (state.keymaps !== null) return { menu: state.menu, keymaps: state.keymaps, nvimNote: null }
  // Neovim started after the panel was opened: nothing was asked, so nothing is awaited either.
  if (state.pending.keymaps === null) return unavailable
  return { menu: state.menu, keymaps: null, nvimNote: null }
}
