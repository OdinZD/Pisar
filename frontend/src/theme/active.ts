import { defaultTheme, themeIds, type ThemeId } from './tokens'

// The only place that knows which theme is active. A plain variable, so code that is not a component
// (the terminal's first options, the preview's first document) can read it; what reacts to a change
// reads ThemeController.id instead, which is reactive.
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

/** Which theme the page starts in: Go's answer if it is a known id, else the one from the URL, else the default. */
export function startTheme(fromUrl: ThemeId | null, fromGo: unknown): ThemeId {
  if (isThemeId(fromGo)) return fromGo
  return fromUrl ?? defaultTheme
}
