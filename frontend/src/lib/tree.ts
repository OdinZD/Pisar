// Pure logic of the project tree: no Svelte, no Wails, no DOM. Components only render what this
// module computes and forward user input to it.
import type { ErrorCode } from './apiError'
import { errorMessage } from './errorMessage'
import { strings } from '../strings'

/** Structurally the same as the generated Node binding; `kind` is a plain string here. */
export interface TreeNode {
  name: string
  rel: string
  kind: string
  children?: TreeNode[] | null
}

export interface ProjectRef {
  id: string
  name: string
  missing: boolean
}

/** What is known about one project's tree. `nodes` stays when a refresh fails (old view is kept). */
export interface TreeState {
  nodes: TreeNode[] | null
  error: ErrorCode | null
}

/** Explicit expand/collapse choices by row key. Absent keys use the default: projects open, folders closed. */
export type Expansion = ReadonlyMap<string, boolean>

interface RowBase {
  key: string
  projectId: string
  depth: number
}
export interface ProjectRow extends RowBase {
  type: 'project'
  name: string
  expanded: boolean
  missing: boolean
  note: string | null
}
export interface DirRow extends RowBase {
  type: 'dir'
  rel: string
  name: string
  expanded: boolean
}
export interface DocRow extends RowBase {
  type: 'doc'
  rel: string
  /** file name with extension, as on disk */
  fileName: string
  /** shown name, without extension */
  title: string
  kindLabel: string
}
export interface MessageRow extends RowBase {
  type: 'message'
  text: string
  tone: 'error' | 'muted'
}
/** Rows that can be selected. */
export type ItemRow = ProjectRow | DirRow | DocRow
export type Row = ItemRow | MessageRow

export interface InputRow extends RowBase {
  type: 'input'
  mode: 'create' | 'rename'
  /** text the field starts with (empty for a new document) */
  initial: string
}
export interface ConfirmRow extends RowBase {
  type: 'confirm'
}
export type DisplayRow = Row | InputRow | ConfirmRow

export function rowKey(projectId: string, rel: string): string {
  // NUL cannot occur in a project id or a path, so keys never collide.
  return `${projectId}\u0000${rel}`
}

export function isItemRow(row: { type: string }): row is ItemRow {
  return row.type === 'project' || row.type === 'dir' || row.type === 'doc'
}

const collator = new Intl.Collator('hr', { numeric: true, sensitivity: 'base' })

/** File name without its document extension: `v1.2.md` → `v1.2`. Folders keep their name. */
export function displayName(name: string, kind: string): string {
  if (kind === 'dir') return name
  const ext = `.${kind}`
  return name.toLowerCase().endsWith(ext) ? name.slice(0, name.length - ext.length) : name
}

/** `md` → `MD`. */
export function kindLabel(kind: string): string {
  return kind.toUpperCase()
}

/** Folders first, then Croatian alphabetical + natural numbers by shown name, ties by full name. */
export function sortNodes(nodes: readonly TreeNode[]): TreeNode[] {
  return [...nodes].sort((a, b) => {
    const dirA = a.kind === 'dir'
    if (dirA !== (b.kind === 'dir')) return dirA ? -1 : 1
    return (
      collator.compare(displayName(a.name, a.kind), displayName(b.name, b.kind)) ||
      collator.compare(a.name, b.name) ||
      // Collator ties (e.g. "a" vs "A") still need a stable, deterministic order.
      (a.name < b.name ? -1 : a.name > b.name ? 1 : 0)
    )
  })
}

export function isExpanded(expansion: Expansion, key: string, defaultValue: boolean): boolean {
  return expansion.get(key) ?? defaultValue
}

