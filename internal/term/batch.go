package term

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// readBufSize is how much one PTY read can return.
	readBufSize = 32 << 10

	// maxRuneTail is the longest incomplete (but so far valid) UTF-8 sequence:
	// a 4-byte character cut after its third byte leaves 3 bytes.
	maxRuneTail = utf8.UTFMax - 1
)

// batcher groups terminal output into payloads and keeps every payload valid
// UTF-8 on its own, because the frontend receives payloads as Go strings and
// a character split across two payloads would arrive as two U+FFFD.
//
// Rules:
//   - A payload is at most max bytes of input plus up to 3 bytes: a cut that
//     would fall inside a character is moved forward to the next character start.
//   - A sequence that is a valid beginning of a character but is not complete
//     yet (at most 3 bytes at the end of the data) is held back until more
//     bytes arrive, or until the final flush.
//   - Bytes that can never be part of a valid character are not held back.
//     A run of them is replaced by one U+FFFD, so for invalid input the
//     payloads are not byte-identical to the input and may be longer than
//     the bound above. For valid input the payloads concatenate to the input.
type batcher struct {
	max     int
	emit    func(string)
	pending []byte
}

func (b *batcher) add(p []byte) { b.pending = append(b.pending, p...) }

// full reports whether enough is pending to flush without waiting for the timer.
func (b *batcher) full() bool { return len(b.pending) >= b.max }

// flush emits everything pending except an incomplete character at the end.
// With final set the incomplete tail is emitted too (as U+FFFD): no more
// bytes will come to complete it.
func (b *batcher) flush(final bool) {
	end := len(b.pending)
	if !final {
		end -= incompleteTail(b.pending)
	}
	for pos := 0; pos < end; {
		cut := min(pos+b.max, end)
		// Do not cut a character in two: move to the next character start.
		// Three steps is the most a valid character can need; more
		// continuation bytes in a row are invalid and may be split anywhere.
		for n := 0; cut < end && n < maxRuneTail && !utf8.RuneStart(b.pending[cut]); n++ {
			cut++
		}
		b.emit(sanitize(b.pending[pos:cut]))
		pos = cut
	}
	b.pending = append(b.pending[:0], b.pending[end:]...)
}

// incompleteTail returns how many bytes at the end of p are the start of a
// valid character whose remaining bytes have not arrived yet.
func incompleteTail(p []byte) int {
	for n := 1; n <= maxRuneTail && n <= len(p); n++ {
		i := len(p) - n
		if !utf8.RuneStart(p[i]) {
			continue // a continuation byte; look further back for its start
		}
		// FullRune is false only for a valid prefix of a character. An invalid
		// sequence counts as "full" (it decodes as one error rune), so garbage
		// is never held back.
		if utf8.FullRune(p[i:]) {
			return 0
		}
		return n
	}
	return 0
}

// sanitize makes p valid UTF-8; valid input is returned unchanged.
func sanitize(p []byte) string {
	s := string(p)
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, string(utf8.RuneError))
}

// pump reads r until it ends and calls emit with grouped payloads (see batcher).
// A pending byte waits at most interval; maxChunk pending bytes are flushed at once.
// interval and maxChunk must be positive. When r ends, everything still pending is emitted, then pump returns: nil
// for io.EOF, otherwise the read error. emit is only called from the calling
// goroutine, one call at a time.
//
// pump blocks until r returns, so the owner must make r end (for a PTY:
// close the process) for pump to return.
func pump(r io.Reader, interval time.Duration, maxChunk int, emit func(string)) error {
	// No defaults here: the numbers live in internal/config and arrive through
	// Config, so a zero value is a bug of the caller, not something to guess.
	if interval <= 0 || maxChunk < 1 {
		return fmt.Errorf("term: invalid batching parameters (interval %v, max chunk %d)", interval, maxChunk)
	}
	type readResult struct {
		data []byte
		err  error
	}
	// Reading happens in its own goroutine so that the timer can fire while
	// a Read is blocked. It ends right after r returns an error.
	results := make(chan readResult)
	go func() {
		defer close(results)
		buf := make([]byte, readBufSize)
		for {
			n, err := r.Read(buf)
			// Copy: buf is reused while the batcher may still hold the bytes.
			if n > 0 {
				results <- readResult{data: append([]byte(nil), buf[:n]...)}
			}
			if err != nil {
				results <- readResult{err: err}
				return
			}
		}
	}()

	b := &batcher{max: maxChunk, emit: emit}
	timer := time.NewTimer(interval)
	timer.Stop()
	defer timer.Stop()
	armed := false

	for {
		select {
		case res := <-results:
			if res.err != nil {
				b.flush(true)
				if errors.Is(res.err, io.EOF) {
					return nil
				}
				return res.err
			}
			b.add(res.data)
			if b.full() {
				b.flush(false)
				timer.Stop()
				armed = false
			} else if !armed && len(b.pending) > 0 {
				// The clock starts when the first not-yet-emitted byte arrives.
				timer.Reset(interval)
				armed = true
			}
		case <-timer.C:
			armed = false
			b.flush(false)
		}
	}
}
