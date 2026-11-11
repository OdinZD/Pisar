package main

// Benchmark mode of the spike: with PISAR_SPIKE_BENCH=<result.json> the
// application drives Neovim itself, as if a key were held down, and measures
// what reaches xterm.js. Without the variable nothing here runs.
//
// What is measured, per phase:
//   - Go side: the time, byte size and UTF-16 length of every pty:data event
//     (emitRecorder wraps the Session's Emitter).
//   - Frontend side (bench.ts): arrival time and parse-finished time of every
//     pty:data event, the gaps between animation frames, and the text of the
//     terminal rows at the end. It comes back through TerminalService.BenchReport.
//   - Neovim itself: what its window shows at the end (Process.Screen), to be
//     compared with the terminal rows.
//
// All times are epoch milliseconds with decimals. Go and the webview read the
// same system clock, so emit-to-receive is a difference of two readings of one
// clock; the webview's reading is performance.timeOrigin + performance.now().

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
	"unicode/utf16"

	"Pisar/spike/internal/nvim"
	"Pisar/spike/internal/term"
)

// Names of the bench settings. The one place they are defined.
const (
	envBench = "PISAR_SPIKE_BENCH" // path of the result file; switches the bench mode on

	// benchTableFlag as the first argument prints the table for the given
	// result files and exits, without starting the application.
	benchTableFlag = "--bench-table"

	eventBenchStart = "bench:start" // payload: the phase name
	eventBenchStop  = "bench:stop"  // payload: the phase name

	phaseRepeat = "repeat"
	phaseFlood  = "flood"
)

const (
	// repeatInterval approximates the key repeat of a held key (about 30 per second).
	repeatInterval = 33 * time.Millisecond

	// quietFor is how long without terminal output counts as "Neovim is done".
	quietFor  = 400 * time.Millisecond
	quietPoll = 25 * time.Millisecond

	// Ctrl-d and Ctrl-u as the terminal sends them.
	keyDown = "\x04"
	keyUp   = "\x15"

	// checkEveryRepeat is the number of presses between looks at Neovim's
	// cursor (about half a second of held key); checkEveryFlood is larger
	// because the presses come without pauses.
	checkEveryRepeat = 15
	checkEveryFlood  = 40

	minBufferLines = 1000 // the opened file must be at least this long

	totalLimit       = 90 * time.Second // the whole measurement
	repeatLimit      = 45 * time.Second
	floodLimit       = 30 * time.Second
	reportTimeout    = 20 * time.Second // the frontend's answer after bench:stop
	screenTimeout    = 5 * time.Second
	versionTimeout   = 5 * time.Second
	frameSlowLimitMs = 33.0  // a frame gap above this missed at least one 60 Hz frame
	frameStallMs     = 100.0 // a gap this long is a visible stall
)

// FrontendReport is what the frontend measured during one phase.
type FrontendReport struct {
	Renderer   string    // "dom" or "webgl"
	Cols, Rows int       // terminal grid
	Visible    bool      // document.visibilityState was "visible" for the whole phase
	RecvMs     []float64 // epoch ms when each pty:data event of the phase arrived, in order
	ParsedMs   []float64 // epoch ms when xterm.js finished processing that event (write callback)
	Units      []int     // payload length in UTF-16 code units, per event
	FrameGaps  []float64 // ms between consecutive animation frames during the phase
	ScreenRows []string  // text of every terminal row after the phase, trailing spaces trimmed
}

// emitSample is what the Go side knows about one pty:data event.
type emitSample struct {
	ms    float64 // epoch ms just before the event was handed to Wails
	bytes int     // UTF-8 length of the payload
	units int     // UTF-16 length of the payload, what JavaScript's string.length reports
}

// epochMs is the current time in epoch milliseconds, with decimals.
func epochMs() float64 { return float64(time.Now().UnixMicro()) / 1e3 }

// utf16Len is the length of s in UTF-16 code units. The Wails runtime turns
// the payload into a JSON string and JavaScript counts that in UTF-16 units,
// so characters outside the BMP count twice. A byte that is not valid UTF-8
// decodes to U+FFFD, which is one unit, exactly as JSON encoding makes it.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if l := utf16.RuneLen(r); l > 0 {
			n += l
		} else {
			n++
		}
	}
	return n
}

// emitRecorder notes every pty:data event while a phase is active.
type emitRecorder struct {
	mu      sync.Mutex
	active  bool
	samples []emitSample
}

