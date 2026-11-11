import { describe, expect, it } from 'vitest'
import { keyToAction, type KeyLike } from './treeKeys'
import type { DirRow, DocRow, ProjectRow } from './tree'

const base = { key: 'k', projectId: 'p', depth: 0 }
const project: ProjectRow = { ...base, type: 'project', name: 'P', expanded: true, missing: false, note: null }
const folder: DirRow = { ...base, type: 'dir', rel: 'd', name: 'd', expanded: false }
const doc: DocRow = { ...base, type: 'doc', rel: 'a.md', fileName: 'a.md', title: 'a', kind: 'md', kindLabel: 'MD' }
const txt: DocRow = { ...doc, key: 'k2', rel: 'mapa/b.txt', fileName: 'b.txt', kind: 'txt', kindLabel: 'TXT' }
const word: DocRow = { ...doc, rel: 'c.docx', fileName: 'c.docx', kind: 'docx', kindLabel: 'DOCX' }
const oldWord: DocRow = { ...word, rel: 'd.doc', fileName: 'd.doc', kind: 'doc', kindLabel: 'DOC' }
const press = (key: string, mods: Partial<KeyLike> = {}): KeyLike => ({ key, metaKey: false, ctrlKey: false, altKey: false, ...mods })

describe('keyToAction', () => {
  it.each([
    ['j', doc, { type: 'move', delta: 1 }],
    ['ArrowDown', null, { type: 'move', delta: 1 }],
    ['k', doc, { type: 'move', delta: -1 }],
    ['ArrowUp', folder, { type: 'move', delta: -1 }],
    ['Enter', folder, { type: 'toggle' }],
    ['Enter', project, { type: 'toggle' }],
    ['Enter', doc, { type: 'open', projectId: 'p', rel: 'a.md' }],
    ['Enter', txt, { type: 'open', projectId: 'p', rel: 'mapa/b.txt' }],
    ['Enter', word, { type: 'open', projectId: 'p', rel: 'c.docx' }],
    ['Enter', oldWord, { type: 'open', projectId: 'p', rel: 'd.doc' }],
    ['Enter', null, null],
    ['a', doc, { type: 'new-document' }],
    ['a', null, { type: 'new-document' }],
    ['r', doc, { type: 'rename' }],
    ['r', folder, null],
    ['r', project, null],
    ['d', doc, { type: 'trash' }],
    ['d', project, { type: 'remove-project' }],
    ['d', folder, null],
    ['Escape', doc, { type: 'focus-editor' }],
    ['Escape', null, { type: 'focus-editor' }],
    ['x', doc, null],
    ['J', doc, null],
  ] as const)('%s on %j', (key, row, expected) => {
    expect(keyToAction(press(key), row)).toEqual(expected)
  })

  it('Escape does nothing while the remove question is open', () => {
    expect(keyToAction(press('Escape'), project, { confirming: true })).toBeNull()
    expect(keyToAction(press('Escape'), project, { confirming: false })).toEqual({ type: 'focus-editor' })
  })

  it.each(['metaKey', 'ctrlKey', 'altKey'] as const)('ignores %s', (mod) => {
    for (const key of ['j', 'k', 'a', 'r', 'd', 'Enter', 'ArrowDown', 'Escape']) {
      expect(keyToAction(press(key, { [mod]: true }), doc)).toBeNull()
    }
  })
})
