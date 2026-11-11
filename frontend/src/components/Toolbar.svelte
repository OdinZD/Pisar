<script lang="ts">
  import { strings } from '../strings'
  import type { TreeController } from '../lib/treeController.svelte'

  let { tree }: { tree: TreeController } = $props()
</script>

<header class="toolbar">
  <div class="where">
    <span class="title">{tree.toolbar.title}</span>
    {#if tree.toolbar.path}
      <span class="path">{tree.toolbar.path}</span>
    {/if}
  </div>
  <button type="button" disabled={!tree.canCreate} onclick={() => tree.startCreate()}>{strings.menuNewDocument}</button>
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