// wrap returns an Emitter that records pty:data events and then passes
// everything on to next. The reading is taken before next runs, so the time
// the Wails runtime spends handing the event over counts as latency.
func (r *emitRecorder) wrap(next term.Emitter) term.Emitter {
	return func(name, data string) {
		if name == term.EventData {
			r.record(data)
		}
		next(name, data)
	}
}

func (r *emitRecorder) record(data string) {
	ms := epochMs()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		r.samples = append(r.samples, emitSample{ms: ms, bytes: len(data), units: utf16Len(data)})
	}
}

func (r *emitRecorder) begin() {
	r.mu.Lock()
	r.active = true
	r.samples = nil
	r.mu.Unlock()
}

func (r *emitRecorder) end() []emitSample {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = false
	s := r.samples
	r.samples = nil
	return s
}

// ---- pure analysis -------------------------------------------------------

// percentile is the nearest-rank percentile (p in 0..100) of vals, which need
// not be sorted. No values give 0.
func percentile(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := slices.Clone(vals)
	slices.Sort(s)
	// Multiplying before dividing keeps whole ranks exact: 95*20/100 is 19.
	rank := int(math.Ceil(p * float64(len(s)) / 100))
	rank = min(max(rank, 1), len(s))
	return s[rank-1]
}

// distribution is the median, the 95th percentile and the maximum of a series.
type distribution struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
}

func distOf(vals []float64) distribution {
	if len(vals) == 0 {
		return distribution{}
	}
	return distribution{P50: percentile(vals, 50), P95: percentile(vals, 95), Max: slices.Max(vals)}
}

// eventMismatch is the first event whose length differs between Go and the
// frontend. A side that has no such event reports -1.
type eventMismatch struct {
	Index   int `json:"index"`
	GoUnits int `json:"goUnits"`
	JSUnits int `json:"jsUnits"`
}

// compareEvents checks that both sides saw the same events in the same order,
// judged by the length of each payload. It returns nil when they agree.
func compareEvents(goUnits, jsUnits []int) *eventMismatch {
	for i := range max(len(goUnits), len(jsUnits)) {
		g, j := -1, -1
		if i < len(goUnits) {
			g = goUnits[i]
		}
		if i < len(jsUnits) {
			j = jsUnits[i]
		}
		if g != j {
			return &eventMismatch{Index: i, GoUnits: g, JSUnits: j}
		}
	}
	return nil
}

// screenMismatch is the first terminal row that differs from what Neovim says
// it shows. Row is 1-based. Missing means the terminal has fewer rows than
// Neovim has lines to show.
type screenMismatch struct {
	Row     int    `json:"row"`
	Neovim  string `json:"neovim"`
	Screen  string `json:"screen"`
	Missing bool   `json:"missing,omitempty"`
}

// compareScreen checks the first len(lines) terminal rows against the buffer
// lines Neovim shows, ignoring spaces at the end of either. It returns nil
// when they agree.
func compareScreen(lines, rows []string) *screenMismatch {
	for i, want := range lines {
		want = strings.TrimRight(want, " ")
		if i >= len(rows) {
			return &screenMismatch{Row: i + 1, Neovim: want, Missing: true}
		}
		if got := strings.TrimRight(rows[i], " "); got != want {
			return &screenMismatch{Row: i + 1, Neovim: want, Screen: got}
		}
	}
	return nil
}

// phaseResult is what the result file holds for one phase.
type phaseResult struct {
	Phase               string          `json:"phase"`
	Renderer            string          `json:"renderer"`
	Cols                int             `json:"cols"`
	Rows                int             `json:"rows"`
	Visible             bool            `json:"visible"`
	Presses             int             `json:"presses"`
	DurationMs          float64         `json:"durationMs"` // first key press to the last event
	BytesOut            int             `json:"bytesOut"`
	Events              int             `json:"events"`
	EventsPerSec        float64         `json:"eventsPerSec"`
	BytesPerSec         float64         `json:"bytesPerSec"`
	ChunkBytes          distribution    `json:"chunkBytes"`
	EmitToRecvMs        distribution    `json:"emitToRecvMs"`
	EmitToParsedMs      distribution    `json:"emitToParsedMs"`
	FrameGapMs          distribution    `json:"frameGapMs"`
	FramesOver33ms      int             `json:"framesOver33ms"`
	FramesOver100ms     int             `json:"framesOver100ms"`
	EventsMatch         bool            `json:"eventsMatch"`
	FirstMismatch       *eventMismatch  `json:"firstMismatch,omitempty"`
	ScreenMatches       bool            `json:"screenMatches"`
	FirstScreenMismatch *screenMismatch `json:"firstScreenMismatch,omitempty"`
	ParsedMissing       int             `json:"parsedMissing"` // events whose parse never finished
	Error               string          `json:"error,omitempty"`
}

