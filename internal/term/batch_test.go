package term

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	batchWait = 5 * time.Second // generous: only bounds a hang, never a happy path

	// The batching numbers of the real application live in internal/config,
	// which this package does not import; the tests use values of the same size.
	testFlushInterval = 8 * time.Millisecond
	testMaxChunk      = 16 << 10
)

// payloads collects emitted payloads; emit may be called from another goroutine.
type payloads struct {
	mu  sync.Mutex
	all []string
}

func (c *payloads) emit(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.all = append(c.all, s)
}

func (c *payloads) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.all...)
}

func (c *payloads) joined() string { return strings.Join(c.snapshot(), "") }

// checkValid fails if any payload is not valid UTF-8 or is empty.
func checkValid(t *testing.T, got []string) {
	t.Helper()
	for i, p := range got {
		if !utf8.ValidString(p) {
			t.Errorf("payload %d is not valid UTF-8: %q", i, p)
		}
		if p == "" {
			t.Errorf("payload %d is empty", i)
		}
	}
}

const (
	croatian = "č ć š ž đ — €"
	emoji    = "a😀b" // the emoji is 4 bytes
)

func TestCutEverywhere(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"croatian", croatian},
		{"emoji", emoji},
		{"mixed", "x" + croatian + emoji + "\x1b[31mč\x1b[0m"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Two cuts, so a character can be spread over three reads.
			for i := 0; i <= len(tc.in); i++ {
				for j := i; j <= len(tc.in); j++ {
					var c payloads
					b := &batcher{max: testMaxChunk, emit: c.emit}
					for _, part := range []string{tc.in[:i], tc.in[i:j], tc.in[j:]} {
						b.add([]byte(part))
						b.flush(false)
					}
					b.flush(true)
					checkValid(t, c.snapshot())
					if got := c.joined(); got != tc.in {
						t.Fatalf("cuts at %d,%d: joined = %q, want %q", i, j, got, tc.in)
					}
				}
			}
		})
	}
}

func TestByteByByte(t *testing.T) {
	var c payloads
	b := &batcher{max: testMaxChunk, emit: c.emit}
	in := croatian + emoji
	for i := 0; i < len(in); i++ {
		b.add([]byte{in[i]})
		b.flush(false)
	}
	if len(b.pending) != 0 {
		t.Errorf("pending = %q after a complete input", b.pending)
	}
	checkValid(t, c.snapshot())
	if got := c.joined(); got != in {
		t.Errorf("joined = %q, want %q", got, in)
	}
}

func TestEmojiTail(t *testing.T) {
	// A 4-byte character cut after its 1st, 2nd and 3rd byte is held back.
	for cut := 1; cut <= 3; cut++ {
		var c payloads
		b := &batcher{max: testMaxChunk, emit: c.emit}
		in := "a😀"
		b.add([]byte(in[:1+cut]))
		b.flush(false)
		if got := c.joined(); got != "a" {
			t.Errorf("cut %d: emitted %q before the rest arrived, want %q", cut, got, "a")
		}
		if len(b.pending) != cut {
			t.Errorf("cut %d: %d bytes held, want %d", cut, len(b.pending), cut)
		}
		b.add([]byte(in[1+cut:]))
		b.flush(false)
		if got := c.joined(); got != in {
			t.Errorf("cut %d: joined = %q, want %q", cut, got, in)
		}
	}
}

