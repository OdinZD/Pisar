<script lang="ts">
  import type { ToolLine } from '../lib/toolsState'
  import { strings } from '../strings'

  // With `error` the same place shows why the editor is not there (a failed start); the key hints
  // belong to the plain empty state only. With `tools` (a tool is not installed) it shows how to get
  // it instead of the invitation to open a document; an error comes first.
  let {
    error,
    tools,
    checking = false,
    oncheck,
  }: { error?: string; tools?: { lines: ToolLine[] } | null; checking?: boolean; oncheck?: () => void } = $props()

  const notice = $derived(error === undefined ? (tools ?? undefined) : undefined)
</script>

<div
  class="empty"
  class:error={error !== undefined}
  class:notice={notice !== undefined}
  role={error !== undefined || notice !== undefined ? 'alert' : undefined}
>
  {#if notice}
    <p class="title">{strings.toolsMissingTitle}</p>
    {#each notice.lines as line (line.command)}
      <p class="tool">{line.text}</p>
      <p class="command">{line.command}</p>
    {/each}
    <button type="button" disabled={checking} onclick={() => oncheck?.()}>{strings.toolsCheckAgain}</button>
  {:else}
    <p class="title">{error ?? strings.emptyOpenDocument}</p>
  {/if}
  {#if error === undefined && !notice}
    <!-- The keys are the ones DESIGN.md names; the search panel itself is opened by the shell. -->
    <p class="hints"><span class="key">␣f</span> <span aria-hidden="true">/</span> <span class="key">⌘P</span> {strings.emptySearchHint}</p>
  {/if}
</div>

<style>
  .empty {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-sm);
    min-width: 0;
    padding: var(--space-lg);
    text-align: center;
    background: var(--bg-editor);
    color: var(--text-muted);
  }
  .error {
    color: var(--danger);
  }
  p {
    margin: 0;
  }
  .notice .title,
  .tool {
    color: var(--text);
  }
  .command {
    font-family: var(--font-mono);
    color: var(--text-muted);
  }
  button {
    margin-top: var(--space-sm);
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
  .hints {
    font-size: var(--font-size-section-title);
    color: var(--text-muted);
  }
  .key {
    font-family: var(--font-mono);
  }
</style>
