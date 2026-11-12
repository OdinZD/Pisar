import { describe, expect, it } from 'vitest'
import {
  copyFinished,
  copyStarted,
  documentActive,
  documentExport,
  documentState,
  isWordRel,
  openExecuting,
  openSettled,
  initialState,
  nvimExited,
  requestOpen,
  requestStart,
  startFailed,
  startSucceeded,
  type ActiveDocument,
  type EditorState,
  type OpenRequest,
  type Phase,
} from './editorState'

const req = (id: number, rel = `d${id}.md`): OpenRequest => ({ id, projectId: 'p', rel })
const doc = (rel: string): ActiveDocument => ({ projectId: 'p', rel, path: `/x/${rel}`, name: rel, readOnly: [], imported: false, unexported: false, simplified: [] })
const empty: ActiveDocument = { projectId: '', rel: '', path: '', name: '', readOnly: [], imported: false, unexported: false, simplified: [] }

const inPhase = (phase: Phase): EditorState => ({
  ...initialState,
  phase,
  error: phase === 'failed' ? 'nvim-missing' : null,
})

describe('start flow', () => {
  it('goes idle -> starting -> running', () => {
    const starting = requestStart(initialState)
    expect(starting.state.phase).toBe('starting')
    expect(starting.run).toBeNull()
    const running = startSucceeded(starting.state)
    expect(running.state.phase).toBe('running')
    expect(running.run).toBeNull()
  })

  it('keeps the error code on failure and starts again from failed', () => {
    const failed = startFailed(requestStart(initialState).state, 'nvim-missing')
    expect(failed.state.phase).toBe('failed')
    expect(failed.state.error).toBe('nvim-missing')
    const again = requestStart(failed.state)
    expect(again.state.phase).toBe('starting')
    expect(again.state.error).toBeNull()
  })

  it.each(['starting', 'running'] as const)('a start request in %s changes nothing', (phase) => {
    const s = inPhase(phase)
    expect(requestStart(s).state).toBe(s)
  })

  it.each(['idle', 'running', 'failed'] as const)('a stale Start answer in %s changes nothing', (phase) => {
    const s = inPhase(phase)
    expect(startSucceeded(s).state).toBe(s)
    expect(startFailed(s, 'internal').state).toBe(s)
  })
})

describe('nvim:exit', () => {
  it.each(['idle', 'starting', 'running', 'failed'] as const)('goes to idle from %s', (phase) => {
    const s: EditorState = { ...inPhase(phase), active: doc('a.md'), modified: true }
    const t = nvimExited(s)
    expect(t.state).toEqual(initialState)
    expect(t.run).toBeNull()
  })

  it('drops a waiting open request', () => {
    const s: EditorState = { ...inPhase('starting'), pendingOpen: req(1) }
    expect(nvimExited(s).dropped).toEqual([{ request: req(1), reason: 'exited' }])
  })
})

