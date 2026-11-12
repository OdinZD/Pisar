import { describe, expect, it } from 'vitest'
import { keyToSearchAction, type SearchKey } from './searchKeys'

const press = (key: string, mods: Partial<SearchKey> = {}): SearchKey => ({
  key,
  metaKey: false,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  ...mods,
})

describe('keyToSearchAction', () => {
  it.each([
    ['ArrowDown', {}, { type: 'move', delta: 1 }],
    ['ArrowUp', {}, { type: 'move', delta: -1 }],
    ['Enter', {}, { type: 'open' }],
    ['Escape', {}, { type: 'close' }],
    ['p', { ctrlKey: true }, { type: 'toggle-scope' }],
    ['P', { ctrlKey: true }, { type: 'toggle-scope' }], // Caps Lock
  ] as const)('%s %j is an action', (key, mods, expected) => {
    expect(keyToSearchAction(press(key, mods))).toEqual(expected)
  })

  it.each([
    // ⌘ belongs to the application; ⌘P comes as a menu event, not as a key of the panel.
    ['p', { metaKey: true }],
    ['P', { metaKey: true, shiftKey: true }],
    ['c', { metaKey: true }],
    ['v', { metaKey: true }],
    ['a', { metaKey: true }],
    ['ArrowDown', { metaKey: true }],
    ['Enter', { metaKey: true }],
    ['Escape', { metaKey: true }],
    ['p', { metaKey: true, ctrlKey: true }],
    // Option and Shift variants keep their meaning in the field.
    ['ArrowDown', { altKey: true }],
    ['ArrowUp', { altKey: true }],
    ['ArrowDown', { shiftKey: true }],
    ['ArrowUp', { shiftKey: true }],
    ['Enter', { shiftKey: true }],
    ['Enter', { altKey: true }],
    ['Escape', { shiftKey: true }],
    ['Escape', { altKey: true }],
    // Ctrl: only P.
    ['n', { ctrlKey: true }],
    ['j', { ctrlKey: true }],
    ['k', { ctrlKey: true }],
    ['ArrowDown', { ctrlKey: true }],
    ['Enter', { ctrlKey: true }],
    ['Escape', { ctrlKey: true }],
    ['p', { ctrlKey: true, shiftKey: true }],
    ['p', { ctrlKey: true, altKey: true }],
    // Plain keys are typing.
    ['p', {}],
    ['a', {}],
    ['j', {}],
    ['k', {}],
    [' ', {}],
    ['Tab', {}],
    ['Backspace', {}],
    ['ArrowLeft', {}],
    ['ArrowRight', {}],
    ['Home', {}],
    ['End', {}],
    ['PageDown', {}],
    ['Dead', {}],
  ] as const)('%s %j is not an action', (key, mods) => {
    expect(keyToSearchAction(press(key, mods))).toBeNull()
  })

  it('leaves Enter, Esc and the arrows to an IME that is composing', () => {
    for (const key of ['Enter', 'Escape', 'ArrowDown', 'ArrowUp']) {
      expect(keyToSearchAction(press(key, { isComposing: true }))).toBeNull()
    }
    expect(keyToSearchAction(press('p', { ctrlKey: true, isComposing: true }))).toBeNull()
  })
})
