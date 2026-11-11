// Reactive state of the editor column and the service calls behind it. All decisions are made by the
// pure functions in editorState.ts; this class holds the state, subscribes to the events and calls
// the Go service. Starting Neovim itself is done by Terminal.svelte (it needs the terminal's size);
// it reports back through terminalStarted() / terminalFailed().
import { Events } from '@wailsio/runtime'
import { DocumentActive, DocumentState, NvimExit } from '../../bindings/events'
import * as service from '../../bindings/Pisar/editorservice'
import { errorCode, type ErrorCode } from './apiError'
import {
  documentActive,
  documentState,
  initialState,
  nvimExited,
  requestOpen,
  requestStart,
  startFailed,
  startSucceeded,
  type ActiveDocument,
  type EditorState,
  type Phase,
  type Transition,
} from './editorState'

interface Waiter {
  resolve: () => void
  reject: (err: unknown) => void
}

// errorCode() reads `cause.code` first, so callers classify this like an error from the service.
function codedError(code: ErrorCode, text: string): Error {
  return new Error(`${code}: ${text}`, { cause: { code } })
}

export class EditorController {
  #state = $state.raw<EditorState>(initialState)

  phase = $derived<Phase>(this.#state.phase)
  /** error code of a failed start; null in every other phase */
  error = $derived<ErrorCode | null>(this.#state.error)
  active = $derived<ActiveDocument | null>(this.#state.active)
  modified = $derived<boolean>(this.#state.modified)

  /** Set by the editor pane: gives the terminal keyboard focus (a no-op while there is none). */
  focusTerminal: () => void = () => {}

  #nextId = 0
  // Promises handed out by open() that wait for an answer, by request id.
  #waiters = new Map<number, Waiter>()
  #stop: (() => void) | null = null

  /** Subscribes to the editor events and returns the cleanup that removes them. */
  start(): () => void {
    this.stop()
    let live = true
    const offs = [
      Events.On(NvimExit, () => this.#apply(nvimExited(this.#state))),
      Events.On(DocumentActive, (ev) => {
        this.#state = documentActive(this.#state, ev.data)
      }),
      Events.On(DocumentState, (ev) => {
        this.#state = documentState(this.#state, ev.data)
      }),
    ]
    // The application was started with a document: Go opens it after the first successful Start,
    // so the editor only has to run.
    service
      .HasInitialDocument()
      .then((has) => {
        if (live && has) this.#apply(requestStart(this.#state))
      })
      .catch((err) => console.error('HasInitialDocument failed', err))

    const cleanup = () => {
      live = false
      for (const off of offs) off()
      if (this.#stop === cleanup) this.#stop = null
    }
    this.#stop = cleanup
    return cleanup
  }

  stop(): void {
    this.#stop?.()
  }

  /**
   * Opens a document, starting the editor first if it does not run. Resolves when Neovim has the
   * document (or when a newer open() replaced this one); rejects with an error that errorCode()
   * understands, e.g. 'nvim-missing' when the editor could not be started.
   */
  open(projectId: string, rel: string): Promise<void> {
    const id = ++this.#nextId
    const done = new Promise<void>((resolve, reject) => this.#waiters.set(id, { resolve, reject }))
    this.#apply(requestOpen(this.#state, { id, projectId, rel }))
    return done
  }

  /** Called by Terminal.svelte when Start has returned successfully. */
  terminalStarted(): void {
    this.#apply(startSucceeded(this.#state))
  }

  /** Called by Terminal.svelte when Start failed. */
  terminalFailed(err: unknown): void {
    console.error('starting the editor failed', err)
    this.#apply(startFailed(this.#state, errorCode(err)))
  }

  #apply(t: Transition): void {
    this.#state = t.state
    for (const { request, reason } of t.dropped) {
      if (reason === 'superseded') this.#settle(request.id)
      else if (reason === 'start-failed') this.#settle(request.id, codedError(t.state.error ?? 'internal', 'editor did not start'))
      else this.#settle(request.id, codedError('not-running', 'editor exited'))
    }
    if (t.run) void this.#execute(t.run.id, t.run.projectId, t.run.rel)
  }

  async #execute(id: number, projectId: string, rel: string): Promise<void> {
    try {
      await service.OpenDocument(projectId, rel)
      this.#settle(id)
    } catch (err) {
      this.#settle(id, err ?? new Error('open failed'))
    }
  }

  // `err` undefined means success.
  #settle(id: number, err?: unknown): void {
    const waiter = this.#waiters.get(id)
    if (!waiter) return
    this.#waiters.delete(id)
    if (err === undefined) waiter.resolve()
    else waiter.reject(err)
  }
}
