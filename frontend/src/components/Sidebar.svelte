<script lang="ts">
  import { strings } from '../strings'
  import { isItemRow } from '../lib/tree'
  import type { TreeController } from '../lib/treeController.svelte'
  import TreeField from './TreeField.svelte'
  import TreeRow from './TreeRow.svelte'

  let { tree }: { tree: TreeController } = $props()

  let treeEl: HTMLElement | undefined = $state()

  $effect(() => {
    tree.focusTree = () => treeEl?.focus()
  })

  const rowId = (i: number) => `tree-row-${i}`
  const activeIndex = $derived(tree.displayRows.findIndex((r) => isItemRow(r) && r.key === tree.selectedKey))

  // One handler for all rows: a click selects the row (and toggles a project or folder).
  function onclick(e: MouseEvent) {
    const el = e.target instanceof Element ? e.target.closest('[data-index]') : null
    const row = el ? tree.displayRows[Number((el as HTMLElement).dataset.index)] : undefined
    if (row && isItemRow(row)) tree.clickRow(row)
  }

  function onkeydown(e: KeyboardEvent) {
    // Keys typed into a field of the tree belong to the field.
    if (e.target !== e.currentTarget) return
    if (tree.handleKey(e)) e.preventDefault()
  }
</script>

<aside class="sidebar">
  <h2 class="section-title">{strings.treeSectionTitle}</h2>

  <div
    bind:this={treeEl}
    class="tree"
    role="tree"
    tabindex="0"
    aria-label={strings.treeLabel}
    aria-activedescendant={activeIndex >= 0 ? rowId(activeIndex) : undefined}
    {onkeydown}
    {onclick}
  >
    {#each tree.displayRows as row, i (row.key)}
      {#if isItemRow(row)}
        <TreeRow {row} id={rowId(i)} index={i} selected={row.key === tree.selectedKey} />
      {:else if row.type === 'message'}
        <p class="message {row.tone}" role="none" style:--depth={row.depth}>{row.text}</p>
      {:else if row.type === 'input'}
        <TreeField
          initial={row.initial}
          depth={row.depth}
          label={row.mode === 'create' ? strings.newDocumentLabel : strings.renameDocumentLabel}
          onsubmit={(text) => (row.mode === 'create' ? tree.submitCreate(text) : tree.submitRename(text))}
          oncancel={(refocus) => tree.cancelEdit(refocus)}
          oninput={() => tree.clearEditError()}
        />
      {:else}
        <div class="confirm" role="none" style:--depth={row.depth}>
          <p>{strings.removeProjectQuestion}</p>
          <div class="buttons">
            <!-- Mouse presses must not pull focus out of the tree: Enter / Esc keep working. -->
            <button type="button" tabindex="-1" onmousedown={(e) => e.preventDefault()} onclick={() => tree.confirmRemove()}>
              {strings.removeProjectConfirm}
            </button>
            <button type="button" tabindex="-1" onmousedown={(e) => e.preventDefault()} onclick={() => tree.cancelRemove()}>
              {strings.cancel}
            </button>
          </div>
        </div>
      {/if}
    {/each}
  </div>

  {#if tree.projects.length === 0}
    <p class="empty">{strings.noProjects}</p>
  {/if}
  {#if tree.statusText}
    <p class="message error status" role="alert">{tree.statusText}</p>
  {/if}
  <button type="button" class="add" onclick={() => tree.addProject()}>{strings.addProject}</button>
</aside>

<style>
  .sidebar {
    display: flex;
    flex-direction: column;
    width: var(--size-tree-width);
    flex: none;
    min-height: 0;
    background: var(--bg-chrome);
    border-right: 1px solid var(--border);
  }
  .section-title {
    margin: 0;
    padding: var(--space-md) var(--space-md) var(--space-xs);
    font-size: var(--font-size-section-title);
    font-weight: var(--font-weight-semibold);
    letter-spacing: var(--font-letter-spacing-section-title);
    color: var(--text-muted);
  }
  .tree {
    flex: 0 1 auto;
    min-height: 0;
    overflow-y: auto;
  }
  /* Always visible while the tree has focus, also after a mouse click; inset so overflow does not clip it. */
  .tree:focus {
    outline: var(--size-focus-ring-width) solid var(--accent);
    outline-offset: calc(var(--size-focus-ring-width) * -1);
  }
  .message,
  .empty {
    margin: 0;
    padding: var(--space-xs) var(--space-md) var(--space-xs) calc(var(--space-sm) + var(--space-lg) + var(--space-xs) + var(--depth, 0) * var(--space-lg));
    color: var(--text-muted);
  }
  .message.error {
    color: var(--text);
  }
  .empty {
    padding-left: var(--space-md);
  }
  .status {
    padding-left: var(--space-md);
  }
  .confirm {
    padding: var(--space-xs) var(--space-md) var(--space-sm) calc(var(--space-sm) + var(--space-lg) + var(--space-xs) + var(--depth) * var(--space-lg));
  }
  .confirm p {
    margin: 0 0 var(--space-xs);
  }
  .buttons {
    display: flex;
    gap: var(--space-sm);
  }
  button {
    height: var(--size-button-height);
    padding: 0 var(--space-md);
    border: 1px solid var(--border-strong);
    border-radius: var(--size-radius);
    background: var(--bg-chrome);
    color: var(--text);
    font: inherit;
    cursor: default;
  }
  .add {
    margin: auto var(--space-sm) var(--space-sm);
    flex: none;
    border-color: transparent;
    text-align: left;
    color: var(--accent);
  }
  .tree + .empty,
  .tree ~ .add {
    flex: none;
  }
</style>
