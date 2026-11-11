<script lang="ts">
  import { onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import { Terminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import { WebglAddon } from '@xterm/addon-webgl'
  import '@xterm/xterm/css/xterm.css'

  import { NvimExit, PtyData } from '../bindings/events'
  import { Renderer, Resize, Start, Trace, Write } from '../bindings/Pisar/spike/terminalservice'
  import { loadTerminalFonts } from './fonts'
  import { createSerialSender } from './serialSender'
  import { strings } from './strings'
  import { terminalFont, terminalTheme } from './terminalTheme'

  type RendererKind = 'dom' | 'webgl'
  // idle: before the first Start; starting: Start is in flight; running: Neovim
  // is up; exited: Neovim is gone and the terminal waits for Enter.
  type Phase = 'idle' | 'starting' | 'running' | 'exited'

  let container: HTMLDivElement

  const errText = (err: unknown): string => (err instanceof Error ? err.message : String(err))

  // Diagnostic line for the Go-side trace file; never allowed to throw.
  function trace(msg: string): void {
    Trace(msg).catch((err) => console.error('trace failed', err))
  }

  onMount(() => {
    let disposed = false
    let phase: Phase = 'idle'
    const cleanups: Array<() => void> = []

    init().catch((err) => {
      console.error('terminal init failed', err)
      trace(`init failed err=${errText(err)}`)
    })

    return () => {
      disposed = true
      for (const cleanup of cleanups.reverse()) cleanup()
    }

    async function init(): Promise<void> {
      // 1. Fonts first: xterm.js fixes the cell size when it opens.
      try {
        await loadTerminalFonts()
        trace('font loaded=true')
      } catch (err) {
        console.error('font load failed, falling back to monospace', err)
        trace(`font loaded=false err=${errText(err)}`)
      }
      if (disposed) return

      // 2. Terminal, fit, open.
      const term = new Terminal({
        // The family list ends in monospace, so a failed font load degrades
        // to the system monospace font without a second code path.
        fontFamily: `"${terminalFont.family}", monospace`,
        fontSize: terminalFont.sizePx,
        fontWeight: terminalFont.weight,
        fontWeightBold: terminalFont.weightBold,
        lineHeight: terminalFont.lineHeightPx / terminalFont.sizePx,
        theme: terminalTheme,
        // Neovim draws on the alternate screen; there is nothing to scroll back to.
        scrollback: 0,
        // Explicitly false: Option must compose characters on the Croatian
        // layout (Option+key types symbols and accents). With true xterm.js would
        // turn Option into Meta and send ESC prefixes instead of the character.
        macOptionIsMeta: false,
        cursorBlink: false,
      })
      const fit = new FitAddon()
      term.loadAddon(fit)
      cleanups.push(() => {
        term.dispose() // also disposes loaded addons
      })
      term.open(container)

      // Window size changes: at most one fit per frame.
      let frame = 0
      function scheduleFit(): void {
        if (frame) return
        frame = requestAnimationFrame(() => {
          frame = 0
          fit.fit()
          void pushSize()
        })
      }

      // Renderer: applied after open() (the addon needs the DOM) and before
      // the first fit, so the grid is computed from the final cell metrics.
      let renderer: RendererKind = 'dom'
      let webgl: WebglAddon | null = null
      function setRenderer(kind: RendererKind): void {
        if (kind === renderer) return
        if (kind === 'webgl') {
          const addon = new WebglAddon()
          try {
            addon.onContextLoss(() => {
              trace('renderer webgl lost its context, falling back to dom')
              setRenderer('dom')
            })
            term.loadAddon(addon)
          } catch (err) {
            console.error('webgl renderer unavailable, staying on dom', err)
            trace(`renderer webgl unavailable, staying on dom err=${errText(err)}`)
            try {
              addon.dispose()
            } catch (disposeErr) {
              console.error('dispose of failed webgl addon', disposeErr)
            }
            return
          }
          webgl = addon
          renderer = 'webgl'
        } else {
          webgl?.dispose()
          webgl = null
          renderer = 'dom'
        }
        calibrateLineHeight() // character metrics can differ between renderers
        trace(`renderer switched to ${renderer}${cellInfo()}`)
        scheduleFit()
      }
      window.pisarSetRenderer = (kind) => {
        if (kind !== 'dom' && kind !== 'webgl') {
          console.error(`pisarSetRenderer: unknown renderer ${String(kind)}`)
          return
        }
        setRenderer(kind)
      }
      cleanups.push(() => {
        delete window.pisarSetRenderer
      })

      // Cell size is not public API of xterm.js; the screen element is exactly
      // cols x rows cells, so dividing gives the size the renderer really uses.
      function cellSize(): { width: number; height: number } | null {
        const screen = term.element?.querySelector('.xterm-screen')
        if (!screen || term.cols < 1 || term.rows < 1) return null
        const box = screen.getBoundingClientRect()
        return { width: box.width / term.cols, height: box.height / term.rows }
      }
      function cellInfo(): string {
        const cell = cellSize()
        if (!cell) return ''
        return ` cell ${cell.width.toFixed(2)}x${cell.height.toFixed(2)} cols=${term.cols} rows=${term.rows}`
      }

      // xterm.js does not multiply the font size by lineHeight: it multiplies
      // the *measured height of a character* (about 1.3 x font size for
      // JetBrains Mono) and floors the result in device pixels. So the ratio
      // 21/13 would give a 27 px row. Measure the natural height with
      // lineHeight 1 and derive the factor that yields exactly lineHeightPx.
      // The 0.5 device pixel keeps float error from flooring one pixel short.
      function calibrateLineHeight(): void {
        term.options.lineHeight = 1
        const natural = cellSize()?.height
        if (!natural) return
        const dpr = window.devicePixelRatio || 1
        term.options.lineHeight = (terminalFont.lineHeightPx * dpr + 0.5) / (natural * dpr)
      }

      let initialRenderer: RendererKind = 'dom'
      try {
        initialRenderer = (await Renderer()) === 'webgl' ? 'webgl' : 'dom'
      } catch (err) {
        console.error('Renderer() failed, using dom', err)
      }
      if (disposed) return
      setRenderer(initialRenderer)
      calibrateLineHeight()

      // fit() needs the renderer's cell size, which can be zero for a moment
      // right after open(); retry for a few frames before giving up.
      for (let attempt = 0; !fit.proposeDimensions(); attempt++) {
        if (attempt >= 30) {
          trace(`fit has no dimensions, using ${term.cols}x${term.rows}`)
          break
        }
        await new Promise((resolve) => requestAnimationFrame(resolve))
        if (disposed) return
      }
      fit.fit()

      // 3. Output first: subscribe before Start, so nothing Neovim draws is missed.
      let firstData = true
      const encoder = new TextEncoder()
      const offData = Events.On(PtyData, (ev) => {
        if (firstData) {
          firstData = false
          trace(`first data bytes=${encoder.encode(ev.data).length}`)
        }
        term.write(ev.data)
      })
      cleanups.push(offData)

      // 4. Input. One serial sender for onData and onBinary, so order is kept.
      const sender = createSerialSender(
        (data) => Write(data),
        (err) => console.error('write failed', err),
      )
      const dataSub = term.onData((data) => {
        if (phase === 'exited') {
          // Only Enter does anything now; everything else is swallowed.
          if (data === '\r') void start()
          return
        }
        sender.send(data)
      })
      let binaryReported = false
      const binarySub = term.onBinary((data) => {
        // Write carries a Go string (UTF-8), so bytes >= 0x80 cannot go through
        // unchanged. Neovim asks for SGR mouse reporting, which never uses
        // onBinary; record it if that ever stops being true.
        if (!binaryReported) {
          binaryReported = true
          trace(`onBinary fired len=${data.length}`)
        }
        if (phase !== 'exited') sender.send(data)
      })
      cleanups.push(() => {
        dataSub.dispose()
        binarySub.dispose()
      })

      // Observing starts only now: pushSize and start() exist, and a resize
      // before this point is covered by the fit() above.
      const observer = new ResizeObserver(scheduleFit)
      observer.observe(container)
      cleanups.push(() => {
        observer.disconnect()
        if (frame) cancelAnimationFrame(frame)
      })

      // Size last reported to Go. Resize is sent only when the grid differs
      // from it, never before Start has finished.
      let sent = { cols: 0, rows: 0 }
      let resizing = false
      async function pushSize(): Promise<void> {
        // One Resize in flight at a time (calls can overtake each other, see
        // serialSender.ts); the loop sends the newest size once it is free.
        if (resizing) return
        resizing = true
        try {
          while (phase === 'running' && (term.cols !== sent.cols || term.rows !== sent.rows)) {
            sent = { cols: term.cols, rows: term.rows }
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

      // 5. Start Neovim with the size fit chose; also used to restart after exit.
      async function start(): Promise<void> {
        if (phase === 'starting' || phase === 'running') return
        if (phase === 'exited') trace('restarting nvim')
        phase = 'starting'
        term.reset() // drops the exit message
        const { cols, rows } = term
        try {
          await Start(cols, rows)
        } catch (err) {
          console.error('start failed', err)
          trace(`start failed err=${errText(err)}`)
          if (disposed) return
          phase = 'exited'
          term.write(strings.startFailed)
          return
        }
        // Neovim can exit before Start returns; the exit handler has already
        // shown its message then.
        if (disposed || phase !== 'starting') return
        sent = { cols, rows }
        phase = 'running'
        trace(`${cellInfo().trim()} renderer=${renderer}`)
        term.focus()
        void pushSize() // the window may have changed size during Start
      }

      const offExit = Events.On(NvimExit, () => {
        trace('nvim exited, waiting for Enter')
        phase = 'exited'
        term.reset()
        term.write(strings.nvimExited)
      })
      cleanups.push(offExit)

      await start()
    }
  })
</script>

<div class="terminal" bind:this={container}></div>

<style>
  .terminal {
    position: fixed;
    inset: 0;
  }
</style>
