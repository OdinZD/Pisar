// Reactive state of the project tree and the service calls behind it. All decisions are made by the
// pure functions in tree.ts / treeKeys.ts; this class only holds state and talks to the Go service.
import { Events } from '@wailsio/runtime'
import { MenuNewDocument, ProjectChanged, ProjectsChanged } from '../../bindings/events'
import * as service from '../../bindings/Pisar/projectservice'
import type { ProjectInfo } from '../../bindings/Pisar/models'
import { strings } from '../strings'
import { errorCode } from './apiError'
import { errorMessage } from './errorMessage'
import { createLatestTracker } from './latest'
import {
  buildRows,
  expandForTarget,
  isItemRow,
  moveSelection,
  newDocumentTarget,
  pruneOverlay,
  reconcileSelection,
  rowKey,
  toggleExpansion,
  toolbarInfo,
  withOverlay,
  type Expansion,
  type ItemRow,
  type Overlay,
  type TreeState,
} from './tree'
import { keyToAction, type KeyLike } from './treeKeys'

const PROJECTS_KEY = '\u0000projects'

export class TreeController {
  projects = $state.raw<ProjectInfo[]>([])
  trees = $state.raw<ReadonlyMap<string, TreeState>>(new Map())
  expansion = $state.raw<Expansion>(new Map())
  selectedKey = $state<string | null>(null)
  overlay = $state.raw<Overlay>({})
  /** error of an action that has no row of its own (adding a project) */
  statusText = $state<string | null>(null)
  busy = $state(false)

  /** Set by the sidebar: gives the tree element focus again after an action. */
  focusTree: () => void = () => {}

  rows = $derived(buildRows(this.projects, this.trees, this.expansion))
  displayRows = $derived(withOverlay(this.rows, this.overlay))
  selectedRow = $derived<ItemRow | null>(this.rows.find((r): r is ItemRow => isItemRow(r) && r.key === this.selectedKey) ?? null)
  canCreate = $derived(newDocumentTarget(this.rows, this.selectedKey, this.projects) !== null)
  toolbar = $derived(toolbarInfo(this.projects, this.rows, this.selectedKey, strings.appName))

  #latest = createLatestTracker()

  /** Loads everything and subscribes to refresh events. Returns the cleanup that unsubscribes them. */
  start(): () => void {
    const offs = [
      Events.On(ProjectsChanged, () => void this.loadProjects()),
      Events.On(ProjectChanged, (ev) => void this.loadTree(String(ev.data))),
      Events.On(MenuNewDocument, () => this.startCreate()),
    ]
    // A folder that came back or vanished while the app was in the background shows up here.
    const onFocus = () => void this.loadProjects()
    window.addEventListener('focus', onFocus)
    void this.loadProjects()
    return () => {
      for (const off of offs) off()
      window.removeEventListener('focus', onFocus)
    }
  }

