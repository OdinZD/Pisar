package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"Pisar/spike/internal/nvim"
	"Pisar/spike/internal/term"
)

func TestPercentile(t *testing.T) {
	tests := []struct {
		name string
		in   []float64
		p    float64
		want float64
	}{
		{"empty", nil, 50, 0},
		{"empty p95", []float64{}, 95, 0},
		{"one", []float64{7}, 50, 7},
		{"one p95", []float64{7}, 95, 7},
		{"odd median", []float64{1, 2, 3}, 50, 2},
		{"even median is the lower middle", []float64{1, 2, 3, 4}, 50, 2},
		{"unsorted", []float64{9, 1, 5, 3, 7}, 50, 5},
		{"unsorted max", []float64{9, 1, 5, 3, 7}, 100, 9},
		{"p95 of 20 is the 19th", seq(20), 95, 19},
		{"p95 of 21 is the 20th", seq(21), 95, 20},
		{"p0 is the smallest", []float64{4, 2, 8}, 0, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := append([]float64(nil), tc.in...)
			if got := percentile(tc.in, tc.p); got != tc.want {
				t.Errorf("percentile(%v, %v) = %v, want %v", tc.in, tc.p, got, tc.want)
			}
			if !reflect.DeepEqual(in, append([]float64(nil), tc.in...)) {
				t.Error("percentile changed its input")
			}
		})
	}
}

// seq returns 1, 2, ..., n in reverse order, so it is also unsorted.
func seq(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(n - i)
	}
	return out
}

