// What the empty editor says when a tool the application needs is not installed or too old. The Go
// side only reports what it found (EditorService.Tools: found, version, minimum, tooOld); the wording
// and the install and upgrade commands are decided here, so they are testable without Wails or a DOM.
import { strings } from '../strings'

export type ToolName = 'nvim' | 'pandoc'

export interface ToolStatus {
  name: string
  found: boolean
  // Optional, so that an answer without them still means what it meant before.
  version?: string
  minimum?: string
  tooOld?: boolean
}

export interface ToolProblem {
  name: ToolName
  kind: 'missing' | 'too-old'
  /** what is installed; '' when missing */
  version: string
  minimum: string
}

export interface ToolLine {
  /** what is missing and what it is needed for */
  text: string
  /** the command that installs it */
  command: string
}

const order: readonly ToolName[] = ['nvim', 'pandoc']

// The order of `order` is the order the user sees the lines in.
const missingText: Record<ToolName, ToolLine> = {
  nvim: { text: strings.toolsNvimMissing, command: strings.toolsNvimInstall },
  pandoc: { text: strings.toolsPandocMissing, command: strings.toolsPandocInstall },
}
const tooOldText: Record<ToolName, { text: string; command: string }> = {
  nvim: { text: strings.toolsNvimTooOld, command: strings.toolsNvimUpgrade },
  pandoc: { text: strings.toolsPandocTooOld, command: strings.toolsPandocUpgrade },
}

/** The tools that were looked up and not found, nvim first. A name this app does not know is ignored. */
export function missingTools(statuses: readonly ToolStatus[]): ToolName[] {
  return order.filter((name) => statuses.some((s) => s.name === name && !s.found))
}

/** What is wrong with each tool (missing, or found but too old), nvim first. A name this app does not know is ignored. */
export function toolProblems(statuses: readonly ToolStatus[]): ToolProblem[] {
  const problems: ToolProblem[] = []
  for (const name of order) {
    const s = statuses.find((x) => x.name === name)
    if (!s) continue
    const version = s.version ?? ''
    const minimum = s.minimum ?? ''
    if (!s.found) problems.push({ name, kind: 'missing', version: '', minimum })
    else if (s.tooOld) problems.push({ name, kind: 'too-old', version, minimum })
  }
  return problems
}

function fill(template: string, values: Record<string, string>): string {
  return template.replace(/\{(\w+)\}/g, (whole, key: string) => values[key] ?? whole)
}

/** null when there is no problem; otherwise a title and one line per problem, in the order of `order`. */
export function toolsNotice(problems: readonly ToolProblem[]): { title: string; lines: ToolLine[] } | null {
  const sorted = order.flatMap((name) => problems.filter((p) => p.name === name))
  if (sorted.length === 0) return null
  const title = sorted.every((p) => p.kind === 'missing') ? strings.toolsMissingTitle : strings.toolsNotReadyTitle
  const lines = sorted.map((p): ToolLine => {
    if (p.kind === 'missing') return missingText[p.name]
    const t = tooOldText[p.name]
    return { text: fill(t.text, { version: p.version, minimum: p.minimum }), command: t.command }
  })
  return { title, lines }
}