describe('open requests', () => {
  it('runs at once while the editor runs', () => {
    const t = requestOpen(inPhase('running'), req(1))
    expect(t.run).toEqual(req(1))
    expect(t.state.pendingOpen).toBeNull()
    expect(t.state.phase).toBe('running')
  })

  it('is kept in idle, starts the editor, and comes back after success', () => {
    const t = requestOpen(initialState, req(1))
    expect(t.run).toBeNull()
    expect(t.state.phase).toBe('starting')
    expect(t.state.pendingOpen).toEqual(req(1))

    const done = startSucceeded(t.state)
    expect(done.run).toEqual(req(1))
    expect(done.state.pendingOpen).toBeNull()
    expect(done.state.phase).toBe('running')
  })

  it('is kept in starting without starting twice', () => {
    const t = requestOpen(inPhase('starting'), req(1))
    expect(t.state.phase).toBe('starting')
    expect(t.state.pendingOpen).toEqual(req(1))
  })

  it('restarts a failed editor', () => {
    const t = requestOpen(inPhase('failed'), req(1))
    expect(t.state.phase).toBe('starting')
    expect(t.state.error).toBeNull()
    expect(t.state.pendingOpen).toEqual(req(1))
  })

  it('keeps only the last of two requests and reports the replaced one', () => {
    const first = requestOpen(initialState, req(1))
    expect(first.dropped).toEqual([])
    const second = requestOpen(first.state, req(2))
    expect(second.state.pendingOpen).toEqual(req(2))
    expect(second.dropped).toEqual([{ request: req(1), reason: 'superseded' }])
    expect(startSucceeded(second.state).run).toEqual(req(2))
  })

  it('is dropped when Start fails', () => {
    const t = startFailed(requestOpen(initialState, req(1)).state, 'internal')
    expect(t.state.phase).toBe('failed')
    expect(t.state.pendingOpen).toBeNull()
    expect(t.run).toBeNull()
    expect(t.dropped).toEqual([{ request: req(1), reason: 'start-failed' }])
    // ... and does not come back after a later successful start.
    expect(startSucceeded(startSucceeded(requestStart(t.state).state).state).run).toBeNull()
  })
})

describe('active document', () => {
  it('document:active sets the document and clears modified', () => {
    const s = documentActive({ ...initialState, active: doc('a.md'), modified: true }, doc('b.md'))
    expect(s.active).toEqual(doc('b.md'))
    expect(s.modified).toBe(false)
  })

  it('document:state for the same path changes modified', () => {
    const s = documentState(documentActive(initialState, doc('a.md')), { path: '/x/a.md', modified: true })
    expect(s.modified).toBe(true)
    expect(documentState(s, { path: '/x/a.md', modified: false }).modified).toBe(false)
  })

  it('document:state for another path is ignored', () => {
    const active = documentActive(initialState, doc('a.md'))
    expect(documentState(active, { path: '/x/other.md', modified: true })).toBe(active)
  })

  it('a new document:active clears the flag of the old one until its own state arrives', () => {
    const modified = documentState(documentActive(initialState, doc('a.md')), { path: '/x/a.md', modified: true })
    const next = documentActive(modified, doc('b.md'))
    expect(next.modified).toBe(false)
    expect(documentState(next, { path: '/x/a.md', modified: true }).modified).toBe(false)
  })

  it('an empty document:active means no active document', () => {
    const s = documentActive({ ...initialState, active: doc('a.md'), modified: true }, empty)
    expect(s.active).toBeNull()
    expect(s.modified).toBe(false)
    expect(documentState(s, { path: '', modified: true }).modified).toBe(false)
  })

  it('a document outside the projects is still active', () => {
    const outside: ActiveDocument = { projectId: '', rel: '', path: '/tmp/x.md', name: 'x.md', readOnly: [], imported: false, unexported: false, simplified: [] }
    expect(documentActive(initialState, outside).active).toEqual(outside)
  })

  it('nvim:exit clears the active document and modified', () => {
    const s = documentState(documentActive(initialState, doc('a.md')), { path: '/x/a.md', modified: true })
    const t = nvimExited(s).state
    expect(t.active).toBeNull()
    expect(t.modified).toBe(false)
  })

  it('a failed start clears the active document', () => {
    const s: EditorState = { ...inPhase('starting'), active: doc('a.md'), modified: true }
    const t = startFailed(s, 'internal').state
    expect(t.active).toBeNull()
    expect(t.modified).toBe(false)
  })
})

