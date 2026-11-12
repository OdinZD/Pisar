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

  describe('run', () => {
    // A call whose completion is controlled by hand, logging when it starts.
    function manualCall(log: string[], name: string) {
      let done: () => void = () => {}
      const call = () => {
        log.push(name)
        return new Promise<void>((resolve) => {
          done = resolve
        })
      }
      return { call, finish: () => done() }
    }

    it('keeps "keys, format, keys" in order when the first send takes long', async () => {
      const m = manualSend()
      const log: string[] = []
      const s = createSerialSender(m.send, () => {})
      const format = manualCall(log, 'format')

      s.send('text') // in flight
      s.run(format.call) // typed "Cmd+B" right after
      s.send('more')
      s.send('!')
      expect(m.calls, 'only the first send started').toEqual(['text'])
      expect(log, 'the format waits for the keys before it').toEqual([])

      m.resolvers[0].ok()
      await tick()
      expect(log).toEqual(['format'])
      expect(m.calls, 'the keys after it wait for the format').toEqual(['text'])

      format.finish()
      await tick()
      expect(m.calls, 'keys after the format are joined').toEqual(['text', 'more!'])
    })

    it('runs the call at once when nothing is queued', async () => {
      const m = manualSend()
      const log: string[] = []
      const s = createSerialSender(m.send, () => {})
      const format = manualCall(log, 'format')

      s.run(format.call)
      expect(log).toEqual(['format'])
      s.send('a')
      expect(m.calls).toEqual([])
      format.finish()
      await tick()
      expect(m.calls).toEqual(['a'])
    })

    it('keeps two formats in the order they were chosen, however long the first takes', async () => {
      const log: string[] = []
      const s = createSerialSender(async () => {}, () => {})
      const first = manualCall(log, 'bold')
      const second = manualCall(log, 'italic')

      s.run(first.call)
      s.run(second.call)
      await tick()
      expect(log, 'the second does not overtake the first').toEqual(['bold'])
      first.finish()
      await tick()
      expect(log).toEqual(['bold', 'italic'])
    })

    it('does not join the data on both sides of a call', async () => {
      const m = manualSend()
      const s = createSerialSender(m.send, () => {})
      const format = manualCall([], 'format')

      s.send('a') // in flight
      s.send('b')
      s.run(format.call)
      s.send('c')
      s.send('d')
      m.resolvers[0].ok()
      await tick()
      expect(m.calls, '"b" was sent before the format started').toEqual(['a', 'b'])
      m.resolvers[1].ok()
      await tick()
      format.finish()
      await tick()
      expect(m.calls).toEqual(['a', 'b', 'cd'])
    })

    it('reports a failed call and goes on', async () => {
      const m = manualSend()
      const errors: unknown[] = []
      const s = createSerialSender(m.send, (e) => errors.push(e))

      s.run(() => Promise.reject(new Error('rpc')))
      s.run(() => {
        throw new Error('sync')
      })
      s.send('a')
      await tick()

      expect(errors.map((e) => (e as Error).message)).toEqual(['rpc', 'sync'])
      expect(m.calls).toEqual(['a'])
    })
  })
})