// analyze turns the Go-side samples, the frontend's report and Neovim's own
// view into one phase result. startMs is when the first key was sent.
func analyze(phase string, presses int, startMs float64, samples []emitSample, rep FrontendReport, scr nvim.Screen) phaseResult {
	res := phaseResult{
		Phase:    phase,
		Renderer: rep.Renderer,
		Cols:     rep.Cols,
		Rows:     rep.Rows,
		Visible:  rep.Visible,
		Presses:  presses,
		Events:   len(samples),
	}

	chunks := make([]float64, len(samples))
	goUnits := make([]int, len(samples))
	for i, s := range samples {
		res.BytesOut += s.bytes
		chunks[i] = float64(s.bytes)
		goUnits[i] = s.units
	}
	if n := len(samples); n > 0 {
		res.DurationMs = samples[n-1].ms - startMs
	}
	if res.DurationMs > 0 {
		secs := res.DurationMs / 1e3
		res.EventsPerSec = float64(res.Events) / secs
		res.BytesPerSec = float64(res.BytesOut) / secs
	}
	res.ChunkBytes = distOf(chunks)

	var recv, parsed []float64
	for i := range min(len(samples), len(rep.RecvMs)) {
		recv = append(recv, rep.RecvMs[i]-samples[i].ms)
	}
	for i := range min(len(samples), len(rep.ParsedMs)) {
		if rep.ParsedMs[i] < 0 { // bench.ts marks an unfinished parse with -1
			res.ParsedMissing++
			continue
		}
		parsed = append(parsed, rep.ParsedMs[i]-samples[i].ms)
	}
	res.EmitToRecvMs = distOf(recv)
	res.EmitToParsedMs = distOf(parsed)

	res.FrameGapMs = distOf(rep.FrameGaps)
	for _, g := range rep.FrameGaps {
		if g > frameSlowLimitMs {
			res.FramesOver33ms++
		}
		if g > frameStallMs {
			res.FramesOver100ms++
		}
	}

	res.FirstMismatch = compareEvents(goUnits, rep.Units)
	res.EventsMatch = res.FirstMismatch == nil
	res.FirstScreenMismatch = compareScreen(scr.Lines, rep.ScreenRows)
	res.ScreenMatches = res.FirstScreenMismatch == nil
	if res.ParsedMissing > 0 {
		res.Error = fmt.Sprintf("%d events were not parsed by xterm.js in time", res.ParsedMissing)
	}
	return res
}

// ---- the result file -----------------------------------------------------

// benchResult is the result file.
type benchResult struct {
	StartedAt   string        `json:"startedAt"`
	NvimVersion string        `json:"nvimVersion,omitempty"`
	File        string        `json:"file,omitempty"`
	FileBytes   int64         `json:"fileBytes,omitempty"`
	Phases      []phaseResult `json:"phases"`
	Error       string        `json:"error,omitempty"`
}

// writeResult writes v as JSON to path atomically: a temporary file in the
// same directory, then a rename, so a reader never sees half a file.
func writeResult(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bench-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary result file: %w", err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return errors.Join(fmt.Errorf("write result: %w", err), tmp.Close(), os.Remove(tmp.Name()))
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(fmt.Errorf("close result: %w", err), os.Remove(tmp.Name()))
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.Join(fmt.Errorf("rename result: %w", err), os.Remove(tmp.Name()))
	}
	return nil
}

// ---- the measurement run -------------------------------------------------

// benchConfig is what newBenchRun needs. The functions exist because the
// Wails application is created after the objects that hold a benchRun.
type benchConfig struct {
	Session  *term.Session
	Recorder *emitRecorder
	Emit     term.Emitter // delivers bench:start and bench:stop, unrecorded
	Quit     func()       // ends the application
	Path     string       // result file
	File     string       // the file Neovim opened; for the result's header
	NvimBin  string
}

