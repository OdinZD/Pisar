import { describe, expect, it } from 'vitest'
import type { ThemeId } from '../theme/tokens'
import { createThemeSender } from './themeSender'

// A send function whose calls are completed by hand; counts how many run at the same time.
function manualSend() {
  const calls: ThemeId[] = []
  const resolvers: Array<{ ok: () => void; fail: (e: Error) => void }> = []
  let running = 0
  let maxRunning = 0
  const send = (id: ThemeId) => {
    calls.push(id)
    running++
    maxRunning = Math.max(maxRunning, running)
    return new Promise<void>((ok, fail) =>
      resolvers.push({
        ok: () => {
          running--
          ok()
        },
        fail: (e) => {
          running--
          fail(e)
        },
      }),
    )
  }
  return { calls, resolvers, send, maxRunning: () => maxRunning }
}

// Lets queued promise callbacks run (all microtasks drain before a timer fires).
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0))

describe('createThemeSender', () => {
  it('sends one request once', async () => {
    const m = manualSend()
    const s = createThemeSender(m.send, () => {})
    s.request('dracula')
    expect(m.calls).toEqual(['dracula'])
    m.resolvers[0].ok()
    await s.idle()
    expect(m.calls).toEqual(['dracula'])
  })

  it('keeps only the newest request made while one is in flight', async () => {
    const m = manualSend()
    const s = createThemeSender(m.send, () => {})
    s.request('pisar')
    s.request('dracula')
    s.request('catppuccin-mocha')
    expect(m.calls, 'only the first is in flight').toEqual(['pisar'])
    m.resolvers[0].ok()
    await tick()
    expect(m.calls, 'the one in between was dropped').toEqual(['pisar', 'catppuccin-mocha'])
    m.resolvers[1].ok()
    await s.idle()
    expect(m.calls).toEqual(['pisar', 'catppuccin-mocha'])
  })

  it('sends the same theme again when it is asked for while it is in flight', async () => {
    const m = manualSend()
    const s = createThemeSender(m.send, () => {})
    s.request('dracula')
    s.request('dracula')
    m.resolvers[0].ok()
    await tick()
    expect(m.calls).toEqual(['dracula', 'dracula'])
    m.resolvers[1].ok()
    await s.idle()
  })

  it('sends a request when nothing is in flight, even if it equals the last one', async () => {
    const m = manualSend()
    const s = createThemeSender(m.send, () => {})
    s.request('dracula')
    m.resolvers[0].ok()
    await s.idle()
    s.request('dracula')
    expect(m.calls).toEqual(['dracula', 'dracula'])
    m.resolvers[1].ok()
    await s.idle()
  })

  it('reports a failure, still sends the one waiting, and keeps working afterwards', async () => {
    const m = manualSend()
    const errors: Array<[ThemeId, unknown]> = []
    const s = createThemeSender(m.send, (id, err) => errors.push([id, err]))
    s.request('pisar')
    s.request('dracula')
    const boom = new Error('boom')
    m.resolvers[0].fail(boom)
    await tick()
    expect(errors).toEqual([['pisar', boom]])
    expect(m.calls).toEqual(['pisar', 'dracula'])
    m.resolvers[1].ok()
    await s.idle()

    s.request('catppuccin-mocha')
    expect(m.calls).toEqual(['pisar', 'dracula', 'catppuccin-mocha'])
    m.resolvers[2].ok()
    await s.idle()
  })

  it('survives a send that throws synchronously', async () => {
    const calls: ThemeId[] = []
    const errors: ThemeId[] = []
    const s = createThemeSender(
      (id) => {
        calls.push(id)
        if (id === 'pisar') throw new Error('sync')
        return Promise.resolve()
      },
      (id) => errors.push(id),
    )
    s.request('pisar')
    s.request('dracula')
    await s.idle()
    expect(calls).toEqual(['pisar', 'dracula'])
    expect(errors).toEqual(['pisar'])
  })

  it('idle resolves only when everything is sent', async () => {
    const m = manualSend()
    const s = createThemeSender(m.send, () => {})
    await s.idle() // nothing to wait for

    let idle = false
    s.request('pisar')
    s.request('dracula')
    void s.idle().then(() => (idle = true))
    m.resolvers[0].ok()
    await tick()
    expect(idle, 'one is still waiting or in flight').toBe(false)
    m.resolvers[1].ok()
    await tick()
    expect(idle).toBe(true)
  })

  it('never has two sends in flight', async () => {
    const m = manualSend()
    const s = createThemeSender(m.send, () => {})
    const ids: ThemeId[] = ['pisar', 'dracula', 'catppuccin-mocha']
    for (let i = 0; i < 9; i++) s.request(ids[i % 3])
    for (let i = 0; i < m.resolvers.length; i++) {
      m.resolvers[i].ok()
      await tick()
    }
    await s.idle()
    expect(m.maxRunning()).toBe(1)
    expect(m.calls).toEqual(['pisar', 'catppuccin-mocha'])
  })
})
