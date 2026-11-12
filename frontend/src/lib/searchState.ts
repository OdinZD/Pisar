// State of the search panel as pure functions. The controller (searchController.svelte.ts) holds the
// state, subscribes to the events and calls the services; every transition here returns the new
// state and the effects the controller has to run. Nothing here touches Wails, timers or the DOM.
import { strings } from '../strings'
import type { ErrorCode } from './apiError'
import { errorMessage } from './errorMessage'
import { highlightSegments, type Segment } from './searchHighlight'

/** One result. The generated `Hit` fits; `matches` may be null there, which means no highlight. */
export interface SearchHit {
  projectId: string
  projectName: string
  rel: string
  /** file name with extension */
  name: string
  kind: string
  matches?: ReadonlyArray<ReadonlyArray<number>> | null
}

/** The project the search is narrowed to. */
export interface SearchScope {
  projectId: string
  name: string
}

/** A search that has been asked for and whose answer is still awaited (or whose call is not made yet). */
export interface PendingSearch {
  token: number
  query: string
  /** "" = all projects */
  projectId: string
}

export interface SearchState {
  open: boolean
  query: string
  /** null = all projects */
  scope: SearchScope | null
  hits: readonly SearchHit[]
  /** index into `hits`; -1 when there is nothing to select */
  selected: number
  /** only an answer carrying this token is used; null = nothing awaited */
  pending: PendingSearch | null
  /** the error to show; never `canceled` */
  error: ErrorCode | null
  /** token of the open that is in flight */
  opening: number | null
  /** the last token handed out, for searches and opens alike; survives closing, so an answer that
   *  arrives after the panel was closed and opened again can never match */
  lastToken: number
}

export const initialSearchState: SearchState = {
  open: false,
  query: '',
  scope: null,
  hits: [],
  selected: -1,
  pending: null,
  error: null,
  opening: null,
  lastToken: 0,
}

/** What the controller has to do after a transition. */
export type SearchEffect =
  /** call SearchDocuments (after the quiet time); drop the call if `pending` has another token by then */
  | { type: 'search'; token: number; query: string; projectId: string }
  | { type: 'open'; token: number; projectId: string; rel: string }
  /** the keyboard goes back to the editor (or, without a running Neovim, to the tree) */
  | { type: 'focus-terminal' }
  | { type: 'focus-input' }

export interface Transition {
  state: SearchState
  effects: SearchEffect[]
}

const unchanged = (state: SearchState): Transition => ({ state, effects: [] })

// A closed panel starts empty; the token counter is the one thing that carries over.
const fresh = (state: SearchState): SearchState => ({ ...initialSearchState, lastToken: state.lastToken })

/**
 * `ui:open-search` or `menu:search`. A closed panel opens empty; an open one keeps its query and only
 * gets the keyboard back. Both ask for the focus: a field that is already in the page takes it now.
 */
export function opened(state: SearchState): Transition {
  return { state: state.open ? state : { ...fresh(state), open: true }, effects: [{ type: 'focus-input' }] }
}

// The query or the scope changed: ask again, or drop everything when there is nothing to ask.
function retarget(state: SearchState): Transition {
  const query = state.query.trim()
  if (query === '') return { state: { ...state, hits: [], selected: -1, pending: null, error: null }, effects: [] }
  const token = state.lastToken + 1
  const pending: PendingSearch = { token, query, projectId: state.scope?.projectId ?? '' }
  // The old hits stay on screen until the answer comes, but opening is refused meanwhile (see activateSelected).
  return { state: { ...state, pending, lastToken: token, error: null }, effects: [{ type: 'search', ...pending }] }
}

export function queryChanged(state: SearchState, query: string): Transition {
  if (!state.open || query === state.query) return unchanged(state)
  return retarget({ ...state, query })
}

/**
 * Ctrl+P. `target` is the project of the active document, or null when there is none; from "all
 * projects" the key then does nothing. From a project it always goes back to "all projects".
 */
export function scopeToggled(state: SearchState, target: SearchScope | null): Transition {
  if (!state.open) return unchanged(state)
  if (state.scope !== null) return retarget({ ...state, scope: null })
  if (target === null) return unchanged(state)
  return retarget({ ...state, scope: target })
}

/** The project the scope key narrows to: the active document's, if the project is still in the list. */
export function scopeTarget(activeProjectId: string | undefined, projects: ReadonlyArray<{ id: string; name: string }>): SearchScope | null {
  if (!activeProjectId) return null
  const project = projects.find((p) => p.id === activeProjectId)
  return project ? { projectId: project.id, name: project.name } : null
}

/** The answer of a search. An answer that is not the awaited one (an older query) is dropped. */
export function searchArrived(state: SearchState, token: number, hits: readonly SearchHit[] | null): Transition {
  if (!state.open || state.pending?.token !== token) return unchanged(state)
  // The generated type allows null for an empty list.
  const list = hits ?? []
  return { state: { ...state, pending: null, error: null, hits: list, selected: list.length > 0 ? 0 : -1 }, effects: [] }
}

