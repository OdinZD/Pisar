import { describe, expect, it } from 'vitest'
import { keyToAction, type KeyLike } from './treeKeys'
import type { DirRow, DocRow, ProjectRow } from './tree'

const base = { key: 'k', projectId: 'p', depth: 0 }
const project: ProjectRow = { ...base, type: 'project', name: 'P', expanded: true, missing: false, note: null }
const folder: DirRow = { ...base, type: 'dir', rel: 'd', name: 'd', expanded: false }
const doc: DocRow = { ...base, type: 'doc', rel: 'a.md', fileName: 'a.md', title: 'a', kindLabel: 'MD' }
const press = (key: string, mods: Partial<KeyLike> = {}): KeyLike => ({ key, metaKey: false, ctrlKey: false, altKey: false, ...mods })

describe('keyToAction', () => {
  it.each([
    ['j', doc, { type: 'move', delta: 1 }],
    ['ArrowDown', null, { type: 'move', delta: 1 }],
    ['k', doc, { type: 'move', delta: -1 }],
    ['ArrowUp', folder, { type: 'move', delta: -1 }],
    ['Enter', folder, { type: 'toggle' }],
    ['Enter', project, { type: 'toggle' }],
    ['Enter', doc, null],
    ['Enter', null, null],
    ['a', doc, { type: 'new-document' }],
    ['a', null, { type: 'new-document' }],
    ['r', doc, { type: 'rename' }],
    ['r', folder, null],
    ['r', project, null],
    ['d', doc, { type: 'trash' }],
    ['d', project, { type: 'remove-project' }],
    ['d', folder, null],
    ['x', doc, null],
    ['J', doc, null],
  ] as const)('%s on %j', (key, row, expected) => {
    expect(keyToAction(press(key), row)).toEqual(expected)
  })

  it.each(['metaKey', 'ctrlKey', 'altKey'] as const)('ignores %s', (mod) => {
    for (const key of ['j', 'k', 'a', 'r', 'd', 'Enter', 'ArrowDown']) {
      expect(keyToAction(press(key, { [mod]: true }), doc)).toBeNull()
    }
  })
})