  // Applies a state change and keeps selection and overlay consistent with the rows it produces.
  #change(mutate: () => void): void {
    const before = this.rows
    mutate()
    const after = this.rows
    this.selectedKey = reconcileSelection(before, after, this.selectedKey)
    this.overlay = pruneOverlay(after, this.overlay)
  }

  async loadProjects(): Promise<void> {
    const token = this.#latest.begin(PROJECTS_KEY)
    let list: ProjectInfo[]
    try {
      list = (await service.Projects()) ?? []
    } catch (err) {
      console.error('loading projects failed', err)
      return
    }
    if (!this.#latest.isLatest(PROJECTS_KEY, token)) return

    const wasMissing = new Set(this.projects.filter((p) => p.missing).map((p) => p.id))
    this.#change(() => {
      this.projects = list
      const ids = new Set(list.map((p) => p.id))
      this.trees = new Map([...this.trees].filter(([id]) => ids.has(id)))
    })
    const stale = list.filter((p) => {
      if (p.missing) return false
      const tree = this.trees.get(p.id)
      return !tree || tree.error !== null || wasMissing.has(p.id)
    })
    await Promise.all(stale.map((p) => this.loadTree(p.id)))
  }

  async loadTree(id: string): Promise<void> {
    const token = this.#latest.begin(id)
    let next: TreeState
    try {
      next = { nodes: (await service.Tree(id)) ?? [], error: null }
    } catch (err) {
      // Keep the previous nodes so a failed refresh does not blank the tree.
      next = { nodes: this.trees.get(id)?.nodes ?? null, error: errorCode(err) }
    }
    if (!this.#latest.isLatest(id, token)) return
    if (!this.projects.some((p) => p.id === id)) return
    this.#change(() => {
      this.trees = new Map(this.trees).set(id, next)
    })
  }

  // Runs one service action at a time, so a double Enter cannot start it twice.
  async #exclusive(action: () => Promise<void>): Promise<void> {
    if (this.busy) return
    this.busy = true
    try {
      await action()
    } finally {
      this.busy = false
    }
  }

  #clearNotices(): void {
    this.statusText = null
    if (this.overlay.notice) this.overlay = { ...this.overlay, notice: undefined }
  }

  /** Key handling of the focused tree (not of its text fields). Returns true if the key was used. */
  handleKey(ev: KeyLike): boolean {
    if (this.overlay.confirmRemove !== undefined) {
      if (ev.metaKey || ev.ctrlKey || ev.altKey) return false
      if (ev.key === 'Enter') void this.confirmRemove()
      else if (ev.key === 'Escape') this.cancelRemove()
      return true // nothing else moves while the question is open
    }
    const action = keyToAction(ev, this.selectedRow)
    if (!action) return false
    this.#clearNotices()
    switch (action.type) {
      case 'move':
        this.selectedKey = moveSelection(this.rows, this.selectedKey, action.delta)
        break
      case 'toggle':
        this.toggleSelected()
        break
      case 'new-document':
        this.startCreate()
        break
      case 'rename':
        this.startRename()
        break
      case 'trash':
        void this.trashSelected()
        break
      case 'remove-project':
        if (this.selectedRow?.type === 'project') this.overlay = { confirmRemove: this.selectedRow.projectId }
        break
    }
    return true
  }

  toggleSelected(): void {
    const row = this.selectedRow
    if (row && (row.type === 'project' || row.type === 'dir') && !(row.type === 'project' && row.missing)) {
      this.#change(() => {
        this.expansion = toggleExpansion(this.expansion, row)
      })
    }
  }

  clickRow(row: ItemRow): void {
    this.#clearNotices()
    this.overlay = { ...this.overlay, confirmRemove: undefined }
    this.selectedKey = row.key
    this.toggleSelected()
    this.focusTree()
  }

  // ---- new document ---------------------------------------------------------------------------

  startCreate(): void {
    if (this.busy) return
    const target = newDocumentTarget(this.rows, this.selectedKey, this.projects)
    if (!target) return
    this.#clearNotices()
    this.expansion = expandForTarget(this.expansion, target)
    this.overlay = { create: { ...target, error: null } }
  }

  async submitCreate(text: string): Promise<void> {
    const edit = this.overlay.create
    if (!edit) return
    const name = text.trim()
    if (name === '') return this.cancelEdit()
    await this.#exclusive(async () => {
      try {
        const node = await service.CreateDocument(edit.projectId, edit.dirRel, name)
        if (this.overlay.create === edit) this.overlay = {}
        await this.loadTree(edit.projectId)
        this.selectedKey = rowKey(edit.projectId, node.rel)
        this.focusTree()
      } catch (err) {
        // The field stays open with its text so the name can be corrected.
        if (this.overlay.create === edit) this.overlay = { create: { ...edit, error: errorMessage(errorCode(err)) } }
      }
    })
  }

  // ---- rename ---------------------------------------------------------------------------------

  startRename(): void {
    const row = this.selectedRow
    if (this.busy || row?.type !== 'doc') return
    this.#clearNotices()
    this.overlay = { rename: { key: row.key, error: null } }
  }

  async submitRename(text: string): Promise<void> {
    const edit = this.overlay.rename
    const row = this.rows.find((r) => r.type === 'doc' && r.key === edit?.key)
    if (!edit || row?.type !== 'doc') return
    const name = text.trim()
    if (name === '' || name === row.title) return this.cancelEdit()
    await this.#exclusive(async () => {
      try {
        const node = await service.RenameDocument(row.projectId, row.rel, name)
        if (this.overlay.rename === edit) this.overlay = {}
        await this.loadTree(row.projectId)
        this.selectedKey = rowKey(row.projectId, node.rel)
        this.focusTree()
      } catch (err) {
        if (this.overlay.rename === edit) this.overlay = { rename: { ...edit, error: errorMessage(errorCode(err)) } }
      }
    })
  }

  /** Escape (refocus the tree) or focus loss (the user went elsewhere, so do not take focus back). */
  cancelEdit(refocus = true): void {
    const { create, rename } = this.overlay
    if (!create && !rename) return
    this.overlay = { ...this.overlay, create: undefined, rename: undefined }
    if (refocus) this.focusTree()
  }

  /** Typing again hides the error of the last attempt. */
  clearEditError(): void {
    const { create, rename } = this.overlay
    if (create?.error) this.overlay = { ...this.overlay, create: { ...create, error: null } }
    if (rename?.error) this.overlay = { ...this.overlay, rename: { ...rename, error: null } }
  }

  // ---- trash, remove project, add project -------------------------------------------------------

  async trashSelected(): Promise<void> {
    const row = this.selectedRow
    if (row?.type !== 'doc') return
    await this.#exclusive(async () => {
      try {
        // The Go side asks for confirmation; declining returns success and nothing changes.
        await service.TrashDocument(row.projectId, row.rel)
        // The refresh moves the selection to the neighbouring row if the document is gone.
        await this.loadTree(row.projectId)
      } catch (err) {
        this.overlay = { ...this.overlay, notice: { key: row.key, text: errorMessage(errorCode(err)) } }
      }
      this.focusTree()
    })
  }

  cancelRemove(): void {
    this.overlay = { ...this.overlay, confirmRemove: undefined }
    this.focusTree()
  }

  async confirmRemove(): Promise<void> {
    const id = this.overlay.confirmRemove
    if (id === undefined) return
    await this.#exclusive(async () => {
      try {
        await service.RemoveProject(id)
        this.overlay = { ...this.overlay, confirmRemove: undefined }
        await this.loadProjects()
      } catch (err) {
        this.overlay = {
          ...this.overlay,
          confirmRemove: undefined,
          notice: { key: rowKey(id, ''), text: errorMessage(errorCode(err)) },
        }
      }
      this.focusTree()
    })
  }

  async addProject(): Promise<void> {
    await this.#exclusive(async () => {
      this.#clearNotices()
      try {
        const info = await service.AddProject()
        // Cancelling the folder dialog is not an error: the service returns an empty project.
        if (info.id !== '') {
          await this.loadProjects()
          this.selectedKey = rowKey(info.id, '')
        }
      } catch (err) {
        this.statusText = errorMessage(errorCode(err))
      }
      this.focusTree()
    })
  }
}
