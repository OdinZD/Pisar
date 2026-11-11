import { mount } from 'svelte'
import App from './App.svelte'
import './app.css'
import { applyTheme } from './theme/cssVars'
import { loadFonts } from './theme/fonts'

applyTheme(document.documentElement)

// Fonts first, so the first paint (and later the terminal cell measurement) uses the real faces.
// A failure is not fatal: the fallback families from the tokens keep the app usable.
try {
  await loadFonts()
} catch (err) {
  console.error('font loading failed, using fallback fonts', err)
}

mount(App, { target: document.getElementById('app')! })
