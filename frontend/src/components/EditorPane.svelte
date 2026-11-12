<script lang="ts">
  import type { EditorController } from '../lib/editorController.svelte'
  import { errorMessage } from '../lib/errorMessage'
  import { noTerminalFocus } from '../lib/focus'
  import type { SearchController } from '../lib/searchController.svelte'
  import { toolsNotice } from '../lib/toolsState'
  import { strings } from '../strings'
  import EmptyState from './EmptyState.svelte'
  import SearchPanel from './SearchPanel.svelte'
  import Terminal from './Terminal.svelte'

  let { editor, search }: { editor: EditorController; search: SearchController } = $props()

  let terminal: ReturnType<typeof Terminal> | undefined = $state()

  // How far above the bottom the status line of Neovim starts, as the terminal last reported it. Only
  // meaningful while the terminal is in the page; without Neovim the search panel sits at the bottom.
  // Missing tools do not block anything: the terminal replaces this notice as soon as Neovim runs.
  const notice = $derived(toolsNotice(editor.missingTools))
  const hasTerminal = $derived(editor.phase === 'starting' || editor.phase === 'running')
  let reportedInset = $state(0)
  const searchInset = $derived(hasTerminal ? reportedInset : 0)
  $effect(() => {
    if (!hasTerminal) reportedInset = 0
  })

  $effect(() => {
    editor.focusTerminal = () => terminal?.focus()
    editor.terminalFocus = () => terminal?.focusState() ?? noTerminalFocus
    editor.runInOrder = (call) => terminal?.runInOrder(call) ?? false
  })
</script>

<main class="pane" aria-label={strings.editorLabel}>
  {#if hasTerminal}
    <Terminal
      bind:this={terminal}
      onstarted={() => editor.terminalStarted()}
      onfailed={(err) => editor.terminalFailed(err)}
      onlayout={(px) => (reportedInset = px)}
      keepsFocus={() => search.open}
    />
  {:else if editor.phase === 'failed'}
    <EmptyState error={errorMessage(editor.error ?? 'internal')} />
  {:else}
    <EmptyState tools={notice} checking={editor.checkingTools} oncheck={() => void editor.checkTools()} />
  {/if}
  {#if search.open}
    <SearchPanel {search} inset={searchInset} />
  {/if}
</main>

<style>
  .pane {
    display: flex;
    flex: 1;
    position: relative;
    /* The preview takes its fixed width first; the editor keeps at least this much (the window is
       never narrower than tree + preview + this while the preview is open). */
    min-width: var(--size-editor-min-width);
    background: var(--bg-editor);
  }
</style>
