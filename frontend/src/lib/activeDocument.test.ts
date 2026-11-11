import { describe, expect, it } from 'vitest'
import {
  activateAction,
  activeRowKey,
  isActiveRow,
  isOpenableKind,
  revealExpansion,
  selectionForActive,
  toolbarInfo,
} from './activeDocument'
import type { ActiveDocument } from './editorState'
import { buildRows, isExpanded, rowKey, type DocRow, type Expansion, type ProjectRef, type TreeNode, type TreeState } from './tree'
import { strings } from '../strings'

const file = (name: string, rel = name): TreeNode => ({ name, rel, kind: name.split('.').pop()!.toLowerCase() })
const dir = (name: string, children: TreeNode[] = [], rel = name): TreeNode => ({ name, rel, kind: 'dir', children })

const p1: ProjectRef = { id: 'p1', name: 'Roman', missing: false }
const p2: ProjectRef = { id: 'p2', name: 'Blog', missing: false }

const active = (projectId: string, rel: string, path = `/home/${projectId}/${rel}`): ActiveDocument => ({
  projectId,
  rel,
  path,
  name: rel.split('/').pop()!,
})

describe('isOpenableKind and activateAction', () => {
  it('opens only md and txt in this phase', () => {
    expect(['md', 'txt', 'docx', 'doc', 'dir', ''].map(isOpenableKind)).toEqual([true, true, false, false, false, false])
  })

  const base = { key: 'k', projectId: 'p', depth: 1 }
  const doc = (kind: string, rel: string): DocRow => ({
    ...base,
    type: 'doc',
    rel,
    fileName: rel,
    title: rel,
    kind,
    kindLabel: kind.toUpperCase(),
  })

  it('opens md and txt with the project and the path', () => {
    expect(activateAction(doc('md', 'a/b.md'))).toEqual({ type: 'open', projectId: 'p', rel: 'a/b.md' })
    expect(activateAction(doc('txt', 'c.txt'))).toEqual({ type: 'open', projectId: 'p', rel: 'c.txt' })
  })

  it('does not open docx and doc', () => {
    expect(activateAction(doc('docx', 'a.docx'))).toEqual({ type: 'unsupported' })
    expect(activateAction(doc('doc', 'a.doc'))).toEqual({ type: 'unsupported' })
  })

  it('toggles projects and folders', () => {
    expect(activateAction({ ...base, type: 'project', name: 'P', expanded: true, missing: false, note: null })).toEqual({ type: 'toggle' })
    expect(activateAction({ ...base, type: 'dir', rel: 'd', name: 'd', expanded: false })).toEqual({ type: 'toggle' })
  })

  it('the unsupported notice text exists', () => {
    expect(strings.openUnsupportedNotice.trim()).not.toBe('')
  })
})

describe('active row', () => {
  const rows = buildRows(
    [p1, p2],
    new Map<string, TreeState>([
      ['p1', { nodes: [file('a.md')], error: null }],
      ['p2', { nodes: [file('a.md')], error: null }],
    ]),
    new Map(),
  )
  const docs = rows.filter((r) => r.type === 'doc')
  const [inP1, inP2] = docs

  it('matches on both the project and the path', () => {
    const a = active('p1', 'a.md')
    expect(isActiveRow(inP1, a)).toBe(true)
    expect(isActiveRow(inP2, a)).toBe(false)
    expect(isActiveRow(inP2, active('p2', 'a.md'))).toBe(true)
    expect(isActiveRow(inP1, active('p1', 'b.md'))).toBe(false)
  })

  it('marks nothing for no document, a file outside the projects or a project row', () => {
    expect(docs.some((r) => isActiveRow(r, null))).toBe(false)
    expect(docs.some((r) => isActiveRow(r, { projectId: '', rel: '', path: '/tmp/a.md', name: 'a.md' }))).toBe(false)
    expect(rows.some((r) => r.type === 'project' && isActiveRow(r, active('p1', 'a.md')))).toBe(false)
    expect(activeRowKey(null)).toBeNull()
    expect(activeRowKey(active('p1', 'a.md'))).toBe(rowKey('p1', 'a.md'))
  })
})

