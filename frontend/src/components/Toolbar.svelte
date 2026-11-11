<script lang="ts">
  import { strings } from '../strings'
  import { documentStatus, toolbarInfo } from '../lib/activeDocument'
  import type { EditorController } from '../lib/editorController.svelte'
  import type { TreeController } from '../lib/treeController.svelte'

  let { tree, editor }: { tree: TreeController; editor: EditorController } = $props()

  const conversion = $derived({ exports: editor.exports, opening: editor.opening, copyError: editor.copyError })
  const info = $derived(toolbarInfo(tree.projects, editor.active, editor.modified, strings.appName, conversion))
  const status = $derived(documentStatus(editor.active, editor.modified, conversion))
</script>

<header class="toolbar">
  <div class="where">
    <span class="title" title={info.title}>{info.title}</span>
    {#if info.path}
      <span class="path" title={info.path}>{info.path}</span>
    {/if}
  </div>
  <div class="actions">
    <!-- Always in the page, so a screen reader announces the text when it changes. Long text is cut
         with an ellipsis on screen only; the full text is in the DOM and in the tooltip. -->
    <div class="status" class:filled={status !== null} aria-live="polite">
      {#if status}
        <span class="state {status.tone}" title={status.text}>{status.text}</span>
        {#if status.note}
          <span class="note" title={status.note}>{status.note}</span>
        {/if}
      {/if}
    </div>
    {#if status?.saveCopy}
      <button type="button" disabled={editor.savingCopy} onclick={() => void editor.saveCopy()}>{strings.saveCopy}</button>
    {/if}
    <button type="button" disabled={!tree.canCreate} onclick={() => tree.startCreate()}>{strings.menuNewDocument}</button>
  </div>
</header>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-lg);
    flex: none;
    height: var(--size-toolbar-height);
    padding: 0 var(--space-lg);
    background: var(--bg-chrome);
    border-bottom: 1px solid var(--border);
  }
  .where {
    display: flex;
    align-items: baseline;
    gap: var(--space-md);
    flex: 1 1 0;
    min-width: 0;
  }
  .title {
    overflow: hidden;
    font-weight: var(--font-weight-semibold);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .path {
    overflow: hidden;
    color: var(--text-muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* The buttons keep their size; the status gives way (ellipsis) so the toolbar never grows. */
  .actions {
    display: flex;
    align-items: center;
    flex: 0 1 auto;
    min-width: 0;
    max-width: 65%;
  }
  .status {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    flex: 0 1 auto;
    min-width: 0;
  }
  .status.filled {
    margin-right: var(--space-md);
  }
  button + button {
    margin-left: var(--space-md);
  }
  .state,
  .note {
    max-width: 100%;
    overflow: hidden;
    color: var(--text-muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Differs by weight, not by colour alone. */
  .state.strong {
    color: var(--text);
    font-weight: var(--font-weight-semibold);
  }
  .state.danger {
    color: var(--danger);
    font-weight: var(--font-weight-semibold);
  }
  button {
    flex: none;
    height: var(--size-button-height);
    padding: 0 var(--space-md);
    border: 1px solid var(--border-strong);
    border-radius: var(--size-radius);
    background: var(--bg-chrome);
    color: var(--text);
    font: inherit;
  }
  button:disabled {
    color: var(--text-muted);
    opacity: 0.6;
  }
</style>
