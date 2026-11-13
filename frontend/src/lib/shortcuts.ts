// The shortcut overview as pure functions: how the keys of the menu and of Neovim are written, the
// fixed rows for the tree and the search panel, and the four sections the panel draws. Nothing here
// touches Svelte, Wails or the DOM.
import type { Keymaps, MenuShortcut } from '../../bindings/Pisar/models'
import { strings } from '../strings'
import type { SearchAction } from './searchKeys'
import type { TreeAction } from './treeKeys'

// ---- accelerators of the menu -----------------------------------------------------------------

// Written in the order macOS lists modifiers: ⌃⌥⇧⌘, then the key.
const modifierOrder = ['⌃', '⌥', '⇧', '⌘'] as const

const modifierSymbols: Readonly<Record<string, (typeof modifierOrder)[number]>> = {
  cmd: '⌘',
  cmdorctrl: '⌘',
  command: '⌘',
  ctrl: '⌃',
  control: '⌃',
  alt: '⌥',
  option: '⌥',
  optionoralt: '⌥',
  shift: '⇧',
}

/**
 * Wails writes an accelerator as "CmdOrCtrl+Shift+p"; macOS shows "⇧⌘P". A part that is not a known
 * modifier stays as it is (before the key), and so does a key name with more than one character.
 */
export function formatAccel(accel: string): string {
  // The key is whatever follows the last separator; "+" itself is the key in "CmdOrCtrl++".
  let key: string
  let rest: string
  if (accel.endsWith('+')) {
    key = '+'
    rest = accel.slice(0, -2)
  } else {
    const i = accel.lastIndexOf('+')
    key = accel.slice(i + 1)
    rest = i < 0 ? '' : accel.slice(0, i)
  }
  const found = new Set<string>()
  const unknown: string[] = []
  for (const part of rest === '' ? [] : rest.split('+')) {
    const symbol = modifierSymbols[part.toLowerCase()]
    if (symbol) found.add(symbol)
    else unknown.push(part)
  }
  const shownKey = [...key].length === 1 ? key.toUpperCase() : key
  return modifierOrder.filter((m) => found.has(m)).join('') + unknown.join('') + shownKey
}

// ---- keys as Neovim writes them ---------------------------------------------------------------

const namedKeys: Readonly<Record<string, string>> = {
  space: '␣',
  lt: '<',
  down: '↓',
  up: '↑',
  left: '←',
  right: '→',
  cr: '↵',
  esc: 'Esc',
  tab: '⇥',
  bs: '⌫',
}

// One key is either "<…>" or a single character (a code point: "č" is one key, not two).
const keyToken = /<[^<>]+>|[\s\S]/gu

function tokenize(keys: string): string[] {
  return keys.match(keyToken) ?? []
}

function formatToken(token: string): string {
  if (!token.startsWith('<') || token.length < 3) return token
  const name = token.slice(1, -1)
  const named = namedKeys[name.toLowerCase()]
  if (named !== undefined) return named
  const ctrl = /^C-([\s\S])$/iu.exec(name)
  return ctrl ? `⌃${ctrl[1]}` : token
}

/** "<Space>gs" → "␣ g s". Keys are separated by a space so a sequence reads as presses. */
export function formatNvimKeys(keys: string): string {
  return tokenize(keys).map(formatToken).join(' ')
}

// ---- the fixed rows ---------------------------------------------------------------------------

/** What `keyToAction` / `keyToSearchAction` receive; the tests feed it to them. */
export interface KeySpec {
  key: string
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
  shiftKey?: boolean
}

export interface StaticShortcut<T extends string> {
  /** every key press that does it (j and ↓ do the same) */
  keys: KeySpec[]
  display: string
  label: string
  /**
   * The kinds of action the keys give, for the tests only (the panel does not show it). A set, because
   * some keys do something else on another kind of row: ↵ opens a document and toggles a folder.
   */
  actions: readonly T[]
}

const plain = (key: string): KeySpec => ({ key, metaKey: false, ctrlKey: false, altKey: false })

// Derived from treeKeys.ts and searchKeys.ts; shortcuts.test.ts feeds every KeySpec back into those
// functions, so the overview cannot list a key that does nothing.
export const treeShortcuts: readonly StaticShortcut<TreeAction['type']>[] = [
  { keys: [plain('j'), plain('ArrowDown')], display: 'j / ↓', label: strings.shortcutsTreeNext, actions: ['move'] },
  { keys: [plain('k'), plain('ArrowUp')], display: 'k / ↑', label: strings.shortcutsTreePrev, actions: ['move'] },
  { keys: [plain('Enter')], display: '↵', label: strings.shortcutsTreeOpen, actions: ['open', 'toggle'] },
  { keys: [plain('a')], display: 'a', label: strings.shortcutsTreeNew, actions: ['new-document'] },
  { keys: [plain('r')], display: 'r', label: strings.shortcutsTreeRename, actions: ['rename'] },
  { keys: [plain('d')], display: 'd', label: strings.shortcutsTreeTrash, actions: ['trash', 'remove-project'] },
  { keys: [plain('Escape')], display: 'Esc', label: strings.shortcutsTreeEditor, actions: ['focus-editor'] },
]

