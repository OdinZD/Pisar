import { describe, expect, it } from 'vitest'
import { strings } from '../strings'
import {
  activateSelected,
  countText,
  dismissed,
  initialSearchState,
  opened,
  openSettled,
  queryChanged,
  rowSelected,
  scopeTarget,
  scopeToggled,
  searchArrived,
  searchFailed,
  searchView,
  selectionMoved,
  type SearchHit,
  type SearchState,
  type Transition,
} from './searchState'

const hit = (rel: string, over: Partial<SearchHit> = {}): SearchHit => ({
  projectId: 'p1',
  projectName: 'Projekt',
  rel,
  name: rel.split('/').pop() ?? rel,
  kind: 'md',
  matches: [[0, 1]],
  ...over,
})
const hits = [hit('a.md'), hit('b.md'), hit('c.md')]
const scope = { projectId: 'p1', name: 'Projekt' }

const state = (over: Partial<SearchState> = {}): SearchState => ({ ...initialSearchState, open: true, ...over })
// An open panel that has an answer on screen.
const answered = (over: Partial<SearchState> = {}): SearchState =>
  state({ query: 'a', hits, selected: 0, lastToken: 5, ...over })

const searchEffect = (t: Transition) => t.effects.find((e) => e.type === 'search')

describe('opened', () => {
  it('opens a closed panel empty and asks for the focus (either event)', () => {
    const t = opened(initialSearchState)
    expect(t.state.open).toBe(true)
    expect(t.state.query).toBe('')
    expect(t.effects).toEqual([{ type: 'focus-input' }])
  })

  it('does not clear the query of an open panel, it only takes the keyboard back', () => {
    const before = answered()
    const t = opened(before)
    expect(t.state).toBe(before)
    expect(t.effects).toEqual([{ type: 'focus-input' }])
  })

  it('starts empty after a close, but keeps counting tokens', () => {
    const closed = dismissed(answered({ scope, lastToken: 9 }), true).state
    const t = opened(closed)
    expect(t.state).toEqual({ ...initialSearchState, open: true, lastToken: 9 })
  })
})

describe('queryChanged', () => {
  it('asks for a search with a new token', () => {
    const t = queryChanged(state({ lastToken: 3 }), 'plan')
    expect(t.state.query).toBe('plan')
    expect(t.state.pending).toEqual({ token: 4, query: 'plan', projectId: '' })
    expect(t.effects).toEqual([{ type: 'search', token: 4, query: 'plan', projectId: '' }])
  })

  it('searches the project of the scope', () => {
    const t = queryChanged(state({ scope }), 'plan')
    expect(searchEffect(t)).toMatchObject({ projectId: 'p1' })
  })

  it('sends the query without surrounding blanks', () => {
    expect(searchEffect(queryChanged(state(), '  plan  '))).toMatchObject({ query: 'plan' })
  })

  it('gives every change its own token', () => {
    const first = queryChanged(state(), 'p')
    const second = queryChanged(first.state, 'pl')
    expect(first.state.pending?.token).not.toBe(second.state.pending?.token)
    expect(second.state.pending?.query).toBe('pl')
  })

  it('keeps the old hits on screen while the new answer is awaited', () => {
    const t = queryChanged(answered(), 'ab')
    expect(t.state.hits).toBe(hits)
  })

  it('an empty or blank query empties the result and asks for nothing', () => {
    for (const blank of ['', '   ']) {
      const t = queryChanged(answered({ query: 'a', pending: { token: 7, query: 'a', projectId: '' } }), blank)
      expect(t.state).toMatchObject({ query: blank, hits: [], selected: -1, pending: null, error: null })
      expect(t.effects).toEqual([])
    }
  })

  it('an emptied query makes the answer of the earlier request useless', () => {
    const asked = queryChanged(state(), 'plan')
    const token = asked.state.pending!.token
    const emptied = queryChanged(asked.state, '')
    const late = searchArrived(emptied.state, token, hits)
    expect(late.state.hits).toEqual([])
  })

  it('clears the error of the last attempt', () => {
    expect(queryChanged(answered({ error: 'internal' }), 'ab').state.error).toBeNull()
  })

  it('ignores the same text again and a closed panel', () => {
    const open = answered()
    expect(queryChanged(open, 'a').state).toBe(open)
    expect(queryChanged(initialSearchState, 'a').state).toBe(initialSearchState)
  })
})

