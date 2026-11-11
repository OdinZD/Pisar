import { test } from 'node:test'
import assert from 'node:assert/strict'

import { createSerialSender } from './serialSender.ts'

// A send function whose calls are completed by hand, to control timing.
function manualSend() {
  const calls: string[] = []
  const resolvers: Array<{ ok: () => void; fail: (e: Error) => void }> = []
  const send = (data: string) => {
    calls.push(data)
    return new Promise<void>((ok, fail) => resolvers.push({ ok, fail }))
  }
  return { calls, resolvers, send }
}

// Lets queued promise callbacks run.
const tick = () => new Promise<void>((r) => setImmediate(r))

test('keeps order when data arrives faster than sends finish', async () => {
  const m = manualSend()
  const s = createSerialSender(m.send, () => {})

  s.send('a')
  s.send('b')
  s.send('c')
  assert.deepEqual(m.calls, ['a'], 'only one send in flight')

  m.resolvers[0].ok()
  await tick()
  assert.deepEqual(m.calls, ['a', 'bc'])

  s.send('d')
  assert.deepEqual(m.calls, ['a', 'bc'], 'still one in flight')
  m.resolvers[1].ok()
  await tick()
  assert.deepEqual(m.calls, ['a', 'bc', 'd'])
  assert.equal(m.calls.join(''), 'abcd')
})

test('data that arrives during a send is joined into one call', async () => {
  const m = manualSend()
  const s = createSerialSender(m.send, () => {})

  s.send('1')
  for (const ch of ['2', '3', '4', '5']) s.send(ch)
  m.resolvers[0].ok()
  await tick()

  assert.deepEqual(m.calls, ['1', '2345'])
})

test('a failed send does not lose the data queued behind it', async () => {
  const m = manualSend()
  const errors: unknown[] = []
  const s = createSerialSender(m.send, (e) => errors.push(e))

  s.send('a')
  s.send('b')
  m.resolvers[0].fail(new Error('boom'))
  await tick()

  assert.deepEqual(m.calls, ['a', 'b'])
  assert.equal(errors.length, 1)
  assert.equal((errors[0] as Error).message, 'boom')

  // The queue still works afterwards.
  m.resolvers[1].ok()
  await tick()
  s.send('c')
  assert.deepEqual(m.calls, ['a', 'b', 'c'])
})

test('a send function that throws synchronously does not stop the queue', async () => {
  const calls: string[] = []
  const errors: unknown[] = []
  const send = (data: string): Promise<void> => {
    calls.push(data)
    if (data === 'x') throw new Error('sync')
    return Promise.resolve()
  }
  const s = createSerialSender(send, (e) => errors.push(e))

  s.send('x')
  s.send('y')
  await tick()

  assert.deepEqual(calls, ['x', 'y'])
  assert.equal(errors.length, 1)
})

test('an empty string is not sent', async () => {
  const m = manualSend()
  const s = createSerialSender(m.send, () => {})

  s.send('')
  assert.deepEqual(m.calls, [])

  s.send('a')
  s.send('')
  m.resolvers[0].ok()
  await tick()
  assert.deepEqual(m.calls, ['a'], 'empty string queued nothing')
})

test('after the queue is empty the next data is sent immediately', async () => {
  const m = manualSend()
  const s = createSerialSender(m.send, () => {})

  s.send('a')
  m.resolvers[0].ok()
  await tick()

  s.send('b')
  assert.deepEqual(m.calls, ['a', 'b'], 'sent synchronously, no waiting')
  m.resolvers[1].ok()
  await tick()

  s.send('c')
  assert.deepEqual(m.calls, ['a', 'b', 'c'])
})
