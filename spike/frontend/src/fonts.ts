import { terminalFont } from './terminalTheme'

// Loads the regular and bold faces declared by @font-face in app.css.
//
// xterm.js measures a character cell once, when the terminal opens, and keeps
// that size. If the font arrives later the grid would be computed from the
// fallback font's metrics, so the terminal must not open before this resolves.
//
// document.fonts.load() is used (not `new FontFace()`) because the faces are
// already declared in CSS, which lets Vite hash and serve the files. It starts
// the download of every face matching the descriptor and resolves with them.
// It resolves with an empty list when nothing matches (a typo in the family),
// so the result is checked: an empty list is a failure too.
export async function loadTerminalFonts(): Promise<void> {
  const descriptors = [terminalFont.weight, terminalFont.weightBold].map(
    (weight) => `${weight} ${terminalFont.sizePx}px "${terminalFont.family}"`,
  )
  const results = await Promise.all(descriptors.map((d) => document.fonts.load(d)))
  results.forEach((faces, i) => {
    if (faces.length === 0) {
      throw new Error(`no @font-face matches ${descriptors[i]}`)
    }
    for (const face of faces) {
      if (face.status !== 'loaded') {
        throw new Error(`${descriptors[i]}: font face status is ${face.status}`)
      }
    }
  })
}