/** A failed search. `canceled` means the call was dropped on the way; whatever is shown stays. */
export function searchFailed(state: SearchState, token: number, code: ErrorCode): Transition {
  if (!state.open || state.pending?.token !== token) return unchanged(state)
  if (code === 'canceled') return { state: { ...state, pending: null }, effects: [] }
  return { state: { ...state, pending: null, hits: [], selected: -1, error: code }, effects: [] }
}

/** ↓ / ↑. The selection stops at the ends. */
export function selectionMoved(state: SearchState, delta: 1 | -1): Transition {
  if (!state.open || state.hits.length === 0) return unchanged(state)
  const selected = Math.min(state.hits.length - 1, Math.max(0, state.selected + delta))
  return selected === state.selected ? unchanged(state) : { state: { ...state, selected }, effects: [] }
}

/** A click on a row selects it (the click then opens it). */
export function rowSelected(state: SearchState, index: number): SearchState {
  return state.open && Number.isInteger(index) && index >= 0 && index < state.hits.length ? { ...state, selected: index } : state
}

/**
 * ↵ (or a click on a row). Refused when nothing is selected, when an open is already running, and
 * while an answer is awaited: the hits on screen then belong to an older query and the user would
 * open something they did not ask for.
 */
export function activateSelected(state: SearchState): Transition {
  const hit = state.hits[state.selected]
  if (!state.open || state.opening !== null || state.pending !== null || !hit) return unchanged(state)
  const token = state.lastToken + 1
  return {
    state: { ...state, opening: token, lastToken: token, error: null },
    effects: [{ type: 'open', token, projectId: hit.projectId, rel: hit.rel }],
  }
}

/**
 * The open has finished: `code` is null for success. Success closes the panel and hands the keyboard
 * to the editor. A failure leaves the panel as it is, with the error (nothing for `canceled`: the user
 * dismissed a dialog on purpose) and the keyboard in the field, which the editor may have taken.
 */
export function openSettled(state: SearchState, token: number, code: ErrorCode | null): Transition {
  if (!state.open || state.opening !== token) return unchanged(state)
  if (code === null) return { state: fresh(state), effects: [{ type: 'focus-terminal' }] }
  return { state: { ...state, opening: null, error: code === 'canceled' ? null : code }, effects: [{ type: 'focus-input' }] }
}

/**
 * Esc (`refocus`: the keyboard goes back to the editor) or a click elsewhere (it does not: it goes
 * where the user clicked). Query, hits and scope are dropped either way.
 */
export function dismissed(state: SearchState, refocus: boolean): Transition {
  if (!state.open) return unchanged(state)
  return { state: fresh(state), effects: refocus ? [{ type: 'focus-terminal' }] : [] }
}

// ---- what the panel draws ---------------------------------------------------------------------

export interface SearchRow {
  /** unique among the rows of one answer */
  key: string
  /** the name without extension, in plain and highlighted pieces */
  segments: Segment[]
  projectName: string
}

export interface SearchView {
  rows: SearchRow[]
  selected: number
  scopeLabel: string
  /** the line under the list: an error (`danger`) or the empty result; null = none */
  message: { text: string; danger: boolean } | null
  /** short text for the live region: the number of hits, or that there are none; "" = say nothing */
  announce: string
}

/** "1 dokument", "2 dokumenta", "5 dokumenata" (11–14 take the last form). */
export function countText(n: number): string {
  const last = n % 10
  const lastTwo = n % 100
  let form = strings.searchCountMany
  if (last === 1 && lastTwo !== 11) form = strings.searchCountOne
  else if (last >= 2 && last <= 4 && (lastTwo < 12 || lastTwo > 14)) form = strings.searchCountFew
  return form.replace('{count}', String(n))
}

export function searchView(state: SearchState): SearchView {
  const rows = state.hits.map((hit) => ({
    key: `${hit.projectId}\u0000${hit.rel}`,
    segments: highlightSegments(hit.name, hit.matches),
    projectName: hit.projectName,
  }))
  const asked = state.query.trim() !== ''
  const noResults = asked && state.pending === null && state.error === null && state.hits.length === 0
  let message: SearchView['message'] = null
  if (state.error !== null) message = { text: errorMessage(state.error), danger: true }
  else if (noResults) message = { text: strings.searchNoResults, danger: false }
  const answered = asked && state.pending === null && state.error === null
  return {
    rows,
    selected: state.selected,
    scopeLabel: state.scope?.name ?? strings.searchScopeAll,
    message,
    announce: answered ? (state.hits.length === 0 ? strings.searchNoResults : countText(state.hits.length)) : '',
  }
}