describe('only the newest answer counts', () => {
  it('drops the answer of an older request and uses the newer one', () => {
    const first = queryChanged(state(), 'p')
    const second = queryChanged(first.state, 'pl')
    const oldToken = first.state.pending!.token
    const newToken = second.state.pending!.token

    const stale = searchArrived(second.state, oldToken, [hit('old.md')])
    expect(stale.state).toBe(second.state)

    const fresh = searchArrived(stale.state, newToken, hits)
    expect(fresh.state.hits).toBe(hits)
    expect(fresh.state.pending).toBeNull()
  })

  it('also when the newer answer comes first', () => {
    const first = queryChanged(state(), 'p')
    const second = queryChanged(first.state, 'pl')
    const newer = searchArrived(second.state, second.state.pending!.token, hits)
    const older = searchArrived(newer.state, first.state.pending!.token, [hit('old.md')])
    expect(older.state.hits).toBe(hits)
  })

  it('drops the answer of an older failure', () => {
    const first = queryChanged(state(), 'p')
    const second = queryChanged(first.state, 'pl')
    const t = searchFailed(second.state, first.state.pending!.token, 'internal')
    expect(t.state).toBe(second.state)
  })

  it('drops an answer that arrives after the panel was closed and opened again', () => {
    const asked = queryChanged(state(), 'plan')
    const token = asked.state.pending!.token
    const reopened = opened(dismissed(asked.state, true).state).state
    expect(searchArrived(reopened, token, hits).state).toBe(reopened)
    // ...even when the new session has asked for something itself
    const again = queryChanged(reopened, 'plan').state
    expect(again.pending!.token).not.toBe(token)
    expect(searchArrived(again, token, hits).state).toBe(again)
  })

  it('drops an answer when the panel is closed', () => {
    const asked = queryChanged(state(), 'plan')
    const closed = dismissed(asked.state, false).state
    expect(searchArrived(closed, asked.state.pending!.token, hits).state).toBe(closed)
  })
})

describe('searchArrived', () => {
  const asked = () => queryChanged(state(), 'a').state

  it('selects the first row', () => {
    const t = searchArrived(asked(), asked().pending!.token, hits)
    expect(t.state).toMatchObject({ hits, selected: 0, pending: null, error: null })
  })

  it('selects the first row again after a new answer, wherever the selection was', () => {
    const before = answered({ selected: 2 })
    const re = queryChanged(before, 'ab').state
    const t = searchArrived(re, re.pending!.token, hits)
    expect(t.state.selected).toBe(0)
  })

  it('selects nothing when there are no hits', () => {
    const s = asked()
    expect(searchArrived(s, s.pending!.token, []).state).toMatchObject({ hits: [], selected: -1 })
  })

  it('takes null for an empty list', () => {
    const s = asked()
    const t = searchArrived(s, s.pending!.token, null)
    expect(t.state).toMatchObject({ hits: [], selected: -1, pending: null })
  })
})

describe('searchFailed', () => {
  const asked = () => queryChanged(answered(), 'ab').state

  it('shows the error by its code and empties the list', () => {
    const s = asked()
    const t = searchFailed(s, s.pending!.token, 'internal')
    expect(t.state).toMatchObject({ error: 'internal', hits: [], selected: -1, pending: null })
    expect(searchView(t.state).message).toEqual({ text: strings.errorGeneric, danger: true })
  })

  it('shows nothing for canceled and keeps what was on screen', () => {
    const s = asked()
    const t = searchFailed(s, s.pending!.token, 'canceled')
    expect(t.state).toMatchObject({ error: null, hits, pending: null })
    expect(searchView(t.state).message).toBeNull()
  })

  it('the next query clears the error', () => {
    const s = asked()
    const failed = searchFailed(s, s.pending!.token, 'not-found').state
    expect(queryChanged(failed, 'abc').state.error).toBeNull()
  })
})

