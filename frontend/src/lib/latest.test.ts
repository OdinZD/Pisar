import { describe, expect, it } from 'vitest'
import { createLatestTracker } from './latest'

describe('createLatestTracker', () => {
  it('rejects an older response that arrives after a newer request began', () => {
    const t = createLatestTracker()
    const first = t.begin('p1')
    const second = t.begin('p1')
    expect(t.isLatest('p1', first)).toBe(false)
    expect(t.isLatest('p1', second)).toBe(true)
  })

  it('does not let requests for different keys disturb each other', () => {
    const t = createLatestTracker()
    const a = t.begin('p1')
    const b = t.begin('p2')
    expect(t.isLatest('p1', a)).toBe(true)
    expect(t.isLatest('p2', b)).toBe(true)
  })
})
