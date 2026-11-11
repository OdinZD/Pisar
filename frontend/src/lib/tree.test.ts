import { describe, expect, it } from 'vitest'
import {
  buildRows,
  displayName,
  expandForTarget,
  isItemRow,
  kindLabel,
  moveSelection,
  newDocumentTarget,
  pruneOverlay,
  reconcileSelection,
  rowKey,
  sortNodes,
  toolbarInfo,
  withOverlay,
  type ProjectRef,
  type TreeNode,
  type TreeState,
} from './tree'

const file = (name: string, rel = name): TreeNode => ({ name, rel, kind: name.split('.').pop()!.toLowerCase() })
const dir = (name: string, children: TreeNode[] = [], rel = name): TreeNode => ({ name, rel, kind: 'dir', children })

const p1: ProjectRef = { id: 'p1', name: 'Roman', missing: false }
const p2: ProjectRef = { id: 'p2', name: 'Blog', missing: false }
const gone: ProjectRef = { id: 'p3', name: 'Stari', missing: true }

function trees(entries: Record<string, TreeNode[] | null>): Map<string, TreeState> {
  return new Map(Object.entries(entries).map(([id, nodes]) => [id, { nodes, error: null }]))
}

describe('sortNodes', () => {
  it('orders by the Croatian alphabet regardless of case', () => {
    const names = ['Članak', 'cesta', 'Ćup', 'dan', 'Đak', 'Šuma', 'sat', 'Žaba', 'zid']
    const sorted = sortNodes(names.map((n) => file(`${n}.md`))).map((n) => displayName(n.name, n.kind))
    expect(sorted).toEqual(['cesta', 'Članak', 'Ćup', 'dan', 'Đak', 'sat', 'Šuma', 'zid', 'Žaba'])
  })

  it('sorts numbers naturally', () => {
    const sorted = sortNodes([file('Poglavlje 10.md'), file('Poglavlje 2.md')]).map((n) => n.name)
    expect(sorted).toEqual(['Poglavlje 2.md', 'Poglavlje 10.md'])
  })

  it('puts folders before documents', () => {
    const sorted = sortNodes([file('a.md'), dir('z')]).map((n) => n.name)
    expect(sorted).toEqual(['z', 'a.md'])
  })

  it('breaks ties by the full name', () => {
    expect(sortNodes([file('a.txt'), file('a.md')]).map((n) => n.name)).toEqual(['a.md', 'a.txt'])
  })
})

describe('displayName and kindLabel', () => {
  it.each([
    ['Bilješke.md', 'md', 'Bilješke', 'MD'],
    ['v1.2.md', 'md', 'v1.2', 'MD'],
    ['Ugovor.DOCX', 'docx', 'Ugovor', 'DOCX'],
    ['stari.doc', 'doc', 'stari', 'DOC'],
    ['bilješke.txt', 'txt', 'bilješke', 'TXT'],
    ['mapa', 'dir', 'mapa', 'DIR'],
  ])('%s', (name, kind, shown, label) => {
    expect(displayName(name, kind)).toBe(shown)
    expect(kindLabel(kind)).toBe(label)
  })
})

describe('buildRows', () => {
  const nodes = [dir('mapa', [file('Članak.docx', 'mapa/Članak.docx')]), file('Bilješke.md')]

  it('hides the children of a collapsed folder and shows them with the right depth once expanded', () => {
    const collapsed = buildRows([p1], trees({ p1: nodes }), new Map())
    expect(collapsed.map((r) => r.key)).toEqual([rowKey('p1', ''), rowKey('p1', 'mapa'), rowKey('p1', 'Bilješke.md')])

    const open = buildRows([p1], trees({ p1: nodes }), new Map([[rowKey('p1', 'mapa'), true]]))
    const inner = open.find((r) => r.key === rowKey('p1', 'mapa/Članak.docx'))
    expect(inner).toMatchObject({ type: 'doc', depth: 2, title: 'Članak', kindLabel: 'DOCX' })
    expect(open.find((r) => r.key === rowKey('p1', 'mapa'))).toMatchObject({ depth: 1, expanded: true })
  })

  it('shows a collapsed project without its children', () => {
    const rows = buildRows([p1], trees({ p1: nodes }), new Map([[rowKey('p1', ''), false]]))
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ type: 'project', expanded: false })
  })

  it('gives a missing project a note and no children', () => {
    const rows = buildRows([gone], trees({ p3: nodes }), new Map())
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ type: 'project', missing: true, expanded: false })
    expect((rows[0] as { note: string }).note).toBeTruthy()
  })

  it('treats a null tree as empty', () => {
    const rows = buildRows([p1], trees({ p1: null }), new Map())
    expect(rows).toHaveLength(1)
  })

  it('shows nothing under a project that has not loaded yet', () => {
    expect(buildRows([p1], new Map(), new Map())).toHaveLength(1)
  })

  it('turns a tree error into a message row and keeps the old nodes', () => {
    const state = new Map<string, TreeState>([['p1', { nodes: [file('a.md')], error: 'too-large' }]])
    const rows = buildRows([p1], state, new Map())
    expect(rows.map((r) => r.type)).toEqual(['project', 'message', 'doc'])
    expect((rows[1] as { text: string }).text).toBeTruthy()
  })

  it('keeps projects in the given order', () => {
    const rows = buildRows([p2, p1], new Map(), new Map())
    expect(rows.map((r) => r.projectId)).toEqual(['p2', 'p1'])
  })
})

