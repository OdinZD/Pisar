import { describe, expect, it } from 'vitest'
import type { Keymaps } from '../../bindings/Pisar/models'
import { strings } from '../strings'
import { keyToSearchAction } from './searchKeys'
import { buildSections, formatAccel, formatNvimKeys, searchShortcuts, treeShortcuts, type KeySpec, type StaticShortcut } from './shortcuts'
import type { DirRow, DocRow, ItemRow, ProjectRow } from './tree'
import { keyToAction } from './treeKeys'

describe('formatAccel', () => {
  it.each([
    // the seven accelerators of the menu
    ['CmdOrCtrl+n', '⌘N'],
    ['CmdOrCtrl+s', '⌘S'],
    ['CmdOrCtrl+p', '⌘P'],
    ['CmdOrCtrl+c', '⌘C'],
    ['CmdOrCtrl+b', '⌘B'],
    ['CmdOrCtrl+i', '⌘I'],
    ['CmdOrCtrl+/', '⌘/'],
    // several modifiers, written in the order of macOS
    ['CmdOrCtrl+Shift+p', '⇧⌘P'],
    ['Shift+CmdOrCtrl+p', '⇧⌘P'],
    ['Ctrl+Alt+Shift+Cmd+x', '⌃⌥⇧⌘X'],
    ['Command+Option+k', '⌥⌘K'],
    ['Control+OptionOrAlt+k', '⌃⌥K'],
    // names are case-insensitive, the letter is upper-cased
    ['cmdorctrl+SHIFT+Q', '⇧⌘Q'],
    ['CMD+q', '⌘Q'],
    // what is not known stays as it is
    ['Hyper+x', 'HyperX'],
    ['CmdOrCtrl+Enter', '⌘Enter'],
    ['CmdOrCtrl+F5', '⌘F5'],
    ['CmdOrCtrl++', '⌘+'],
    ['s', 'S'],
    ['', ''],
  ])('%s → %s', (accel, expected) => {
    expect(formatAccel(accel)).toBe(expected)
  })
})

describe('formatNvimKeys', () => {
  it.each([
    ['<Space>f', '␣ f'],
    ['<Space>gs', '␣ g s'],
    ['<Space>?', '␣ ?'],
    ['<Down>', '↓'],
    ['<Up>', '↑'],
    ['<Left>', '←'],
    ['<Right>', '→'],
    ['<lt>', '<'],
    ['<Space><lt>', '␣ <'],
    ['<CR>', '↵'],
    ['<Esc>', 'Esc'],
    ['<Tab>', '⇥'],
    ['<BS>', '⌫'],
    ['<C-g>', '⌃g'],
    ['<Space><C-g>', '␣ ⌃g'],
    ['č', 'č'],
    ['<Space>čž', '␣ č ž'],
    ['<F5>', '<F5>'],
    ['<Space><F5>x', '␣ <F5> x'],
    ['<', '<'],
    ['', ''],
  ])('%s → %s', (keys, expected) => {
    expect(formatNvimKeys(keys)).toBe(expected)
  })
})

const base = { key: 'k', projectId: 'p', depth: 0 }
const project: ProjectRow = { ...base, type: 'project', name: 'P', expanded: true, missing: false, note: null }
const folder: DirRow = { ...base, type: 'dir', rel: 'd', name: 'd', expanded: false }
const doc: DocRow = { ...base, type: 'doc', rel: 'a.md', fileName: 'a.md', title: 'a', kind: 'md', kindLabel: 'MD' }
const rowKinds: ItemRow[] = [doc, project, folder]

