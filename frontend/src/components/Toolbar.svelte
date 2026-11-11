<script lang="ts">
  import { strings } from '../strings'
  import { toolbarInfo } from '../lib/activeDocument'
  import type { EditorController } from '../lib/editorController.svelte'
  import type { TreeController } from '../lib/treeController.svelte'

  let { tree, editor }: { tree: TreeController; editor: EditorController } = $props()

  const info = $derived(toolbarInfo(tree.projects, editor.active, editor.modified, strings.appName))
</script>

<header class="toolbar">
  <div class="where">
    <span class="title">{info.title}</span>
    {#if info.path}
      <span class="path">{info.path}</span>
    {/if}
  </div>
  <div class="actions">
    <!-- Always in the page, so a screen reader announces the text when it changes. -->
    <span class="state" class:modified={info.modified} aria-live="polite">{info.stateText ?? ''}</span>
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
    min-width: 0;
  }
  .title {
    font-weight: var(--font-weight-semibold);
    white-space: nowrap;
  }
  .path {
    overflow: hidden;
    color: var(--text-muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .actions {
    display: flex;
    align-items: center;
    flex: none;
  }
  .state {
    color: var(--text-muted);
    white-space: nowrap;
  }
  .state:not(:empty) {
    margin-right: var(--space-md);
  }
  /* Differs by weight and colour, not by the word alone. */
  .state.modified {
    color: var(--text);
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
