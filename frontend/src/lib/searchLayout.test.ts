import { describe, expect, it } from 'vitest'
import { statusInset } from './searchLayout'

describe('statusInset', () => {
  it.each([
    // 30 rows of 21 px = 630 px, in a column of exactly 630 px: the last two rows are 42 px.
    [630, 30, 21, 2, 42],
    // The remainder of the fit (here 10 px) lies below the grid and belongs to the inset.
    [640, 30, 21, 2, 52],
    // A different number of reserved rows.
    [630, 30, 21, 1, 21],
    [630, 30, 21, 0, 0],
    // Fractional cells (a Retina display) are kept as they are.
    [100, 4, 22.5, 2, 55],
    // Not more rows than reserved: no row of the grid is covered, so the inset is the whole column.
    [42, 2, 21, 2, 42],
    [21, 1, 21, 2, 21],
  ])('column %d px, %d rows of %d px, %d reserved -> %d px', (height, rows, cell, reserved, expected) => {
    expect(statusInset(height, rows, cell, reserved)).toBe(expected)
  })

  it.each([
    ['unmeasured cell', 630, 30, 0],
    ['negative cell', 630, 30, -1],
    ['NaN cell', 630, 30, NaN],
    ['no rows', 630, 0, 21],
    ['NaN height', NaN, 30, 21],
    ['infinite height', Infinity, 30, 21],
  ])('%s gives 0', (_, height, rows, cell) => {
    expect(statusInset(height, rows, cell, 2)).toBe(0)
  })

  it('is never negative', () => {
    expect(statusInset(10, 30, 21, 2)).toBe(0)
  })
})