func TestDistOf(t *testing.T) {
	if got := distOf(nil); got != (distribution{}) {
		t.Errorf("distOf(nil) = %+v, want zeros", got)
	}
	got := distOf([]float64{10, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	if want := (distribution{P50: 5, P95: 10, Max: 10}); got != want {
		t.Errorf("distOf = %+v, want %+v", got, want)
	}
}

func TestCompareEvents(t *testing.T) {
	tests := []struct {
		name   string
		goSide []int
		js     []int
		want   *eventMismatch
	}{
		{"both empty", nil, nil, nil},
		{"equal", []int{5, 7, 9}, []int{5, 7, 9}, nil},
		{"js lost the last", []int{5, 7, 9}, []int{5, 7}, &eventMismatch{Index: 2, GoUnits: 9, JSUnits: -1}},
		{"js has an extra", []int{5}, []int{5, 3}, &eventMismatch{Index: 1, GoUnits: -1, JSUnits: 3}},
		{"js lost the first", []int{5, 7}, []int{7}, &eventMismatch{Index: 0, GoUnits: 5, JSUnits: 7}},
		{"same count, one length differs", []int{5, 7, 9}, []int{5, 8, 9}, &eventMismatch{Index: 1, GoUnits: 7, JSUnits: 8}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := compareEvents(tc.goSide, tc.js)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("compareEvents = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestUTF16Len(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"ascii", "abc", 3},
		{"croatian letters are one unit each", "čćšžđ", 5},
		{"quotes and dash", "„a” —", 5},
		{"outside the BMP is two units", "a😀b", 4},
		{"escape sequence", "\x1b[1;1H", 6},
		{"invalid byte becomes one U+FFFD", "a\xffb", 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := utf16Len(tc.in); got != tc.want {
				t.Errorf("utf16Len(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestCompareScreen(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		rows  []string
		want  *screenMismatch
	}{
		{"no lines", nil, []string{"x"}, nil},
		{"equal", []string{"a", "b"}, []string{"a", "b", "~"}, nil},
		{"trailing spaces are ignored on both sides", []string{"a  ", "b"}, []string{"a", "b   "}, nil},
		{"empty line", []string{"a", "", "c"}, []string{"a", "", "c"}, nil},
		{"different row", []string{"a", "b", "c"}, []string{"a", "x", "c"}, &screenMismatch{Row: 2, Neovim: "b", Screen: "x"}},
		{"leading spaces count", []string{" a"}, []string{"a"}, &screenMismatch{Row: 1, Neovim: " a", Screen: "a"}},
		{"fewer rows than lines", []string{"a", "b", "c"}, []string{"a"}, &screenMismatch{Row: 2, Neovim: "b", Missing: true}},
		{"no rows at all, empty line", []string{""}, nil, &screenMismatch{Row: 1, Missing: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := compareScreen(tc.lines, tc.rows)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("compareScreen = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAnalyze(t *testing.T) {
	samples := []emitSample{
		{ms: 1010, bytes: 100, units: 90},
		{ms: 1020, bytes: 300, units: 280},
		{ms: 1030, bytes: 200, units: 200},
	}
	rep := FrontendReport{
		Renderer:   "dom",
		Cols:       100,
		Rows:       30,
		Visible:    true,
		RecvMs:     []float64{1011, 1022, 1033},
		ParsedMs:   []float64{1012, 1025, -1},
		Units:      []int{90, 280, 200},
		FrameGaps:  []float64{16, 17, 40, 120},
		ScreenRows: []string{"a", "b"},
	}
	scr := nvim.Screen{Lines: []string{"a", "b"}}
	got := analyze("repeat", 7, 1000, samples, rep, scr)

	if got.Events != 3 || got.BytesOut != 600 || got.Presses != 7 {
		t.Errorf("counts: events=%d bytes=%d presses=%d", got.Events, got.BytesOut, got.Presses)
	}
	if got.DurationMs != 30 || got.EventsPerSec != 100 || got.BytesPerSec != 20000 {
		t.Errorf("rates: duration=%v events/s=%v bytes/s=%v", got.DurationMs, got.EventsPerSec, got.BytesPerSec)
	}
	if want := (distribution{P50: 200, P95: 300, Max: 300}); got.ChunkBytes != want {
		t.Errorf("ChunkBytes = %+v, want %+v", got.ChunkBytes, want)
	}
	if want := (distribution{P50: 2, P95: 3, Max: 3}); got.EmitToRecvMs != want {
		t.Errorf("EmitToRecvMs = %+v, want %+v", got.EmitToRecvMs, want)
	}
	if want := (distribution{P50: 2, P95: 5, Max: 5}); got.EmitToParsedMs != want {
		t.Errorf("EmitToParsedMs = %+v, want %+v", got.EmitToParsedMs, want)
	}
	if got.ParsedMissing != 1 || got.Error == "" {
		t.Errorf("ParsedMissing = %d, Error = %q; want one missing and an error", got.ParsedMissing, got.Error)
	}
	if got.FramesOver33ms != 2 || got.FramesOver100ms != 1 {
		t.Errorf("frames over 33/100 ms = %d/%d, want 2/1", got.FramesOver33ms, got.FramesOver100ms)
	}
	if !got.EventsMatch || !got.ScreenMatches || !got.Visible || got.Renderer != "dom" {
		t.Errorf("flags: %+v", got)
	}

	rep.Units = []int{90, 281, 200}
	rep.ScreenRows = []string{"a", "x"}
	got = analyze("repeat", 7, 1000, samples, rep, scr)
	if got.EventsMatch || got.FirstMismatch == nil || got.FirstMismatch.Index != 1 {
		t.Errorf("events should differ at 1: %+v", got.FirstMismatch)
	}
	if got.ScreenMatches || got.FirstScreenMismatch == nil || got.FirstScreenMismatch.Row != 2 {
		t.Errorf("screen should differ at row 2: %+v", got.FirstScreenMismatch)
	}

	if empty := analyze("x", 0, 0, nil, FrontendReport{}, nvim.Screen{}); empty.DurationMs != 0 ||
		empty.EventsPerSec != 0 || !empty.EventsMatch || !empty.ScreenMatches {
		t.Errorf("empty analyze = %+v", empty)
	}
}

func TestEmitRecorder(t *testing.T) {
	var passed []string
	rec := &emitRecorder{}
	emit := rec.wrap(func(name, data string) { passed = append(passed, name+"="+data) })

	emit(term.EventData, "before")
	rec.begin()
	emit(term.EventData, "čć😀")
	emit(term.EventSaved, "/x") // not terminal output
	emit(term.EventData, "ab")
	got := rec.end()
	emit(term.EventData, "after")

	if len(passed) != 5 {
		t.Errorf("%d events passed on, want all 5", len(passed))
	}
	if len(got) != 2 {
		t.Fatalf("recorded %d events, want 2: %+v", len(got), got)
	}
	if got[0].bytes != 2+2+4 || got[0].units != 2+2 || got[1].bytes != 2 || got[1].units != 2 {
		t.Errorf("samples = %+v", got)
	}
	if got[1].ms < got[0].ms || got[0].ms <= 0 {
		t.Errorf("times not increasing: %+v", got)
	}
}

func TestWriteResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	in := benchResult{StartedAt: "t", Error: "boom", Phases: []phaseResult{{Phase: "repeat", Events: 3}}}
	if err := writeResult(path, in); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out benchResult
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "boom" || len(out.Phases) != 1 || out.Phases[0].Events != 3 {
		t.Errorf("read back %+v", out)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the result", len(entries))
	}
	if err := writeResult(filepath.Join(dir, "missing", "r.json"), in); err == nil {
		t.Error("writing into a missing directory succeeded")
	}
}

func TestBenchTable(t *testing.T) {
	good := phaseResult{Phase: "repeat", Renderer: "dom", EventsMatch: true, ScreenMatches: true, Visible: true, EventsPerSec: 120}
	bad := good
	bad.Phase = "flood"
	bad.ScreenMatches = false
	bad.FirstScreenMismatch = &screenMismatch{Row: 3, Neovim: "a", Screen: "b"}

	dir := t.TempDir()
	write := func(name string, r benchResult) string {
		p := filepath.Join(dir, name)
		if err := writeResult(p, r); err != nil {
			t.Fatal(err)
		}
		return p
	}
	okPath := write("ok.json", benchResult{Phases: []phaseResult{good}})
	badPath := write("bad.json", benchResult{Phases: []phaseResult{good, bad}})
	errPath := write("err.json", benchResult{Phases: []phaseResult{good}, Error: "timeout"})

	var out, errOut bytes.Buffer
	if code := benchTableMain([]string{okPath}, &out, &errOut); code != 0 {
		t.Errorf("good result: exit %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "repeat") || !strings.Contains(out.String(), "eventsMatch") {
		t.Errorf("table lacks rows: %q", out.String())
	}
	for name, paths := range map[string][]string{
		"screen mismatch": {okPath, badPath},
		"run error":       {errPath},
		"missing file":    {filepath.Join(dir, "nope.json")},
	} {
		out.Reset()
		errOut.Reset()
		if code := benchTableMain(paths, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "FAIL") {
			t.Errorf("%s: exit %d, stderr %q; want a failure", name, code, errOut.String())
		}
	}
}
