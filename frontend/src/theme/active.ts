import { defaultTheme, themeIds, type ThemeId } from './tokens'

// The only place that knows which theme is active. A plain variable: the theme is chosen before the
// application is mounted and nothing reacts to a change yet.
let active: ThemeId = defaultTheme

export function activeTheme(): ThemeId {
  return active
}

export function setActiveTheme(id: ThemeId): void {
  active = id
}

export function isThemeId(value: unknown): value is ThemeId {
  return typeof value === 'string' && (themeIds as readonly string[]).includes(value)
}

/** The theme named by the `theme` parameter of a location.search string, or null if absent or unknown. */
export function themeFromSearch(search: string): ThemeId | null {
  const value = new URLSearchParams(search).get('theme')
  return isThemeId(value) ? value : null
}
