<script lang="ts">
  import { onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import { Terminal, type ITheme } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'

  import { PtyData } from '../../bindings/events'
  import { Resize, Start, Write } from '../../bindings/Pisar/editorservice'
  import { noTerminalFocus, type TerminalFocus } from '../lib/focus'
  import { statusInset } from '../lib/searchLayout'
  import { createSerialSender } from '../lib/serialSender'
  import { fontStack } from '../theme/cssVars'
  import { nvimColorscheme } from '../theme/nvimColorscheme'
  import { terminalTheme } from '../theme/terminalTheme'
  import { fontWeights, search, typography } from '../theme/tokens'

  // The controller learns the outcome of Start here; the terminal is unmounted by the parent when
  // the editor stops, and no callback fires after that. `onlayout` reports, after every fit, how far
  // above the bottom of the terminal Neovim's status line starts (the search panel sits there).
  let {
    onstarted,
    onfailed,
    onlayout,
    keepsFocus,
  }: {
    onstarted: () => void
    onfailed: (err: unknown) => void
    onlayout?: (statusInsetPx: number) => void
    // True while something else (the open search panel) has the keyboard on purpose: a start that
    // finishes then must not pull it into the terminal.
    keepsFocus?: () => boolean
  } = $props()

  let container: HTMLDivElement
  let term: Terminal | undefined

  /** Gives the terminal keyboard focus. */
  export function focus(): void {
    term?.focus()
  }

  /**
   * Whether the terminal's input has the keyboard focus, and the text selected in it with the mouse
   * (xterm.js' own selection, not Neovim's visual mode). Used by the Copy menu item.
   */
  export function focusState(): TerminalFocus {
    const t = term
    if (!t) return noTerminalFocus
    return {
      focused: t.textarea !== undefined && t.textarea === document.activeElement,
      selection: t.hasSelection() ? t.getSelection() : '',
    }
  }

  onMount(() => {
    let disposed = false
    const cleanups: Array<() => void> = []

    init().catch((err) => {
      console.error('terminal init failed', err)
      if (!disposed) onfailed(err)
    })

    return () => {
      disposed = true
      for (const cleanup of cleanups.reverse()) cleanup()
    }

    // xterm.js measures the cell when it opens, so the container must be laid out first.
    async function waitForSize(): Promise<void> {
      while (!disposed && (container.clientWidth === 0 || container.clientHeight === 0)) {
        await new Promise((resolve) => requestAnimationFrame(resolve))
      }
    }

    async function init(): Promise<void> {
      await waitForSize()
      if (disposed) return

      const t = new Terminal({
        fontFamily: fontStack('mono'),
        fontSize: typography.editorSize,
        fontWeight: fontWeights.regular,
        fontWeightBold: fontWeights.bold,
        lineHeight: 1, // replaced by calibrateLineHeight() once the cell is measured
        // The colours come from the tokens; `satisfies` fails the build if the shape stops fitting xterm.js.
        theme: terminalTheme() satisfies ITheme,
        // Neovim draws on the alternate screen; there is nothing to scroll back to.
        scrollback: 0,
        // Explicitly false: on the Croatian layout Option composes characters (Option+key types
        // symbols and accents). With true xterm.js would turn Option into Meta and send ESC
        // prefixes instead of the character.
        macOptionIsMeta: false,
        // Neovim turns on mouse reporting, and then a drag goes to Neovim instead of selecting text.
        // With this, Option+drag selects in the terminal anyway (xterm.js: SelectionService.shouldForceSelection).
        macOptionClickForcesSelection: true,
        cursorBlink: false,
      })
      term = t
      const fit = new FitAddon()
      t.loadAddon(fit)
      cleanups.push(() => {
        t.dispose() // also disposes the loaded addons
        term = undefined
      })
      // Keys with Cmd belong to the application (Cmd+S, Cmd+C, Cmd+V) and never reach Neovim.
      // xterm.js would still encode some of them (Cmd+Enter as CR, Cmd+A as its own select-all),
      // so it skips every event with metaKey. Returning false does not cancel the event, so the
      // browser keeps its default: paste arrives as a "paste" event, which xterm.js handles itself
      // and sends through onData. Option and Ctrl combinations are untouched.
      t.attachCustomKeyEventHandler((ev) => !ev.metaKey)
      t.open(container)

      const onClick = () => t.focus()
      container.addEventListener('click', onClick)
      cleanups.push(() => container.removeEventListener('click', onClick))

      // Cell size is not public API of xterm.js; the screen element is exactly cols x rows cells,
      // so dividing gives the size the renderer really uses.
      function cellHeight(): number | null {
        const screen = t.element?.querySelector('.xterm-screen')
        if (!screen || t.cols < 1 || t.rows < 1) return null
        return screen.getBoundingClientRect().height / t.rows
      }

      // xterm.js does not multiply the font size by lineHeight: it multiplies the measured height of
      // a character (about 1.3 x the font size here) and floors the result in device
      // pixels, so 21/13 would give a 27 px row. Measure the natural height with lineHeight 1 and
      // derive the factor that yields exactly editorLineHeight. The 0.5 device pixel keeps float
      // error from flooring one pixel short.
      function calibrateLineHeight(): void {
        t.options.lineHeight = 1
        const natural = cellHeight()
        if (!natural) return
        const dpr = window.devicePixelRatio || 1
        t.options.lineHeight = (typography.editorLineHeight * dpr + 0.5) / (natural * dpr)
      }
      calibrateLineHeight()

      function reportLayout(): void {
        const cell = cellHeight()
        onlayout?.(cell === null ? 0 : statusInset(container.clientHeight, t.rows, cell, search.reservedRows))
      }

      // fit() needs the renderer's cell size, which can be zero for a moment right after open().
      for (let attempt = 0; !fit.proposeDimensions(); attempt++) {
        if (attempt >= 30) throw new Error('terminal has no size to fit to')
        await new Promise((resolve) => requestAnimationFrame(resolve))
        if (disposed) return
      }
      fit.fit()
      reportLayout()

      // Output first: subscribe before Start, so nothing Neovim draws is missed.
      cleanups.push(Events.On(PtyData, (ev) => t.write(ev.data)))

      // Input: one serial sender for onData and onBinary keeps the order of the keystrokes. Until
      // Start has finished there is no Neovim to receive them, so they are dropped.
      let running = false
      const sender = createSerialSender(
        (data) => Write(data),
        (err) => console.error('write failed', err),
      )
      const dataSub = t.onData((data) => {
        if (running) sender.send(data)
      })
      // Neovim asks for SGR mouse reporting, which never uses onBinary (it would carry bytes >= 0x80,
      // which a Go string cannot take unchanged); forwarded only for completeness.
      const binarySub = t.onBinary((data) => {
        if (running) sender.send(data)
      })
      cleanups.push(() => {
        dataSub.dispose()
        binarySub.dispose()
      })

      // Size last reported to Go. Resize is sent only when the grid differs from it, one call at a
      // time (calls can overtake each other, see serialSender.ts), and never before Start is done.
      let sent = { cols: 0, rows: 0 }
      let resizing = false
      async function pushSize(): Promise<void> {
        if (resizing) return
        resizing = true
        try {
          while (!disposed && running && (t.cols !== sent.cols || t.rows !== sent.rows)) {
            sent = { cols: t.cols, rows: t.rows }
            try {
              await Resize(sent.cols, sent.rows)
            } catch (err) {
              console.error('resize failed', err)
            }
          }
        } finally {
          resizing = false
        }
      }

      // Window size changes: at most one fit per frame.
      let frame = 0
      const observer = new ResizeObserver(() => {
        if (frame) return
        frame = requestAnimationFrame(() => {
          frame = 0
          fit.fit()
          reportLayout()
          void pushSize()
        })
      })
      observer.observe(container)
      cleanups.push(() => {
        observer.disconnect()
        if (frame) cancelAnimationFrame(frame)
      })

      const { cols, rows } = t
      try {
        await Start(cols, rows, nvimColorscheme())
      } catch (err) {
        if (!disposed) onfailed(err)
        return
      }
      if (disposed) return
      sent = { cols, rows }
      running = true
      onstarted()
      if (!keepsFocus?.()) t.focus()
      void pushSize() // the window may have changed size while Start was running
    }
  })
</script>

<div class="terminal" bind:this={container}></div>

<style>
  .terminal {
    position: absolute;
    inset: 0;
    background: var(--bg-editor);
  }
</style>
