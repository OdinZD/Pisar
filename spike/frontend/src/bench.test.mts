import { test } from 'node:test'
import assert from 'node:assert/strict'

import { createBench, createClock } from './bench.ts'

const end = { renderer: 'dom', cols: 100, rows: 30, screenRows: ['a', 'b'] }

test('onEvent returns indexes in order and onParsed fills the right one', () => {
  const b = createBench()
  b.start('repeat')
  assert.equal(b.onEvent(10, 100), 0)
  assert.equal(b.onEvent(20, 110), 1)
  assert.equal(b.onEvent(30, 120), 2)
  assert.equal(b.unparsed(), 3)
  // Parsing finishes out of the order the events arrived in the test, but xterm.js
  // would keep order; the collector must not depend on it.
  b.onParsed(1, 115)
  b.onParsed(0, 105)
  assert.equal(b.unparsed(), 1)
  const r = b.finish(end)
  assert.deepEqual(r.RecvMs, [100, 110, 120])
  assert.deepEqual(r.ParsedMs, [105, 115, -1])
  assert.deepEqual(r.Units, [10, 20, 30])
})

test('frame gaps come from consecutive frame times', () => {
  const b = createBench()
  b.start('flood')
  b.onFrame(1000)
  b.onFrame(1016.5)
  b.onFrame(1050)
  b.onFrame(1200)
  const r = b.finish(end)
  assert.deepEqual(r.FrameGaps, [16.5, 33.5, 150])
})

test('nothing is collected outside a phase', () => {
  const b = createBench()
  assert.equal(b.active(), false)
  assert.equal(b.onEvent(5, 1), -1)
  b.onParsed(0, 2)
  b.onParsed(-1, 2)
  b.onFrame(10)
  b.onFrame(20)
  b.markHidden()
  b.start('repeat')
  assert.equal(b.active(), true)
  assert.equal(b.onEvent(1, 1), 0) // starts from zero, the earlier calls left nothing
  const r = b.finish(end)
  assert.equal(r.Visible, true)
  assert.deepEqual(r.FrameGaps, [])
  assert.equal(b.active(), false)
  // After finish the collector is idle again.
  assert.equal(b.onEvent(1, 1), -1)
  b.onFrame(30)
  assert.equal(b.finish(end).RecvMs.length, 1) // a second finish still returns the old data, but adds nothing
})

test('finish returns arrays of equal length for the events', () => {
  const b = createBench()
  b.start('repeat')
  for (let i = 0; i < 50; i++) {
    const idx = b.onEvent(i, 1000 + i)
    if (i % 3 !== 0) b.onParsed(idx, 1001 + i)
  }
  const r = b.finish(end)
  assert.equal(r.RecvMs.length, 50)
  assert.equal(r.ParsedMs.length, 50)
  assert.equal(r.Units.length, 50)
  assert.equal(r.ParsedMs.filter((v) => v < 0).length, 17)
  assert.equal(r.Renderer, 'dom')
  assert.equal(r.Cols, 100)
  assert.equal(r.Rows, 30)
  assert.deepEqual(r.ScreenRows, ['a', 'b'])
})

test('onParsed ignores an unknown index and a second report for one event', () => {
  const b = createBench()
  b.start('repeat')
  const i = b.onEvent(1, 1)
  b.onParsed(7, 5)
  b.onParsed(i, 5)
  b.onParsed(i, 9)
  assert.equal(b.unparsed(), 0)
  assert.deepEqual(b.finish(end).ParsedMs, [5])
})

test('start drops what an earlier phase collected, and hidden is reported', () => {
  const b = createBench()
  b.start('repeat')
  b.onEvent(1, 1)
  b.markHidden()
  assert.equal(b.finish(end).Visible, false)
  b.start('flood')
  assert.equal(b.phase(), 'flood')
  const r = b.finish(end)
  assert.equal(r.Visible, true)
  assert.equal(r.RecvMs.length, 0)
})

test('the clock uses timeOrigin when there is one, Date.now otherwise', () => {
  const a = createClock({ now: () => 250.5, timeOrigin: 1_700_000_000_000 })
  assert.equal(a.source, 'timeOrigin')
  assert.equal(a.now(), 1_700_000_000_250.5)

  const b = createClock({ now: () => 1 })
  assert.equal(b.source, 'Date.now')
  assert.ok(Math.abs(b.now() - Date.now()) < 1000)

  const real = createClock()
  assert.ok(Math.abs(real.now() - Date.now()) < 1000)
})
