// Pure logic that connects the tree with the editor: what Enter or a click does to a row, which row
// is the document Neovim has open, and what the toolbar says about it. No Svelte, Wails or DOM.
import { strings } from '../strings'
import { errorCode, knownErrorCode, type ErrorCode } from './apiError'
import { errorMessage } from './errorMessage'
import type { ActiveDocument, ExportRecord } from './editorState'
import {
  isExpanded,
  isItemRow,
  parentRel,
  rowKey,
  type Expansion,
  type ItemRow,
  type ProjectRef,
  type Row,
} from './tree'

/** Every kind the tree shows; Word documents are opened through their Markdown working copy. */
const openableKinds: ReadonlySet<string> = new Set(['md', 'txt', 'docx', 'doc'])

export function isOpenableKind(kind: string): boolean {
  return openableKinds.has(kind)
}

/** What Enter or a click on a row does. */
export type ActivateAction =
  | { type: 'toggle' }
  | { type: 'open'; projectId: string; rel: string }

export function activateAction(row: ItemRow): ActivateAction {
  if (row.type !== 'doc') return { type: 'toggle' }
  return { type: 'open', projectId: row.projectId, rel: row.rel }
}

/**
 * The notice under a row whose opening failed; null when there is nothing to tell. Closing the
 * recovery dialog (`canceled`) is the user's own choice, not a failure.
 */
export function openFailureText(err: unknown): string | null {
  const code = errorCode(err)
  return code === 'canceled' ? null : errorMessage(code)
}

/**
 * The row key of the active document, or null when no row can stand for it: no document, or one
 * outside the projects (empty project id), which the tree does not show.
 */
export function activeRowKey(active: ActiveDocument | null): string | null {
  if (active === null || active.projectId === '' || active.rel === '') return null
  return rowKey(active.projectId, active.rel)
}

/** A row is active only if both the project and the path match: the same path in another project is not. */
export function isActiveRow(row: Row, active: ActiveDocument | null): boolean {
  return row.type === 'doc' && row.key === activeRowKey(active)
}

/**
 * The expansion in which the active document is visible: its project and every folder up to it are
 * open. Choices for other rows stay; the same object is returned when nothing has to change.
 */
export function revealExpansion(expansion: Expansion, active: ActiveDocument | null): Expansion {
  if (activeRowKey(active) === null || active === null) return expansion
  const wanted: [string, boolean][] = [[rowKey(active.projectId, ''), true]]
  const folders = parentRel(active.rel).split('/')
  for (let i = 0; i < folders.length && folders[i] !== ''; i++) {
    wanted.push([rowKey(active.projectId, folders.slice(0, i + 1).join('/')), false])
  }
  // The second element is the default of that row (projects start open, folders closed).
  const closed = wanted.filter(([key, dflt]) => !isExpanded(expansion, key, dflt))
  if (closed.length === 0) return expansion
  const next = new Map(expansion)
  for (const [key] of closed) next.set(key, true)
  return next
}

/** The selection after the active document changed: its row if the tree has it, else unchanged. */
export function selectionForActive(rows: readonly Row[], selectedKey: string | null, active: ActiveDocument | null): string | null {
  const key = activeRowKey(active)
  return key !== null && rows.some((r) => isItemRow(r) && r.key === key) ? key : selectedKey
}

export interface ToolbarInfo {
  title: string
  /** "Project / folder / subfolder", or the absolute folder of a file outside the projects; empty without a document */
  path: string
  /** what the toolbar says about the document (see documentStatus); null while there is nothing to say */
  stateText: string | null
  /** the buffer has unsaved changes */
  modified: boolean
}

function stripExtension(name: string): string {
  const i = name.lastIndexOf('.')
  // A leading dot is part of the name (`.md` alone is not an empty name with an extension).
  return i > 0 ? name.slice(0, i) : name
}

function dirname(path: string): string {
  const i = path.lastIndexOf('/')
  if (i < 0) return ''
  return i === 0 ? '/' : path.slice(0, i)
}

/** What the editor controller knows about conversions, besides the active document and its modified flag. */
export interface ConversionState {
  exports: ReadonlyMap<string, ExportRecord>
  /** a Word document is being opened (converted) right now */
  opening: boolean
  /** the error of the last "Spremi kao kopiju" */
  copyError: ErrorCode | null
}

