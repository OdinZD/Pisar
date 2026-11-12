<script lang="ts">
  import type { EditorController } from '../lib/editorController.svelte'
  import type { PreviewController } from '../lib/previewController.svelte'
  import { appOrigin, previewFrameDocument, previewPageId, previewSandbox } from '../lib/previewFrame'
  import { previewView } from '../lib/previewState'
  import { createPageWriter, type FrameDocument } from '../lib/previewWrite'
  import { strings } from '../strings'
  import { activeTheme } from '../theme/active'
  import { previewFontFaceSpecs } from '../theme/fonts'
  import { themes } from '../theme/tokens'

  let { preview, editor }: { preview: PreviewController; editor: EditorController } = $props()

  // Fonts are fetched by the frame itself, so it needs absolute addresses on the application's origin.
  const origin = appOrigin(window.location)
  const srcdoc = previewFrameDocument({
    origin,
    palette: themes[activeTheme()].colors,
    faces: previewFontFaceSpecs().map((f) => ({ ...f, url: new URL(f.url, window.location.href).href })),
  })

  const view = $derived(previewView(preview.content))

  let frame: HTMLIFrameElement | undefined = $state()
  // Counts the loads of the frame's document; the effect below runs again after each one.
  let loads = $state(0)
  const writer = createPageWriter()
  // Documents that already have the click handler; a document is only ever wired once.
  const wired = new WeakSet<Document>()

  // After a click in the frame the keyboard goes back to the terminal (rule: the terminal gets all
  // keys). The text stays selected in the frame, but copying it is not wired (see the report).
  function wire(doc: Document): void {
    if (wired.has(doc)) return
    wired.add(doc)
    doc.addEventListener('mouseup', () => editor.focusTerminal())
  }

  // The new content goes into the container inside the frame's document. The frame is never reloaded
  // (srcdoc is set once) and its scroller stays, so the scroll position survives a refresh.
  $effect(() => {
    void loads
    const html = view.kind === 'page' ? view.html : ''
    const doc = frame?.contentDocument
    if (!doc) return
    // Until the frame's own document has loaded there is no container (it is still about:blank);
    // the load event runs this effect again.
    if (writer.write(doc as FrameDocument, previewPageId, html)) wire(doc)
  })
</script>

<aside class="preview" aria-label={strings.previewLabel}>
  <!-- allow-same-origin only: no scripts, forms, popups or navigation. The page content is written by
       the effect above into the frame's own document; it never enters this component's markup. -->
  <iframe
    bind:this={frame}
    class:hidden={view.kind !== 'page'}
    title={strings.previewLabel}
    sandbox={previewSandbox}
    {srcdoc}
    tabindex="-1"
    onload={() => loads++}
  ></iframe>
  {#if view.kind === 'message'}
    <!-- A click on the bare desk must not take the keyboard from the terminal. -->
    <div class="message-area" role="presentation" onmousedown={(e) => e.preventDefault()}>
      <p class="message" class:danger={view.danger} role="status">{view.text}</p>
    </div>
  {/if}
</aside>

<style>
  .preview {
    position: relative;
    flex: none;
    width: var(--size-preview-width);
    background: var(--bg-desk);
    border-left: 1px solid var(--border);
  }
  iframe {
    display: block;
    width: 100%;
    height: 100%;
    border: 0;
  }
  /* Hidden, not removed: the loaded document stays, so the next page needs no reload. */
  iframe.hidden {
    visibility: hidden;
  }
  .message-area {
    position: absolute;
    inset: 0;
    padding: var(--space-xl);
  }
  .message {
    margin: 0;
    color: var(--text-muted);
  }
  .message.danger {
    color: var(--danger);
  }
</style>
