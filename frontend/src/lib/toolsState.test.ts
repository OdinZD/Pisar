import { describe, expect, it } from 'vitest'
import { strings } from '../strings'
import { missingTools, toolsNotice, type ToolStatus } from './toolsState'

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
  it('is null when nothing is missing', () => {
    expect(toolsNotice([])).toBeNull()
  })

  it('has one line for nvim', () => {
    expect(toolsNotice(['nvim'])).toEqual({
      lines: [{ text: strings.toolsNvimMissing, command: strings.toolsNvimInstall }],
    })
  })

  it('has one line for pandoc', () => {
    expect(toolsNotice(['pandoc'])).toEqual({
      lines: [{ text: strings.toolsPandocMissing, command: strings.toolsPandocInstall }],
    })
  })

  it('has both lines, nvim first, whatever the order of the input', () => {
    const both = {
      lines: [
        { text: strings.toolsNvimMissing, command: strings.toolsNvimInstall },
        { text: strings.toolsPandocMissing, command: strings.toolsPandocInstall },
      ],
    }
    expect(toolsNotice(['nvim', 'pandoc'])).toEqual(both)
    expect(toolsNotice(['pandoc', 'nvim'])).toEqual(both)
  })
})
