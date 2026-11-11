<script lang="ts">
  let {
    initial,
    depth,
    label,
    onsubmit,
    oncancel,
    oninput,
  }: {
    initial: string
    depth: number
    label: string
    onsubmit: (text: string) => void
    oncancel: (refocus: boolean) => void
    oninput: () => void
  } = $props()

  function focusAndSelect(node: HTMLInputElement) {
    node.focus()
    node.select()
  }

  function onkeydown(e: KeyboardEvent & { currentTarget: HTMLInputElement }) {
    if (e.isComposing) return // Enter that confirms an IME candidate is not a submit
    if (e.key === 'Enter') {
      e.preventDefault()
      onsubmit(e.currentTarget.value)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      oncancel(true)
    }
  }
</script>

<div class="field" role="none" style:--depth={depth}>
  <input
    type="text"
    value={initial}
    aria-label={label}
    autocomplete="off"
    spellcheck="false"
    {@attach focusAndSelect}
    {onkeydown}
    {oninput}
    onblur={() => oncancel(false)}
  />
</div>

<style>
  .field {
    display: flex;
    align-items: center;
    height: var(--size-row-height);
    padding: 0 var(--space-md) 0 calc(var(--space-sm) + var(--space-lg) + var(--space-xs) + var(--depth) * var(--space-lg));
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
</style>
