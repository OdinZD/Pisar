import { describe, expect, it } from 'vitest'
import { createSerialSender } from './serialSender'

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

// Lets queued promise callbacks run (all microtasks drain before a timer fires).
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0))

describe('createSerialSender', () => {
  it('keeps order when data arrives faster than sends finish', async () => {
    const m = manualSend()
    const s = createSerialSender(m.send, () => {})

    s.send('a')
    s.send('b')
    s.send('c')
    expect(m.calls, 'only one send in flight').toEqual(['a'])

    m.resolvers[0].ok()
    await tick()
    expect(m.calls).toEqual(['a', 'bc'])

    s.send('d')
    expect(m.calls, 'still one in flight').toEqual(['a', 'bc'])
    m.resolvers[1].ok()
    await tick()
    expect(m.calls).toEqual(['a', 'bc', 'd'])
    expect(m.calls.join('')).toBe('abcd')
  })

  it('joins data that arrives during a send into one call', async () => {
    const m = manualSend()
    const s = createSerialSender(m.send, () => {})

    s.send('1')
    for (const ch of ['2', '3', '4', '5']) s.send(ch)
    m.resolvers[0].ok()
    await tick()

    expect(m.calls).toEqual(['1', '2345'])
  })

  it('does not lose the data queued behind a failed send', async () => {
    const m = manualSend()
    const errors: unknown[] = []
    const s = createSerialSender(m.send, (e) => errors.push(e))

    s.send('a')
    s.send('b')
    m.resolvers[0].fail(new Error('boom'))
    await tick()

    expect(m.calls).toEqual(['a', 'b'])
    expect(errors).toHaveLength(1)
    expect((errors[0] as Error).message).toBe('boom')

    // The queue still works afterwards.
    m.resolvers[1].ok()
    await tick()
    s.send('c')
    expect(m.calls).toEqual(['a', 'b', 'c'])
  })

  it('keeps going when the send function throws synchronously', async () => {
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

    expect(calls).toEqual(['x', 'y'])
    expect(errors).toHaveLength(1)
  })

  it('does not send an empty string', async () => {
    const m = manualSend()
    const s = createSerialSender(m.send, () => {})

    s.send('')
    expect(m.calls).toEqual([])

    s.send('a')
    s.send('')
    m.resolvers[0].ok()
    await tick()
    expect(m.calls, 'empty string queued nothing').toEqual(['a'])
  })

  it('sends the next data immediately once the queue is empty', async () => {
    const m = manualSend()
    const s = createSerialSender(m.send, () => {})

    s.send('a')
    m.resolvers[0].ok()
    await tick()

    s.send('b')
    expect(m.calls, 'sent synchronously, no waiting').toEqual(['a', 'b'])
    m.resolvers[1].ok()
    await tick()

    s.send('c')
    expect(m.calls).toEqual(['a', 'b', 'c'])
  })
})
