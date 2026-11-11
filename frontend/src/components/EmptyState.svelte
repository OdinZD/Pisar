<script lang="ts">
  import { strings } from '../strings'
</script>

<div class="empty">{strings.emptyOpenDocument}</div>

<style>
  .empty {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
    min-width: 0;
    background: var(--bg-editor);
    color: var(--text-muted);
  }
</style>
