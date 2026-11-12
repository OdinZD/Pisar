// Turns a search hit into text segments for the panel. The result is plain strings, never HTML: the
// component renders each segment as text, so a file name can contain anything.
export interface Segment {
  text: string
  /** the segment is (part of) the match */
  hit: boolean
}

/**
 * The name without its extension, the way the service computes it (everything from the last dot;
 * a name without a dot is whole). Hit ranges never reach into the extension.
 */
export function baseName(name: string): string {
  const i = name.lastIndexOf('.')
  return i < 0 ? name : name.slice(0, i)
}

function isHigh(code: number): boolean {
  return code >= 0xd800 && code <= 0xdbff
}
function isLow(code: number): boolean {
  return code >= 0xdc00 && code <= 0xdfff
}

/**
 * The ranges to draw as hits: `[start, end)` in UTF-16 units of a text of length `length`, sorted,
 * not overlapping and not touching. The service promises that, but a wrong range must not break the
 * panel, so anything else is repaired: non-integers and malformed entries are dropped, ranges are
 * clipped to the text, empty or reversed ones dropped, overlapping and adjacent ones merged.
 * A range never starts or ends inside a surrogate pair of `text`: it grows to cover the pair.
 */
function usableRanges(text: string, matches: unknown): Array<[number, number]> {
  if (!Array.isArray(matches)) return []
  const length = text.length
  const ranges: Array<[number, number]> = []
  for (const m of matches as unknown[]) {
    if (!Array.isArray(m) || m.length < 2) continue
    const [rawStart, rawEnd] = m as [unknown, unknown]
    if (typeof rawStart !== 'number' || typeof rawEnd !== 'number' || !Number.isInteger(rawStart) || !Number.isInteger(rawEnd)) continue
    let start = Math.max(0, rawStart)
    let end = Math.min(length, rawEnd)
    if (end <= start) continue
    if (start > 0 && isLow(text.charCodeAt(start)) && isHigh(text.charCodeAt(start - 1))) start--
    if (end < length && isHigh(text.charCodeAt(end - 1)) && isLow(text.charCodeAt(end))) end++
    ranges.push([start, end])
  }
  ranges.sort((a, b) => a[0] - b[0] || a[1] - b[1])
  const merged: Array<[number, number]> = []
  for (const [start, end] of ranges) {
    const last = merged[merged.length - 1]
    if (last && start <= last[1]) last[1] = Math.max(last[1], end)
    else merged.push([start, end])
  }
  return merged
}

/**
 * The name without extension cut into alternating plain and hit segments. Joining the texts always
 * gives exactly the name without extension; no segment is empty. `matches` may be null or wrong.
 */
export function highlightSegments(name: string, matches: unknown): Segment[] {
  const text = baseName(name)
  const segments: Segment[] = []
  let at = 0
  for (const [start, end] of usableRanges(text, matches)) {
    if (start > at) segments.push({ text: text.slice(at, start), hit: false })
    segments.push({ text: text.slice(start, end), hit: true })
    at = end
  }
  if (at < text.length) segments.push({ text: text.slice(at), hit: false })
  return segments
}
