import { describe, expect, it } from 'vitest'
import { strings } from '../strings'
import { sizes } from '../theme/tokens'
import { errorMessage } from './errorMessage'
import {
  callFailed,
  contentArrived,
  initialPreviewState,
  isNarrow,
  nvimExited,
  previewView,
  shownContent,
  toggled,
  widthChanged,
  type PreviewContentEvent,
  type PreviewState,
} from './previewState'

const ev = (over: Partial<PreviewContentEvent> = {}): PreviewContentEvent => ({ path: '/p/a.md', state: 'html', html: '<p>x</p>', code: '', ...over })
const open: PreviewState = { ...initialPreviewState, open: true }
const limit = sizes.previewCollapseWidth

describe('limits', () => {
  it('the collapse width is tree + preview + the editor minimum', () => {
    expect(sizes.treeWidth + sizes.previewWidth + sizes.editorMinWidth).toBe(sizes.previewCollapseWidth)
    expect(limit).toBe(1100)
  })
})

describe('toggled', () => {
  it('opens a closed pane and closes an open one', () => {
    expect(toggled(initialPreviewState).open).toBe(true)
    expect(toggled(open).open).toBe(false)
  })

  it('does not open in a narrow window', () => {
    const narrow = { ...initialPreviewState, narrow: true }
    expect(toggled(narrow)).toBe(narrow)
  })

  it('drops the content when it closes, so the next opening never shows an old document', () => {
    const shown = contentArrived(open, ev())
    expect(shown.content).not.toBeNull()
    expect(toggled(shown).content).toBeNull()
  })

  it('opening twice and closing twice ends where it started', () => {
    expect(toggled(toggled(initialPreviewState))).toEqual(initialPreviewState)
  })
})

describe('contentArrived', () => {
  it('is ignored while the pane is closed (a late event after SetPreview(false))', () => {
    expect(contentArrived(initialPreviewState, ev())).toBe(initialPreviewState)
  })

  it('goes html -> unsupported -> error and back', () => {
    let s = contentArrived(open, ev())
    expect(s.content).toEqual({ state: 'html', path: '/p/a.md', html: '<p>x</p>' })
    s = contentArrived(s, ev({ path: '/p/a.txt', state: 'unsupported', html: '' }))
    expect(s.content).toEqual({ state: 'unsupported', path: '/p/a.txt' })
    s = contentArrived(s, ev({ state: 'error', html: '', code: 'pandoc-missing' }))
    expect(s.content).toEqual({ state: 'error', path: '/p/a.md', code: 'pandoc-missing' })
    s = contentArrived(s, ev({ html: '<p>y</p>' }))
    expect(s.content).toEqual({ state: 'html', path: '/p/a.md', html: '<p>y</p>' })
  })

  it('keeps the pane open', () => {
    expect(contentArrived(open, ev()).open).toBe(true)
  })
})

describe('shownContent', () => {
  it('maps an unknown error code to internal', () => {
    expect(shownContent(ev({ state: 'error', code: 'from-the-future' }))).toEqual({ state: 'error', path: '/p/a.md', code: 'internal' })
  })

  it('shows a state this version does not know as an internal error', () => {
    expect(shownContent(ev({ state: 'pdf' }))).toEqual({ state: 'error', path: '/p/a.md', code: 'internal' })
  })

  it('knows the codes the service sends', () => {
    for (const code of ['preview-too-large', 'pandoc-missing', 'convert-failed', 'canceled', 'internal']) {
      expect(shownContent(ev({ state: 'error', code }))).toMatchObject({ code })
    }
  })
})

describe('callFailed', () => {
  it('shows the error of a failed SetPreview while the pane is open', () => {
    expect(callFailed(open, 'not-running').content).toEqual({ state: 'error', path: '', code: 'not-running' })
  })
  it('ignores it while the pane is closed', () => {
    expect(callFailed(initialPreviewState, 'internal')).toBe(initialPreviewState)
  })
})

describe('nvimExited', () => {
  it('clears the content and leaves the pane open', () => {
    const s = nvimExited(contentArrived(open, ev()))
    expect(s.content).toBeNull()
    expect(s.open).toBe(true)
  })
  it('changes nothing when there is no content', () => {
    expect(nvimExited(open)).toBe(open)
  })
})

describe('widthChanged', () => {
  it('the limit is exact: 1100 is wide enough, 1099 is not', () => {
    expect(isNarrow(limit)).toBe(false)
    expect(isNarrow(limit - 1)).toBe(true)
    expect(widthChanged(open, limit).open).toBe(true)
    expect(widthChanged(open, limit - 1).open).toBe(false)
  })

  it('narrowing closes the pane and drops its content', () => {
    const s = widthChanged(contentArrived(open, ev()), limit - 1)
    expect(s).toEqual({ open: false, narrow: true, content: null })
  })

  it('widening again does not open it', () => {
    const narrow = widthChanged(open, 800)
    const wide = widthChanged(narrow, 1600)
    expect(wide).toEqual({ open: false, narrow: false, content: null })
  })

  it('a pane that was closed by the user stays closed when the window changes', () => {
    expect(widthChanged(initialPreviewState, 1600)).toBe(initialPreviewState)
    expect(widthChanged(initialPreviewState, 900).narrow).toBe(true)
  })

  it('does not rebuild the state while it is still narrow', () => {
    const narrow = widthChanged(initialPreviewState, 900)
    expect(widthChanged(narrow, 800)).toBe(narrow)
  })

  it('the pane can be opened again after the window is wide enough', () => {
    const s = toggled(widthChanged(widthChanged(open, 900), 1300))
    expect(s.open).toBe(true)
  })
})

describe('previewView', () => {
  it('is empty without content', () => {
    expect(previewView(null)).toEqual({ kind: 'empty' })
  })
  it('shows the html as a page', () => {
    expect(previewView({ state: 'html', path: '', html: '<p>x</p>' })).toEqual({ kind: 'page', html: '<p>x</p>' })
  })
  it('says what .txt and unnamed buffers get', () => {
    expect(previewView({ state: 'unsupported', path: '' })).toEqual({ kind: 'message', text: strings.previewUnsupported, danger: false })
    expect(strings.previewUnsupported).toBe('Pregled je dostupan za Markdown i Wordove dokumente.')
  })
  it('shows an error by its code, as danger', () => {
    expect(previewView({ state: 'error', path: '', code: 'preview-too-large' })).toEqual({
      kind: 'message',
      text: errorMessage('preview-too-large'),
      danger: true,
    })
  })
})
