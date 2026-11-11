<script lang="ts">
  import type { ItemRow } from '../lib/tree'

  let { row, id, index, selected }: { row: ItemRow; id: string; index: number; selected: boolean } = $props()

  let el: HTMLElement | undefined = $state()

  // The tree scrolls, so keyboard movement must bring the selected row into view.
  $effect(() => {
    if (selected) el?.scrollIntoView({ block: 'nearest' })
  })
</script>

<!-- Focus stays on the tree element (aria-activedescendant); the tree delegates clicks via data-index. -->
<div
  bind:this={el}
  {id}
  class="row {row.type}"
  class:selected
  class:missing={row.type === 'project' && row.missing}
  style:--depth={row.depth}
  role="treeitem"
  data-index={index}
  aria-selected={selected}
  aria-level={row.depth + 1}
  aria-expanded={row.type === 'doc' ? undefined : row.expanded}
>
  <span class="caret" aria-hidden="true">{row.type === 'doc' ? '' : row.expanded ? '▾' : '▸'}</span>
  <span class="name">{row.type === 'doc' ? row.title : row.name}</span>
  {#if row.type === 'project' && row.note}
    <span class="note">{row.note}</span>
  {/if}
  {#if row.type === 'doc'}
    <span class="kind">{row.kindLabel}</span>
  {/if}
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-xs);
    height: var(--size-row-height);
    padding: 0 var(--space-md) 0 calc(var(--space-sm) + var(--depth) * var(--space-lg));
    color: var(--text);
    cursor: default;
    user-select: none;
  }
  .caret {
    flex: none;
    width: var(--space-lg);
    text-align: center;
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .project {
    font-weight: var(--font-weight-semibold);
  }
  .dir {
    color: var(--text-muted);
  }
  .missing {
    color: var(--text-muted);
  }
  .note,
  .kind {
    flex: none;
    color: var(--text-muted);
    font-size: var(--font-size-doc-kind);
  }
  .kind {
    font-family: var(--font-mono);
  }
  .note {
    font-weight: var(--font-weight-regular);
  }
  .selected {
    background: var(--bg-selected);
  }
  .doc.selected {
    background: var(--accent);
    color: var(--accent-text);
  }
  .doc.selected .kind {
    color: var(--accent-text);
  }
</style>
