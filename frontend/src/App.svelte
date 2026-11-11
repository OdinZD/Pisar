<script lang="ts">
  import { onMount } from 'svelte'
  import EmptyState from './components/EmptyState.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Toolbar from './components/Toolbar.svelte'
  import { TreeController } from './lib/treeController.svelte'

  const tree = new TreeController()

  // start() returns the cleanup that removes every listener when the app is torn down.
  onMount(() => tree.start())
</script>

<div class="app">
  <Toolbar {tree} />
  <div class="body">
    <Sidebar {tree} />
    <EmptyState />
  </div>
</div>

<style>
  .app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
  }
  .body {
    display: flex;
    flex: 1;
    min-height: 0;
  }
</style>
