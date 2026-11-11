// Collector for the benchmark mode (see bench.go): it gathers what the
// frontend measures during one phase. It knows neither Wails nor xterm.js, so
// it can be tested on its own. All times are milliseconds; the caller decides
// which clock, as long as it is the same one for every call (events use epoch
// time, frame times may be any monotonic clock because only gaps are kept).

// Field names are the Go field names of FrontendReport (bench.go): Wails
// encodes the struct without JSON tags.
export interface FrontendReport {
  Renderer: string
  Cols: number
  Rows: number
  Visible: boolean
  RecvMs: number[]
  ParsedMs: number[]
  Units: number[]
  FrameGaps: number[]
  ScreenRows: string[]
}

export interface PhaseEnd {
  renderer: string
  cols: number
  rows: number
  screenRows: string[]
}

// Marks an event whose parse has not finished, so that arrays keep one entry
// per event. Go counts negative entries as missing.
const NOT_PARSED = -1

export interface Bench {
  // Begins collecting. Anything collected before is dropped.
  start(phase: string): void
  // True from start() until finish().
  active(): boolean
  phase(): string
  // Notes one pty:data event: its length in UTF-16 units (string.length) and
  // the time it arrived. Returns the event's index, or -1 when no phase runs
  // (nothing is stored then).
  onEvent(units: number, recvMs: number): number
  // Notes that xterm.js finished parsing event index. Ignored for -1 and for
  // an index that is not known.
  onParsed(index: number, parsedMs: number): void
  // Notes an animation frame at time nowMs. Stores the gap to the previous one.
  onFrame(nowMs: number): void
  // The window stopped being visible: the phase's report says so.
  markHidden(): void
  // Events whose parse has not finished yet.
  unparsed(): number
  // Ends the phase and returns the report. Later calls to onEvent collect nothing.
  finish(end: PhaseEnd): FrontendReport
}

export function createBench(): Bench {
  let running = false
  let name = ''
  let visible = true
  let recv: number[] = []
  let parsed: number[] = []
  let units: number[] = []
  let gaps: number[] = []
  let lastFrame: number | null = null
  let pending = 0

  return {
    start(phase) {
      running = true
      name = phase
      visible = true
      recv = []
      parsed = []
      units = []
      gaps = []
      lastFrame = null
      pending = 0
    },
    active: () => running,
    phase: () => name,
    onEvent(len, recvMs) {
      if (!running) return -1
      recv.push(recvMs)
      parsed.push(NOT_PARSED)
      units.push(len)
      pending++
      return recv.length - 1
    },
    onParsed(index, parsedMs) {
      if (index < 0 || index >= parsed.length || parsed[index] !== NOT_PARSED) return
      parsed[index] = parsedMs
      pending--
    },
    onFrame(nowMs) {
      if (!running) return
      if (lastFrame !== null) gaps.push(nowMs - lastFrame)
      lastFrame = nowMs
    },
    markHidden() {
      if (running) visible = false
    },
    unparsed: () => pending,
    finish(end) {
      running = false
      return {
        Renderer: end.renderer,
        Cols: end.cols,
        Rows: end.rows,
        Visible: visible,
        RecvMs: recv,
        ParsedMs: parsed,
        Units: units,
        FrameGaps: gaps,
        ScreenRows: end.screenRows,
      }
    },
  }
}

export interface Clock {
  now(): number
  // Which clock it is, for the trace: "timeOrigin" or "Date.now".
  source: string
}

// Epoch milliseconds with decimals, to compare with Go's clock. Where
// performance.timeOrigin is missing, Date.now() (whole milliseconds) is used.
export function createClock(perf: { now(): number; timeOrigin?: number } | undefined = globalThis.performance): Clock {
  if (perf && typeof perf.timeOrigin === 'number' && typeof perf.now === 'function') {
    const origin = perf.timeOrigin
    return { now: () => origin + perf.now(), source: 'timeOrigin' }
  }
  return { now: () => Date.now(), source: 'Date.now' }
}
