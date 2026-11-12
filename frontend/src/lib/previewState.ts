// State of the preview pane as pure functions. The controller (previewController.svelte.ts) holds the
// state, subscribes to the events and calls the Go service; nothing here touches Wails or the DOM.
import { strings } from '../strings'
import { sizes } from '../theme/tokens'
import { knownErrorCode, type ErrorCode } from './apiError'
import { errorMessage } from './errorMessage'

/** The payload of the preview:content event. */
export interface PreviewContentEvent {
  path: string
  state: string
  html: string
  code: string
}

/** What the pane shows. `html` is only set in the `html` state, `code` only in `error`. */
export type ShownContent =
  | { state: 'html'; path: string; html: string }
  | { state: 'unsupported'; path: string }
  | { state: 'error'; path: string; code: ErrorCode }

export interface PreviewState {
  /** the pane is open */
  open: boolean
  /** the window is narrower than the preview needs: the pane cannot be open */
  narrow: boolean
  /** the last content; null before the first one, after the pane is closed and after nvim:exit */
  content: ShownContent | null
}

export const initialPreviewState: PreviewState = { open: false, narrow: false, content: null }

/** Below this width the preview closes itself (tree + preview + the editor's minimum). */
export function isNarrow(width: number): boolean {
  return width < sizes.previewCollapseWidth
}

/**
 * The button or `ui:toggle-preview`. Opening is refused in a narrow window. Closing drops the content:
 * opening again always brings a fresh one (SetPreview(true) answers with one), and the old one could
 * belong to another document by then.
 */
export function toggled(state: PreviewState): PreviewState {
  if (state.open) return { ...state, open: false, content: null }
  return state.narrow ? state : { ...state, open: true }
}

/** Maps a preview:content event; a state this version does not know is shown as an internal error. */
export function shownContent(ev: PreviewContentEvent): ShownContent {
  switch (ev.state) {
    case 'html':
      return { state: 'html', path: ev.path, html: ev.html }
    case 'unsupported':
      return { state: 'unsupported', path: ev.path }
    case 'error':
      return { state: 'error', path: ev.path, code: knownErrorCode(ev.code) }
    default:
      return { state: 'error', path: ev.path, code: 'internal' }
  }
}

/** A content event. After SetPreview(false) one may still arrive; a closed pane ignores it. */
export function contentArrived(state: PreviewState, ev: PreviewContentEvent): PreviewState {
  if (!state.open) return state
  return { ...state, content: shownContent(ev) }
}

/** A failed SetPreview call is shown in the pane like any other error. */
export function callFailed(state: PreviewState, code: ErrorCode): PreviewState {
  if (!state.open) return state
  return { ...state, content: { state: 'error', path: '', code } }
}

/** nvim:exit: what was shown belongs to a document that is gone; the pane itself stays open. */
export function nvimExited(state: PreviewState): PreviewState {
  return state.content === null ? state : { ...state, content: null }
}

/**
 * The window width changed. Narrowing below the limit closes the pane; widening again does not
 * open it: the user decides that.
 */
export function widthChanged(state: PreviewState, width: number): PreviewState {
  const narrow = isNarrow(width)
  if (narrow) return state.open || !state.narrow ? { ...state, narrow, open: false, content: null } : state
  return state.narrow ? { ...state, narrow } : state
}

/** What the pane draws. */
export type PreviewView =
  /** nothing yet: the bare desk */
  | { kind: 'empty' }
  /** a message on the desk; `danger` for an error */
  | { kind: 'message'; text: string; danger: boolean }
  /** the page; the html goes into the frame, never into the application's DOM */
  | { kind: 'page'; html: string }

export function previewView(content: ShownContent | null): PreviewView {
  if (content === null) return { kind: 'empty' }
  switch (content.state) {
    case 'html':
      return { kind: 'page', html: content.html }
    case 'unsupported':
      return { kind: 'message', text: strings.previewUnsupported, danger: false }
    case 'error':
      return { kind: 'message', text: errorMessage(content.code), danger: true }
  }
}
