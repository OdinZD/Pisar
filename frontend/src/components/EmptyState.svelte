<script lang="ts">
  import { strings } from '../strings'

  // With `error` the same place shows why the editor is not there (a failed start); the key hints
  // belong to the plain empty state only.
  let { error }: { error?: string } = $props()
</script>

<div class="empty" class:error={error !== undefined} role={error !== undefined ? 'alert' : undefined}>
  <p class="title">{error ?? strings.emptyOpenDocument}</p>
  {#if error === undefined}
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
  .hints {
    font-size: var(--font-size-section-title);
    color: var(--text-muted);
  }
  .key {
    font-family: var(--font-mono);
  }
</style>
