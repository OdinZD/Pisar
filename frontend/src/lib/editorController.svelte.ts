// Reactive state of the editor column and the service calls behind it. All decisions are made by the
// pure functions in editorState.ts; this class holds the state, subscribes to the events and calls
// the Go service. Starting Neovim itself is done by Terminal.svelte (it needs the terminal's size);
// it reports back through terminalStarted() / terminalFailed().
import { Events } from '@wailsio/runtime'
import { DocumentActive, DocumentExport, DocumentState, MenuCopy, MenuFormat, NvimExit } from '../../bindings/events'
import * as service from '../../bindings/Pisar/editorservice'
import { errorCode, type ErrorCode } from './apiError'
import { performCopy } from './copyAction'
import {
  copyFinished,
  copyStarted,
  documentActive,
  documentExport,
  documentState,
  initialState,
  nvimExited,
  openExecuting,
  openSettled,
  requestOpen,
  requestStart,
  startFailed,
  startSucceeded,
  type ActiveDocument,
  type EditorState,
  type ExportRecord,
  type OpenRequest,
  type Phase,
  type Transition,
} from './editorState'
import { describeFocus, noTerminalFocus, type TerminalFocus } from './focus'
import { shouldFormat } from './formatAction'
import { missingTools, type ToolName } from './toolsState'

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
  /** the last export step of the documents that have one, by path of the user's file */
  exports = $derived<ReadonlyMap<string, ExportRecord>>(this.#state.exports)
  /** a Word document is being opened, i.e. converted, right now */
  opening = $derived<boolean>(this.#state.wordOpens.length > 0)
  /** "Spremi kao kopiju" is running */
  savingCopy = $derived<boolean>(this.#state.savingCopy)
  /** the error of the last "Spremi kao kopiju", for the toolbar */
  copyError = $derived<ErrorCode | null>(this.#state.copyError)

  #tools = $state.raw<ToolName[]>([])
  /** the tools (nvim, pandoc) the last check did not find; empty when all are there or the check failed */
  missingTools = $derived<ToolName[]>(this.#tools)
  /** a tool check is running right now */
  checkingTools = $state(false)

  /** Set by the editor pane: gives the terminal keyboard focus (a no-op while there is none). */
  focusTerminal: () => void = () => {}

  /** Set by the editor pane: whether the terminal has the focus and what it has selected with the mouse. */
  terminalFocus: () => TerminalFocus = () => noTerminalFocus

  /**
   * Set by the editor pane: queues a call behind the keystrokes already typed (and before the ones
   * typed later), so it reaches Neovim in the order the user pressed the keys. False when there is no
   * running terminal; the call does not run then.
   */
  runInOrder: (call: () => Promise<void>) => boolean = () => false

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
      Events.On(DocumentExport, (ev) => {
        this.#state = documentExport(this.#state, ev.data)
      }),
      Events.On(MenuCopy, () => void this.#copyFromMenu()),
      Events.On(MenuFormat, (ev) => this.#formatFromMenu(ev.data)),
    ]
    // The application was started with a document: Go opens it after the first successful Start,
    // so the editor only has to run.
    service
      .HasInitialDocument()
      .then((has) => {
        if (live && has) this.#apply(requestStart(this.#state))
      })
      .catch((err) => console.error('HasInitialDocument failed', err))

    void this.checkTools()

    const cleanup = () => {
      live = false
      for (const off of offs) off()
      if (this.#stop === cleanup) this.#stop = null
    }
    this.#stop = cleanup
    return cleanup
  }

  /**
   * Looks the tools up again. A failed call is only logged and counts as "nothing is missing": a
   * warning without evidence would be worse than none.
   */
  async checkTools(): Promise<void> {
    if (this.checkingTools) return
    this.checkingTools = true
    try {
      // The binding types the list as nullable (a Go nil slice), though Tools always returns both.
      this.#tools = missingTools((await service.Tools()) ?? [])
    } catch (err) {
      console.error('Tools failed', err)
      this.#tools = []
    } finally {
      this.checkingTools = false
    }
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

  /**
   * "Spremi kao kopiju" for the active Word document. The service opens a native dialog; closing it is
   * not an error. A real failure goes to the toolbar. Afterwards the terminal gets the keyboard back.
   */
  async saveCopy(): Promise<void> {
    if (this.#state.savingCopy) return
    this.#state = copyStarted(this.#state)
    try {
      await service.SaveCopy()
      this.#state = copyFinished(this.#state, null)
    } catch (err) {
      console.error('save copy failed', err)
      this.#state = copyFinished(this.#state, errorCode(err))
    }
    this.focusTerminal()
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

  // The Copy menu item only tells us it was chosen; what to copy depends on where the focus is. The
  // menu does not move the DOM focus, so document.activeElement is still the element the user was in.
  async #copyFromMenu(): Promise<void> {
    const focus = describeFocus(document.activeElement, this.terminalFocus())
    try {
      await performCopy(focus, { copy: () => service.Copy(), copyText: (text) => service.CopyText(text) })
    } catch (err) {
      console.error('copy failed', err)
    }
  }

  // Cmd+B / Cmd+I. Only the terminal may be formatted from: in an input field the shortcut must leave
  // the document alone. The call goes through the terminal's ordered queue, so text typed just before
  // is in Neovim before the marks. A failure is only logged: an action that does not apply (a read-only
  // document, a prompt waiting for a key) already returns success, and a real failure is rare and not
  // something the user can act on. Focus stays where it is; the menu does not move it.
  #formatFromMenu(kind: string): void {
    if (!shouldFormat(describeFocus(document.activeElement, this.terminalFocus()), this.phase)) return
    const queued = this.runInOrder(async () => {
      try {
        await service.Format(kind)
      } catch (err) {
        console.error('format failed', err)
      }
    })
    if (!queued) console.error('format dropped: the terminal is not running')
  }

  #apply(t: Transition): void {
    this.#state = t.state
    for (const { request, reason } of t.dropped) {
      if (reason === 'superseded') this.#settle(request.id)
      else if (reason === 'start-failed') this.#settle(request.id, codedError(t.state.error ?? 'internal', 'editor did not start'))
      else this.#settle(request.id, codedError('not-running', 'editor exited'))
    }
    if (t.run) void this.#execute(t.run)
  }

  // A Word document is converted inside this call, which can take long: the state says so meanwhile.
  async #execute(request: OpenRequest): Promise<void> {
    this.#state = openExecuting(this.#state, request)
    try {
      await service.OpenDocument(request.projectId, request.rel)
      this.#state = openSettled(this.#state, request.id)
      this.#settle(request.id)
    } catch (err) {
      this.#state = openSettled(this.#state, request.id)
      this.#settle(request.id, err ?? new Error('open failed'))
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
