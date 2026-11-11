// Pure logic that connects the tree with the editor: what Enter or a click does to a row, which row
// is the document Neovim has open, and what the toolbar says about it. No Svelte, Wails or DOM.
import { strings } from '../strings'
import type { ActiveDocument } from './editorState'
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

/** Kinds Neovim can open in this phase. `docx` and `doc` need the converter, which comes later. */
const openableKinds: ReadonlySet<string> = new Set(['md', 'txt'])

export function isOpenableKind(kind: string): boolean {
  return openableKinds.has(kind)
}

/** What Enter or a click on a row does. */
export type ActivateAction =
  | { type: 'toggle' }
  | { type: 'open'; projectId: string; rel: string }
  /** a document the editor cannot open yet; the service is not called */
  | { type: 'unsupported' }

export function activateAction(row: ItemRow): ActivateAction {
  if (row.type !== 'doc') return { type: 'toggle' }
  return isOpenableKind(row.kind) ? { type: 'open', projectId: row.projectId, rel: row.rel } : { type: 'unsupported' }
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
  /** "Izmijenjeno" or "Spremljeno"; null while there is no active document */
  stateText: string | null
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

/** Toolbar content for the document Neovim has open. */
export function toolbarInfo(
  projects: readonly ProjectRef[],
  active: ActiveDocument | null,
  modified: boolean,
  appName: string,
): ToolbarInfo {
  if (active === null) return { title: appName, path: '', stateText: null, modified: false }
  const stateText = modified ? strings.documentModified : strings.documentSaved
  const name = active.name !== '' ? active.name : active.rel.split('/').pop() ?? ''
  const title = stripExtension(name)
  if (active.projectId === '') return { title, path: dirname(active.path), stateText, modified }
  const project = projects.find((p) => p.id === active.projectId)
  const folders = active.rel.split('/').slice(0, -1)
  return { title, path: [project?.name ?? '', ...folders].filter((s) => s !== '').join(' / '), stateText, modified }
}
