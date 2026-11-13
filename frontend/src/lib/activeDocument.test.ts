import { describe, expect, it } from 'vitest'
import {
  activateAction,
  activeRowKey,
  documentStatus,
  isActiveRow,
  isOpenableKind,
  openFailureText,
  readOnlyReasonText,
  readOnlyText,
  revealExpansion,
  simplifiedText,
  selectionForActive,
  toolbarInfo,
  type ConversionState,
} from './activeDocument'
import { documentActive, documentExport, documentState, initialState, nvimExited, openExecuting, openSettled, type ActiveDocument, type EditorState } from './editorState'
import { buildRows, isExpanded, rowKey, type DocRow, type Expansion, type ProjectRef, type TreeNode, type TreeState } from './tree'
import { strings } from '../strings'

const file = (name: string, rel = name): TreeNode => ({ name, rel, kind: name.split('.').pop()!.toLowerCase() })
const dir = (name: string, children: TreeNode[] = [], rel = name): TreeNode => ({ name, rel, kind: 'dir', children })

const noDocFlags = { readOnly: [], imported: false, unexported: false, simplified: [] }

const p1: ProjectRef = { id: 'p1', name: 'Roman', missing: false }
const p2: ProjectRef = { id: 'p2', name: 'Blog', missing: false }

const active = (projectId: string, rel: string, path = `/home/${projectId}/${rel}`): ActiveDocument => ({
  projectId,
  rel,
  path,
  name: rel.split('/').pop()!,
  readOnly: [],
  imported: false,
  unexported: false,
  simplified: [],
})