describe('selectionMoved', () => {
  it('moves down and up', () => {
    const down = selectionMoved(answered(), 1)
    expect(down.state.selected).toBe(1)
    expect(selectionMoved(down.state, -1).state.selected).toBe(0)
  })

  it('stops at the ends, without wrapping', () => {
    const top = answered({ selected: 0 })
    expect(selectionMoved(top, -1).state).toBe(top)
    const bottom = answered({ selected: 2 })
    expect(selectionMoved(bottom, 1).state).toBe(bottom)
  })

  it('does nothing without results', () => {
    const none = state({ query: 'zzz' })
    expect(selectionMoved(none, 1).state).toBe(none)
    expect(selectionMoved(none, -1).state).toBe(none)
  })

  it('does nothing in a closed panel', () => {
    const closed = { ...answered(), open: false }
    expect(selectionMoved(closed, 1).state).toBe(closed)
  })
})

describe('rowSelected', () => {
  it('selects a row by index and ignores bad indexes', () => {
    expect(rowSelected(answered(), 2).selected).toBe(2)
    const s = answered()
    for (const i of [-1, 3, 1.5, NaN]) expect(rowSelected(s, i)).toBe(s)
  })
})

describe('scopeToggled', () => {
  it('narrows to the project of the active document and searches again there', () => {
    const t = scopeToggled(answered({ lastToken: 5 }), scope)
    expect(t.state.scope).toEqual(scope)
    expect(t.effects).toEqual([{ type: 'search', token: 6, query: 'a', projectId: 'p1' }])
    expect(t.state.pending).toEqual({ token: 6, query: 'a', projectId: 'p1' })
  })

  it('goes back to all projects and searches again there', () => {
    const t = scopeToggled(answered({ scope }), scope)
    expect(t.state.scope).toBeNull()
    expect(searchEffect(t)).toMatchObject({ projectId: '' })
  })

  it('goes back to all projects even when the active document has no project any more', () => {
    expect(scopeToggled(answered({ scope }), null).state.scope).toBeNull()
  })

  it('does nothing without a project to narrow to', () => {
    const s = answered()
    const t = scopeToggled(s, null)
    expect(t.state).toBe(s)
    expect(t.effects).toEqual([])
  })

  it('with an empty query it changes the scope and asks for nothing', () => {
    const t = scopeToggled(state(), scope)
    expect(t.state.scope).toEqual(scope)
    expect(t.effects).toEqual([])
  })

  it('makes the answer of the search in the old scope useless', () => {
    const before = queryChanged(state(), 'plan').state
    const t = scopeToggled(before, scope)
    expect(searchArrived(t.state, before.pending!.token, hits).state).toBe(t.state)
  })
})

describe('scopeTarget', () => {
  const projects = [
    { id: 'p1', name: 'Prvi' },
    { id: 'p2', name: 'Drugi' },
  ]
  it('is the project of the active document', () => {
    expect(scopeTarget('p2', projects)).toEqual({ projectId: 'p2', name: 'Drugi' })
  })
  it.each([[undefined], [''], ['gone']])('is null for %j', (id) => {
    expect(scopeTarget(id, projects)).toBeNull()
  })
})

describe('activateSelected', () => {
  it('opens the selected hit', () => {
    const t = activateSelected(answered({ selected: 1, lastToken: 5 }))
    expect(t.effects).toEqual([{ type: 'open', token: 6, projectId: 'p1', rel: 'b.md' }])
    expect(t.state.opening).toBe(6)
  })

  it('opens the hit of its own project', () => {
    const other = [hit('x.md', { projectId: 'p2' })]
    const t = activateSelected(answered({ hits: other, selected: 0 }))
    expect(t.effects[0]).toMatchObject({ type: 'open', projectId: 'p2', rel: 'x.md' })
  })

  it.each([
    ['nothing selected', answered({ selected: -1 })],
    ['no results', state({ query: 'zzz' })],
    ['an answer is awaited', answered({ pending: { token: 9, query: 'ab', projectId: '' } })],
    ['an open is already running', answered({ opening: 8 })],
    ['the panel is closed', { ...answered(), open: false }],
  ])('does nothing when %s', (_, s) => {
    const t = activateSelected(s)
    expect(t.state).toBe(s)
    expect(t.effects).toEqual([])
  })

  it('clears an old error', () => {
    expect(activateSelected(answered({ error: 'not-found' })).state.error).toBeNull()
  })
})

