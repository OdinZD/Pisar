import { mount } from 'svelte'
import { Theme } from '../bindings/Pisar/settingsservice'
import App from './App.svelte'
import './app.css'
import { activeTheme, setActiveTheme, startTheme, themeFromSearch } from './theme/active'
import { applyTheme } from './theme/cssVars'
import { loadFonts } from './theme/fonts'
import { defaultTheme, themes } from './theme/tokens'

// Go puts the remembered theme into the URL (?theme=<id>), so the first paint is in the right colours
// without waiting for anything. The URL can be stale after a reload that followed a theme change,
// hence Go is asked as well, below.
const fromUrl = themeFromSearch(location.search)
setActiveTheme(fromUrl ?? defaultTheme)
applyTheme(document.documentElement, themes[activeTheme()].colors)

// Fonts first, so the first paint (and later the terminal cell measurement) uses the real faces.
// A failure is not fatal: the fallback families from the tokens keep the app usable.
async function loadFontsOrFallback(): Promise<void> {
  try {
    await loadFonts()
  } catch (err) {
    console.error('font loading failed, using fallback fonts', err)
  }
}

// Not fatal either: the page then stays in the theme from the URL.
async function themeFromGo(): Promise<unknown> {
  try {
    return await Theme()
  } catch (err) {
    console.error('asking for the theme failed, using the one from the URL', err)
    return undefined
  }
}

const [, fromGo] = await Promise.all([loadFontsOrFallback(), themeFromGo()])

const theme = startTheme(fromUrl, fromGo)
if (theme !== activeTheme()) {
  setActiveTheme(theme)
  applyTheme(document.documentElement, themes[theme].colors)
}

mount(App, { target: document.getElementById('app')! })