describe('isOpenableKind and activateAction', () => {
  it('opens md, txt, docx and doc, and nothing else', () => {
    expect(['md', 'txt', 'docx', 'doc', 'dir', ''].map(isOpenableKind)).toEqual([true, true, true, true, false, false])
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

  it('opens docx and doc like md', () => {
    expect(activateAction(doc('docx', 'a.docx'))).toEqual({ type: 'open', projectId: 'p', rel: 'a.docx' })
    expect(activateAction(doc('doc', 'a.doc'))).toEqual({ type: 'open', projectId: 'p', rel: 'a.doc' })
  })

  it('toggles projects and folders', () => {
    expect(activateAction({ ...base, type: 'project', name: 'P', expanded: true, missing: false, note: null })).toEqual({ type: 'toggle' })
    expect(activateAction({ ...base, type: 'dir', rel: 'd', name: 'd', expanded: false })).toEqual({ type: 'toggle' })
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
    expect(docs.some((r) => isActiveRow(r, { ...noDocFlags, projectId: '', rel: '', path: '/tmp/a.md', name: 'a.md' }))).toBe(false)
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
    expect(revealExpansion(e, { ...noDocFlags, projectId: '', rel: '', path: '/x/y.md', name: 'y.md' })).toBe(e)
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
    expect(selectionForActive(rows, sel, { ...noDocFlags, projectId: '', rel: '', path: '/x/y.md', name: 'y.md' })).toBe(sel)
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
    const outside = { ...noDocFlags, projectId: '', rel: '', path: '/Users/ana/Desktop/zapis.txt', name: 'zapis.txt' }
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

describe('openFailureText', () => {
  it('says nothing when the recovery dialog was closed', () => {
    expect(openFailureText({ cause: { code: 'canceled' } })).toBeNull()
  })

  it.each([
    ['pandoc-missing', strings.errorPandocMissing],
    ['pandoc-too-old', strings.errorPandocTooOld],
    ['convert-failed', strings.errorConvertFailed],
    ['not-word', strings.errorNotWord],
    ['document-too-large', strings.errorDocumentTooLarge],
    ['read-only', strings.errorReadOnly],
    ['conflict', strings.errorConflict],
    ['damaged-work', strings.errorDamagedWork],
  ])('tells %s', (code, text) => {
    expect(openFailureText({ cause: { code } })).toBe(text)
  })

  it('falls back to the general text for an unknown error', () => {
    expect(openFailureText(new Error('boom'))).toBe(strings.errorGeneric)
  })
})

describe('read-only reasons', () => {
  const known: [string, string][] = [
    ['comments', strings.readOnlyComments],
    ['tracked-changes', strings.readOnlyTrackedChanges],
    ['text-boxes', strings.readOnlyTextBoxes],
    ['embedded-objects', strings.readOnlyEmbeddedObjects],
    ['tables', strings.readOnlyTables],
    ['hard-links', strings.readOnlyHardLinks],
    ['no-permission', strings.readOnlyNoPermission],
    ['locked', strings.readOnlyLocked],
    ['not-regular', strings.readOnlyNotRegular],
  ]

  it.each(known)('%s has its own text', (code, text) => {
    expect(readOnlyReasonText(code)).toBe(text)
    expect(text).not.toBe(strings.readOnlyOther)
  })

  it('all known texts differ from each other', () => {
    expect(new Set(known.map(([, t]) => t)).size).toBe(known.length)
  })

  it.each(['future-reason', '', 'constructor', '__proto__'])('shows readOnlyOther, never the raw code, for %j', (code) => {
    expect(readOnlyReasonText(code)).toBe(strings.readOnlyOther)
    expect(readOnlyText([code])).toBe('Samo za čitanje: nije moguće spremiti')
  })

  it('joins several reasons with a comma', () => {
    expect(readOnlyText(['comments', 'locked'])).toBe('Samo za čitanje: dokument ima komentare, datoteka je zaključana')
  })

  it('tells the same text once', () => {
    expect(readOnlyText(['x', 'y'])).toBe('Samo za čitanje: nije moguće spremiti')
  })
})

describe('documentStatus', () => {
  const docx = (over: Partial<ActiveDocument> = {}): ActiveDocument => ({ ...active('p1', 'a.docx', '/x/a.docx'), ...over })
  const conv = (over: Partial<ConversionState> = {}): ConversionState => ({ exports: new Map(), opening: false, copyError: null, ...over })
  const exportsOf = (...records: [string, 'converting' | 'failed' | 'canceled', string?][]) =>
    new Map(records.map(([path, phase, code]) => [path, { phase, code: code ?? '' }]))

  it('f) says Spremljeno when nothing is wrong', () => {
    expect(documentStatus(docx(), false, conv())).toEqual({ kind: 'saved', text: 'Spremljeno', tone: 'muted', note: null, saveCopy: false })
  })

  it('says nothing without a document and without a conversion', () => {
    expect(documentStatus(null, false, conv())).toBeNull()
    expect(documentStatus(null, true, conv({ copyError: 'conflict' }))).toBeNull()
  })

  it('a) a conversion in progress, for the opening or for the export of the active document', () => {
    expect(documentStatus(docx(), false, conv({ opening: true }))).toMatchObject({ kind: 'converting', text: 'Konverzija u tijeku…' })
    const exporting = conv({ exports: exportsOf(['/x/a.docx', 'converting']) })
    expect(documentStatus(docx(), false, exporting)).toMatchObject({ kind: 'converting', text: 'Konverzija u tijeku…' })
  })

  it('a) wins over every other state', () => {
    const all = conv({ opening: true, copyError: 'conflict', exports: exportsOf(['/x/a.docx', 'converting']) })
    const doc = docx({ readOnly: ['comments'], unexported: true })
    expect(documentStatus(doc, true, all)?.kind).toBe('converting')
  })

  it('a) shows even without an active document while a Word document opens', () => {
    expect(documentStatus(null, false, conv({ opening: true }))).toMatchObject({ kind: 'converting', note: null, saveCopy: false })
    expect(toolbarInfo([p1], null, false, 'Pisar', conv({ opening: true }))).toEqual({
      title: 'Pisar',
      path: '',
      stateText: 'Konverzija u tijeku…',
      modified: false,
    })
  })

  it('a) ignores the converting record of another document', () => {
    expect(documentStatus(docx(), false, conv({ exports: exportsOf(['/x/b.docx', 'converting']) }))?.kind).toBe('saved')
  })

  it('b) modified wins over a failure, canceled, unexported and read-only', () => {
    const failed = conv({ copyError: 'conflict', exports: exportsOf(['/x/a.docx', 'failed', 'conflict']) })
    const doc = docx({ readOnly: ['comments'], unexported: true })
    expect(documentStatus(doc, true, failed)).toMatchObject({ kind: 'modified', text: 'Izmijenjeno', tone: 'strong', saveCopy: false })
  })

  it('c) a failed export shows the message of its code in danger', () => {
    const failed = conv({ exports: exportsOf(['/x/a.docx', 'failed', 'conflict']) })
    expect(documentStatus(docx(), false, failed)).toEqual({
      kind: 'failed',
      text: strings.errorConflict,
      tone: 'danger',
      note: null,
      saveCopy: false,
    })
  })

  it('c) an unknown export code gets the general message', () => {
    const failed = conv({ exports: exportsOf(['/x/a.docx', 'failed', 'something-new']) })
    expect(documentStatus(docx(), false, failed)?.text).toBe(strings.errorGeneric)
  })

  it('c) a failure of SaveCopy is shown the same way', () => {
    expect(documentStatus(docx(), false, conv({ copyError: 'read-only' }))).toMatchObject({ kind: 'failed', text: strings.errorReadOnly, tone: 'danger' })
  })

  it('c) wins over unexported and read-only', () => {
    const failed = conv({ exports: exportsOf(['/x/a.docx', 'failed', 'convert-failed']) })
    expect(documentStatus(docx({ unexported: true, readOnly: ['locked'] }), false, failed)?.kind).toBe('failed')
  })

  it('d) a canceled export or an unexported working copy', () => {
    const canceled = conv({ exports: exportsOf(['/x/a.docx', 'canceled']) })
    expect(documentStatus(docx(), false, canceled)).toMatchObject({ kind: 'unexported', text: 'Izmjene još nisu u dokumentu', tone: 'strong' })
    expect(documentStatus(docx({ unexported: true }), false, conv())).toMatchObject({ kind: 'unexported' })
  })

  it('d) wins over read-only', () => {
    expect(documentStatus(docx({ unexported: true, readOnly: ['comments'] }), false, conv())?.kind).toBe('unexported')
  })

  it('e) read-only with the reasons and the Save a copy button', () => {
    expect(documentStatus(docx({ readOnly: ['comments', 'tables'] }), false, conv())).toEqual({
      kind: 'read-only',
      text: 'Samo za čitanje: dokument ima komentare, dokument ima tablicu koju Markdown ne može prikazati',
      tone: 'muted',
      note: null,
      saveCopy: true,
    })
  })

  it('the button shows only in e)', () => {
    const states = [
      documentStatus(docx(), false, conv({ opening: true })),
      documentStatus(docx(), true, conv()),
      documentStatus(docx(), false, conv({ copyError: 'conflict' })),
      documentStatus(docx({ unexported: true }), false, conv()),
      documentStatus(docx(), false, conv()),
    ]
    expect(states.map((s) => s?.saveCopy)).toEqual([false, false, false, false, false])
  })

  it('an empty readOnly list is not read-only', () => {
    expect(documentStatus(docx({ readOnly: [] }), false, conv())?.kind).toBe('saved')
  })

  describe('the note about an imported .doc', () => {
    const imported = (over: Partial<ActiveDocument> = {}) => docx({ imported: true, ...over })

    it.each([
      ['a)', imported(), false, conv({ opening: true })],
      ['b)', imported(), true, conv()],
      ['d)', imported({ unexported: true }), false, conv()],
      ['f)', imported(), false, conv()],
    ])('comes with %s', (_name, doc, modified, c) => {
      expect(documentStatus(doc, modified, c)?.note).toBe(strings.documentImported)
    })

    it.each([
      ['c)', imported(), false, conv({ exports: exportsOf(['/x/a.docx', 'failed', 'conflict']) })],
      ['e)', imported({ readOnly: ['locked'] }), false, conv()],
    ])('does not come with %s', (_name, doc, modified, c) => {
      expect(documentStatus(doc, modified, c)?.note).toBeNull()
    })

    it('is not there for a document that was not imported', () => {
      expect(documentStatus(docx(), false, conv())?.note).toBeNull()
    })
  })

  describe('the note about formatting that saving simplifies', () => {
    const simplified = (...codes: string[]) => docx({ simplified: codes })
    const allCodes = [
      'alignment',
      'text-color',
      'font',
      'spacing',
      'indentation',
      'page-breaks',
      'tabs',
      'fields',
      'table-formatting',
      'drawings',
    ]
    const names = [
      'poravnanje',
      'boja teksta',
      'font i veličina',
      'prored i razmaci',
      'uvlake',
      'prijelomi stranice',
      'tabulatori',
      'polja (broj stranice, sadržaj)',
      'oblikovanje tablica',
      'crtani oblici',
    ]

    it.each(allCodes.map((code, i) => [code, names[i]]))('%s is told as "%s"', (code, name) => {
      expect(documentStatus(simplified(code), false, conv())?.note).toBe(`Spremanje pojednostavljuje oblikovanje: ${name}`)
    })

    it('joins several with a comma, in the order they came', () => {
      expect(documentStatus(simplified('fields', 'alignment', 'tabs'), false, conv())?.note).toBe(
        'Spremanje pojednostavljuje oblikovanje: polja (broj stranice, sadržaj), poravnanje, tabulatori',
      )
      expect(simplifiedText(allCodes)).toBe(`${strings.documentSimplified}: ${names.join(', ')}`)
    })

    it('leaves out a code it does not know, and says nothing if none is known', () => {
      expect(documentStatus(simplified('future-code', 'tabs', '', 'constructor'), false, conv())?.note).toBe(
        'Spremanje pojednostavljuje oblikovanje: tabulatori',
      )
      expect(documentStatus(simplified('future-code', '__proto__'), false, conv())?.note).toBeNull()
    })

    it('says nothing when there is nothing to simplify', () => {
      expect(documentStatus(simplified(), false, conv())?.note).toBeNull()
    })

    it.each([
      ['converting', simplified('tabs'), false, conv({ opening: true })],
      ['modified', simplified('tabs'), true, conv()],
      ['unexported', docx({ simplified: ['tabs'], unexported: true }), false, conv()],
      ['saved', simplified('tabs'), false, conv()],
    ])('comes with the state %s', (_name, doc, modified, c) => {
      expect(documentStatus(doc, modified, c)?.note).toBe('Spremanje pojednostavljuje oblikovanje: tabulatori')
    })

    it('comes with an export that was canceled', () => {
      const c = conv({ exports: exportsOf(['/x/a.docx', 'canceled']) })
      expect(documentStatus(simplified('tabs'), false, c)).toMatchObject({ kind: 'unexported', note: 'Spremanje pojednostavljuje oblikovanje: tabulatori' })
    })

    it('is not shown for a read-only document', () => {
      const doc = docx({ simplified: ['tabs'], readOnly: ['comments'] })
      expect(documentStatus(doc, false, conv())).toMatchObject({ kind: 'read-only', note: null })
      // Not even while it converts: a document that cannot be saved has nothing to warn about.
      expect(documentStatus(doc, false, conv({ opening: true }))?.note).toBeNull()
    })

    it('is not shown next to an export error', () => {
      const failed = conv({ exports: exportsOf(['/x/a.docx', 'failed', 'conflict']) })
      expect(documentStatus(simplified('tabs'), false, failed)).toMatchObject({ kind: 'failed', note: null })
      expect(documentStatus(simplified('tabs'), false, conv({ copyError: 'conflict' }))).toMatchObject({ kind: 'failed', note: null })
    })

    it('gives way to the note about an import from .doc', () => {
      const doc = docx({ imported: true, simplified: ['tabs', 'font'] })
      expect(documentStatus(doc, false, conv())?.note).toBe(strings.documentImported)
      expect(documentStatus(doc, true, conv())?.note).toBe(strings.documentImported)
    })

    it('is not there without an active document', () => {
      expect(documentStatus(null, false, conv({ opening: true }))?.note).toBeNull()
    })
  })

  describe('toolbarInfo', () => {
    it('takes text and modified from the status', () => {
      const failed = conv({ copyError: 'conflict' })
      expect(toolbarInfo([p1], docx(), false, 'Pisar', failed)).toMatchObject({ title: 'a', stateText: strings.errorConflict, modified: false })
      expect(toolbarInfo([p1], docx(), true, 'Pisar', failed)).toMatchObject({ stateText: 'Izmijenjeno', modified: true })
      expect(toolbarInfo([p1], docx(), true, 'Pisar', conv({ opening: true }))).toMatchObject({ stateText: 'Konverzija u tijeku…', modified: false })
    })
  })
})

// The events in the orders Go can send them, run through the state functions as the controller does.
describe('toolbar over a stream of events', () => {
  const open = (s: EditorState, rel: string) => openExecuting(s, { id: 1, projectId: 'p1', rel })
  const text = (s: EditorState) =>
    documentStatus(s.active, s.modified, { exports: s.exports, opening: s.wordOpens.length > 0, copyError: s.copyError })?.text ?? null
  const wordDoc = (rel: string, over: Partial<ActiveDocument> = {}) => ({ ...active('p1', rel, `/x/${rel}`), ...over })
  const start = () => documentActive(initialState, wordDoc('a.docx'))
  const ex = (path: string, phase: string, code = '') => ({ path, phase, code })

  it.each([
    ['state first', [() => 'state', () => 'converting']],
    ['converting first', [() => 'converting', () => 'state']],
  ])('%s: still converting after :w', (_name, order) => {
    let s = documentState(start(), { path: '/x/a.docx', modified: true })
    expect(text(s)).toBe('Izmijenjeno')
    for (const step of order) {
      s = step() === 'state' ? documentState(s, { path: '/x/a.docx', modified: false }) : documentExport(s, ex('/x/a.docx', 'converting'))
    }
    expect(text(s)).toBe('Konverzija u tijeku…')
    expect(text(documentExport(s, ex('/x/a.docx', 'saved')))).toBe('Spremljeno')
  })

  it('.doc saved as a new .docx ends as Spremljeno, nowhere "Konverzija u tijeku…"', () => {
    let s = documentActive(initialState, { ...wordDoc('Ime.doc'), imported: true })
    s = documentExport(s, ex('/x/Ime.doc', 'converting'))
    expect(text(s)).toBe('Konverzija u tijeku…')
    s = documentExport(s, ex('/x/Ime.docx', 'saved'))
    s = documentActive(s, wordDoc('Ime.docx'))
    expect(text(s)).toBe('Spremljeno')
    expect(s.exports.size).toBe(0)
  })

  it('failed, then new changes, then a new save', () => {
    let s = documentExport(documentExport(start(), ex('/x/a.docx', 'converting')), ex('/x/a.docx', 'failed', 'conflict'))
    expect(text(s)).toBe(strings.errorConflict)
    s = documentState(s, { path: '/x/a.docx', modified: true })
    expect(text(s)).toBe('Izmijenjeno')
    s = documentExport(documentState(s, { path: '/x/a.docx', modified: false }), ex('/x/a.docx', 'converting'))
    expect(text(s)).toBe('Konverzija u tijeku…')
    expect(text(documentExport(s, ex('/x/a.docx', 'saved')))).toBe('Spremljeno')
  })

  it('canceled says that the changes are not in the document, and reloaded ends that', () => {
    let s = documentExport(documentExport(start(), ex('/x/a.docx', 'converting')), ex('/x/a.docx', 'canceled'))
    expect(text(s)).toBe('Izmjene još nisu u dokumentu')
    s = documentExport(documentActive(s, wordDoc('a.docx', { readOnly: ['comments'] })), ex('/x/a.docx', 'reloaded'))
    expect(text(s)).toBe('Samo za čitanje: dokument ima komentare')
  })

  it('an event for another document shows when that document becomes active', () => {
    let s = documentExport(start(), ex('/x/b.docx', 'failed', 'convert-failed'))
    expect(text(s)).toBe('Spremljeno')
    s = documentActive(s, wordDoc('b.docx'))
    expect(text(s)).toBe(strings.errorConvertFailed)
  })

  it('a failure after nvim:exit is kept, the document is gone', () => {
    let s = nvimExited(documentExport(start(), ex('/x/a.docx', 'converting'))).state
    expect(text(s)).toBeNull()
    s = documentExport(s, ex('/x/a.docx', 'failed', 'conflict'))
    expect(text(documentActive(s, wordDoc('a.docx')))).toBe(strings.errorConflict)
  })

  it('opening: converting without an active document, off again when it ends or fails', () => {
    let s = open(initialState, 'a.docx')
    expect(text(s)).toBe('Konverzija u tijeku…')
    expect(text(openSettled(s, 1))).toBeNull() // also the failure path: the controller settles the same way
    s = documentActive(openSettled(s, 1), wordDoc('a.docx'))
    expect(text(s)).toBe('Spremljeno')
  })

  it('opening a Markdown document shows no conversion', () => {
    expect(text(open(initialState, 'a.md'))).toBeNull()
  })
})
