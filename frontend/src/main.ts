import { mount } from 'svelte'
import App from './App.svelte'
import './app.css'
import { activeTheme, setActiveTheme, themeFromSearch } from './theme/active'
import { applyTheme } from './theme/cssVars'
import { loadFonts } from './theme/fonts'
import { defaultTheme, themes } from './theme/tokens'

// The theme is chosen once, before anything is drawn; ?theme=<id> is for trying one out.
setActiveTheme(themeFromSearch(location.search) ?? defaultTheme)
applyTheme(document.documentElement, themes[activeTheme()].colors)

// Fonts first, so the first paint (and later the terminal cell measurement) uses the real faces.
// A failure is not fatal: the fallback families from the tokens keep the app usable.
try {
  await loadFonts()
} catch (err) {
  console.error('font loading failed, using fallback fonts', err)
}

mount(App, { target: document.getElementById('app')! })