describe('moveSelection', () => {
  const state = new Map<string, TreeState>([
    ['p1', { nodes: [file('a.md'), file('b.md')], error: 'too-large' }],
  ])
  const rows = buildRows([p1], state, new Map())
  const [project, , a, b] = rows

  it('moves down and up and stays at the ends', () => {
    expect(moveSelection(rows, a.key, 1)).toBe(b.key)
    expect(moveSelection(rows, b.key, 1)).toBe(b.key)
    expect(moveSelection(rows, b.key, -1)).toBe(a.key)
    expect(moveSelection(rows, project.key, -1)).toBe(project.key)
  })

  it('skips message rows', () => {
    expect(moveSelection(rows, project.key, 1)).toBe(a.key)
    expect(rows[1].type).toBe('message')
  })

  it('selects the first row when nothing is selected', () => {
    expect(moveSelection(rows, null, 1)).toBe(project.key)
    expect(moveSelection(rows, 'stale', 1)).toBe(project.key)
    expect(moveSelection([], null, 1)).toBeNull()
  })
})

describe('newDocumentTarget', () => {
  const t = trees({ p1: [dir('mapa', [file('x.md', 'mapa/x.md')]), file('a.md')], p2: [] })
  const open = new Map([[rowKey('p1', 'mapa'), true]])
  const rows = buildRows([p1, p2, gone], t, open)
  const target = (key: string | null, projects = [p1, p2, gone]) => newDocumentTarget(rows, key, projects)

  it('uses the selected folder', () => {
    expect(target(rowKey('p1', 'mapa'))).toEqual({ projectId: 'p1', dirRel: 'mapa' })
  })
  it('uses the parent folder of a selected document', () => {
    expect(target(rowKey('p1', 'mapa/x.md'))).toEqual({ projectId: 'p1', dirRel: 'mapa' })
    expect(target(rowKey('p1', 'a.md'))).toEqual({ projectId: 'p1', dirRel: '' })
  })
  it('uses the root of a selected project', () => {
    expect(target(rowKey('p2', ''))).toEqual({ projectId: 'p2', dirRel: '' })
  })
  it('falls back to the first available project without a selection', () => {
    expect(target(null)).toEqual({ projectId: 'p1', dirRel: '' })
    expect(newDocumentTarget(rows, null, [gone, p2])).toEqual({ projectId: 'p2', dirRel: '' })
  })
  it('falls back from a selected missing project to the first available one', () => {
    expect(target(rowKey('p3', ''))).toEqual({ projectId: 'p1', dirRel: '' })
  })
  it('is null without any available project', () => {
    expect(newDocumentTarget([], null, [])).toBeNull()
    expect(newDocumentTarget(rows, rowKey('p3', ''), [gone])).toBeNull()
  })
  it('expandForTarget opens the project and the folder', () => {
    const next = expandForTarget(new Map(), { projectId: 'p1', dirRel: 'mapa' })
    expect(next.get(rowKey('p1', ''))).toBe(true)
    expect(next.get(rowKey('p1', 'mapa'))).toBe(true)
  })
})

