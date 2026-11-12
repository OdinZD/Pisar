// What the empty editor says when a tool the application needs is not installed. The Go side only
// reports which tools were found (EditorService.Tools); the wording and the install commands are
// decided here, so they are testable without Wails or a DOM.
import { strings } from '../strings'

export type ToolName = 'nvim' | 'pandoc'

export interface ToolStatus {
  name: string
  found: boolean
}

export interface ToolLine {
  /** what is missing and what it is needed for */
  text: string
  /** the command that installs it */
  command: string
}

// The order of this table is the order the user sees the lines in.
const known: Record<ToolName, ToolLine> = {
  nvim: { text: strings.toolsNvimMissing, command: strings.toolsNvimInstall },
  pandoc: { text: strings.toolsPandocMissing, command: strings.toolsPandocInstall },
}
const order: readonly ToolName[] = ['nvim', 'pandoc']

/** The tools that were looked up and not found, nvim first. A name this app does not know is ignored. */
export function missingTools(statuses: readonly ToolStatus[]): ToolName[] {
  return order.filter((name) => statuses.some((s) => s.name === name && !s.found))
}

/** null when nothing is missing; otherwise one line per missing tool, in the order of `order`. */
export function toolsNotice(missing: readonly ToolName[]): { lines: ToolLine[] } | null {
  const lines = order.filter((name) => missing.includes(name)).map((name) => known[name])
  return lines.length > 0 ? { lines } : null
}