describe('the fixed rows', () => {
  it.each(treeShortcuts.flatMap((s) => s.keys.map((k) => [s.display, k] as const)))('the tree really handles %s %j', (_display, spec) => {
    const ev = { shiftKey: false, ...spec }
    expect(rowKinds.some((row) => keyToAction(ev, row) !== null)).toBe(true)
  })

  it.each(searchShortcuts.flatMap((s) => s.keys.map((k) => [s.display, k] as const)))('the search panel really handles %s %j', (_display, spec: KeySpec) => {
    expect(keyToSearchAction({ shiftKey: false, ...spec })).not.toBeNull()
  })

  // The tests above only say that a key does something; these say what. Swapping two keys in
  // treeKeys.ts (r and d, j and k) must fail here.
  const moveDelta: Readonly<Record<string, 1 | -1>> = {
    [strings.shortcutsTreeNext]: 1,
    [strings.shortcutsTreePrev]: -1,
    [strings.shortcutsSearchNext]: 1,
    [strings.shortcutsSearchPrev]: -1,
  }

  const checkActions = (shortcuts: readonly StaticShortcut<string>[], actionsOf: (spec: KeySpec) => ({ type: string; delta?: number } | null)[]) => {
    for (const s of shortcuts) {
      const seen = new Set<string>()
      for (const spec of s.keys) {
        for (const action of actionsOf(spec)) {
          if (action === null) continue
          expect(s.actions, `${s.display} gives ${action.type}`).toContain(action.type)
          seen.add(action.type)
          if (action.type === 'move') expect(action.delta, `${s.display} moves`).toBe(moveDelta[s.label])
        }
      }
      expect([...seen].sort(), `kinds of action of ${s.display}`).toEqual([...s.actions].sort())
    }
  }

  it('the tree gives exactly the kinds of action each fixed row names', () => {
    checkActions(treeShortcuts, (spec) => rowKinds.map((row) => keyToAction({ shiftKey: false, ...spec }, row)))
  })

  it('the search panel gives exactly the kinds of action each fixed row names', () => {
    checkActions(searchShortcuts, (spec) => [keyToSearchAction({ shiftKey: false, ...spec })])
  })

  it('every row has a key, a display and a label', () => {
    for (const s of [...treeShortcuts, ...searchShortcuts]) {
      expect(s.keys.length).toBeGreaterThan(0)
      expect(s.display).not.toBe('')
      expect(s.label.trim()).not.toBe('')
    }
  })
})

const map = (mode: string, keys: string, desc: string) => ({ mode, keys, desc })
const keymaps = (maps: Keymaps['maps'], groups: Keymaps['groups'] = {}): Keymaps => ({ maps, groups })
const nvimSection = (km: Keymaps | null, nvimNote: string | null = null) => buildSections({ menu: [], keymaps: km, nvimNote })[3]

