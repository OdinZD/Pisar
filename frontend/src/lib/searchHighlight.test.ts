import { describe, expect, it } from 'vitest'
import { baseName, highlightSegments, type Segment } from './searchHighlight'

const plain = (text: string): Segment => ({ text, hit: false })
const hit = (text: string): Segment => ({ text, hit: true })
const joined = (segments: Segment[]) => segments.map((s) => s.text).join('')

describe('baseName', () => {
  it.each([
    ['a.md', 'a'],
    ['a.b.md', 'a.b'],
    ['plan', 'plan'],
    ['kraj.', 'kraj'],
    ['.md', ''],
    ['', ''],
  ])('%j -> %j', (name, expected) => {
    expect(baseName(name)).toBe(expected)
  })
})

describe('highlightSegments', () => {
  it.each([
    ['ASCII', 'izvjestaj.md', [[0, 3]], [hit('izv'), plain('jestaj')]],
    ['ASCII in the middle', 'izvjestaj.md', [[3, 6]], [plain('izv'), hit('jes'), plain('taj')]],
    ['diacritics', 'Čašić plan.docx', [[0, 2]], [hit('Ča'), plain('šić plan')]],
    ['range at the start', 'abc.txt', [[0, 1]], [hit('a'), plain('bc')]],
    ['range at the end of the name', 'abc.txt', [[2, 3]], [plain('ab'), hit('c')]],
    ['range over the whole name', 'abc.txt', [[0, 3]], [hit('abc')]],
    ['two ranges', 'ab cd ef.md', [[0, 2], [6, 8]], [hit('ab'), plain(' cd '), hit('ef')]],
    ['more than one dot', 'a.b.md', [[2, 3]], [plain('a.'), hit('b')]],
    ['no matches', 'abc.md', [], [plain('abc')]],
    ['null matches', 'abc.md', null, [plain('abc')]],
    ['undefined matches', 'abc.md', undefined, [plain('abc')]],
    ['no extension', 'abc', [[1, 2]], [plain('a'), hit('b'), plain('c')]],
  ] as const)('%s', (_, name, matches, expected) => {
    expect(highlightSegments(name, matches)).toEqual(expected)
  })

  it('keeps a decomposed letter together when the range covers its combining mark', () => {
    // "Č" as C + U+030C, then "ak": the service reports [0,2) for the query "c".
    const name = 'Čak.md'
    expect(highlightSegments(name, [[0, 2]])).toEqual([hit('Č'), plain('ak')])
  })

  it('follows the range, also when it ends between a letter and its mark', () => {
    // The service decides; the panel does not second-guess it. Nothing is lost either way.
    const name = 'Čak.md'
    const segments = highlightSegments(name, [[0, 1]])
    expect(segments).toEqual([hit('C'), plain('̌ak')])
    expect(joined(segments)).toBe('Čak')
  })

  it('counts UTF-16 units: an emoji before the match takes two', () => {
    const name = '😀 plan.md'
    expect(highlightSegments(name, [[3, 7]])).toEqual([plain('😀 '), hit('plan')])
  })

  it('does not cut a surrogate pair in two', () => {
    const name = 'a😀b.md' // a=0, 😀=1..2, b=3
    expect(highlightSegments(name, [[2, 3]])).toEqual([plain('a'), hit('😀'), plain('b')])
    expect(highlightSegments(name, [[1, 2]])).toEqual([plain('a'), hit('😀'), plain('b')])
  })

  it('never reaches into the extension', () => {
    expect(highlightSegments('ab.md', [[1, 5]])).toEqual([plain('a'), hit('b')])
    expect(highlightSegments('ab.md', [[3, 5]])).toEqual([plain('ab')])
  })

  it.each([
    ['start beyond the end', 'abc.md', [[10, 12]]],
    ['negative start', 'abc.md', [[-5, 2]]],
    ['reversed', 'abc.md', [[2, 1]]],
    ['empty', 'abc.md', [[1, 1]]],
    ['not numbers', 'abc.md', [['a', 'b']]],
    ['fractions and NaN', 'abc.md', [[0.5, 2], [NaN, 2], [0, Infinity]]],
    ['too short', 'abc.md', [[1], []]],
    ['null entries', 'abc.md', [null, undefined, 3]],
    ['not an array at all', 'abc.md', 'oops'],
    ['an object', 'abc.md', { 0: [0, 1] }],
    ['unsorted', 'abc.md', [[2, 3], [0, 1]]],
    ['overlapping', 'abcdef.md', [[0, 3], [2, 5]]],
    ['adjacent', 'abcdef.md', [[0, 2], [2, 4]]],
    ['duplicate', 'abcdef.md', [[1, 3], [1, 3]]],
    ['empty name', '.md', [[0, 2]]],
    ['empty string', '', [[0, 1]]],
  ] as const)('does not throw, lose or repeat text on %s', (_, name, matches) => {
    const segments = highlightSegments(name, matches)
    expect(joined(segments)).toBe(baseName(name))
    expect(segments.every((s) => s.text !== '')).toBe(true)
  })

  it('repairs ranges: clips, sorts and merges', () => {
    expect(highlightSegments('abc.md', [[-5, 2]])).toEqual([hit('ab'), plain('c')])
    expect(highlightSegments('abc.md', [[1, 99]])).toEqual([plain('a'), hit('bc')])
    expect(highlightSegments('abc.md', [[2, 3], [0, 1]])).toEqual([hit('a'), plain('b'), hit('c')])
    expect(highlightSegments('abcdef.md', [[0, 3], [2, 5]])).toEqual([hit('abcde'), plain('f')])
    expect(highlightSegments('abcdef.md', [[0, 2], [2, 4]])).toEqual([hit('abcd'), plain('ef')])
    expect(highlightSegments('abc.md', [[2, 1]])).toEqual([plain('abc')])
  })

  it('never alternates two segments of the same kind', () => {
    const segments = highlightSegments('abcdef.md', [[0, 2], [2, 4], [3, 5]])
    for (let i = 1; i < segments.length; i++) expect(segments[i].hit).not.toBe(segments[i - 1].hit)
  })

  it('keeps markup-looking names as text', () => {
    expect(highlightSegments('<b>x</b>.md', [[0, 3]])).toEqual([hit('<b>'), plain('x</b>')])
  })
})
