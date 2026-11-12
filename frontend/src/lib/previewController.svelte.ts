// Reactive state of the preview pane and the service calls behind it. All decisions are made by the
// pure functions in previewState.ts; this class holds the state, subscribes to the events and tells
// the Go service whether the pane is open.
import { Events } from '@wailsio/runtime'
import { NvimExit, PreviewContent, UiTogglePreview } from '../../bindings/events'
import * as service from '../../bindings/Pisar/editorservice'
import { errorCode } from './apiError'
import {
  callFailed,
  contentArrived,
  initialPreviewState,
  nvimExited,
  toggled,
  widthChanged,
  type PreviewState,
  type ShownContent,
} from './previewState'

export class PreviewController {
  #state = $state.raw<PreviewState>(initialPreviewState)

  open = $derived<boolean>(this.#state.open)
  /** the window is too narrow for the pane: the button is disabled and the pane stays closed */
  narrow = $derived<boolean>(this.#state.narrow)
  content = $derived<ShownContent | null>(this.#state.content)

  // SetPreview calls run one after the other: Wails answers every call in its own goroutine, so
  // two calls in flight could reach Go in either order, and the last one must win.
  #calls: Promise<void> = Promise.resolve()
  #stop: (() => void) | null = null

  /**
   * Subscribes to the events, tells Go the current state once (a page that was loaded again asks
   * again) and returns the cleanup that removes the listeners.
   */
  start(): () => void {
    this.stop()
    const onResize = () => this.#apply(widthChanged(this.#state, window.innerWidth))
    window.addEventListener('resize', onResize)
    const offs = [
      Events.On(PreviewContent, (ev) => {
        this.#state = contentArrived(this.#state, ev.data)
      }),
      Events.On(UiTogglePreview, () => this.toggle()),
      Events.On(NvimExit, () => {
        this.#state = nvimExited(this.#state)
      }),
    ]
    this.#state = widthChanged(this.#state, window.innerWidth)
    this.#send(this.#state.open)

    const cleanup = () => {
      window.removeEventListener('resize', onResize)
      for (const off of offs) off()
      if (this.#stop === cleanup) this.#stop = null
    }
    this.#stop = cleanup
    return cleanup
  }

  stop(): void {
    this.#stop?.()
  }

  /** The button or `ui:toggle-preview`. */
  toggle(): void {
    this.#apply(toggled(this.#state))
  }

  // Go has to hear about every change of `open`, whatever caused it (button, key, window width).
  #apply(next: PreviewState): void {
    const changed = next.open !== this.#state.open
    this.#state = next
    if (changed) this.#send(next.open)
  }

  #send(enabled: boolean): void {
    this.#calls = this.#calls
      .then(() => service.SetPreview(enabled))
      .catch((err) => {
        console.error('SetPreview failed', err)
        this.#state = callFailed(this.#state, errorCode(err))
      })
  }
}
