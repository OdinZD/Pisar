// State machine of the editor column as pure functions. The controller (editorController.svelte.ts)
// holds the state and runs the effects these functions return; nothing here touches Wails or the DOM.
//
// Phases:
//   idle      Neovim is not running; the column shows the empty state
//   starting  the terminal is mounted and Start is in flight
//   running   Neovim is up
//   failed    Start failed; `error` holds the stable error code
import type { ErrorCode } from './apiError'

export type Phase = 'idle' | 'starting' | 'running' | 'failed'

/** The payload of the document:active event. All fields empty means a buffer without a file. */
export interface ActiveDocument {
  projectId: string
  rel: string
  path: string
  name: string
}

/** The payload of the document:state event. */
export interface DocumentModified {
  path: string
  modified: boolean
}

/** A request to open a document. `id` lets the controller settle the promise it handed to the caller. */
export interface OpenRequest {
  id: number
  projectId: string
  rel: string
}

export interface EditorState {
  phase: Phase
  /** set only in `failed` */
  error: ErrorCode | null
  /** an open request that waits for the editor to run; only the newest one is kept */
  pendingOpen: OpenRequest | null
  active: ActiveDocument | null
  /** the modified flag of `active`; false while there is no active document */
  modified: boolean
}

export type DropReason =
  /** a newer request replaced it; the caller's intent was overridden, not failed */
  | 'superseded'
  /** Start failed (the code is in the new state's `error`) */
  | 'start-failed'
  /** Neovim exited before it could run */
  | 'exited'

export interface DroppedRequest {
  request: OpenRequest
  reason: DropReason
}

/** The new state plus what the controller has to do about open requests. */
export interface Transition {
  state: EditorState
  /** open this document now (the editor runs) */
  run: OpenRequest | null
  dropped: DroppedRequest[]
}

export const initialState: EditorState = {
  phase: 'idle',
  error: null,
  pendingOpen: null,
  active: null,
  modified: false,
}

const same = (state: EditorState): Transition => ({ state, run: null, dropped: [] })

/** `idle` and `failed` go to `starting`; in `starting` and `running` nothing changes. */
export function requestStart(state: EditorState): Transition {
  if (state.phase !== 'idle' && state.phase !== 'failed') return same(state)
  return same({ ...state, phase: 'starting', error: null })
}

/** Start returned. A stale answer (the editor exited meanwhile) changes nothing. */
export function startSucceeded(state: EditorState): Transition {
  if (state.phase !== 'starting') return same(state)
  return { state: { ...state, phase: 'running', pendingOpen: null }, run: state.pendingOpen, dropped: [] }
}

/** Start failed; a waiting open request cannot be fulfilled and is dropped. */
export function startFailed(state: EditorState, code: ErrorCode): Transition {
  if (state.phase !== 'starting') return same(state)
  const dropped: DroppedRequest[] = state.pendingOpen ? [{ request: state.pendingOpen, reason: 'start-failed' }] : []
  return {
    state: { ...state, phase: 'failed', error: code, pendingOpen: null, active: null, modified: false },
    run: null,
    dropped,
  }
}

/** The nvim:exit event: back to the empty state from any phase. */
export function nvimExited(state: EditorState): Transition {
  const dropped: DroppedRequest[] = state.pendingOpen ? [{ request: state.pendingOpen, reason: 'exited' }] : []
  return { state: { ...initialState }, run: null, dropped }
}

/**
 * A request to open a document. While the editor runs it is executed at once; otherwise it is kept
 * (replacing an older one) and the editor is started if it is not already starting.
 */
export function requestOpen(state: EditorState, request: OpenRequest): Transition {
  if (state.phase === 'running') return { state, run: request, dropped: [] }
  const starting = requestStart(state).state
  const dropped: DroppedRequest[] = state.pendingOpen ? [{ request: state.pendingOpen, reason: 'superseded' }] : []
  return { state: { ...starting, pendingOpen: request }, run: null, dropped }
}

const isEmptyDocument = (d: ActiveDocument) => d.projectId === '' && d.rel === '' && d.path === '' && d.name === ''

/** The document:active event. The old modified flag belongs to the old document, so it is cleared. */
export function documentActive(state: EditorState, doc: ActiveDocument): EditorState {
  return { ...state, active: isEmptyDocument(doc) ? null : { ...doc }, modified: false }
}

/** The document:state event; it only counts for the path of the active document. */
export function documentState(state: EditorState, change: DocumentModified): EditorState {
  if (state.active === null || state.active.path !== change.path) return state
  return { ...state, modified: change.modified }
}
