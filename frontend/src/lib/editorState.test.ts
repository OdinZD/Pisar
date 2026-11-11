import { describe, expect, it } from 'vitest'
import {
  documentActive,
  documentState,
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
const doc = (rel: string): ActiveDocument => ({ projectId: 'p', rel, path: `/x/${rel}`, name: rel })
const empty: ActiveDocument = { projectId: '', rel: '', path: '', name: '' }

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
    const outside: ActiveDocument = { projectId: '', rel: '', path: '/tmp/x.md', name: 'x.md' }
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