type reportMsg struct {
	phase  string
	report FrontendReport
}

// benchRun drives one measurement. It is created only in bench mode.
type benchRun struct {
	cfg     benchConfig
	ctx     context.Context
	cancel  context.CancelFunc
	started sync.Once
	ended   sync.Once
	reports chan reportMsg

	mu     sync.Mutex // guards result
	result benchResult
}

// newBenchRun creates the run and arms the overall time limit: when it
// passes, the result is written with an error and the application quits.
func newBenchRun(cfg benchConfig) *benchRun {
	ctx, cancel := context.WithTimeout(context.Background(), totalLimit)
	b := &benchRun{
		cfg:     cfg,
		ctx:     ctx,
		cancel:  cancel,
		reports: make(chan reportMsg, 4),
		result:  benchResult{StartedAt: time.Now().Format(time.RFC3339), File: cfg.File},
	}
	context.AfterFunc(ctx, func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			b.finish(fmt.Errorf("the measurement did not finish within %v", totalLimit))
		}
	})
	return b
}

// onStarted is called after Neovim runs and shows the initial file. It starts
// the measurement once; later calls (a reloaded frontend) do nothing.
// Safe on a nil *benchRun.
func (b *benchRun) onStarted() {
	if b == nil {
		return
	}
	b.started.Do(func() {
		go func() {
			b.finish(b.run(b.ctx))
		}()
	})
}

// deliver hands the frontend's report to the run waiting for it.
func (b *benchRun) deliver(phase string, report FrontendReport) error {
	select {
	case b.reports <- reportMsg{phase: phase, report: report}:
		return nil
	default:
		return errors.New("bench: report queue is full")
	}
}

func (b *benchRun) addPhase(p phaseResult) {
	b.mu.Lock()
	b.result.Phases = append(b.result.Phases, p)
	b.mu.Unlock()
}

// finish writes the result, with err in its error field if there is one, and
// quits the application. Only the first call does anything.
func (b *benchRun) finish(err error) {
	b.ended.Do(func() {
		b.cancel()
		b.mu.Lock()
		if err != nil {
			b.result.Error = err.Error()
		}
		werr := writeResult(b.cfg.Path, b.result)
		b.mu.Unlock()
		if werr != nil {
			log.Printf("bench: %v", werr)
		}
		if err != nil {
			log.Printf("bench: %v", err)
		}
		b.cfg.Quit()
	})
}

// run is the whole measurement: both phases, with the cursor sent back to the
// top between them.
func (b *benchRun) run(ctx context.Context) error {
	if err := b.header(ctx); err != nil {
		return err
	}
	scr, err := b.screen(ctx)
	if err != nil {
		return fmt.Errorf("read the screen: %w", err)
	}
	if scr.LastLine < minBufferLines {
		return fmt.Errorf("the open buffer has %d lines, the measurement needs at least %d", scr.LastLine, minBufferLines)
	}
	if err := b.waitQuiet(ctx); err != nil {
		return err
	}

	atBottom := func(s nvim.Screen) bool { return s.CursorLine >= s.LastLine }
	atTop := func(s nvim.Screen) bool { return s.CursorLine <= 1 }

	err = b.phase(ctx, phaseRepeat, repeatLimit, atBottom, func(ctx context.Context) (int, error) {
		return b.pressUntil(ctx, keyDown, repeatInterval, checkEveryRepeat, atBottom)
	})
	if err != nil {
		return err
	}

	if err := b.goTop(ctx); err != nil {
		return err
	}

	return b.phase(ctx, phaseFlood, floodLimit, atTop, func(ctx context.Context) (int, error) {
		down, err := b.pressUntil(ctx, keyDown, 0, checkEveryFlood, atBottom)
		if err != nil {
			return down, err
		}
		up, err := b.pressUntil(ctx, keyUp, 0, checkEveryFlood, atTop)
		return down + up, err
	})
}

// header fills in the facts about the run: the Neovim version and the file size.
func (b *benchRun) header(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, b.cfg.NvimBin, "--version").Output()
	if err != nil {
		return fmt.Errorf("nvim --version: %w", err)
	}
	version, _, _ := strings.Cut(string(out), "\n")
	var size int64
	if b.cfg.File != "" {
		fi, err := os.Stat(b.cfg.File)
		if err != nil {
			return fmt.Errorf("stat the opened file: %w", err)
		}
		size = fi.Size()
	}
	b.mu.Lock()
	b.result.NvimVersion = strings.TrimSpace(version)
	b.result.FileBytes = size
	b.mu.Unlock()
	return nil
}