describe('buildSections', () => {
  it('has the four sections in order, the Neovim one also without data', () => {
    const sections = buildSections({ menu: [], keymaps: null, nvimNote: 'nema' })
    expect(sections.map((s) => s.id)).toEqual(['app', 'tree', 'search', 'nvim'])
    expect(sections.map((s) => s.title)).toEqual([strings.shortcutsApp, strings.shortcutsTree, strings.shortcutsSearch, strings.shortcutsNvim])
    expect(sections[3]).toEqual({ id: 'nvim', title: strings.shortcutsNvim, rows: [], note: 'nema' })
    expect(sections[0].note).toBeNull()
  })

  it('writes the menu accelerators as macOS does; an empty menu gives no rows', () => {
    const [app] = buildSections({ menu: [{ label: 'Spremi', accel: 'CmdOrCtrl+s' }, { label: 'Pregled', accel: 'CmdOrCtrl+Shift+p' }], keymaps: null, nvimNote: null })
    expect(app.rows).toEqual([
      { keys: '⌘S', label: 'Spremi', modes: [], group: null },
      { keys: '⇧⌘P', label: 'Pregled', modes: [], group: null },
    ])
    expect(buildSections({ menu: [], keymaps: null, nvimNote: null })[0].rows).toEqual([])
  })

  it('fills the tree and the search sections from the fixed rows', () => {
    const sections = buildSections({ menu: [], keymaps: null, nvimNote: null })
    expect(sections[1].rows.map((r) => r.keys)).toEqual(treeShortcuts.map((s) => s.display))
    expect(sections[2].rows.map((r) => r.label)).toEqual(searchShortcuts.map((s) => s.label))
  })

  it('merges the same keys and description of several modes into one row, modes in the order n, x, i', () => {
    const section = nvimSection(
      keymaps([
        map('i', '<Space>b', 'Podebljano'),
        map('x', '<Space>b', 'Podebljano'),
        map('n', '<Space>b', 'Podebljano'),
        map('n', '<Space>b', 'Drugo'),
        map('n', '<Down>', 'Dolje'),
      ]),
    )
    expect(section.rows).toEqual([
      { keys: '␣ b', label: 'Podebljano', modes: [strings.shortcutsModeNormal, strings.shortcutsModeVisual, strings.shortcutsModeInsert], group: null },
      { keys: '␣ b', label: 'Drugo', modes: [strings.shortcutsModeNormal], group: null },
      { keys: '↓', label: 'Dolje', modes: [strings.shortcutsModeNormal], group: null },
    ])
  })

  it('keeps the order in which the rows arrived', () => {
    const section = nvimSection(keymaps([map('n', 'z', 'Z'), map('n', 'a', 'A'), map('x', 'z', 'Z')]))
    expect(section.rows.map((r) => r.label)).toEqual(['Z', 'A'])
  })

  it('names the group by the longest prefix after the leader', () => {
    const section = nvimSection(
      keymaps([map('n', '<Space>gs', 'Status'), map('n', '<Space>gb', 'Grana'), map('n', '<Space>f', 'Traži'), map('n', '<Space>gh', 'Hunk')], {
        g: 'Git',
        gh: 'Hunkovi',
      }),
    )
    expect(section.rows.map((r) => r.group)).toEqual(['Git', 'Git', null, 'Hunkovi'])
  })

  it('matches a group on the boundary of a key', () => {
    const section = nvimSection(
      keymaps([map('n', '<Space><C-g>', 'A'), map('n', '<Space><lt>x', 'B'), map('n', '<Space>g', 'C'), map('n', 'gs', 'D')], { g: 'Git', '<': 'Manje' }),
    )
    // "<C-g>" is not "g"; "<lt>" is one key; the keys without the leader have no group.
    expect(section.rows.map((r) => r.group)).toEqual([null, null, 'Git', null])
  })

  it('has no group without groups, or with null groups and maps', () => {
    expect(nvimSection(keymaps([map('n', '<Space>gs', 'S')])).rows[0].group).toBeNull()
    expect(nvimSection({ maps: null, groups: null }).rows).toEqual([])
    expect(nvimSection({ maps: [map('n', '<Space>gs', 'S')], groups: null }).rows[0].group).toBeNull()
  })

  it('drops the "Pisar: " prefix of a description, only at its start', () => {
    const prefixed = strings.nvimMapSearch
    const section = nvimSection(keymaps([map('n', '<Space>f', prefixed), map('n', 'x', `Moj ${prefixed}`), map('n', 'y', 'Bez prefiksa')]))
    expect(section.rows.map((r) => r.label)).toEqual(['traži dokument', `Moj ${prefixed}`, 'Bez prefiksa'])
  })

  it('strips the same prefix from every description init.lua writes', () => {
    const texts = Object.entries(strings).filter(([key]) => key.startsWith('nvimMap'))
    expect(texts.length).toBeGreaterThan(0)
    const rows = nvimSection(keymaps(texts.map(([, text], i) => map('n', `k${i}`, text)))).rows
    for (const row of rows) expect(row.label).not.toMatch(/^Pisar: /)
  })

  it('shows the Neovim section with its note and no rows when there are no keymaps', () => {
    expect(nvimSection(null, strings.shortcutsNvimUnavailable)).toMatchObject({ rows: [], note: strings.shortcutsNvimUnavailable })
  })
})
