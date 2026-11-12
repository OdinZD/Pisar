<script lang="ts">
  import type { SearchController } from '../lib/searchController.svelte'
  import { keyToSearchAction } from '../lib/searchKeys'
  import { strings } from '../strings'
  import { search as searchTokens } from '../theme/tokens'

  // `inset` is how far above the bottom of the editor column the panel ends (the status line and the
  // command line of Neovim stay visible under it).
  let { search, inset }: { search: SearchController; inset: number } = $props()

  let panel: HTMLElement | undefined = $state()
  let input: HTMLInputElement | undefined = $state()
  let list: HTMLElement | undefined = $state()

  const view = $derived(search.view)
  const rowId = (i: number) => `search-row-${i}`
  const listId = 'search-list'

  // The field takes the keyboard when the panel appears, wherever it was (terminal, tree, a button).
  function takeFocus(node: HTMLInputElement) {
    node.focus()
  }

  $effect(() => {
    search.focusInput = () => input?.focus()
    return () => {
      search.focusInput = () => {}
    }
  })

  // A click anywhere outside the panel closes it. Capturing, so it also sees clicks that other
  // handlers stop. The keyboard is not pushed back to the terminal: the click puts it where it belongs
  // (a click in the terminal focuses it, a click in the tree focuses the tree).
  $effect(() => {
    const onMouseDown = (e: MouseEvent) => {
      if (panel && e.target instanceof Node && !panel.contains(e.target)) search.dismiss(false)
    }
    document.addEventListener('mousedown', onMouseDown, true)
    return () => document.removeEventListener('mousedown', onMouseDown, true)
  })

  // The selected row is always in view, also when the selection jumps back to the top after new hits.
  $effect(() => {
    void view.rows
    const row = view.selected >= 0 ? list?.children[view.selected] : undefined
    row?.scrollIntoView({ block: 'nearest' })
  })

  function onkeydown(e: KeyboardEvent) {
    const action = keyToSearchAction(e)
    if (!action) return
    e.preventDefault() // also keeps Ctrl+P from moving the caret (macOS text fields treat it as "up")
    search.handleAction(action)
  }

  function oninput(e: Event & { currentTarget: HTMLInputElement }) {
    search.setQuery(e.currentTarget.value)
  }

  // Tab (or anything else that moves the keyboard out of the panel) closes it: with the panel open the
  // terminal must not receive keys, and a field without the focus would leave it showing for nothing.
  function onfocusout(e: FocusEvent) {
    if (panel && e.relatedTarget instanceof Node && !panel.contains(e.relatedTarget)) search.dismiss(false)
  }

  // Presses on the panel itself must not take the keyboard out of the field; the field keeps its own
  // default (caret, selection).
  function onmousedown(e: MouseEvent) {
    if (e.target !== input) e.preventDefault()
  }

  function onclick(e: MouseEvent) {
    const el = e.target instanceof Element ? e.target.closest<HTMLElement>('[data-index]') : null
    if (el) search.clickRow(Number(el.dataset.index))
  }
</script>

<!-- The wrapper only routes pointer and focus events (it is not interactive itself): mouse presses that
     keep the keyboard in the field, clicks on rows, focus leaving the panel. -->
<div
  bind:this={panel}
  class="panel"
  role="presentation"
  style:--search-inset="{inset}px"
  style:--search-rows={searchTokens.visibleRows}
  {onmousedown}
  {onclick}
  {onfocusout}
>
<section class="search" role="search" aria-label={strings.searchLabel}>
  <div class="head">
    <span class="label">{strings.searchLabel}</span>
    <input
      bind:this={input}
      type="text"
      role="combobox"
      aria-label={strings.searchInputLabel}
      aria-autocomplete="list"
      aria-expanded={view.rows.length > 0}
      aria-controls={view.rows.length > 0 ? listId : undefined}
      aria-activedescendant={view.selected >= 0 ? rowId(view.selected) : undefined}
      autocomplete="off"
      spellcheck="false"
      value={search.query}
      {@attach takeFocus}
      {onkeydown}
      {oninput}
    />
    <span class="scope">{view.scopeLabel}</span>
  </div>

  {#if view.rows.length > 0}
    <!-- Focus stays in the field (aria-activedescendant); a click on a row opens it (handled by the wrapper). -->
    <div bind:this={list} id={listId} class="list" role="listbox" aria-label={strings.searchLabel}>
      {#each view.rows as row, i (row.key)}
        <div id={rowId(i)} class="row" class:selected={i === view.selected} role="option" aria-selected={i === view.selected} data-index={i}>
          <span class="name">{#each row.segments as seg, s (s)}{#if seg.hit}<span class="hit">{seg.text}</span>{:else}{seg.text}{/if}{/each}</span>
          <span class="project">{row.projectName}</span>
        </div>
      {/each}
    </div>
  {/if}

  {#if view.message}
    <p class="message" class:danger={view.message.danger} role={view.message.danger ? 'alert' : undefined}>{view.message.text}</p>
  {/if}

  <!-- The number of hits for screen readers; the sighted see the list. -->
  <div class="live" aria-live="polite">{view.announce}</div>

  <div class="hints">
    <span><span class="key">↵</span> {strings.searchHintOpen}</span>
    <span aria-hidden="true">·</span>
    <span><span class="key">Esc</span> {strings.searchHintClose}</span>
    <span aria-hidden="true">·</span>
    <span><span class="key">⌃P</span> {strings.searchHintScope}</span>
  </div>
</section>
</div>

<style>
  .search {
    display: flex;
    flex-direction: column;
  }
  .panel {
    position: absolute;
    left: 0;
    right: 0;
    bottom: var(--search-inset, 0px);
    background: var(--bg-app);
    border-top: 1px solid var(--border);
    color: var(--text);
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--space-md);
    padding: var(--space-sm) var(--space-md);
  }
  .label {
    flex: none;
    font-size: var(--font-size-section-title);
    font-weight: var(--font-weight-semibold);
    letter-spacing: var(--font-letter-spacing-section-title);
    color: var(--text-muted);
  }
  input {
    flex: 1;
    min-width: 0;
    height: calc(var(--size-row-height) - var(--space-xs));
    padding: 0 var(--space-sm);
    border: 1px solid var(--border-strong);
    border-radius: var(--size-radius);
    background: var(--bg-editor);
    color: var(--text);
    font: inherit;
  }
  .scope {
    flex: none;
    color: var(--text-muted);
  }
  .list {
    max-height: calc(var(--size-row-height) * var(--search-rows));
    overflow-y: auto;
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-md);
    height: var(--size-row-height);
    padding: 0 var(--space-md);
    cursor: default;
    user-select: none;
  }
  .row.selected {
    background: var(--bg-selected);
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hit {
    color: var(--accent);
    font-weight: var(--font-weight-semibold);
  }
  .project {
    flex: none;
    max-width: 40%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-muted);
  }
  .message {
    margin: 0;
    padding: var(--space-xs) var(--space-md);
    color: var(--text-muted);
  }
  .message.danger {
    color: var(--danger);
  }
  .live {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
  .hints {
    display: flex;
    gap: var(--space-sm);
    padding: var(--space-xs) var(--space-md) var(--space-sm);
    font-size: var(--font-size-section-title);
    color: var(--text-muted);
  }
  .key {
    font-family: var(--font-mono);
  }
</style>