func (b *benchRun) screen(ctx context.Context) (nvim.Screen, error) {
	ctx, cancel := context.WithTimeout(ctx, screenTimeout)
	defer cancel()
	return b.cfg.Session.Screen(ctx)
}

// goTop sends the cursor to the first line and waits for the output to settle.
func (b *benchRun) goTop(ctx context.Context) error {
	if err := b.cfg.Session.Write("gg"); err != nil {
		return fmt.Errorf("send gg: %w", err)
	}
	if err := b.waitQuiet(ctx); err != nil {
		return err
	}
	scr, err := b.screen(ctx)
	if err != nil {
		return fmt.Errorf("read the screen after gg: %w", err)
	}
	if scr.CursorLine != 1 {
		return fmt.Errorf("gg left the cursor on line %d", scr.CursorLine)
	}
	return nil
}

// waitQuiet returns once the terminal output has paused for quietFor. Output
// is counted in events, which the Session already does.
func (b *benchRun) waitQuiet(ctx context.Context) error {
	last := b.cfg.Session.Stats().Chunks
	since := time.Now()
	tick := time.NewTicker(quietPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for quiet output: %w", ctx.Err())
		case <-tick.C:
		}
		if cur := b.cfg.Session.Stats().Chunks; cur != last {
			last, since = cur, time.Now()
		} else if time.Since(since) >= quietFor {
			return nil
		}
	}
}

// pressUntil sends key, one byte string per press, until done accepts
// Neovim's screen or ctx ends. every is the pause between presses (0: none).
// Neovim is asked for its screen after every checkEvery presses only, so the
// questions do not slow the presses down. It reports ctx ending as a normal
// return; the caller looks at the screen to see whether the goal was reached.
func (b *benchRun) pressUntil(ctx context.Context, key string, every time.Duration, checkEvery int, done func(nvim.Screen) bool) (int, error) {
	var tick <-chan time.Time
	if every > 0 {
		t := time.NewTicker(every)
		defer t.Stop()
		tick = t.C
	}
	presses := 0
	for {
		if tick != nil {
			select {
			case <-tick:
			case <-ctx.Done():
				return presses, nil
			}
		} else if ctx.Err() != nil {
			return presses, nil
		}
		if err := b.cfg.Session.Write(key); err != nil {
			return presses, fmt.Errorf("send key: %w", err)
		}
		presses++
		if presses%checkEvery != 0 {
			continue
		}
		scr, err := b.screen(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return presses, nil
			}
			return presses, fmt.Errorf("read the screen: %w", err)
		}
		if done(scr) {
			return presses, nil
		}
	}
}

// phase runs one measured phase: tells the frontend to start collecting,
// lets drive press keys, waits for quiet output, tells the frontend to stop
// and report, and stores the analysis. The frontend's reading of the
// terminal and Neovim's own are taken at the same quiet moment. goal says
// whether Neovim's screen shows the place the phase was meant to reach; if
// the phase limit passed first, that is an error of the phase.
func (b *benchRun) phase(ctx context.Context, name string, limit time.Duration, goal func(nvim.Screen) bool, drive func(context.Context) (int, error)) error {
	b.cfg.Recorder.begin()
	b.cfg.Emit(eventBenchStart, name)
	startMs := epochMs()

	driveCtx, cancel := context.WithTimeout(ctx, limit)
	presses, driveErr := drive(driveCtx)
	cancel()

	var scr nvim.Screen
	var scrErr error
	quietErr := driveErr
	if quietErr == nil {
		quietErr = b.waitQuiet(ctx)
	}
	if quietErr == nil {
		scr, scrErr = b.screen(ctx)
	}
	samples := b.cfg.Recorder.end()
	// Sent even when something failed, so the frontend stops its frame loop.
	b.cfg.Emit(eventBenchStop, name)
	switch {
	case driveErr != nil:
		return fmt.Errorf("phase %s: %w", name, driveErr)
	case quietErr != nil:
		return fmt.Errorf("phase %s: %w", name, quietErr)
	case scrErr != nil:
		return fmt.Errorf("phase %s: read the screen: %w", name, scrErr)
	}

	rep, err := b.awaitReport(ctx, name)
	if err != nil {
		return fmt.Errorf("phase %s: %w", name, err)
	}
	res := analyze(name, presses, startMs, samples, rep, scr)
	if !goal(scr) {
		res.Error = strings.TrimSpace(res.Error + fmt.Sprintf(" the cursor is on line %d of %d: the phase limit of %v passed before it reached its goal",
			scr.CursorLine, scr.LastLine, limit))
	}
	b.addPhase(res)
	if res.Error != "" {
		return fmt.Errorf("phase %s: %s", name, res.Error)
	}
	return nil
}

