<script lang="ts">
  import type { ShortcutsController } from '../lib/shortcutsController.svelte'
  import { keyToShortcutsAction } from '../lib/shortcutsKeys'
  import { strings } from '../strings'
  import { sizes } from '../theme/tokens'

  let { shortcuts }: { shortcuts: ShortcutsController } = $props()

  let panel: HTMLElement | undefined = $state()
  let list: HTMLElement | undefined = $state()

  // The list takes the keyboard when the panel appears, wherever it was (terminal, tree, search field).
  function takeFocus(node: HTMLElement) {
    node.focus()
  }

  $effect(() => {
    shortcuts.focusPanel = () => list?.focus()
    shortcuts.scrollPanel = (delta) => list?.scrollBy({ top: delta * sizes.rowHeight })
    return () => {
      shortcuts.focusPanel = () => {}
      shortcuts.scrollPanel = () => {}
    }
  })

  // A click anywhere outside the panel closes it. Capturing, so it also sees clicks that other
  // handlers stop. The keyboard is not pushed back to the terminal: the click puts it where it belongs.
  $effect(() => {
    const onMouseDown = (e: MouseEvent) => {
      if (panel && e.target instanceof Node && !panel.contains(e.target)) shortcuts.dismiss(false)
    }
    document.addEventListener('mousedown', onMouseDown, true)
    return () => document.removeEventListener('mousedown', onMouseDown, true)
  })

  function onkeydown(e: KeyboardEvent) {
    const action = keyToShortcutsAction(e)
    if (!action) return
    e.preventDefault()
    shortcuts.handleAction(action)
  }

  // Anything that moves the keyboard out of the panel (a click elsewhere is handled above, this is Tab
  // or a script) closes it: with the panel open the terminal must not receive keys.
  function onfocusout(e: FocusEvent) {
    if (panel && e.relatedTarget instanceof Node && !panel.contains(e.relatedTarget)) shortcuts.dismiss(false)
  }

  // A press on the title line must not take the keyboard off the list (Esc would then go nowhere).
  function onmousedown(e: MouseEvent) {
    if (!(e.target instanceof Node && list?.contains(e.target))) e.preventDefault()
  }
</script>

<!-- The wrapper only routes key, pointer and focus events; the list inside it is what has the keyboard. -->
<div bind:this={panel} class="panel" role="dialog" aria-label={strings.shortcutsTitle} tabindex="-1" {onkeydown} {onmousedown} {onfocusout}>
  <div class="head">
    <h2 class="title">{strings.shortcutsTitle}</h2>
    <span class="hint"><span class="key">Esc</span> {strings.shortcutsHintClose}</span>
  </div>

  <!-- tabindex -1: a focusable container is how the keyboard scrolls it; it needs no ring of its own, the
       panel is the focused thing. -->
  <div bind:this={list} class="sections" tabindex="-1" {@attach takeFocus}>
    {#each shortcuts.sections as section (section.id)}
      <section class="section" aria-labelledby="shortcuts-{section.id}">
        <h3 id="shortcuts-{section.id}" class="section-title">{section.title}</h3>
        {#if section.rows.length > 0}
          <table>
            <tbody>
              {#each section.rows as row, i (i)}
                <tr>
                  <td class="keys">{row.keys}</td>
                  <td class="label">
                    {row.label}
                    {#if row.group}<span class="muted">{row.group}</span>{/if}
                    {#if row.modes.length > 0}<span class="muted">{row.modes.join(', ')}</span>{/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        {#if section.note}
          <p class="note">{section.note}</p>
        {/if}
      </section>
    {/each}
  </div>
</div>

<style>
  .panel {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-app);
    color: var(--text);
  }
  .panel:focus {
    outline: none;
  }
  .head {
    flex: none;
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-md);
    padding: var(--space-md) var(--space-lg);
    border-bottom: 1px solid var(--border);
  }
  .title {
    margin: 0;
    font-size: inherit;
    font-weight: var(--font-weight-semibold);
  }
  .hint {
    color: var(--text-muted);
    font-size: var(--font-size-section-title);
  }
  .key {
    font-family: var(--font-mono);
  }
  .sections {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-sm) var(--space-lg) var(--space-lg);
  }
  .sections:focus {
    outline: none;
  }
  .section-title {
    margin: var(--space-lg) 0 var(--space-xs);
    font-size: var(--font-size-section-title);
    font-weight: var(--font-weight-semibold);
    letter-spacing: var(--font-letter-spacing-section-title);
    color: var(--text-muted);
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  td {
    min-height: var(--size-row-height);
    height: var(--size-row-height);
    padding: 0;
    vertical-align: middle;
  }
  .keys {
    /* The keys column takes what its widest entry needs; the label gets the rest. */
    width: 1%;
    padding-right: var(--space-lg);
    white-space: nowrap;
    font-family: var(--font-mono);
  }
  .muted {
    margin-left: var(--space-sm);
    color: var(--text-muted);
  }
  .note {
    margin: 0;
    padding: var(--space-xs) 0;
    color: var(--text-muted);
  }
</style>
