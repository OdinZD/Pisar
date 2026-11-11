import interRegular from '../assets/fonts/Inter-Regular.woff2'
import interSemiBold from '../assets/fonts/Inter-SemiBold.woff2'
import jetBrainsMonoBold from '../assets/fonts/JetBrainsMono-Bold.woff2'
import jetBrainsMonoRegular from '../assets/fonts/JetBrainsMono-Regular.woff2'
import { fontFamilyNames, fontWeights, type FontRole, type FontWeightToken } from './tokens'

export interface FontFaceSpec {
  family: string
  weight: number
  url: string
}

function spec(role: FontRole, weight: FontWeightToken, url: string): FontFaceSpec {
  return { family: fontFamilyNames[role], weight: fontWeights[weight], url }
}

/** Every face the UI needs. Family and weight come from the tokens; only the files are listed here. */
export function fontFaceSpecs(): FontFaceSpec[] {
  return [
    spec('ui', 'regular', interRegular),
    spec('ui', 'semibold', interSemiBold),
    spec('mono', 'regular', jetBrainsMonoRegular),
    spec('mono', 'bold', jetBrainsMonoBold),
  ]
}

// FontFace + document.fonts.add (not @font-face in CSS) keeps the family names in tokens.ts only.
// xterm.js measures a cell once when the terminal opens, so the editor font must be ready before that.
export async function loadFonts(): Promise<void> {
  await Promise.all(
    fontFaceSpecs().map(async (s) => {
      const name = `${s.family} ${s.weight}`
      try {
        const face = new FontFace(s.family, `url("${s.url}")`, { weight: String(s.weight), style: 'normal' })
        await face.load()
        document.fonts.add(face)
      } catch (err) {
        throw new Error(`font ${name}: ${err instanceof Error ? err.message : String(err)}`, { cause: err })
      }
    }),
  )
}
