// Reactive state of the active theme and what a theme switch has to touch. Go announces the choice
// (theme:changed); this class recolours the interface through the CSS variables, sets `id` for the
// components that own a canvas of their own (the terminal, the preview frame), and sends Neovim its
// new colorscheme through a serial sender (see themeSender.ts).
import { Events } from '@wailsio/runtime'
import { ThemeChanged } from '../../bindings/events'
import { SetColorscheme } from '../../bindings/Pisar/editorservice'
import { Theme } from '../../bindings/Pisar/settingsservice'
import { activeTheme, isThemeId, setActiveTheme } from '../theme/active'
import { applyTheme } from '../theme/cssVars'
import { nvimColorscheme } from '../theme/nvimColorscheme'
import { themes, type ThemeId } from '../theme/tokens'
import { createThemeSender } from './themeSender'

export class ThemeController {
  /** the theme in force; components read it to recolour what the CSS variables do not reach */
  id = $state<ThemeId>(activeTheme())

  // A failed send leaves Neovim in the old theme while the interface is already in the new one: known
  // and accepted, there is nothing the user could do about it, so it is only logged.
  #sender = createThemeSender(
    (id) => SetColorscheme(nvimColorscheme(themes[id])),
    (id, err) => console.error(`sending the colorscheme of theme ${id} to Neovim failed`, err),
  )

  /** Subscribes to theme:changed and returns the cleanup that removes the listener. */
  start(): () => void {
    const stop = Events.On(ThemeChanged, (ev) => this.#apply(ev.data))
    // A theme chosen between main.ts asking Go and the subscription above was announced to nobody.
    // Asking once more, now that the listener exists, closes that gap.
    Theme().then(
      (id) => {
        if (id !== this.id) this.#apply(id)
      },
      (err: unknown) => console.error('asking for the theme after start failed', err),
    )
    return stop
  }

  #apply(id: unknown): void {
    if (!isThemeId(id)) {
      console.error('theme:changed with an unknown theme id', id)
      return
    }
    setActiveTheme(id)
    applyTheme(document.documentElement, themes[id].colors)
    this.id = id
    this.#sender.request(id)
  }

  /**
   * Sends the colorscheme of the current theme to Neovim again. For a theme that changed while Neovim
   * was still starting: Start writes the colorscheme it was given, which may be the older one.
   */
  resend(): void {
    this.#sender.request(this.id)
  }
}