export const noConversion: ConversionState = { exports: new Map(), opening: false, copyError: null }

/** Reason codes of document:active that have their own text; anything else is shown as readOnlyOther. */
const readOnlyReasons: Record<string, string> = {
  comments: strings.readOnlyComments,
  'tracked-changes': strings.readOnlyTrackedChanges,
  'text-boxes': strings.readOnlyTextBoxes,
  tables: strings.readOnlyTables,
  'hard-links': strings.readOnlyHardLinks,
  'no-permission': strings.readOnlyNoPermission,
  locked: strings.readOnlyLocked,
  'not-regular': strings.readOnlyNotRegular,
}

/** The text of one reason code. The raw code is never shown: it is not for the user. */
export function readOnlyReasonText(code: string): string {
  return Object.hasOwn(readOnlyReasons, code) ? readOnlyReasons[code] : strings.readOnlyOther
}

/** "Samo za čitanje: razlog, razlog"; codes that map to the same text are told once. */
export function readOnlyText(codes: readonly string[]): string {
  const reasons = [...new Set(codes.map(readOnlyReasonText))]
  return `${strings.documentReadOnly}: ${reasons.join(', ')}`
}

export type StatusKind = 'converting' | 'modified' | 'failed' | 'unexported' | 'read-only' | 'saved'

/** How a status looks: muted is the quiet default, strong is heavier (not only another colour), danger is red and heavy. */
export type StatusTone = 'muted' | 'strong' | 'danger'

export interface DocumentStatus {
  kind: StatusKind
  text: string
  tone: StatusTone
  /** a second line next to the state, or null */
  note: string | null
  /** show the "Spremi kao kopiju" button */
  saveCopy: boolean
}

const tones: Record<StatusKind, StatusTone> = {
  converting: 'muted',
  modified: 'strong',
  failed: 'danger',
  unexported: 'strong',
  'read-only': 'muted',
  saved: 'muted',
}

/**
 * What the toolbar says about the document, by priority: a running conversion, unsaved changes
 * (an old export failure is no longer the topic, the next save will tell), a failed export or
 * SaveCopy, changes that are not in the document yet, read-only, saved. Null means nothing to say:
 * no document and no conversion in progress.
 */
export function documentStatus(active: ActiveDocument | null, modified: boolean, conversion: ConversionState): DocumentStatus | null {
  const record = active === null ? undefined : conversion.exports.get(active.path)
  const make = (kind: StatusKind, text: string, noteAllowed = true): DocumentStatus => ({
    kind,
    text,
    tone: tones[kind],
    note: noteAllowed && active?.imported ? strings.documentImported : null,
    saveCopy: kind === 'read-only',
  })

  if (conversion.opening || record?.phase === 'converting') return make('converting', strings.documentConverting)
  if (active === null) return null
  if (modified) return make('modified', strings.documentModified)
  const failure = conversion.copyError ?? (record?.phase === 'failed' ? knownErrorCode(record.code) : null)
  if (failure !== null) return make('failed', errorMessage(failure), false)
  if (record?.phase === 'canceled' || active.unexported) return make('unexported', strings.documentUnexported)
  if (active.readOnly.length > 0) return make('read-only', readOnlyText(active.readOnly), false)
  return make('saved', strings.documentSaved)
}

/** Toolbar content for the document Neovim has open. */
export function toolbarInfo(
  projects: readonly ProjectRef[],
  active: ActiveDocument | null,
  modified: boolean,
  appName: string,
  conversion: ConversionState = noConversion,
): ToolbarInfo {
  const status = documentStatus(active, modified, conversion)
  const stateText = status?.text ?? null
  if (active === null) return { title: appName, path: '', stateText, modified: false }
  const isModified = status?.kind === 'modified'
  const name = active.name !== '' ? active.name : active.rel.split('/').pop() ?? ''
  const title = stripExtension(name)
  if (active.projectId === '') return { title, path: dirname(active.path), stateText, modified: isModified }
  const project = projects.find((p) => p.id === active.projectId)
  const folders = active.rel.split('/').slice(0, -1)
  return { title, path: [project?.name ?? '', ...folders].filter((s) => s !== '').join(' / '), stateText, modified: isModified }
}