describe('document:active details', () => {
  it('reads readOnly null as an empty list and missing flags as false', () => {
    const s = documentActive(initialState, { projectId: 'p', rel: 'a.docx', path: '/x/a.docx', name: 'a.docx', readOnly: null })
    expect(s.active).toMatchObject({ readOnly: [], imported: false, unexported: false, simplified: [] })
  })

  it('reads simplified null or missing as an empty list', () => {
    const base = { projectId: 'p', rel: 'a.docx', path: '/x/a.docx', name: 'a.docx' }
    expect(documentActive(initialState, { ...base, simplified: null }).active?.simplified).toEqual([])
    expect(documentActive(initialState, base).active?.simplified).toEqual([])
  })

  it('keeps the codes of the simplified formatting, and copies the list', () => {
    const codes = ['alignment', 'tabs']
    const s = documentActive(initialState, { ...doc('a.docx'), simplified: codes })
    expect(s.active?.simplified).toEqual(['alignment', 'tabs'])
    expect(s.active?.simplified).not.toBe(codes)
  })

  it('keeps reasons and flags, and copies the list', () => {
    const reasons = ['comments', 'locked']
    const s = documentActive(initialState, { ...doc('a.docx'), readOnly: reasons, imported: true, unexported: true })
    expect(s.active).toMatchObject({ readOnly: ['comments', 'locked'], imported: true, unexported: true })
    expect(s.active?.readOnly).not.toBe(reasons)
  })

  it('clears the error of the last SaveCopy', () => {
    const s = copyFinished(copyStarted(initialState), 'conflict')
    expect(documentActive(s, doc('b.docx')).copyError).toBeNull()
  })
})

describe('document:export', () => {
  const ev = (path: string, phase: string, code = '') => ({ path, phase, code })
  const apply = (s: EditorState, ...events: ReturnType<typeof ev>[]) => events.reduce(documentExport, s)
  const active = (rel: string, over: Partial<ActiveDocument> = {}) => documentActive(initialState, { ...doc(rel), ...over })

  it('converting then saved leaves no record', () => {
    const s = apply(initialState, ev('/x/a.docx', 'converting'))
    expect(s.exports.get('/x/a.docx')).toEqual({ phase: 'converting', code: '' })
    expect(apply(s, ev('/x/a.docx', 'saved')).exports.size).toBe(0)
  })

  it('converting then failed keeps the code', () => {
    const s = apply(initialState, ev('/x/a.docx', 'converting'), ev('/x/a.docx', 'failed', 'conflict'))
    expect([...s.exports]).toEqual([['/x/a.docx', { phase: 'failed', code: 'conflict' }]])
  })

  it('converting then canceled keeps a canceled record', () => {
    const s = apply(initialState, ev('/x/a.docx', 'converting'), ev('/x/a.docx', 'canceled'))
    expect([...s.exports]).toEqual([['/x/a.docx', { phase: 'canceled', code: '' }]])
  })

  it('reloaded drops the record of the path, failure included', () => {
    const s = apply(initialState, ev('/x/a.docx', 'failed', 'conflict'), ev('/x/a.docx', 'reloaded'))
    expect(s.exports.size).toBe(0)
  })

  it('a final event for one path drops the converting record of another (.doc saved as .docx)', () => {
    const s = apply(initialState, ev('/x/Ime.doc', 'converting'), ev('/x/Ime.docx', 'saved'))
    expect(s.exports.size).toBe(0)
  })

  it('a final event keeps the failures of other paths', () => {
    const s = apply(initialState, ev('/x/b.docx', 'failed', 'conflict'), ev('/x/a.docx', 'converting'), ev('/x/a.docx', 'saved'))
    expect([...s.exports]).toEqual([['/x/b.docx', { phase: 'failed', code: 'conflict' }]])
  })

  it('ignores a phase it does not know', () => {
    const s = apply(initialState, ev('/x/a.docx', 'converting'))
    expect(documentExport(s, ev('/x/a.docx', 'later-phase'))).toBe(s)
  })

  it('does not change the input', () => {
    const before = apply(initialState, ev('/x/a.docx', 'converting'))
    documentExport(before, ev('/x/a.docx', 'saved'))
    expect(before.exports.size).toBe(1)
  })

  it('saved clears unexported of the active document with that path only', () => {
    const s = active('a.docx', { unexported: true })
    expect(documentExport(s, ev('/x/a.docx', 'saved')).active?.unexported).toBe(false)
    expect(documentExport(s, ev('/x/other.docx', 'saved')).active?.unexported).toBe(true)
    expect(documentExport(s, ev('/x/a.docx', 'failed', 'conflict')).active?.unexported).toBe(true)
  })

  it('saved empties simplified of the active document with that path only', () => {
    const s = documentActive(initialState, { ...doc('a.docx'), simplified: ['alignment', 'tabs'] })
    expect(documentExport(s, ev('/x/a.docx', 'saved')).active?.simplified).toEqual([])
    expect(documentExport(s, ev('/x/other.docx', 'saved')).active?.simplified).toEqual(['alignment', 'tabs'])
    // A failed or canceled export saved nothing: the warning stays.
    expect(documentExport(s, ev('/x/a.docx', 'failed', 'conflict')).active?.simplified).toEqual(['alignment', 'tabs'])
    expect(documentExport(s, ev('/x/a.docx', 'canceled')).active?.simplified).toEqual(['alignment', 'tabs'])
    expect(documentExport(s, ev('/x/a.docx', 'reloaded')).active?.simplified).toEqual(['alignment', 'tabs'])
  })

  it('an event for another document leaves the active one alone and waits for its document', () => {
    const s = documentActive(apply(active('a.docx'), ev('/x/b.docx', 'failed', 'conflict')), doc('b.docx'))
    expect(s.exports.get('/x/b.docx')).toEqual({ phase: 'failed', code: 'conflict' })
    expect(s.active?.path).toBe('/x/b.docx')
  })

  it('nvim:exit clears the active document but keeps the export records', () => {
    const s = apply(active('a.docx'), ev('/x/a.docx', 'failed', 'conflict'), ev('/x/b.docx', 'converting'))
    const after = nvimExited(s).state
    expect(after.active).toBeNull()
    expect([...after.exports.keys()]).toEqual(['/x/a.docx', '/x/b.docx'])
  })
})

