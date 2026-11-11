import { describe, expect, it } from 'vitest'
import { describeFocus, noTerminalFocus, shouldFocusTreeAfterExit } from './focus'

const input = (value: string, start: number | null, end: number | null) => ({
  tagName: 'INPUT',
  value,
  selectionStart: start,
  selectionEnd: end,
})

describe('describeFocus', () => {
  it('terminal wins over the hidden textarea xterm.js focuses', () => {
    const textarea = { tagName: 'TEXTAREA', value: '', selectionStart: 0, selectionEnd: 0 }
    expect(describeFocus(textarea, { focused: true, selection: 'sel' })).toEqual({ kind: 'terminal', selection: 'sel' })
  })

  it('reads the highlighted part of an input field', () => {
    expect(describeFocus(input('Pisar čšđ', 6, 9), noTerminalFocus)).toEqual({ kind: 'field', selection: 'čšđ' })
  })

  it('an input field without a highlight has an empty selection', () => {
    expect(describeFocus(input('abc', 2, 2), noTerminalFocus)).toEqual({ kind: 'field', selection: '' })
  })

  it('an input type without selection offsets', () => {
    expect(describeFocus(input('abc', null, null), noTerminalFocus)).toEqual({ kind: 'field', selection: '' })
  })

  it.each([null, { tagName: 'DIV' }, { tagName: 'BODY' }])('%j is not a field', (el) => {
    expect(describeFocus(el, noTerminalFocus)).toEqual({ kind: 'other' })
  })
})

describe('shouldFocusTreeAfterExit', () => {
  it.each([
    [null, noTerminalFocus, true],
    [{ tagName: 'BODY' }, noTerminalFocus, true],
    [{ tagName: 'DIV' }, noTerminalFocus, true],
    [{ tagName: 'INPUT' }, noTerminalFocus, false], // the user is typing a name
    [{ tagName: 'TEXTAREA' }, { focused: true, selection: '' }, true], // xterm.js' own textarea, about to go
    [{ tagName: 'TEXTAREA' }, noTerminalFocus, false],
  ])('%j with %j -> %s', (el, terminal, expected) => {
    expect(shouldFocusTreeAfterExit(el, terminal)).toBe(expected)
  })
})
