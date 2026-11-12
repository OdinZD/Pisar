<script lang="ts">
  import type { EditorController } from '../lib/editorController.svelte'
  import { errorMessage } from '../lib/errorMessage'
  import { noTerminalFocus } from '../lib/focus'
  import { strings } from '../strings'
  import EmptyState from './EmptyState.svelte'
  import Terminal from './Terminal.svelte'

  let { editor }: { editor: EditorController } = $props()

  let terminal: ReturnType<typeof Terminal> | undefined = $state()

  $effect(() => {
    editor.focusTerminal = () => terminal?.focus()
    editor.terminalFocus = () => terminal?.focusState() ?? noTerminalFocus
  })
</script>

<main class="pane" aria-label={strings.editorLabel}>
  {#if editor.phase === 'starting' || editor.phase === 'running'}
    <Terminal bind:this={terminal} onstarted={() => editor.terminalStarted()} onfailed={(err) => editor.terminalFailed(err)} />
  {:else if editor.phase === 'failed'}
    <EmptyState error={errorMessage(editor.error ?? 'internal')} />
  {:else}
    <EmptyState />
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