func TestInvalid(t *testing.T) {
	const bad = "�"
	tests := []struct {
		name string
		in   string
		want string // after one final flush, invalid runs replaced by U+FFFD
	}{
		{"ff", "ab\xffcd", "ab" + bad + "cd"},
		{"lone continuation", "ab\x80cd", "ab" + bad + "cd"},
		{"cut char then ASCII", "ab\xe2\x82Acd", "ab" + bad + "Acd"},
		{"cut char then start", "\xe2\x82" + "č", bad + "č"},
		{"overlong", "a\xc0\xafb", "a" + bad + "b"},
		{"only garbage", "\xff\xfe\x80", bad},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c payloads
			b := &batcher{max: testMaxChunk, emit: c.emit}
			b.add([]byte(tc.in))
			b.flush(false)
			// Whatever is held back must be a valid beginning, never garbage:
			// at most the 2-byte prefix of the cut character, and only when it ends the input.
			if len(b.pending) > maxRuneTail {
				t.Errorf("%d bytes held back", len(b.pending))
			}
			b.flush(true)
			if len(b.pending) != 0 {
				t.Errorf("%q left after the final flush", b.pending)
			}
			checkValid(t, c.snapshot())
			if got := c.joined(); got != tc.want {
				t.Errorf("joined = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGarbageNotHeld(t *testing.T) {
	var c payloads
	b := &batcher{max: testMaxChunk, emit: c.emit}
	b.add([]byte("ab\xff"))
	b.flush(false)
	if len(b.pending) != 0 {
		t.Errorf("garbage byte held back: %q", b.pending)
	}
	if got := c.joined(); got != "ab�" {
		t.Errorf("emitted %q", got)
	}
}

func TestInvalidFlow(t *testing.T) {
	// Garbage in the middle of the stream must not stop the valid text around it.
	var c payloads
	in := "before\xff\x80\xe2\x82Zč\xf0\x9f" // ends in a cut emoji
	if err := pump(strings.NewReader(in), time.Hour, testMaxChunk, c.emit); err != nil {
		t.Fatal(err)
	}
	checkValid(t, c.snapshot())
	got := c.joined()
	for _, want := range []string{"before", "Zč"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from %q", want, got)
		}
	}
}

func TestEOFTail(t *testing.T) {
	var c payloads
	// "€" is e2 82 ac; the stream ends after two bytes.
	if err := pump(strings.NewReader("ab\xe2\x82"), time.Hour, testMaxChunk, c.emit); err != nil {
		t.Fatal(err)
	}
	checkValid(t, c.snapshot())
	if got := c.joined(); got != "ab�" {
		t.Errorf("joined = %q, want the tail emitted as U+FFFD", got)
	}
}

func TestSizeLimit(t *testing.T) {
	const maxChunk = 10
	tests := []struct {
		name string
		unit string
	}{
		{"ascii", "x"},
		{"two", "č"},
		{"three", "€"},
		{"four", "😀"},
		{"mixed", "ač€😀"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.Repeat(tc.unit, 60)
			var c payloads
			b := &batcher{max: maxChunk, emit: c.emit}
			b.add([]byte(in))
			if !b.full() {
				t.Fatal("not full")
			}
			b.flush(false)
			got := c.snapshot()
			if len(got) < 2 {
				t.Fatalf("%d payloads, want several", len(got))
			}
			for i, p := range got {
				// The documented rule: max bytes, plus up to 3 to finish a character.
				if len(p) > maxChunk+maxRuneTail {
					t.Errorf("payload %d has %d bytes, limit %d", i, len(p), maxChunk+maxRuneTail)
				}
			}
			checkValid(t, got)
			if joined := c.joined(); joined != in {
				t.Errorf("joined differs from input")
			}
		})
	}
}

func TestSizeNoWait(t *testing.T) {
	// With an hour-long interval only the size rule can flush.
	pr, pw := io.Pipe()
	var c payloads
	done := make(chan error, 1)
	go func() { done <- pump(pr, time.Hour, 10, c.emit) }()

	in := strings.Repeat("x", 100)
	if _, err := pw.Write([]byte(in)); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "size flush", func() bool { return len(c.joined()) == len(in) })
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTimeFlush(t *testing.T) {
	const interval = 10 * time.Millisecond
	pr, pw := io.Pipe()
	var c payloads
	done := make(chan error, 1)
	go func() { done <- pump(pr, interval, testMaxChunk, c.emit) }()

	// Far below the size limit, and the stream stays open: only the timer can send it.
	if _, err := pw.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "timed flush", func() bool { return c.joined() == "hi" })
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := c.snapshot(); len(got) != 1 {
		t.Errorf("payloads = %q, want one", got)
	}
}

func TestEmpty(t *testing.T) {
	var c payloads
	if err := pump(strings.NewReader(""), time.Millisecond, testMaxChunk, c.emit); err != nil {
		t.Fatal(err)
	}
	if got := c.snapshot(); len(got) != 0 {
		t.Errorf("payloads = %q, want none", got)
	}
	b := &batcher{max: 5, emit: c.emit}
	b.flush(false)
	b.flush(true)
	if got := c.snapshot(); len(got) != 0 {
		t.Errorf("empty flush emitted %q", got)
	}
}

func TestPumpBadParams(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		maxChunk int
	}{
		{"zero interval", 0, testMaxChunk},
		{"negative interval", -time.Second, testMaxChunk},
		{"zero max chunk", time.Millisecond, 0},
		{"negative max chunk", time.Millisecond, -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c payloads
			if err := pump(strings.NewReader("data"), tc.interval, tc.maxChunk, c.emit); err == nil {
				t.Error("pump accepted invalid parameters")
			}
			if got := c.snapshot(); len(got) != 0 {
				t.Errorf("payloads = %q, want none", got)
			}
		})
	}
}

func TestPumpError(t *testing.T) {
	boom := errors.New("boom")
	r := io.MultiReader(strings.NewReader("data"), errReader{boom})
	var c payloads
	if err := pump(r, time.Hour, testMaxChunk, c.emit); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
	if got := c.joined(); got != "data" {
		t.Errorf("joined = %q, want the data read before the error", got)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

// slowReader returns its pieces one per Read, pausing between them, so the
// timer flushes in between and the pieces really reach the batcher separately.
type slowReader struct {
	pieces []string
	pause  time.Duration
}

func (r *slowReader) Read(p []byte) (int, error) {
	if len(r.pieces) == 0 {
		return 0, io.EOF
	}
	time.Sleep(r.pause)
	n := copy(p, r.pieces[0])
	r.pieces[0] = r.pieces[0][n:]
	if r.pieces[0] == "" {
		r.pieces = r.pieces[1:]
	}
	return n, nil
}

func TestPumpCuts(t *testing.T) {
	// Same as TestCutEverywhere, through the real read loop and timer.
	in := croatian + emoji
	for i := 1; i < len(in); i++ {
		var c payloads
		r := &slowReader{pieces: []string{in[:i], in[i:]}, pause: 8 * time.Millisecond}
		if err := pump(r, 2*time.Millisecond, testMaxChunk, c.emit); err != nil {
			t.Fatal(err)
		}
		checkValid(t, c.snapshot())
		if got := c.joined(); got != in {
			t.Fatalf("cut at %d: joined = %q, want %q", i, got, in)
		}
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(batchWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}
