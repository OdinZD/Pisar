<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import EditorPane from './components/EditorPane.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Toolbar from './components/Toolbar.svelte'
  import { EditorController } from './lib/editorController.svelte'
  import { TreeController } from './lib/treeController.svelte'

  const editor = new EditorController()
  const tree = new TreeController(editor)

  // Follows the document Neovim has open, whoever changed it. The effect depends only on
  // editor.active: followActive reads and writes tree state, which untrack keeps out of the
  // dependencies, so the effect cannot trigger itself.
  $effect(() => {
    const active = editor.active
    untrack(() => tree.followActive(active))
  })

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
  <Toolbar {tree} {editor} />
  <div class="body">
    <Sidebar {tree} {editor} />
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