describe('openSettled', () => {
  const opening = () => activateSelected(answered()).state

  it('closes the panel, empties it and gives the keyboard to the editor on success', () => {
    const s = opening()
    const t = openSettled(s, s.opening!, null)
    expect(t.state).toEqual({ ...initialSearchState, lastToken: s.lastToken })
    expect(t.effects).toEqual([{ type: 'focus-terminal' }])
  })

  it('keeps the panel on failure, with the error, and takes the keyboard back into the field', () => {
    const s = opening()
    const t = openSettled(s, s.opening!, 'not-found')
    expect(t.state).toMatchObject({ open: true, query: 'a', hits, selected: 0, opening: null, error: 'not-found' })
    expect(t.effects).toEqual([{ type: 'focus-input' }])
    expect(searchView(t.state).message).toEqual({ text: strings.errorNotFound, danger: true })
  })

  it('shows no message for canceled, keeps the panel and can try again', () => {
    const s = opening()
    const t = openSettled(s, s.opening!, 'canceled')
    expect(t.state).toMatchObject({ open: true, error: null, opening: null })
    expect(searchView(t.state).message).toBeNull()
    expect(activateSelected(t.state).effects).toHaveLength(1)
  })

  it('ignores an outcome of an open that is not the current one', () => {
    const s = opening()
    expect(openSettled(s, s.opening! + 1, null).state).toBe(s)
    expect(openSettled(answered(), 6, null).state.open).toBe(true)
  })

  it('ignores the outcome when the panel was closed meanwhile', () => {
    const s = opening()
    const closed = dismissed(s, true).state
    const t = openSettled(closed, s.opening!, null)
    expect(t.state).toBe(closed)
    expect(t.effects).toEqual([])
  })

  it('ignores the outcome when the panel was closed and opened again meanwhile', () => {
    const s = opening()
    const reopened = opened(dismissed(s, true).state).state
    expect(openSettled(reopened, s.opening!, null).state).toBe(reopened)
  })
})

describe('dismissed', () => {
  it('Esc closes, clears query, hits, scope and error, and asks for the focus', () => {
    const s = answered({ scope, error: 'internal', pending: { token: 6, query: 'a', projectId: 'p1' }, opening: 7, lastToken: 7 })
    const t = dismissed(s, true)
    expect(t.state).toEqual({ ...initialSearchState, lastToken: 7 })
    expect(t.effects).toEqual([{ type: 'focus-terminal' }])
  })

  it('a click elsewhere closes the same way but leaves the keyboard where the click put it', () => {
    const t = dismissed(answered(), false)
    expect(t.state.open).toBe(false)
    expect(t.effects).toEqual([])
  })

  it('does nothing when it is closed already', () => {
    expect(dismissed(initialSearchState, true)).toEqual({ state: initialSearchState, effects: [] })
  })
})

