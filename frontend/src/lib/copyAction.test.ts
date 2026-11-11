import { describe, expect, it } from 'vitest'
import { performCopy, type CopyFocus, type CopyPorts } from './copyAction'

function fakePorts(copyResult: boolean | Error): { ports: CopyPorts; calls: string[] } {
  const calls: string[] = []
  return {
    calls,
    ports: {
      copy: async () => {
        calls.push('copy')
        if (copyResult instanceof Error) throw copyResult
        return copyResult
      },
      copyText: async (text) => {
        calls.push(`copyText:${text}`)
      },
    },
  }
}

describe('performCopy', () => {
  it.each<[string, CopyFocus, boolean, string[]]>([
    ['terminal, Neovim copied', { kind: 'terminal', selection: '' }, true, ['copy']],
    ['terminal, Neovim copied, mouse selection too', { kind: 'terminal', selection: 'mouse' }, true, ['copy']],
    ['terminal, no visual mode, mouse selection', { kind: 'terminal', selection: 'mouse' }, false, ['copy', 'copyText:mouse']],
    ['terminal, no visual mode, no selection', { kind: 'terminal', selection: '' }, false, ['copy']],
    ['field with highlighted text', { kind: 'field', selection: 'čćž' }, false, ['copyText:čćž']],
    ['field without highlighted text', { kind: 'field', selection: '' }, false, []],
    ['somewhere else (the tree)', { kind: 'other' }, true, []],
  ])('%s', async (_name, focus, copyResult, expected) => {
    const { ports, calls } = fakePorts(copyResult)
    await performCopy(focus, ports)
    expect(calls).toEqual(expected)
  })

  it('does not fall back to the mouse selection when Copy fails', async () => {
    const { ports, calls } = fakePorts(new Error('rpc failed'))
    await expect(performCopy({ kind: 'terminal', selection: 'old' }, ports)).rejects.toThrow('rpc failed')
    expect(calls).toEqual(['copy'])
  })
})