export const searchShortcuts: readonly StaticShortcut<SearchAction['type']>[] = [
  { keys: [plain('ArrowDown')], display: '↓', label: strings.shortcutsSearchNext, actions: ['move'] },
  { keys: [plain('ArrowUp')], display: '↑', label: strings.shortcutsSearchPrev, actions: ['move'] },
  { keys: [plain('Enter')], display: '↵', label: strings.shortcutsSearchOpen, actions: ['open'] },
  { keys: [plain('Escape')], display: 'Esc', label: strings.shortcutsSearchClose, actions: ['close'] },
  { keys: [{ ...plain('p'), ctrlKey: true }], display: '⌃P', label: strings.shortcutsSearchScope, actions: ['toggle-scope'] },
]

// ---- the sections -----------------------------------------------------------------------------

export interface Row {
  keys: string
  label: string
  /** the modes (as the user reads them) in which the keys do this; empty for rows without a mode */
  modes: string[]
  /** the name of the group the keys belong to; null = none */
  group: string | null
}

export interface Section {
  id: 'app' | 'tree' | 'search' | 'nvim'
  title: string
  rows: Row[]
  /** a line instead of (or above) the rows, e.g. why there are none; null = none */
  note: string | null
}

export interface SectionsInput {
  menu: readonly MenuShortcut[]
  keymaps: Keymaps | null
  nvimNote: string | null
}

// init.lua puts "Pisar: " in front of the descriptions it writes, so that they are recognisable in
// the output of :map; the overview is Pisar's own, so it leaves the prefix out. The prefix is read
// from a text of strings.json instead of being typed again here.
const descPrefix = strings.nvimMapRows.slice(0, strings.nvimMapRows.indexOf(': ') + 2)

const leader = '<Space>'

const modeOrder = ['n', 'x', 'i']

const modeLabels: Readonly<Record<string, string>> = {
  n: strings.shortcutsModeNormal,
  x: strings.shortcutsModeVisual,
  i: strings.shortcutsModeInsert,
}

const sameTokens = (a: readonly string[], b: readonly string[]) => a.length <= b.length && a.every((t, i) => t === b[i])

/**
 * The name of the longest group whose key is a prefix of the keys after the leader. Compared key by
 * key, so the group "g" does not claim "<Space><C-g>".
 */
function groupOf(keys: string, groups: Readonly<Record<string, string | undefined>>): string | null {
  if (!keys.startsWith(leader)) return null
  const after = tokenize(keys.slice(leader.length))
  let best: { length: number; name: string } | null = null
  for (const [groupKeys, name] of Object.entries(groups)) {
    if (!name) continue
    const tokens = tokenize(groupKeys)
    if (tokens.length === 0 || !sameTokens(tokens, after)) continue
    if (best === null || tokens.length > best.length) best = { length: tokens.length, name }
  }
  return best?.name ?? null
}

function nvimRows(keymaps: Keymaps): Row[] {
  const groups = keymaps.groups ?? {}
  // The same keys with the same description in several modes are one row. Insertion order of the Map
  // is the order of first appearance.
  const merged = new Map<string, { keys: string; desc: string; modes: Set<string> }>()
  for (const map of keymaps.maps ?? []) {
    const id = `${map.keys}\u0000${map.desc}`
    const entry = merged.get(id) ?? { keys: map.keys, desc: map.desc, modes: new Set<string>() }
    entry.modes.add(map.mode)
    merged.set(id, entry)
  }
  const rank = (mode: string) => {
    const i = modeOrder.indexOf(mode)
    return i < 0 ? modeOrder.length : i
  }
  return [...merged.values()].map((entry) => ({
    keys: formatNvimKeys(entry.keys),
    label: entry.desc.startsWith(descPrefix) ? entry.desc.slice(descPrefix.length) : entry.desc,
    modes: [...entry.modes].sort((a, b) => rank(a) - rank(b)).map((mode) => modeLabels[mode] ?? mode),
    group: groupOf(entry.keys, groups),
  }))
}

const staticRows = (list: readonly StaticShortcut<string>[]): Row[] =>
  list.map((s) => ({ keys: s.display, label: s.label, modes: [], group: null }))

export function buildSections(input: SectionsInput): Section[] {
  return [
    {
      id: 'app',
      title: strings.shortcutsApp,
      rows: input.menu.map((m) => ({ keys: formatAccel(m.accel), label: m.label, modes: [], group: null })),
      note: null,
    },
    { id: 'tree', title: strings.shortcutsTree, rows: staticRows(treeShortcuts), note: null },
    { id: 'search', title: strings.shortcutsSearch, rows: staticRows(searchShortcuts), note: null },
    {
      id: 'nvim',
      title: strings.shortcutsNvim,
      rows: input.keymaps ? nvimRows(input.keymaps) : [],
      note: input.nvimNote,
    },
  ]
}
