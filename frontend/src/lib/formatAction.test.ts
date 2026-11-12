import { describe, expect, it } from 'vitest'
import type { CopyFocus } from './copyAction'
import type { Phase } from './editorState'
import { shouldFormat } from './formatAction'

const focuses: Array<[string, CopyFocus]> = [
  ['terminal', { kind: 'terminal', selection: '' }],
  ['terminal with a mouse selection', { kind: 'terminal', selection: 'text' }],
  ['input field (search, new name, rename)', { kind: 'field', selection: '' }],
  ['input field with highlighted text', { kind: 'field', selection: 'ime' }],
  ['tree, button, preview or nothing', { kind: 'other' }],
]
const phases: Phase[] = ['idle', 'starting', 'running', 'failed']

describe('shouldFormat', () => {
  // The only yes: the terminal has the keyboard and Neovim runs.
  const cases = focuses.flatMap(([name, focus]) =>
    phases.map((phase): [string, Phase, CopyFocus, boolean] => [
      name,
      phase,
      focus,
      focus.kind === 'terminal' && phase === 'running',
    ]),
  )

  it.each(cases)('%s while %s', (_name, phase, focus, want) => {
    expect(shouldFormat(focus, phase)).toBe(want)
  })

  it('has one yes per terminal focus and no other', () => {
    expect(cases.filter(([, , , want]) => want)).toHaveLength(2)
  })
})