describe('opening a Word document', () => {
  it.each([
    ['a.docx', true],
    ['mapa/A.DOC', true],
    ['a.md', false],
    ['a.txt', false],
    ['doc', false],
    ['docx.md', false],
    ['a.docx.md', false],
  ])('isWordRel(%s) is %s', (rel, expected) => {
    expect(isWordRel(rel)).toBe(expected)
  })

  it('a Word request marks the opening until it settles', () => {
    const s = openExecuting(initialState, req(1, 'a.docx'))
    expect(s.wordOpens).toEqual([1])
    expect(openSettled(s, 1).wordOpens).toEqual([])
  })

  it('a Markdown request does not mark anything', () => {
    expect(openExecuting(initialState, req(1, 'a.md'))).toBe(initialState)
  })

  it('stays marked while another Word request runs', () => {
    const s = openExecuting(openExecuting(initialState, req(1, 'a.docx')), req(2, 'b.doc'))
    expect(openSettled(s, 1).wordOpens).toEqual([2])
  })

  it('settling an unknown request changes nothing (also after nvim:exit)', () => {
    const s = openExecuting(initialState, req(1, 'a.docx'))
    expect(openSettled(s, 9)).toBe(s)
    expect(openSettled(nvimExited(s).state, 1).wordOpens).toEqual([])
  })
})

describe('SaveCopy', () => {
  it('is in flight until it returns', () => {
    const s = copyStarted(initialState)
    expect(s.savingCopy).toBe(true)
    expect(copyFinished(s, null)).toMatchObject({ savingCopy: false, copyError: null })
  })

  it('keeps the error of a failure', () => {
    expect(copyFinished(copyStarted(initialState), 'read-only')).toMatchObject({ savingCopy: false, copyError: 'read-only' })
  })

  it('does not treat canceled as an error', () => {
    expect(copyFinished(copyStarted(initialState), 'canceled').copyError).toBeNull()
  })

  it('a new attempt clears the old error', () => {
    expect(copyStarted(copyFinished(copyStarted(initialState), 'conflict')).copyError).toBeNull()
  })
})