describe('revealExpansion', () => {
  const open = (e: Expansion, key: string, dflt: boolean) => isExpanded(e, key, dflt)

  it('opens the project and every folder up to the document', () => {
    const collapsed: Expansion = new Map([
      [rowKey('p1', ''), false],
      [rowKey('p1', 'a'), false],
      [rowKey('p1', 'x'), false],
    ])
    const next = revealExpansion(collapsed, active('p1', 'a/b/c.md'))
    expect(open(next, rowKey('p1', ''), true)).toBe(true)
    expect(open(next, rowKey('p1', 'a'), false)).toBe(true)
    expect(open(next, rowKey('p1', 'a/b'), false)).toBe(true)
    expect(open(next, rowKey('p1', 'a/b/c.md'), false)).toBe(false) // the file itself gets no entry
    expect(open(next, rowKey('p1', 'x'), false)).toBe(false)
    expect(collapsed.get(rowKey('p1', ''))).toBe(false) // the input is not mutated
  })

  it('opens only the project for a document in its root', () => {
    const next = revealExpansion(new Map([[rowKey('p1', ''), false]]), active('p1', 'c.md'))
    expect([...next]).toEqual([[rowKey('p1', ''), true]])
  })

  it('returns the same map when everything is open already', () => {
    const opened: Expansion = new Map([
      [rowKey('p1', 'a'), true],
      [rowKey('p1', 'a/b'), true],
    ])
    expect(revealExpansion(opened, active('p1', 'a/b/c.md'))).toBe(opened) // the project is open by default
  })

  it('does nothing for a document outside the projects or no document', () => {
    const e: Expansion = new Map()
    expect(revealExpansion(e, null)).toBe(e)
    expect(revealExpansion(e, { projectId: '', rel: '', path: '/x/y.md', name: 'y.md' })).toBe(e)
  })

  it('makes the row of the document visible in the rows', () => {
    const nodes = [dir('a', [dir('b', [file('c.md', 'a/b/c.md')], 'a/b')])]
    const trees = new Map<string, TreeState>([['p1', { nodes, error: null }]])
    const a = active('p1', 'a/b/c.md')
    const rows = buildRows([p1], trees, revealExpansion(new Map(), a))
    expect(rows.some((r) => isActiveRow(r, a))).toBe(true)
  })
})

describe('selectionForActive', () => {
  const rows = buildRows([p1], new Map([['p1', { nodes: [file('a.md'), file('b.md')], error: null }]]), new Map())

  it('selects the row of the active document', () => {
    expect(selectionForActive(rows, rowKey('p1', 'a.md'), active('p1', 'b.md'))).toBe(rowKey('p1', 'b.md'))
    expect(selectionForActive(rows, null, active('p1', 'b.md'))).toBe(rowKey('p1', 'b.md'))
  })

  it('keeps the selection when the tree has no such row', () => {
    expect(selectionForActive(rows, rowKey('p1', 'a.md'), active('p1', 'nema.md'))).toBe(rowKey('p1', 'a.md'))
    expect(selectionForActive(rows, null, active('p1', 'nema.md'))).toBeNull()
  })

  it('keeps the selection for no document or one outside the projects', () => {
    const sel = rowKey('p1', 'a.md')
    expect(selectionForActive(rows, sel, null)).toBe(sel)
    expect(selectionForActive(rows, sel, { projectId: '', rel: '', path: '/x/y.md', name: 'y.md' })).toBe(sel)
  })
})

describe('toolbarInfo', () => {
  const projects = [p1]

  it('shows the project name for a document in its root', () => {
    expect(toolbarInfo(projects, active('p1', 'Bilješke.md'), false, 'Pisar')).toMatchObject({ title: 'Bilješke', path: 'Roman' })
  })

  it('shows Project / folder / subfolder for a nested document', () => {
    expect(toolbarInfo(projects, active('p1', 'mapa/pod/Članak.md'), false, 'Pisar')).toMatchObject({
      title: 'Članak',
      path: 'Roman / mapa / pod',
    })
  })

  it('shows the absolute folder for a file outside the projects', () => {
    const outside = { projectId: '', rel: '', path: '/Users/ana/Desktop/zapis.txt', name: 'zapis.txt' }
    expect(toolbarInfo(projects, outside, false, 'Pisar')).toMatchObject({ title: 'zapis', path: '/Users/ana/Desktop' })
    expect(toolbarInfo(projects, { ...outside, path: '/zapis.txt' }, false, 'Pisar').path).toBe('/')
  })

  it('shows the app name and no state without an active document', () => {
    expect(toolbarInfo(projects, null, true, 'Pisar')).toEqual({ title: 'Pisar', path: '', stateText: null, modified: false })
  })

  it('says Izmijenjeno or Spremljeno', () => {
    expect(toolbarInfo(projects, active('p1', 'a.md'), true, 'Pisar')).toMatchObject({ stateText: 'Izmijenjeno', modified: true })
    expect(toolbarInfo(projects, active('p1', 'a.md'), false, 'Pisar')).toMatchObject({ stateText: 'Spremljeno', modified: false })
  })

  it('keeps dots inside the name', () => {
    expect(toolbarInfo(projects, active('p1', 'v1.2.md'), false, 'Pisar').title).toBe('v1.2')
    expect(toolbarInfo(projects, active('p1', 'arhiva/.md'), false, 'Pisar').title).toBe('.md')
  })

  it('leaves out the project name when the project is not known yet', () => {
    expect(toolbarInfo([], active('p9', 'mapa/a.md'), false, 'Pisar').path).toBe('mapa')
  })
})
