import { describe, expect, it } from 'vitest'
import type { Keymaps, MenuShortcut } from '../../bindings/Pisar/models'
import { strings } from '../strings'
import {
  dismissed,
  initialShortcutsState,
  keymapsArrived,
  keymapsFailed,
  menuArrived,
  shortcutsView,
  toggled,
  type ShortcutsState,
} from './shortcutsState'

const menu: MenuShortcut[] = [{ label: 'Spremi', accel: 'CmdOrCtrl+s' }]
const keymaps: Keymaps = { maps: [{ mode: 'n', keys: '<Space>f', desc: 'Pisar: traži dokument' }], groups: {} }

const open = (over: Partial<ShortcutsState> = {}): ShortcutsState => ({ ...initialShortcutsState, open: true, ...over })

describe('toggled', () => {
  it('opens with Neovim: loads both lists and takes the focus', () => {
    const t = toggled(initialShortcutsState, true)
    expect(t.state.open).toBe(true)
    expect(t.state.pending).toEqual({ menu: 1, keymaps: 2 })
    expect(t.effects).toEqual([{ type: 'load-menu', token: 1 }, { type: 'load-keymaps', token: 2 }, { type: 'focus-panel' }])
  })

  it('opens without Neovim: loads only the menu list', () => {
    const t = toggled(initialShortcutsState, false)
    expect(t.state.pending).toEqual({ menu: 1, keymaps: null })
    expect(t.effects).toEqual([{ type: 'load-menu', token: 1 }, { type: 'focus-panel' }])
  })

  it('closes an open panel and hands the keyboard back', () => {
    const t = toggled(open({ menu, keymaps, lastToken: 4 }), true)
    expect(t.state).toEqual({ ...initialShortcutsState, lastToken: 4 })
    expect(t.effects).toEqual([{ type: 'focus-terminal' }])
  })

  it('keeps counting tokens across a close', () => {
    const closed = toggled(toggled(initialShortcutsState, true).state, true).state
    const t = toggled(closed, true)
    expect(t.state.pending).toEqual({ menu: 3, keymaps: 4 })
  })
})

describe('dismissed', () => {
  it('closes with the focus going back to the editor', () => {
    const t = dismissed(open({ menu, lastToken: 2 }), true)
    expect(t.state).toEqual({ ...initialShortcutsState, lastToken: 2 })
    expect(t.effects).toEqual([{ type: 'focus-terminal' }])
  })

  it('closes without moving the focus (the user put it elsewhere)', () => {
    const t = dismissed(open(), false)
    expect(t.state.open).toBe(false)
    expect(t.effects).toEqual([])
  })

  it('does nothing when it is closed', () => {
    const t = dismissed(initialShortcutsState, true)
    expect(t.state).toBe(initialShortcutsState)
    expect(t.effects).toEqual([])
  })
})

describe('answers', () => {
  const opened = toggled(initialShortcutsState, true).state // menu token 1, keymaps token 2

  it('are stored when the token is the awaited one', () => {
    const afterMenu = menuArrived(opened, 1, menu).state
    expect(afterMenu.menu).toBe(menu)
    expect(afterMenu.pending).toEqual({ menu: null, keymaps: 2 })
    const afterKeymaps = keymapsArrived(afterMenu, 2, keymaps).state
    expect(afterKeymaps.keymaps).toBe(keymaps)
    expect(afterKeymaps.pending).toEqual({ menu: null, keymaps: null })
  })

  it('treat a null menu list as empty', () => {
    expect(menuArrived(opened, 1, null).state.menu).toEqual([])
  })

  it('with another token are dropped', () => {
    expect(menuArrived(opened, 9, menu).state).toBe(opened)
    expect(keymapsArrived(opened, 9, keymaps).state).toBe(opened)
    expect(keymapsFailed(opened, 9).state).toBe(opened)
  })

  it('that arrive after the close are dropped', () => {
    const closed = dismissed(opened, true).state
    expect(menuArrived(closed, 1, menu).state).toBe(closed)
    expect(keymapsArrived(closed, 2, keymaps).state).toBe(closed)
    expect(keymapsFailed(closed, 2).state).toBe(closed)
  })

  it('that arrive after the close and a new open are dropped', () => {
    const closed = dismissed(opened, true).state
    const again = toggled(closed, true).state // tokens 3 and 4
    expect(menuArrived(again, 1, menu).state).toBe(again)
    expect(keymapsArrived(again, 2, keymaps).state).toBe(again)
    expect(keymapsFailed(again, 2).state).toBe(again)
    expect(menuArrived(again, 3, menu).state.menu).toBe(menu)
  })

  it('a failed call marks the Neovim list as failed', () => {
    const t = keymapsFailed(opened, 2)
    expect(t.state.nvimFailed).toBe(true)
    expect(t.state.pending.keymaps).toBeNull()
  })
})

describe('shortcutsView', () => {
  const note = strings.shortcutsNvimUnavailable

  it('says that Neovim cannot answer when it is not running', () => {
    const state = toggled(initialShortcutsState, false).state
    expect(shortcutsView(state, false)).toEqual({ menu: [], keymaps: null, nvimNote: note })
  })

  it('shows nothing and no note while the answer is awaited', () => {
    const state = toggled(initialShortcutsState, true).state
    expect(shortcutsView(state, true)).toEqual({ menu: [], keymaps: null, nvimNote: null })
  })

  it('says that Neovim cannot answer when the call failed', () => {
    const opened = toggled(initialShortcutsState, true).state
    const state = keymapsFailed(menuArrived(opened, 1, menu).state, 2).state
    expect(shortcutsView(state, true)).toEqual({ menu, keymaps: null, nvimNote: note })
  })

  it('hands over the shortcuts that arrived', () => {
    const opened = toggled(initialShortcutsState, true).state
    const state = keymapsArrived(menuArrived(opened, 1, menu).state, 2, keymaps).state
    expect(shortcutsView(state, true)).toEqual({ menu, keymaps, nvimNote: null })
  })

  it('does not show old shortcuts once Neovim is gone', () => {
    const opened = toggled(initialShortcutsState, true).state
    const state = keymapsArrived(opened, 2, keymaps).state
    expect(shortcutsView(state, false).keymaps).toBeNull()
    expect(shortcutsView(state, false).nvimNote).toBe(note)
  })

  it('does not wait for a list that was never asked for (Neovim started after opening)', () => {
    const state = toggled(initialShortcutsState, false).state
    expect(shortcutsView(state, true).nvimNote).toBe(note)
  })
})
