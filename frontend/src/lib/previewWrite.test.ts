import { describe, expect, it } from 'vitest'
import { createPageWriter, imageSettleMs, type FrameDocument, type ImageLike, type PageNode, type Timers } from './previewWrite'

class FakeImage implements ImageLike {
  complete = false
  #on = new Map<string, Set<() => void>>()
  addEventListener(type: string, fn: () => void): void {
    if (!this.#on.has(type)) this.#on.set(type, new Set())
    this.#on.get(type)?.add(fn)
  }
  removeEventListener(type: string, fn: () => void): void {
    this.#on.get(type)?.delete(fn)
  }
  fire(type: 'load' | 'error'): void {
    this.complete = true
    for (const fn of [...(this.#on.get(type) ?? [])]) fn()
  }
  listeners(): number {
    return [...this.#on.values()].reduce((n, s) => n + s.size, 0)
  }
}

class FakePage implements PageNode {
  html = ''
  offsetHeight = 500
  style = { minHeight: '' }
  /** what the next assignment to innerHTML will contain as images */
  nextImages = 0
  images: FakeImage[] = []
  /** the scroller clamps to this when the page content is replaced (a stand-in for a shorter layout) */
  onWrite: () => void = () => {}
  /** every value minHeight had, in order */
  heights: string[] = []
  get innerHTML(): string {
    return this.html
  }
  set innerHTML(v: string) {
    this.heights.push(this.style.minHeight)
    this.html = v
    this.images = Array.from({ length: this.nextImages }, () => new FakeImage())
    this.onWrite()
  }
  querySelectorAll(): ArrayLike<ImageLike> {
    return this.images
  }
}

class FakeDoc implements FrameDocument {
  page: FakePage | null = new FakePage()
  scrollingElement = { scrollTop: 0 }
  getElementById(id: string): PageNode | null {
    return id === 'page' ? this.page : null
  }
}

class FakeTimers implements Timers {
  pending = new Map<number, { fn: () => void; ms: number }>()
  #next = 1
  set(fn: () => void, ms: number): unknown {
    const id = this.#next++
    this.pending.set(id, { fn, ms })
    return id
  }
  clear(handle: unknown): void {
    this.pending.delete(handle as number)
  }
  runAll(): void {
    for (const [id, t] of [...this.pending]) {
      this.pending.delete(id)
      t.fn()
    }
  }
}

describe('createPageWriter', () => {
  it('reports that the container is not there yet', () => {
    const doc = new FakeDoc()
    doc.page = null
    expect(createPageWriter().write(doc, 'page', '<p>x</p>')).toBe(false)
  })

  it('writes the html into the container', () => {
    const doc = new FakeDoc()
    expect(createPageWriter().write(doc, 'page', '<p>x</p>')).toBe(true)
    expect(doc.page?.html).toBe('<p>x</p>')
  })

  it('keeps the scroll position when the content is replaced', () => {
    const doc = new FakeDoc()
    const w = createPageWriter()
    w.write(doc, 'page', '<p>a</p>')
    doc.scrollingElement.scrollTop = 321
    // The browser jumps to the top when the container is emptied; the writer puts it back.
    if (doc.page) doc.page.onWrite = () => (doc.scrollingElement.scrollTop = 0)
    w.write(doc, 'page', '<p>b</p>')
    expect(doc.page?.html).toBe('<p>b</p>')
    expect(doc.scrollingElement.scrollTop).toBe(321)
  })

  it('does not write the same html into the same document twice', () => {
    const doc = new FakeDoc()
    const w = createPageWriter()
    w.write(doc, 'page', '<p>a</p>')
    const writes = doc.page?.heights.length
    expect(w.write(doc, 'page', '<p>a</p>')).toBe(true)
    expect(doc.page?.heights.length).toBe(writes)
  })

  it('writes again into a new document (the frame was loaded again), even for the same html', () => {
    const w = createPageWriter()
    const a = new FakeDoc()
    const b = new FakeDoc()
    w.write(a, 'page', '<p>a</p>')
    w.write(b, 'page', '<p>a</p>')
    expect(b.page?.html).toBe('<p>a</p>')
  })

  it('writes the empty string (clears the page)', () => {
    const doc = new FakeDoc()
    const w = createPageWriter()
    w.write(doc, 'page', '<p>a</p>')
    w.write(doc, 'page', '')
    expect(doc.page?.html).toBe('')
  })

  it('holds the old height during the write and releases it at once when there are no pictures', () => {
    const doc = new FakeDoc()
    createPageWriter().write(doc, 'page', '<p>a</p>')
    expect(doc.page?.heights).toEqual(['500px'])
    expect(doc.page?.style.minHeight).toBe('')
  })

  it('holds the height until every picture has loaded or failed', () => {
    const doc = new FakeDoc()
    const timers = new FakeTimers()
    const w = createPageWriter(timers)
    if (doc.page) doc.page.nextImages = 2
    w.write(doc, 'page', '<img><img>')
    const [a, b] = doc.page?.images ?? []
    expect(doc.page?.style.minHeight).toBe('500px')
    a.fire('load')
    expect(doc.page?.style.minHeight).toBe('500px')
    b.fire('error')
    expect(doc.page?.style.minHeight).toBe('')
    expect(a.listeners() + b.listeners()).toBe(0)
    expect(timers.pending.size).toBe(0)
  })

  it('lets go after a timeout when a picture never answers', () => {
    const doc = new FakeDoc()
    const timers = new FakeTimers()
    if (doc.page) doc.page.nextImages = 1
    createPageWriter(timers).write(doc, 'page', '<img>')
    expect([...timers.pending.values()].map((t) => t.ms)).toEqual([imageSettleMs])
    timers.runAll()
    expect(doc.page?.style.minHeight).toBe('')
  })

  it('a newer write ends the hold of the older one', () => {
    const doc = new FakeDoc()
    const timers = new FakeTimers()
    const w = createPageWriter(timers)
    if (doc.page) doc.page.nextImages = 1
    w.write(doc, 'page', '<img>')
    const old = doc.page?.images[0]
    if (doc.page) doc.page.nextImages = 0
    w.write(doc, 'page', '<p>b</p>')
    expect(old?.listeners()).toBe(0)
    expect(timers.pending.size).toBe(0)
    expect(doc.page?.style.minHeight).toBe('')
  })
})