describe('searchView', () => {
  it('shows only the hints for an empty query', () => {
    const v = searchView(state())
    expect(v).toMatchObject({ rows: [], message: null, announce: '', scopeLabel: strings.searchScopeAll })
  })

  it('says there are no documents for a query without hits', () => {
    const v = searchView(state({ query: 'zzz' }))
    expect(v.message).toEqual({ text: strings.searchNoResults, danger: false })
    expect(v.announce).toBe(strings.searchNoResults)
  })

  it('does not say it while the answer is awaited', () => {
    const v = searchView(queryChanged(state(), 'zzz').state)
    expect(v.message).toBeNull()
    expect(v.announce).toBe('')
  })

  describe('"no documents" while the next answer is awaited', () => {
    const noDocs = { text: strings.searchNoResults, danger: false }
    // an open panel whose last answer to "zzz" was empty
    const emptied = () => {
      const asked = queryChanged(state(), 'zzz').state
      return searchArrived(asked, asked.pending!.token, []).state
    }

    it('keeps the line on screen after another character and after the second empty answer', () => {
      const next = queryChanged(emptied(), 'zzzz').state
      expect(next.pending).not.toBeNull()
      expect(searchView(next).message).toEqual(noDocs)
      const again = searchArrived(next, next.pending!.token, []).state
      expect(searchView(again).message).toEqual(noDocs)
    })

    it('drops the line when hits arrive, and the next character no longer brings it back', () => {
      const next = queryChanged(emptied(), 'zz').state
      const found = searchArrived(next, next.pending!.token, hits).state
      expect(searchView(found).message).toBeNull()
      expect(searchView(queryChanged(found, 'zzz').state).message).toBeNull()
    })

    it('keeps the line when the scope changes while waiting', () => {
      const t = scopeToggled(emptied(), scope)
      expect(t.state.pending).not.toBeNull()
      expect(searchView(t.state).message).toEqual(noDocs)
    })

    it('drops the line when the query is emptied, and a new query waits without it', () => {
      const cleared = queryChanged(emptied(), '').state
      expect(searchView(cleared).message).toBeNull()
      expect(searchView(queryChanged(cleared, 'a').state).message).toBeNull()
    })

    it('shows the error instead, and the next character shows neither', () => {
      const next = queryChanged(emptied(), 'zzzz').state
      const failed = searchFailed(next, next.pending!.token, 'internal').state
      expect(searchView(failed).message).toEqual({ text: strings.errorGeneric, danger: true })
      expect(searchView(queryChanged(failed, 'zzzzz').state).message).toBeNull()
    })

    it('does not carry over a close and reopen: the first query waits without the line', () => {
      const reopened = opened(dismissed(emptied(), true).state).state
      expect(searchView(queryChanged(reopened, 'a').state).message).toBeNull()
    })

    it('keeps the line after a canceled answer', () => {
      const next = queryChanged(emptied(), 'zzzz').state
      const canceled = searchFailed(next, next.pending!.token, 'canceled').state
      expect(canceled.pending).toBeNull()
      expect(searchView(canceled).message).toEqual(noDocs)
    })

    it('a late empty answer with an old token does not set the flag', () => {
      const first = queryChanged(state(), 'a').state
      const second = queryChanged(first, 'ab').state
      const late = searchArrived(second, first.pending!.token, [])
      expect(late.state).toBe(second)
      expect(late.state.emptyAnswer).toBe(false)
      expect(searchView(late.state).message).toBeNull()
    })

    it('still says nothing to screen readers while the answer is awaited', () => {
      expect(searchView(queryChanged(emptied(), 'zzzz').state).announce).toBe('')
    })
  })

  it('builds rows without extension, highlighted, with the project and the selection', () => {
    const v = searchView(answered({ selected: 1 }))
    // each hit file is one letter long, matched whole
    expect(v.rows.map((r) => r.segments)).toEqual([
      [{ text: 'a', hit: true }],
      [{ text: 'b', hit: true }],
      [{ text: 'c', hit: true }],
    ])
    expect(v.rows[0].projectName).toBe('Projekt')
    expect(v.selected).toBe(1)
    expect(new Set(v.rows.map((r) => r.key)).size).toBe(3)
    expect(v.announce).toBe('3 dokumenta')
  })

  it('shows null matches without highlight', () => {
    const v = searchView(answered({ hits: [hit('plan.md', { matches: null })], selected: 0 }))
    expect(v.rows[0].segments).toEqual([{ text: 'plan', hit: false }])
  })

  it('tells the name of the project when the scope is one', () => {
    expect(searchView(state({ scope: { projectId: 'p1', name: 'Diplomski' } })).scopeLabel).toBe('Diplomski')
  })

  it('keeps the same name in two projects apart', () => {
    const v = searchView(answered({ hits: [hit('a.md'), hit('a.md', { projectId: 'p2' })] }))
    expect(new Set(v.rows.map((r) => r.key)).size).toBe(2)
  })
})

describe('countText', () => {
  it.each([
    [1, '1 dokument'],
    [2, '2 dokumenta'],
    [4, '4 dokumenta'],
    [5, '5 dokumenata'],
    [11, '11 dokumenata'],
    [12, '12 dokumenata'],
    [14, '14 dokumenata'],
    [21, '21 dokument'],
    [22, '22 dokumenta'],
    [50, '50 dokumenata'],
  ])('%d -> %s', (n, expected) => {
    expect(countText(n)).toBe(expected)
  })
})