function pushNodes(
  out: Row[],
  projectId: string,
  nodes: readonly TreeNode[],
  depth: number,
  expansion: Expansion,
): void {
  for (const node of sortNodes(nodes)) {
    const key = rowKey(projectId, node.rel)
    if (node.kind === 'dir') {
      const expanded = isExpanded(expansion, key, false)
      out.push({ type: 'dir', key, projectId, depth, rel: node.rel, name: node.name, expanded })
      if (expanded) pushNodes(out, projectId, node.children ?? [], depth + 1, expansion)
    } else {
      out.push({
        type: 'doc',
        key,
        projectId,
        depth,
        rel: node.rel,
        fileName: node.name,
        title: displayName(node.name, node.kind),
        kindLabel: kindLabel(node.kind),
      })
    }
  }
}

/** The visible rows, top to bottom. A project without an entry in `trees` has not loaded yet. */
export function buildRows(
  projects: readonly ProjectRef[],
  trees: ReadonlyMap<string, TreeState>,
  expansion: Expansion,
): Row[] {
  const out: Row[] = []
  for (const p of projects) {
    const key = rowKey(p.id, '')
    const expanded = !p.missing && isExpanded(expansion, key, true)
    out.push({
      type: 'project',
      key,
      projectId: p.id,
      depth: 0,
      name: p.name,
      expanded,
      missing: p.missing,
      note: p.missing ? strings.projectMissingNote : null,
    })
    if (!expanded) continue
    const tree = trees.get(p.id)
    if (!tree) continue
    if (tree.error) {
      out.push({
        type: 'message',
        key: `${key}error`,
        projectId: p.id,
        depth: 1,
        text: errorMessage(tree.error),
        tone: 'error',
      })
    }
    pushNodes(out, p.id, tree.nodes ?? [], 1, expansion)
  }
  return out
}

/** The next selection for j/k (delta ±1). Stays at the ends; with no selection picks the first row. */
export function moveSelection(rows: readonly Row[], selectedKey: string | null, delta: 1 | -1): string | null {
  const items = rows.filter(isItemRow)
  if (items.length === 0) return null
  const i = items.findIndex((r) => r.key === selectedKey)
  if (i < 0) return items[0].key
  return items[Math.min(items.length - 1, Math.max(0, i + delta))].key
}

export function parentRel(rel: string): string {
  const i = rel.lastIndexOf('/')
  return i < 0 ? '' : rel.slice(0, i)
}

export interface CreateTarget {
  projectId: string
  /** "" is the project root */
  dirRel: string
}

/**
 * Where a new document goes: the selected folder; the parent folder of a selected document; the
 * root of a selected project. Without a usable selection: the root of the first available project.
 */
export function newDocumentTarget(
  rows: readonly Row[],
  selectedKey: string | null,
  projects: readonly ProjectRef[],
): CreateTarget | null {
  const sel = rows.find((r): r is ItemRow => isItemRow(r) && r.key === selectedKey)
  if (sel?.type === 'dir') return { projectId: sel.projectId, dirRel: sel.rel }
  if (sel?.type === 'doc') return { projectId: sel.projectId, dirRel: parentRel(sel.rel) }
  if (sel?.type === 'project' && !sel.missing) return { projectId: sel.projectId, dirRel: '' }
  const first = projects.find((p) => !p.missing)
  return first ? { projectId: first.id, dirRel: '' } : null
}

/** Marks the rows of a target (and its project) as expanded so the new-document field is visible. */
export function expandForTarget(expansion: Expansion, target: CreateTarget): Expansion {
  const next = new Map(expansion)
  next.set(rowKey(target.projectId, ''), true)
  if (target.dirRel !== '') next.set(rowKey(target.projectId, target.dirRel), true)
  return next
}

export function toggleExpansion(expansion: Expansion, row: ProjectRow | DirRow): Expansion {
  const next = new Map(expansion)
  next.set(row.key, !row.expanded)
  return next
}

/**
 * Selection after the rows changed. Same key stays. A row that disappeared hands the selection to
 * the row now at its old position (or the last one). A selection that was not visible before and is
 * not visible now is dropped.
 */