// awaitReport waits for the frontend's report of phase.
func (b *benchRun) awaitReport(ctx context.Context, phase string) (FrontendReport, error) {
	timer := time.NewTimer(reportTimeout)
	defer timer.Stop()
	for {
		select {
		case m := <-b.reports:
			if m.phase == phase {
				return m.report, nil
			}
		case <-timer.C:
			return FrontendReport{}, fmt.Errorf("the frontend sent no report within %v", reportTimeout)
		case <-ctx.Done():
			return FrontendReport{}, fmt.Errorf("wait for the frontend's report: %w", ctx.Err())
		}
	}
}

// ---- the table -----------------------------------------------------------

// errWriter remembers the first failed write, so the printing code can stay
// a plain list of lines and still not lose the error.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err == nil {
		_, e.err = fmt.Fprintf(e.w, format, args...)
	}
}

// formatTable renders one row per phase and renderer, and returns the problems
// that make a run a failure (an error, a mismatch).
func formatTable(w io.Writer, results []benchResult) (problems []string) {
	tab := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	tw := &errWriter{w: tab}
	tw.printf("%s\n", "renderer\tphase\tevents/s\tMB/s\temit>recv p50/p95/max ms\temit>parsed p50/p95/max ms\tframe p95/max ms\t>33ms\t>100ms\teventsMatch\tscreenMatches\tvisible")
	for _, r := range results {
		for _, p := range r.Phases {
			tw.printf("%s\t%s\t%.0f\t%.2f\t%.1f/%.1f/%.1f\t%.1f/%.1f/%.1f\t%.1f/%.1f\t%d\t%d\t%t\t%t\t%t\n",
				p.Renderer, p.Phase, p.EventsPerSec, p.BytesPerSec/1e6,
				p.EmitToRecvMs.P50, p.EmitToRecvMs.P95, p.EmitToRecvMs.Max,
				p.EmitToParsedMs.P50, p.EmitToParsedMs.P95, p.EmitToParsedMs.Max,
				p.FrameGapMs.P95, p.FrameGapMs.Max, p.FramesOver33ms, p.FramesOver100ms,
				p.EventsMatch, p.ScreenMatches, p.Visible)
			label := p.Renderer + "/" + p.Phase
			if p.Error != "" {
				problems = append(problems, label+": "+p.Error)
			}
			if !p.EventsMatch {
				problems = append(problems, fmt.Sprintf("%s: events differ: %+v", label, p.FirstMismatch))
			}
			if !p.ScreenMatches {
				problems = append(problems, fmt.Sprintf("%s: screen differs: %+v", label, p.FirstScreenMismatch))
			}
		}
		if r.Error != "" {
			problems = append(problems, "run: "+r.Error)
		}
		if len(r.Phases) == 0 {
			problems = append(problems, "run: no phase was measured")
		}
	}
	// The tabwriter holds everything back until Flush.
	if err := tab.Flush(); err != nil && tw.err == nil {
		tw.err = err
	}
	if tw.err != nil {
		problems = append(problems, fmt.Sprintf("print table: %v", tw.err))
	}
	return problems
}

// benchTableMain prints the table for the result files and returns the exit
// code: 0 when no file has a problem.
func benchTableMain(paths []string, stdout, stderr io.Writer) int {
	var results []benchResult
	bad := false
	errs := &errWriter{w: stderr}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			errs.printf("FAIL %v\n", err)
			bad = true
			continue
		}
		var r benchResult
		if err := json.Unmarshal(data, &r); err != nil {
			errs.printf("FAIL decode %s: %v\n", path, err)
			bad = true
			continue
		}
		results = append(results, r)
	}
	for _, p := range formatTable(stdout, results) {
		errs.printf("FAIL %s\n", p)
		bad = true
	}
	if bad || errs.err != nil {
		return 1
	}
	return 0
}
