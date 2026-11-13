import { describe, expect, it } from 'vitest'
import { keyToShortcutsAction, type ShortcutsKey } from './shortcutsKeys'

const press = (key: string, mods: Partial<ShortcutsKey> = {}): ShortcutsKey => ({
  key,
  metaKey: false,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  ...mods,
})

describe('keyToShortcutsAction', () => {
  it.each([
    ['Escape', {}, { type: 'close' }],
    ['j', {}, { type: 'scroll', delta: 1 }],
    ['k', {}, { type: 'scroll', delta: -1 }],
  ] as const)('%s %j is an action', (key, mods, expected) => {
    expect(keyToShortcutsAction(press(key, mods))).toEqual(expected)
  })

  it.each([
    // The browser scrolls the focused list with these.
    ['ArrowDown', {}],
    ['ArrowUp', {}],
    ['PageDown', {}],
    ['PageUp', {}],
    [' ', {}],
    ['Home', {}],
    ['End', {}],
    ['Enter', {}],
    ['x', {}],
    ['J', { shiftKey: true }],
    // ⌘/ is a menu event, never a key of the panel; no other ⌘ shortcut is intercepted either.
    ['/', { metaKey: true }],
    ['Escape', { metaKey: true }],
    ['j', { metaKey: true }],
    ['Escape', { ctrlKey: true }],
    ['Escape', { altKey: true }],
    ['Escape', { shiftKey: true }],
    ['j', { ctrlKey: true }],
    ['k', { altKey: true }],
    // An IME that is composing owns Esc.
    ['Escape', { isComposing: true }],
    ['j', { isComposing: true }],
  ] as const)('%s %j is left to the browser', (key, mods) => {
    expect(keyToShortcutsAction(press(key, mods))).toBeNull()
  })
})
