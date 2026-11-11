<script lang="ts">
  import { onMount } from 'svelte'
  import EditorPane from './components/EditorPane.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Toolbar from './components/Toolbar.svelte'
  import { EditorController } from './lib/editorController.svelte'
  import { TreeController } from './lib/treeController.svelte'

  const tree = new TreeController()
  const editor = new EditorController()

  // start() returns the cleanup that removes every listener when the app is torn down.
  onMount(() => {
    const stopTree = tree.start()
    const stopEditor = editor.start()
    return () => {
      stopTree()
      stopEditor()
    }
  })
</script>

<div class="app">
  <Toolbar {tree} />
  <div class="body">
    <Sidebar {tree} />
    <EditorPane {editor} />
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
