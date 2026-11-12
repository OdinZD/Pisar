// Serial, latest-wins sender for the Neovim colorscheme of a theme switch.
//
// Wails v3.1.0 handles every bound-method call in its own goroutine (see serialSender.ts), so two
// SetColorscheme calls in flight could reach Go in either order and leave Neovim in the older theme.
// At most one send is in flight; while it runs only the NEWEST requested theme is kept, because the
// ones in between would be replaced at once anyway. A request that arrives while a send of the same
// theme is in flight is still sent again: it was made after that send began (for instance Start
// wrote the old colorscheme meanwhile), so the earlier send does not prove Neovim has the theme.
//
// No dependency on Wails or Svelte, so it can be tested on its own.
import type { ThemeId } from '../theme/tokens'

export interface ThemeSender {
  /** Asks for the theme to be sent. Returns at once. */
  request(id: ThemeId): void
  /** Resolves when nothing is in flight and nothing waits; for tests. */
  idle(): Promise<void>
}

// onError is called for each failed send; a failure never stops the queue.
export function createThemeSender(
  send: (id: ThemeId) => Promise<void>,
  onError: (id: ThemeId, err: unknown) => void,
): ThemeSender {
  let busy = false
  let pending: ThemeId | null = null
  let waiters: Array<() => void> = []

  async function drain(first: ThemeId): Promise<void> {
    busy = true
    try {
      let next: ThemeId | null = first
      while (next !== null) {
        const id: ThemeId = next
        pending = null
        try {
          await send(id)
        } catch (err) {
          onError(id, err)
        }
        next = pending
      }
    } finally {
      busy = false
      const done = waiters
      waiters = []
      for (const resolve of done) resolve()
    }
  }

  return {
    request(id: ThemeId): void {
      if (busy) pending = id
      else void drain(id)
    },
    idle(): Promise<void> {
      if (!busy) return Promise.resolve()
      return new Promise<void>((resolve) => waiters.push(resolve))
    },
  }
}
