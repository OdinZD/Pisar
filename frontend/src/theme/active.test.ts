import { describe, expect, it } from 'vitest'
import { activeTheme, isThemeId, setActiveTheme, startTheme, themeFromSearch } from './active'
import { defaultTheme, themeIds } from './tokens'

describe('isThemeId', () => {
  it.each(themeIds)('accepts the known id %s', (id) => {
    expect(isThemeId(id)).toBe(true)
  })

  it.each([['nepoznata'], [''], ['Pisar'], ['__proto__'], ['toString']])('refuses the string %j', (s) => {
    expect(isThemeId(s)).toBe(false)
  })

  it.each([[undefined], [null], [1], [{}], [['pisar']], [true]])('refuses the non-string %j', (v) => {
    expect(isThemeId(v)).toBe(false)
  })
})

describe('themeFromSearch', () => {
  it.each([
    ['', null],
    ['?theme=pisar', 'pisar'],
    ['theme=pisar', 'pisar'],
    ['?x=1&theme=pisar', 'pisar'],
    ['?theme=nepoznata', null],
    ['?theme=', null],
    ['?x=1', null],
  ])('%j gives %j', (search, want) => {
    expect(themeFromSearch(search)).toBe(want)
  })

  it('every id in the registry can be asked for', () => {
    for (const id of themeIds) expect(themeFromSearch(`?theme=${id}`)).toBe(id)
  })
})

describe('startTheme', () => {
  it('prefers a valid answer from Go to the URL', () => {
    expect(startTheme('pisar', 'dracula')).toBe('dracula')
    expect(startTheme(null, 'dracula')).toBe('dracula')
  })

  it.each([[undefined], [''], ['x'], [1], [null], [{}]])('falls back to the URL when Go answered %j', (v) => {
    expect(startTheme('dracula', v)).toBe('dracula')
  })

  it.each([[undefined], [''], ['x'], [7]])('gives the default when both are unusable (Go: %j)', (v) => {
    expect(startTheme(null, v)).toBe(defaultTheme)
  })
})

describe('activeTheme', () => {
  // Module state: the first test must run before any set. vitest runs a file's tests in order.
  it('is the default until one is set', () => {
    expect(activeTheme()).toBe(defaultTheme)
  })

  it('returns what was set', () => {
    for (const id of themeIds) {
      setActiveTheme(id)
      expect(activeTheme()).toBe(id)
    }
    setActiveTheme(defaultTheme)
  })
})
