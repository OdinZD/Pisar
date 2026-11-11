<script lang="ts">
  import { strings } from '../strings'

  // With `error` the same place shows why the editor is not there (a failed start).
  let { error }: { error?: string } = $props()
</script>

<div class="empty" class:error={error !== undefined} role={error !== undefined ? 'alert' : undefined}>
  {error ?? strings.emptyOpenDocument}
</div>

<style>
  .empty {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
    min-width: 0;
    padding: var(--space-lg);
    text-align: center;
    background: var(--bg-editor);
    color: var(--text-muted);
  }
  .error {
    color: var(--danger);
  }
</style>
