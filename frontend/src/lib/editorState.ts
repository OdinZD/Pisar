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

/**
 * The active document as the state keeps it. For a Word document `path`, `rel` and `name` are those
 * of the user's .docx or .doc, never of the Markdown working copy Neovim edits.
 */
export interface ActiveDocument {
  projectId: string
  rel: string
  path: string
  name: string
  /** codes of the reasons why the document must not be saved; empty when it may be */
  readOnly: string[]
  /** the document came from a .doc, so its first save makes a new .docx */
  imported: boolean
  /** the working copy has text that is not in the user's file yet */
  unexported: boolean
  /** codes of the direct formatting that saving would flatten; empty when there is none or after a save */
  simplified: string[]
}

/**
 * The payload of the document:active event. All fields empty means a buffer without a file. The
 * generated type allows `readOnly: null` and `simplified: null`; each means the same as an empty list.
 */
export interface DocumentActiveEvent {
  projectId: string
  rel: string
  path: string
  name: string
  readOnly?: string[] | null
  imported?: boolean
  unexported?: boolean
  simplified?: string[] | null
}

/** The payload of the document:export event. */
export interface DocumentExportEvent {
  path: string
  phase: string
  code: string
}

/**
 * The last export step of a document that is still of interest, by the path of the user's file. A
 * finished export (`saved`, `reloaded`) leaves no record.
 */
export interface ExportRecord {
  phase: 'converting' | 'failed' | 'canceled'
  /** the error code of a `failed` export, otherwise "" */
  code: string
}

/** The payload of the document:state event. */
export interface DocumentModified {
  path: string
  modified: boolean
}

/**
 * A request to open a document. `id` lets the controller settle the promise it handed to the caller.
 * `userLua` asks for the user's own user.lua instead of a project document; `projectId` and `rel` are
 * then ''. Every transition treats it like any other request, and with an empty `rel` it is not a
 * Word document (see isWordRel), so it never marks a conversion.
 */
export interface OpenRequest {
  id: number
  projectId: string
  rel: string
  userLua?: true
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
  /** export records by path of the user's file; kept across nvim:exit, an export can outlast Neovim */
  exports: ReadonlyMap<string, ExportRecord>
  /** ids of the open requests for a Word document that are executing right now */
  wordOpens: readonly number[]
  /** SaveCopy is in flight */
  savingCopy: boolean
  /** the error of the last SaveCopy; null when it succeeded, was canceled, or a newer document came */
  copyError: ErrorCode | null
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
  exports: new Map(),
  wordOpens: [],
  savingCopy: false,
  copyError: null,
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
  // Export records stay: the export of a document can still be running (and fail) after Neovim is gone.
  return { state: { ...initialState, exports: state.exports }, run: null, dropped }
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

const isEmptyDocument = (d: DocumentActiveEvent) => d.projectId === '' && d.rel === '' && d.path === '' && d.name === ''

/**
 * The document:active event. The old modified flag belongs to the old document, so it is cleared;
 * so is the error of the last SaveCopy. Export records are per path and stay.
 */
export function documentActive(state: EditorState, doc: DocumentActiveEvent): EditorState {
  const active: ActiveDocument | null = isEmptyDocument(doc)
    ? null
    : {
        projectId: doc.projectId,
        rel: doc.rel,
        path: doc.path,
        name: doc.name,
        readOnly: [...(doc.readOnly ?? [])],
        imported: doc.imported ?? false,
        unexported: doc.unexported ?? false,
        simplified: [...(doc.simplified ?? [])],
      }
  return { ...state, active, modified: false, copyError: null }
}

/** The document:state event; it only counts for the path of the active document. */
export function documentState(state: EditorState, change: DocumentModified): EditorState {
  if (state.active === null || state.active.path !== change.path) return state
  return { ...state, modified: change.modified }
}

/**
 * The document:export event. Exports run one at a time, so every end of an export (anything but
 * `converting`) also drops all `converting` records: after a .doc is saved as a new .docx the
 * `converting` record sits under the old name and nothing else would ever remove it.
 */
export function documentExport(state: EditorState, ev: DocumentExportEvent): EditorState {
  const next = new Map(state.exports)
  switch (ev.phase) {
    case 'converting':
      next.set(ev.path, { phase: 'converting', code: '' })
      return { ...state, exports: next }
    case 'saved':
    case 'reloaded':
    case 'failed':
    case 'canceled':
      for (const [path, record] of next) if (record.phase === 'converting') next.delete(path)
      break
    default:
      return state // a phase this version does not know
  }
  if (ev.phase === 'failed') next.set(ev.path, { phase: 'failed', code: ev.code })
  else if (ev.phase === 'canceled') next.set(ev.path, { phase: 'canceled', code: '' })
  else next.delete(ev.path)
  // What was saved is in the user's file now, and the formatting that the save simplifies is
  // simplified; document:active does not come again after a plain save.
  const saved =
    ev.phase === 'saved' && state.active !== null && state.active.path === ev.path && (state.active.unexported || state.active.simplified.length > 0)
  return { ...state, exports: next, active: saved && state.active ? { ...state.active, unexported: false, simplified: [] } : state.active }
}

/** Word documents are the ones whose opening runs a conversion. */
export function isWordRel(rel: string): boolean {
  return /\.docx?$/i.test(rel)
}

/** An open request starts executing: for a Word document the toolbar says that the conversion runs. */
export function openExecuting(state: EditorState, request: OpenRequest): EditorState {
  if (!isWordRel(request.rel)) return state
  return { ...state, wordOpens: [...state.wordOpens, request.id] }
}

/** The open request is settled, successfully or not. */
export function openSettled(state: EditorState, id: number): EditorState {
  if (!state.wordOpens.includes(id)) return state
  return { ...state, wordOpens: state.wordOpens.filter((x) => x !== id) }
}

/** SaveCopy was called. */
export function copyStarted(state: EditorState): EditorState {
  return { ...state, savingCopy: true, copyError: null }
}

/** SaveCopy returned; `code` is null on success. Closing the dialog (`canceled`) is not an error. */
export function copyFinished(state: EditorState, code: ErrorCode | null): EditorState {
  return { ...state, savingCopy: false, copyError: code === 'canceled' ? null : code }
}
