<script lang="ts">
  import type { EditorController } from '../lib/editorController.svelte'
  import { errorMessage } from '../lib/errorMessage'
  import { strings } from '../strings'
  import EmptyState from './EmptyState.svelte'
  import Terminal from './Terminal.svelte'

  let { editor }: { editor: EditorController } = $props()

  let terminal: ReturnType<typeof Terminal> | undefined = $state()

  $effect(() => {
    editor.focusTerminal = () => terminal?.focus()
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
    min-width: 0;
    background: var(--bg-editor);
  }
</style>
