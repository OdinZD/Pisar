// Writes the page content into the preview frame's own document, in place. The frame is never
// reloaded and its scrolling element is never replaced, so the scroll position stays; the container is
// filled in one synchronous step, so no half-empty page is ever painted.
//
// The only place where the HTML of a user's document touches a DOM, and it is the frame's: the node
// handed in here is the container inside the frame's document, never an element of the application.
// Written against small structural types so it can be tested without a DOM.

/** The images of a freshly written page; `complete` is false until the picture is decoded. */
export interface ImageLike {
  complete: boolean
  addEventListener(type: 'load' | 'error', listener: () => void): void
  removeEventListener(type: 'load' | 'error', listener: () => void): void
}

/** The container inside the frame (`#page`). */
export interface PageNode {
  innerHTML: string
  /** height with padding, as laid out now */
  offsetHeight: number
  style: { minHeight: string }
  querySelectorAll(selector: 'img'): ArrayLike<ImageLike>
}

export interface FrameDocument {
  getElementById(id: string): PageNode | null
  scrollingElement: { scrollTop: number } | null
}

export interface Timers {
  set(fn: () => void, ms: number): unknown
  clear(handle: unknown): void
}

/** How long a page keeps its old height while pictures decode (they are data: URIs, so it is short). */
export const imageSettleMs = 1000

const realTimers: Timers = {
  set: (fn, ms) => setTimeout(fn, ms),
  clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
}

export interface PageWriter {
  /**
   * Puts `html` into the container of `doc`. Returns false when the container is not there yet (the
   * frame has not loaded its document); the caller tries again after `load`. The same html into the
   * same document is not written twice.
   */
  write(doc: FrameDocument, pageId: string, html: string): boolean
}

export function createPageWriter(timers: Timers = realTimers): PageWriter {
  let lastDoc: FrameDocument | null = null
  let lastHtml: string | null = null
  let release: (() => void) | null = null

  return {
    write(doc, pageId, html) {
      const page = doc.getElementById(pageId)
      if (page === null) return false
      if (doc === lastDoc && html === lastHtml) return true

      release?.() // a hold from the previous write ends here, so its height is not mistaken for the page's
      const scroller = doc.scrollingElement
      const top = scroller?.scrollTop ?? 0
      // A new <img> has no height until its picture is decoded. Without this the page would be shorter
      // for a moment, and the browser would clamp the scroll position to the shorter page.
      page.style.minHeight = `${page.offsetHeight}px`
      page.innerHTML = html
      if (scroller && scroller.scrollTop !== top) scroller.scrollTop = top

      lastDoc = doc
      lastHtml = html
      const pending = Array.from(page.querySelectorAll('img')).filter((img) => !img.complete)
      const listeners: Array<{ img: ImageLike; fn: () => void }> = []
      let handle: unknown
      let done = false
      const finish = () => {
        if (done) return
        done = true
        if (handle !== undefined) timers.clear(handle)
        for (const { img, fn } of listeners) {
          img.removeEventListener('load', fn)
          img.removeEventListener('error', fn)
        }
        release = null
        page.style.minHeight = ''
      }
      release = finish
      if (pending.length === 0) {
        finish() // nothing to wait for
        return true
      }
      let left = pending.length
      for (const img of pending) {
        const fn = () => {
          img.removeEventListener('load', fn)
          img.removeEventListener('error', fn)
          if (--left === 0) finish()
        }
        listeners.push({ img, fn })
        img.addEventListener('load', fn)
        img.addEventListener('error', fn)
      }
      handle = timers.set(finish, imageSettleMs)
      return true
    },
  }
}