export function reconcileSelection(
  before: readonly Row[],
  after: readonly Row[],
  selectedKey: string | null,
): string | null {
  if (selectedKey === null) return null
  const items = after.filter(isItemRow)
  if (items.some((r) => r.key === selectedKey)) return selectedKey
  const old = before.filter(isItemRow).findIndex((r) => r.key === selectedKey)
  if (old < 0 || items.length === 0) return null
  return items[Math.min(old, items.length - 1)].key
}

/** What the user is currently editing or being asked, drawn between the tree rows. */
export interface Overlay {
  create?: CreateTarget & { error: string | null }
  rename?: { key: string; error: string | null }
  confirmRemove?: string
  notice?: { key: string; text: string }
}

function overlayMessage(row: RowBase, suffix: string, text: string): MessageRow {
  return { type: 'message', key: `${row.key}${suffix}`, projectId: row.projectId, depth: row.depth, text, tone: 'error' }
}

/** Rows plus the transient rows of the overlay. Navigation uses `rows`, rendering uses this. */
export function withOverlay(rows: readonly Row[], overlay: Overlay): DisplayRow[] {
  const out: DisplayRow[] = []
  for (const row of rows) {
    if (row.type === 'doc' && overlay.rename?.key === row.key) {
      out.push({ type: 'input', mode: 'rename', key: `${row.key}input`, projectId: row.projectId, depth: row.depth, initial: row.title })
      if (overlay.rename.error) out.push(overlayMessage(row, 'rename-error', overlay.rename.error))
      continue
    }
    out.push(row)
    const { create } = overlay
    const createHere =
      create &&
      create.projectId === row.projectId &&
      ((row.type === 'project' && create.dirRel === '') || (row.type === 'dir' && row.rel === create.dirRel))
    if (createHere) {
      const depth = row.depth + 1
      const input: InputRow = { type: 'input', mode: 'create', key: `${row.key}create`, projectId: row.projectId, depth, initial: '' }
      out.push(input)
      if (create.error) out.push(overlayMessage(input, 'error', create.error))
    }
    if (row.type === 'project' && overlay.confirmRemove === row.projectId) {
      out.push({ type: 'confirm', key: `${row.key}confirm`, projectId: row.projectId, depth: row.depth + 1 })
    }
    if (overlay.notice?.key === row.key) {
      out.push(overlayMessage({ ...row, depth: row.depth + 1 }, 'notice', overlay.notice.text))
    }
  }
  return out
}

/** Drops overlay parts whose row is gone (the folder or document vanished while editing). */
export function pruneOverlay(rows: readonly Row[], overlay: Overlay): Overlay {
  const { create, rename, confirmRemove, notice } = overlay
  const has = (key: string) => rows.some((r) => r.key === key)
  const next: Overlay = {}
  if (create && has(rowKey(create.projectId, create.dirRel))) next.create = create
  if (rename && has(rename.key)) next.rename = rename
  if (confirmRemove !== undefined && has(rowKey(confirmRemove, ''))) next.confirmRemove = confirmRemove
  if (notice && has(notice.key)) next.notice = notice
  return next
}

export interface ToolbarInfo {
  title: string
  /** "Project / folder / subfolder"; empty when no document is selected */
  path: string
}

/** Toolbar text for the selected row: only a document shows its name and location. */
export function toolbarInfo(
  projects: readonly ProjectRef[],
  rows: readonly Row[],
  selectedKey: string | null,
  appName: string,
): ToolbarInfo {
  const doc = rows.find((r): r is DocRow => r.type === 'doc' && r.key === selectedKey)
  if (!doc) return { title: appName, path: '' }
  const project = projects.find((p) => p.id === doc.projectId)
  const folders = doc.rel.split('/').slice(0, -1)
  return { title: doc.title, path: [project?.name ?? '', ...folders].filter((s) => s !== '').join(' / ') }
}