describe('reconcileSelection', () => {
  const rowsOf = (...names: string[]) => buildRows([p1], trees({ p1: names.map((n) => file(n)) }), new Map())
  const k = (n: string) => rowKey('p1', n)

  it('keeps a key that still exists', () => {
    expect(reconcileSelection(rowsOf('a.md', 'b.md'), rowsOf('a.md', 'b.md', 'c.md'), k('b.md'))).toBe(k('b.md'))
  })
  it('moves to the row at the same position when the row is gone', () => {
    expect(reconcileSelection(rowsOf('a.md', 'b.md', 'c.md'), rowsOf('a.md', 'c.md'), k('b.md'))).toBe(k('c.md'))
  })
  it('moves to the new last row when the last row is gone', () => {
    expect(reconcileSelection(rowsOf('a.md', 'b.md'), rowsOf('a.md'), k('b.md'))).toBe(k('a.md'))
  })
  it('drops the selection when nothing is left', () => {
    expect(reconcileSelection(rowsOf('a.md'), [], k('a.md'))).toBeNull()
  })
  it('keeps no selection as no selection', () => {
    expect(reconcileSelection(rowsOf('a.md'), rowsOf('a.md'), null)).toBeNull()
  })
  it('selects the renamed row when asked for its new key', () => {
    expect(reconcileSelection(rowsOf('a.md'), rowsOf('novo.md'), k('novo.md'))).toBe(k('novo.md'))
  })
})

describe('withOverlay', () => {
  const rows = buildRows([p1], trees({ p1: [dir('mapa', [], 'mapa'), file('a.md')] }), new Map([[rowKey('p1', 'mapa'), true]]))

  it('puts the new-document field first among the children of the target', () => {
    const shown = withOverlay(rows, { create: { projectId: 'p1', dirRel: '', error: null } })
    expect(shown.map((r) => r.type)).toEqual(['project', 'input', 'dir', 'doc'])
    expect(shown[1]).toMatchObject({ mode: 'create', depth: 1 })
    const inFolder = withOverlay(rows, { create: { projectId: 'p1', dirRel: 'mapa', error: 'greška' } })
    expect(inFolder.map((r) => r.type)).toEqual(['project', 'dir', 'input', 'message', 'doc'])
    expect(inFolder[2]).toMatchObject({ depth: 2 })
  })

  it('replaces the document row with a field for renaming', () => {
    const shown = withOverlay(rows, { rename: { key: rowKey('p1', 'a.md'), error: null } })
    expect(shown.at(-1)).toMatchObject({ type: 'input', mode: 'rename', initial: 'a' })
    expect(shown.filter((r) => r.type === 'doc')).toHaveLength(0)
  })

  it('adds the removal question below the project and a notice below its row', () => {
    const shown = withOverlay(rows, { confirmRemove: 'p1', notice: { key: rowKey('p1', 'a.md'), text: 'x' } })
    expect(shown.map((r) => r.type)).toEqual(['project', 'confirm', 'dir', 'doc', 'message'])
  })

  it('prunes parts whose row vanished', () => {
    const overlay = {
      create: { projectId: 'p1', dirRel: 'nema', error: null },
      rename: { key: rowKey('p1', 'a.md'), error: null },
      confirmRemove: 'p9',
    }
    expect(pruneOverlay(rows, overlay)).toEqual({ rename: overlay.rename })
  })
})

describe('toolbarInfo', () => {
  const rows = buildRows(
    [p1],
    trees({ p1: [dir('mapa', [dir('pod', [file('Članak.md', 'mapa/pod/Članak.md')], 'mapa/pod')], 'mapa'), file('Bilješke.md')] }),
    new Map([
      [rowKey('p1', 'mapa'), true],
      [rowKey('p1', 'mapa/pod'), true],
    ]),
  )
  it('shows only the project for a document in the root', () => {
    expect(toolbarInfo([p1], rows, rowKey('p1', 'Bilješke.md'), 'Pisar')).toEqual({ title: 'Bilješke', path: 'Roman' })
  })
  it('shows project / folder / subfolder for a nested document', () => {
    expect(toolbarInfo([p1], rows, rowKey('p1', 'mapa/pod/Članak.md'), 'Pisar')).toEqual({
      title: 'Članak',
      path: 'Roman / mapa / pod',
    })
  })
  it('shows the app name without a selected document', () => {
    expect(toolbarInfo([p1], rows, null, 'Pisar')).toEqual({ title: 'Pisar', path: '' })
    expect(toolbarInfo([p1], rows, rowKey('p1', 'mapa'), 'Pisar')).toEqual({ title: 'Pisar', path: '' })
  })
})

describe('isItemRow', () => {
  it('recognises selectable rows', () => {
    expect(isItemRow({ type: 'doc' })).toBe(true)
    expect(isItemRow({ type: 'message' })).toBe(false)
    expect(isItemRow({ type: 'confirm' })).toBe(false)
  })
})
