import { describe, expect, it } from 'vitest'
import { strings } from '../strings'
import { missingTools, toolProblems, toolsNotice, type ToolProblem, type ToolStatus } from './toolsState'

const nvim = (found: boolean): ToolStatus => ({ name: 'nvim', found })
const pandoc = (found: boolean): ToolStatus => ({ name: 'pandoc', found })

describe('missingTools', () => {
  it('is empty when everything is found or nothing was reported', () => {
    expect(missingTools([nvim(true), pandoc(true)])).toEqual([])
    expect(missingTools([])).toEqual([])
  })

  it('names what was not found', () => {
    expect(missingTools([nvim(false), pandoc(true)])).toEqual(['nvim'])
    expect(missingTools([nvim(true), pandoc(false)])).toEqual(['pandoc'])
  })

  it('puts nvim before pandoc whatever the order of the input', () => {
    expect(missingTools([pandoc(false), nvim(false)])).toEqual(['nvim', 'pandoc'])
  })

  it('ignores a tool it does not know', () => {
    expect(missingTools([{ name: 'git', found: false }, nvim(true)])).toEqual([])
  })
})

describe('toolsNotice', () => {
  const missing = (name: 'nvim' | 'pandoc'): ToolProblem => ({ name, kind: 'missing', version: '', minimum: '' })

  it('is null when nothing is missing', () => {
    expect(toolsNotice([])).toBeNull()
  })

  it('has one line for nvim', () => {
    expect(toolsNotice([missing('nvim')])).toEqual({
      title: strings.toolsMissingTitle,
      lines: [{ text: strings.toolsNvimMissing, command: strings.toolsNvimInstall }],
    })
  })

  it('has one line for pandoc', () => {
    expect(toolsNotice([missing('pandoc')])).toEqual({
      title: strings.toolsMissingTitle,
      lines: [{ text: strings.toolsPandocMissing, command: strings.toolsPandocInstall }],
    })
  })

  it('has both lines, nvim first, whatever the order of the input', () => {
    const both = {
      title: strings.toolsMissingTitle,
      lines: [
        { text: strings.toolsNvimMissing, command: strings.toolsNvimInstall },
        { text: strings.toolsPandocMissing, command: strings.toolsPandocInstall },
      ],
    }
    expect(toolsNotice([missing('nvim'), missing('pandoc')])).toEqual(both)
    expect(toolsNotice([missing('pandoc'), missing('nvim')])).toEqual(both)
  })
})

const tooOld = (name: string, version: string, minimum: string): ToolStatus => ({
  name,
  found: true,
  version,
  minimum,
  tooOld: true,
})
const current = (name: string, version: string, minimum: string): ToolStatus => ({
  name,
  found: true,
  version,
  minimum,
  tooOld: false,
})

describe('toolProblems', () => {
  it('is empty when every tool is usable or nothing was reported', () => {
    expect(toolProblems([current('nvim', '0.12.6', '0.12.0'), current('pandoc', '3.13', '3.13')])).toEqual([])
    expect(toolProblems([])).toEqual([])
  })

  it('reports a missing tool', () => {
    expect(toolProblems([{ name: 'nvim', found: false, version: '', minimum: '0.12.0', tooOld: false }, pandoc(true)])).toEqual([
      { name: 'nvim', kind: 'missing', version: '', minimum: '0.12.0' },
    ])
  })

  it('reports a tool that is too old, with both versions', () => {
    expect(toolProblems([nvim(true), tooOld('pandoc', '3.9', '3.13')])).toEqual([
      { name: 'pandoc', kind: 'too-old', version: '3.9', minimum: '3.13' },
    ])
  })

  it('reports both, nvim first, whatever the order of the input', () => {
    const want: ToolProblem[] = [
      { name: 'nvim', kind: 'missing', version: '', minimum: '0.12.0' },
      { name: 'pandoc', kind: 'too-old', version: '3.9', minimum: '3.13' },
    ]
    const statuses = [
      { name: 'nvim', found: false, minimum: '0.12.0' },
      tooOld('pandoc', '3.9', '3.13'),
    ]
    expect(toolProblems(statuses)).toEqual(want)
    expect(toolProblems([...statuses].reverse())).toEqual(want)
  })

  it('does not call a tool whose version is unknown too old', () => {
    expect(toolProblems([{ name: 'nvim', found: true, version: '', minimum: '0.12.0', tooOld: false }])).toEqual([])
  })

  it('ignores a tool it does not know', () => {
    expect(toolProblems([{ name: 'git', found: false }, tooOld('git', '1', '2')])).toEqual([])
  })

  it('reads the old shape (found only) as before', () => {
    expect(toolProblems([nvim(false), pandoc(true)])).toEqual([{ name: 'nvim', kind: 'missing', version: '', minimum: '' }])
    expect(toolProblems([nvim(true), pandoc(true)])).toEqual([])
  })
})

describe('toolsNotice from problems', () => {
  const nvimMissing: ToolProblem = { name: 'nvim', kind: 'missing', version: '', minimum: '0.12.0' }
  const nvimOld: ToolProblem = { name: 'nvim', kind: 'too-old', version: '0.11.4', minimum: '0.12.0' }
  const pandocMissing: ToolProblem = { name: 'pandoc', kind: 'missing', version: '', minimum: '3.13' }
  const pandocOld: ToolProblem = { name: 'pandoc', kind: 'too-old', version: '3.9', minimum: '3.13' }

  it('is null when there is no problem', () => {
    expect(toolsNotice([])).toBeNull()
  })

  it('is titled "missing" when every problem is a missing tool', () => {
    expect(toolsNotice([nvimMissing])?.title).toBe(strings.toolsMissingTitle)
    expect(toolsNotice([nvimMissing, pandocMissing])?.title).toBe(strings.toolsMissingTitle)
  })

  it('is titled "not ready" as soon as one tool is too old', () => {
    expect(toolsNotice([nvimOld])?.title).toBe(strings.toolsNotReadyTitle)
    expect(toolsNotice([pandocOld])?.title).toBe(strings.toolsNotReadyTitle)
    expect(toolsNotice([nvimMissing, pandocOld])?.title).toBe(strings.toolsNotReadyTitle)
  })

  it('keeps the install line for a missing tool', () => {
    expect(toolsNotice([nvimMissing])?.lines).toEqual([
      { text: strings.toolsNvimMissing, command: strings.toolsNvimInstall },
    ])
  })

  it('names both versions and the upgrade command for a tool that is too old', () => {
    expect(toolsNotice([nvimOld])?.lines).toEqual([
      { text: 'Neovim 0.11.4 je prestar. Pisar treba 0.12.0 ili noviji.', command: 'brew upgrade neovim' },
    ])
    const [line] = toolsNotice([pandocOld])?.lines ?? []
    expect(line.text).toContain('pandoc 3.9 je prestar')
    expect(line.text).toContain('3.13 ili noviji')
    expect(line.command).toBe('brew upgrade pandoc')
  })

  it('puts nvim before pandoc whatever the order of the input', () => {
    const want = toolsNotice([nvimOld, pandocMissing])
    expect(toolsNotice([pandocMissing, nvimOld])).toEqual(want)
    expect(want?.lines.map((l) => l.command)).toEqual([strings.toolsNvimUpgrade, strings.toolsPandocInstall])
  })

  it('leaves no placeholder in any text', () => {
    const notice = toolsNotice([nvimOld, pandocOld])
    for (const line of notice?.lines ?? []) expect(line.text).not.toMatch(/[{}]/)
  })
})
