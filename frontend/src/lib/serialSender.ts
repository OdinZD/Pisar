// Serial, coalescing sender for terminal input.
//
// Wails v3.1.0 handles every bound-method call in its own goroutine
// (AssetServer.ServeWebViewRequest starts `go processWebViewRequest`), so two
// Write calls in flight can reach Neovim in either order. Keystrokes must keep
// their order, so at most one send is in flight: data that arrives meanwhile is
// joined and sent as one string when the previous send finishes.
//
// No dependency on Wails or xterm.js, so it can be tested on its own.

export type SendFn = (data: string) => Promise<void>

export interface SerialSender {
  // Queues data. Returns at once; an empty string is ignored.
  send(data: string): void
}

// onError is called with each failed send. A failure never stops the queue:
// data queued behind it is still sent.
export function createSerialSender(
  send: SendFn,
  onError: (err: unknown) => void,
): SerialSender {
  let pending = ''
  let busy = false

  async function drain(): Promise<void> {
    busy = true
    try {
      while (pending !== '') {
        const batch = pending
        pending = ''
        try {
          await send(batch)
        } catch (err) {
          onError(err)
        }
      }
    } finally {
      busy = false
    }
  }

  return {
    send(data: string): void {
      if (data === '') return
      pending += data
      // When idle, drain() runs up to its first await synchronously, so the
      // first send happens in this very call.
      if (!busy) void drain()
    },
  }
}
