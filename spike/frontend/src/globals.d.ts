export {}

declare global {
  interface Window {
    // Manual switch between the DOM and WebGL renderers (see Terminal.svelte).
    pisarSetRenderer?: (kind: 'dom' | 'webgl') => void
  }
}
