// Serial, coalescing sender for terminal input.
//
// Wails v3.1.0 handles every bound-method call in its own goroutine
// (AssetServer.ServeWebViewRequest starts `go processWebViewRequest`), so two
// Write calls in flight can reach Neovim in either order. Keystrokes must keep
// their order, so at most one send is in flight: data that arrives meanwhile is
// joined and sent as one string when the previous send finishes.
//
// Other calls that must stay in order with the keystrokes (a formatting action
// chosen from the menu) go through the same queue with run(): they start only
// when everything queued before them has finished, and data queued after them
// waits for them. Without that, "text" followed by Cmd+B could reach Neovim as
// the format first and the text second, and the marks would land before the
// text instead of after it.
//
// No dependency on Wails or xterm.js, so it can be tested on its own.

export type SendFn = (data: string) => Promise<void>

export interface SerialSender {
  // Queues data. Returns at once; an empty string is ignored.
  send(data: string): void
  // Queues a call that runs after everything queued before it has finished and
  // before anything queued after it starts. Returns at once. If the call fails
  // the error goes to onError, like a failed send.
  run(call: () => Promise<void>): void
}

type Item = { kind: 'data'; data: string } | { kind: 'call'; call: () => Promise<void> }

// onError is called with each failed send. A failure never stops the queue:
// data queued behind it is still sent.
export function createSerialSender(
  send: SendFn,
  onError: (err: unknown) => void,
): SerialSender {
  const queue: Item[] = []
  let busy = false

  async function drain(): Promise<void> {
    busy = true
    try {
      for (let item = queue.shift(); item !== undefined; item = queue.shift()) {
        try {
          await (item.kind === 'data' ? send(item.data) : item.call())
        } catch (err) {
          onError(err)
        }
      }
    } finally {
      busy = false
    }
  }

  function start(): void {
    // When idle, drain() runs up to its first await synchronously, so the
    // first send happens in this very call.
    if (!busy) void drain()
  }

  return {
    send(data: string): void {
      if (data === '') return
      // Only data that follows data is joined; a call in between is a barrier.
      const last = queue[queue.length - 1]
      if (last?.kind === 'data') last.data += data
      else queue.push({ kind: 'data', data })
      start()
    },
    run(call: () => Promise<void>): void {
      queue.push({ kind: 'call', call })
      start()
    },
  }
}
