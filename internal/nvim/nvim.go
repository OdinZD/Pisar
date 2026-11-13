// Package nvim runs a real Neovim in a PTY and controls it over msgpack-RPC.
//
// Two channels connect to the process. The PTY carries the screen and the
// keystrokes (an xterm.js terminal will read and write it). The RPC unix
// socket carries commands and notifications. Both belong to one Process.
package nvim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/neovim/go-client/msgpack/rpc"
	nvimrpc "github.com/neovim/go-client/nvim"
)

const (
	// connectPollInterval is how often Start retries the socket until Neovim listens.
	connectPollInterval = 25 * time.Millisecond

	// serverExitGrace is how long the Neovim server process may outlive the
	// TUI process before it is killed. A responsive server exits within
	// milliseconds of the TUI's end; one stuck in a busy loop never will.
	serverExitGrace = 500 * time.Millisecond
	serverPollEvery = 10 * time.Millisecond

	// maxSocketPath is the longest unix socket path that fits sun_path.
	// macOS declares char sun_path[104] in <sys/un.h> (the NUL takes one byte).
	// Linux has 108, so this limit is safe on both.
	maxSocketPath = 103

	// Neovim's TUI reads $TERM for key and escape-sequence handling (:help $TERM).
	// xterm.js speaks xterm, so "xterm-256color" is the exact match; Neovim has
	// a builtin entry for it even without a terminfo database. COLORTERM=truecolor
	// is the documented signal (:help tui-colors) that 24-bit colour works,
	// which xterm.js supports.
	termValue      = "xterm-256color"
	colorTermValue = "truecolor"

	// Names shared with nvim/init.lua.
	notifyGroup    = "PisarNotify"
	channelVar     = "pisar_channel"
	savedMethod    = "pisar_saved"
	enteredMethod  = "pisar_entered"
	modifiedMethod = "pisar_modified"
	focusMethod    = "pisar_focus"
	byeMethod      = "pisar_bye"
	changedMethod  = "pisar_changed"
	previewMethod  = "pisar_preview"
	searchMethod   = "pisar_search"
	// keysMethod is the notification of <leader>?. It is not the global Lua
	// function _G.pisar_keys: that one is called by Go, this one is sent to it.
	keysMethod = "pisar_keys"
	previewVar = "pisar_preview"
	// textEnv carries the interface texts to init.lua as JSON (Options.Text). The
	// name is shared with nvim/init.lua.
	textEnv = "PISAR_TEXT"

	// promptMarker is what the Lua chunks raise for ErrPromptOpen; call turns it
	// into the error, so that no sentence for the user is spelled out here.
	promptMarker = "pisar:prompt-open"

	socketPrefix = "n" // short on purpose: a socket path may be at most maxSocketPath bytes
)

// EventKind identifies the type of an Event.
type EventKind int

const (
	EventSaved           EventKind = iota + 1 // a buffer was written; Path is its file
	EventBufferEntered                        // another buffer became current; Path is its file, "" if it has none
	EventModifiedChanged                      // the 'modified' state of a buffer changed; Path, Modified
	EventFocusTree                            // the user asked for the project tree (<leader>e)
	EventChanged                              // the text of the current buffer changed, or another buffer became current; only while SetPreview(true)
	EventTogglePreview                        // the user asked to switch the preview (<leader>p)
	EventOpenSearch                           // the user asked for the document search (<leader>f)
	EventOpenShortcuts                        // the user asked for the overview of shortcuts (<leader>?)
)

// Event is a notification from Neovim.
type Event struct {
	Kind     EventKind
	Path     string // absolute path as Neovim reports it; "" for a buffer without a file
	Modified bool   // for EventBufferEntered and EventModifiedChanged
}

// Buffer describes one buffer.
type Buffer struct {
	Path     string // "" for an unnamed buffer
	Modified bool
}

var (
	// ErrNotRunning is returned (wrapped) once the Neovim process is gone.
	ErrNotRunning = errors.New("nvim: process is not running")
	// ErrSocketPathTooLong is returned by Start when the socket path would not fit sun_path.
	ErrSocketPathTooLong = errors.New("nvim: socket path too long")
	// ErrUnnamedBuffer is returned (wrapped) by SaveAll when a modified buffer has no file to write to.
	ErrUnnamedBuffer = errors.New("nvim: a modified buffer has no file name")
	// ErrPromptOpen is returned (wrapped) by Save, SaveAll, Wipe and Reload while
	// Neovim waits for the answer to a question (a prompt, hit-enter, "-- More --"):
	// the call is refused and changes nothing. The sentence the user
	// reads is the interface's (the nvimPromptOpen text), chosen by whoever shows
	// the error; the text of this one is for logs.
	ErrPromptOpen = errors.New("nvim: refused while a prompt is open")
	// ErrBufferModified is returned (wrapped) by Wipe for a buffer with unsaved changes.
	ErrBufferModified = errors.New("nvim: buffer has unsaved changes")
	// ErrBufferTooLarge is returned (wrapped) by BufferText when the text is over the limit it was given.
	ErrBufferTooLarge = errors.New("nvim: buffer text is too large")
	// ErrNoBuffer is returned (wrapped) by SetReadOnly and Reload when Neovim has no buffer for the file.
	ErrNoBuffer = errors.New("nvim: no buffer for that file")
)

// Options configures Start. Cols, Rows, StartTimeout and StopTimeout are
// required: their defaults live in internal/config, not here.
type Options struct {
	Bin          string        // nvim binary name or path; resolved with exec.LookPath
	InitLua      string        // absolute path, passed as `-u`
	AppName      string        // NVIM_APPNAME (isolates config/data/state from the user's own Neovim)
	SocketDir    string        // existing directory owned by the caller; the socket file is created inside it
	Cols, Rows   uint16        // initial terminal size; required (> 0)
	Env          []string      // extra "K=V" entries, applied after os.Environ() and after the package's own
	StartTimeout time.Duration // max wait until Neovim is up; required (> 0)
	StopTimeout  time.Duration // max wait per shutdown stage; required (> 0)
	// Text holds the interface texts this package and init.lua show to the user,
	// by their keys in frontend/src/strings.json (the keys that start with
	// "nvim"). Neovim gets them as JSON in the environment variable PISAR_TEXT,
	// because init.lua needs them while it loads, before Go is connected: they
	// are the descriptions of the mappings and the sentences of the formatting
	// message. The sentences about user.lua are put together here. Optional: a
	// text that is missing leaves a mapping without a description and a message
	// without its frame (only the facts), which is what a test that is not about
	// the wording needs. Invalid JSON, which only a hand-made PISAR_TEXT can
	// give, is the same as none.
	Text map[string]string
}

func (o Options) validate() error {
	switch {
	case o.Cols == 0 || o.Rows == 0:
		return fmt.Errorf("nvim: invalid initial size %dx%d", o.Cols, o.Rows)
	case o.StartTimeout <= 0:
		return fmt.Errorf("nvim: invalid start timeout %v", o.StartTimeout)
	case o.StopTimeout <= 0:
		return fmt.Errorf("nvim: invalid stop timeout %v", o.StopTimeout)
	case o.InitLua == "":
		// Without init.lua no autocmd exists, so Start could never finish.
		return errors.New("nvim: init.lua path is required")
	}
	return nil
}

// termAnswers are the terminal queries the pump answers itself, because the
// UI that reads our PTY (xterm.js) may not be attached when Neovim asks, and
// Neovim stalls on unanswered ones:
//
//   - At startup Neovim 0.12 asks for the background colour followed by a
//     status report (DSR, "\x1b[5n") and waits 100 ms for the report. Without
//     one it prints a warning ending in a hit-enter prompt, and startup stops
//     until a key arrives. "\x1b[0n" is "terminal OK".
//   - At startup and at exit it sends primary device attributes ("\x1b[c")
//     as the end marker of its capability queries and waits up to a second
//     for the reply, which would delay every shutdown by that second.
//     "\x1b[?1;2c" is the VT100 answer; claiming nothing extra is right,
//     since Neovim then falls back to plain features.
//   - At startup Neovim sends "CSI 4:3 m" (curly underline) followed by the
//     query DECRQSS "m" ("\x1bP$qm\x1b\\") and reads from the answer whether the
//     terminal can draw it: only an answer that contains "4:3" turns the curly
//     underline (and the underline colour, SGR 58) on; anything else gives a
//     plain straight underline in the text colour, which loses the wavy red
//     line of a misspelled word (measured with Neovim 0.12, see writing_test.go).
//     xterm.js 6.0.0 answers "0m" to this query whatever was set (its
//     requestStatusString marks the SGR report as broken), although its DOM
//     renderer draws style 3 as a wavy underline. When both answers arrive,
//     in either order, the one with "4:3" wins, so the later answer of
//     xterm.js changes nothing.
//
// A later duplicate answer from xterm.js is harmless.
var termAnswers = []struct{ query, reply string }{
	{"\x1b[5n", "\x1b[0n"},
	{"\x1b[c", "\x1b[?1;2c"},
	{"\x1bP$qm\x1b\\", "\x1bP1$r0;4:3m\x1b\\"},
}

// queryScanner finds the queries of termAnswers in PTY output that arrives in
// pieces of any size: a query may be cut between two reads.
type queryScanner struct {
	window []byte // the end of the output so far, long enough to hold the start of a cut query
}

// feed adds the next piece of output and returns the answers to the queries
// that end in it.
func (q *queryScanner) feed(data []byte) []string {
	tail := len(q.window)
	q.window = append(q.window, data...)
	var replies []string
	for _, a := range termAnswers {
		for i := 0; i+len(a.query) <= len(q.window); i++ {
			// Only matches ending in the new data, or the tail would be answered twice.
			if i+len(a.query) > tail && string(q.window[i:i+len(a.query)]) == a.query {
				replies = append(replies, a.reply)
			}
		}
	}
	keep := 0
	for _, a := range termAnswers {
		keep = max(keep, len(a.query)-1)
	}
	if len(q.window) > keep {
		q.window = append(q.window[:0], q.window[len(q.window)-keep:]...)
	}
	return replies
}

// socketSeq makes socket names unique within this process; the pid in the
// name keeps them unique across processes sharing a directory.
var socketSeq atomic.Uint64

// Process is a running Neovim. All methods are safe for concurrent use.
// Callers must call Close even after Neovim exited on its own: Close
// releases the PTY and the output pipe, which stay readable until then so
// the final output is not lost.
type Process struct {
	cmd         *exec.Cmd
	pty         *os.File // PTY master; Write and Resize use it, only pump reads it
	out         *os.File // read end of the pipe pump forwards PTY output into
	pumpDone    chan struct{}
	sock        string
	stopTimeout time.Duration
	texts       map[string]string // Options.Text

	done   chan struct{} // closed once the process has been reaped
	reaped chan struct{} // closed when all RPC resources have been released

	mu        sync.Mutex // guards nv, serveDone, serverPid, user, torn and calls admission
	nv        *nvimrpc.Nvim
	serveDone chan struct{}
	serverPid int // Neovim's server process; 0 until known
	user      UserConfig
	torn      bool
	calls     sync.WaitGroup // in-flight RPC calls

	events   chan Event
	evMu     sync.Mutex
	evClosed bool

	closeMu   sync.Mutex // serializes Close
	closed    bool
	drainOnce sync.Once
	drain     sync.WaitGroup

	// stoppedBy records which Close stage ended the process (1 RPC, 2 SIGTERM,
	// 3 SIGKILL); 0 means it exited on its own. Tests use it to prove escalation.
	stoppedBy atomic.Int32
}

// Start launches Neovim in a PTY and connects to its RPC socket.
//
// ctx bounds only the startup. It is deliberately not tied to the process
// lifetime: a caller passing a timeout context to Start must not see Neovim
// killed when that context is cancelled later. Use Close to stop it.
// On any error the process is stopped and cleaned up before Start returns.
func Start(ctx context.Context, opts Options) (*Process, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("nvim: start: %w", err)
	}

	bin, err := exec.LookPath(opts.Bin)
	if err != nil {
		return nil, fmt.Errorf("nvim: find binary: %w", err)
	}
	if fi, err := os.Stat(opts.SocketDir); err != nil {
		return nil, fmt.Errorf("nvim: socket dir: %w", err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("nvim: socket dir %q is not a directory", opts.SocketDir)
	}
	// Base 36 keeps the name short: the path limit of a Unix socket is tight
	// once the directory is a long temporary one (as in tests).
	sock := filepath.Join(opts.SocketDir, socketPrefix+
		strconv.FormatInt(int64(os.Getpid()), 36)+"-"+strconv.FormatUint(socketSeq.Add(1), 36)+".sock")
	if len(sock) > maxSocketPath {
		return nil, fmt.Errorf("%w: %d bytes, limit %d: %s", ErrSocketPathTooLong, len(sock), maxSocketPath, sock)
	}

	// Arguments are separate elements, never a shell string.
	args := []string{"--listen", sock, "-u", opts.InitLua}
	cmd := exec.Command(bin, args...)
	env := os.Environ()
	if len(opts.Text) > 0 {
		// Marshalling a map of strings cannot fail.
		text, _ := json.Marshal(opts.Text)
		env = append(env, textEnv+"="+string(text))
	}
	if opts.AppName != "" {
		env = append(env, "NVIM_APPNAME="+opts.AppName)
	}
	// exec.Cmd keeps the last value of a duplicated key, so opts.Env wins.
	cmd.Env = append(append(env, "TERM="+termValue, "COLORTERM="+colorTermValue), opts.Env...)

	// StartWithSize runs the child in a new session (Setsid), so it leads its
	// own process group with pgid == pid. That group holds only the TUI
	// process: the server process it spawns (the one behind the RPC socket)
	// starts its own session, so it needs its own pid for signalling.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: opts.Rows, Cols: opts.Cols})
	if err != nil {
		return nil, fmt.Errorf("nvim: start process: %w", err)
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("nvim: create output pipe: %w", err)
	}

	p := &Process{
		cmd:         cmd,
		pty:         ptmx,
		out:         outR,
		pumpDone:    make(chan struct{}),
		sock:        sock,
		stopTimeout: opts.StopTimeout,
		texts:       opts.Text,
		done:        make(chan struct{}),
		reaped:      make(chan struct{}),
		events:      make(chan Event, eventBuffer),
	}
	// The reaper must exist from the first instant so no failure path can leave a zombie.
	go p.reap()
	go p.pump(outW)

	if err := p.connect(ctx, opts.StartTimeout); err != nil {
		// Fresh context: ctx may be the very reason Start failed.
		if cerr := p.Close(context.Background()); cerr != nil {
			err = errors.Join(err, cerr)
		}
		return nil, err
	}
	return p, nil
}

// connect dials the socket, registers handlers and waits until init.lua ran.
func (p *Process) connect(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var nv *nvimrpc.Nvim
	for nv == nil {
		var err error
		nv, err = nvimrpc.Dial(p.sock,
			nvimrpc.DialContext(ctx),
			nvimrpc.DialServe(false), // we run Serve ourselves to know when it ends
			nvimrpc.DialLogf(func(string, ...interface{}) {}))
		if err == nil {
			break
		}
		nv = nil
		select {
		case <-ctx.Done():
			return fmt.Errorf("nvim: connect to %s: %w (last dial error: %w)", p.sock, ctx.Err(), err)
		case <-p.done:
			return fmt.Errorf("nvim: process exited before the RPC socket was ready: %w", ErrNotRunning)
		case <-time.After(connectPollInterval):
		}
	}

	// One notification stream on one channel: Neovim sends in the order things
	// happened and go-client runs handlers one at a time in that order, so the
	// events of a buffer keep their order.
	handlers := map[string]interface{}{
		savedMethod: func(path string) { p.emit(Event{Kind: EventSaved, Path: path}) },
		enteredMethod: func(path string, modified bool) {
			p.emit(Event{Kind: EventBufferEntered, Path: path, Modified: modified})
		},
		modifiedMethod: func(path string, modified bool) {
			p.emit(Event{Kind: EventModifiedChanged, Path: path, Modified: modified})
		},
		focusMethod:   func() { p.emit(Event{Kind: EventFocusTree}) },
		changedMethod: func() { p.emit(Event{Kind: EventChanged}) },
		previewMethod: func() { p.emit(Event{Kind: EventTogglePreview}) },
		searchMethod:  func() { p.emit(Event{Kind: EventOpenSearch}) },
		keysMethod:    func() { p.emit(Event{Kind: EventOpenShortcuts}) },
		byeMethod:     p.closeEvents,
	}
	for method, h := range handlers {
		if err := nv.RegisterHandler(method, h); err != nil {
			_ = nv.Close()
			return fmt.Errorf("nvim: register handler %s: %w", method, err)
		}
	}

	serveDone := make(chan struct{})
	p.mu.Lock()
	if p.torn {
		p.mu.Unlock()
		_ = nv.Close()
		return fmt.Errorf("nvim: process exited during startup: %w", ErrNotRunning)
	}
	p.nv, p.serveDone = nv, serveDone
	p.mu.Unlock()
	go func() {
		defer close(serveDone)
		// Serve only returns when the connection ends; the reason (EOF at
		// shutdown, usually) is of no use to callers, who see ErrNotRunning.
		_ = nv.Serve()
	}()

	// The socket opens before init.lua has run. Without waiting, an early
	// write could happen before the BufWritePost autocmd exists and its
	// event would be lost for good.
	for {
		var state int
		err := p.execLua(ctx, "wait for startup", &state, `
			if vim.v.vim_did_enter ~= 1 then return 0 end
			local ok, acs = pcall(vim.api.nvim_get_autocmds, { group = ..., event = "BufWritePost" })
			if ok and #acs > 0 then return 2 end
			return 1`, notifyGroup)
		if err != nil {
			return err
		}
		if state == 1 {
			return errors.New("nvim: init.lua did not register the " + notifyGroup + " autocmd group")
		}
		if state == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("nvim: wait for startup: %w", ctx.Err())
		case <-time.After(connectPollInterval):
		}
	}

	var pid int
	if err := p.execLua(ctx, "server pid", &pid, `return vim.fn.getpid()`); err != nil {
		return err
	}
	p.mu.Lock()
	p.serverPid = pid
	p.mu.Unlock()

	return p.readUserConfig(ctx)
}

// reap waits for the process, then releases everything that depends on it.
func (p *Process) reap() {
	defer close(p.reaped)
	// The exit status is not an error for us: a forced stop is a signal exit.
	_ = p.cmd.Wait()
	// Sweep stragglers. While any member of the group lives its pgid cannot
	// be reused, so the signal cannot hit an unrelated process.
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	p.reapServer()
	close(p.done)

	p.mu.Lock()
	p.torn = true // no new RPC calls from here on
	nv, serveDone := p.nv, p.serveDone
	p.mu.Unlock()

	if nv != nil {
		// Closing the endpoint fails pending calls with rpc.ErrClosed and ends Serve.
		_ = nv.Close()
		<-serveDone
	}
	p.calls.Wait()
	// Neovim removes the socket on a clean exit; after a kill it stays behind.
	_ = os.Remove(p.sock)
	p.closeEvents()
}

// reapServer makes sure the server process is gone. Normally it exits on its
// own when the TUI process ends; a server stuck in a busy loop would not,
// and would be left running forever, so it gets SIGKILL after a short grace.
func (p *Process) reapServer() {
	p.mu.Lock()
	pid := p.serverPid
	p.mu.Unlock()
	if pid == 0 {
		return
	}
	// A dead process' pid cannot be reused within these few hundred
	// milliseconds in any practical sense (pids are handed out sequentially).
	deadline := time.Now().Add(serverExitGrace)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(serverPollEvery)
	}
	if !processAlive(pid) {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	for processAlive(pid) { // SIGKILL cannot be handled; this ends as soon as the kernel reaps it
		time.Sleep(serverPollEvery)
	}
}

func processAlive(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// Read reads PTY output. It returns io.EOF once the process has exited and
// the remaining output has been consumed.
func (p *Process) Read(b []byte) (int, error) {
	n, err := p.out.Read(b)
	if errors.Is(err, os.ErrClosed) {
		err = io.EOF
	}
	return n, err
}

// pump moves PTY output into the pipe Read consumes, and answers the queries
// in termAnswers. It ends when the PTY ends (the master reports EIO or EOF
// once the slave side is gone), which closes the pipe so Read returns io.EOF
// after the buffered output. A full pipe, with nobody reading, stalls the pump
// and thus Neovim, exactly like an unread terminal would.
func (p *Process) pump(w *os.File) {
	defer close(p.pumpDone)
	defer func() {
		// Closing a pipe end we own cannot fail in a way that matters;
		// Read reports EOF either way.
		_ = w.Close()
	}()

	buf := make([]byte, 32<<10)
	var scan queryScanner
	for {
		n, err := p.pty.Read(buf)
		if n > 0 {
			for _, reply := range scan.feed(buf[:n]) {
				// If this fails the PTY is going away, which the pump sees on its next read.
				_, _ = p.pty.Write([]byte(reply))
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return // Close closed the read end
			}
		}
		if err != nil {
			return
		}
	}
}

// Write writes keystrokes to the PTY.
func (p *Process) Write(b []byte) (int, error) {
	if p.exited() {
		return 0, fmt.Errorf("write to pty: %w", ErrNotRunning)
	}
	n, err := p.pty.Write(b)
	if err != nil && (errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EIO)) {
		// EIO: the slave side is gone, i.e. the process is exiting.
		err = fmt.Errorf("write to pty: %w", ErrNotRunning)
	}
	return n, err
}

// Resize sets the terminal size; Neovim learns about it through SIGWINCH,
// so the change is asynchronous.
func (p *Process) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return fmt.Errorf("nvim: resize: invalid size %dx%d", cols, rows)
	}
	if p.exited() {
		return fmt.Errorf("resize: %w", ErrNotRunning)
	}
	if err := pty.Setsize(p.pty, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		if errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("resize: %w", ErrNotRunning)
		}
		return fmt.Errorf("nvim: resize: %w", err)
	}
	return nil
}

// OpenFile edits path in the current window. A relative path is resolved
// against this process's working directory. It fails like :edit does, for
// example when the current buffer has unsaved changes.
func (p *Process) OpenFile(ctx context.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("nvim: open file: %w", err)
	}
	// The path travels as a structured argument, and magic.file/magic.bar
	// switch off %, # and wildcard expansion and `|` splitting, so the name
	// is taken literally (:help nvim_cmd()).
	//
	// The :redraw is not cosmetic. Some failures leave messages on the screen
	// that open a hit-enter prompt only after the call has returned, which
	// freezes Neovim (measured with Neovim 0.12: E303, swap directory not
	// writable, at any width). Redrawing inside the call settles the messages
	// first. The error is re-raised unchanged, so Go still sees it.
	return p.execLua(ctx, "open file", nil, `
		local ok, err = pcall(vim.cmd, { cmd = "edit", args = { (...) }, magic = { file = false, bar = false } })
		vim.cmd.redraw()
		if not ok then
			error(err, 0)
		end`, abs)
}

// bufferLua defines pathOf for the chunks below: the file of a buffer, or ""
// when it has none or is not an ordinary file (help, quickfix, terminal...),
// the same rule init.lua applies to events.
const bufferLua = `
	local function pathOf(b)
		if vim.bo[b].buftype ~= "" then return "" end
		return vim.api.nvim_buf_get_name(b)
	end`

// findLua defines buffersOf(file), the buffers whose name is file, and needs
// bufferLua. Neovim stores a buffer name with the symbolic links of its folder
// resolved (/private/var/... for /var/...) and the file name as given, so both
// sides are brought to that form. For a folder that is gone, the nearest one
// that exists is resolved (vim.uv.fs_realpath returns nil for a path that does
// not exist; measured with Neovim 0.12).
const findLua = `
	local function normalize(file)
		local dir, tail = vim.fs.dirname(file), vim.fs.basename(file)
		while true do
			local real = vim.uv.fs_realpath(dir)
			if real then return vim.fs.joinpath(real, tail) end
			local parent = vim.fs.dirname(dir)
			if parent == dir then return file end
			tail = vim.fs.joinpath(vim.fs.basename(dir), tail)
			dir = parent
		end
	end

	local function buffersOf(file)
		local want = normalize(file)
		local hits = {}
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			local name = pathOf(b)
			if name ~= "" and normalize(name) == want then
				table.insert(hits, b)
			end
		end
		return hits
	end`

// Current returns the current buffer. Path is "" for an unnamed buffer and for
// one that is not an ordinary file.
func (p *Process) Current(ctx context.Context) (Buffer, error) {
	var b Buffer
	// The keys of the returned table are the field names of Buffer, which is
	// how the msgpack decoder fills the struct.
	err := p.execLua(ctx, "current buffer", &b, bufferLua+`
		local b = vim.api.nvim_get_current_buf()
		return { Path = pathOf(b), Modified = vim.bo[b].modified }`)
	if err != nil {
		return Buffer{}, err
	}
	return b, nil
}

// Modified returns every listed buffer of an ordinary kind that has unsaved
// changes, ordered by buffer number (nvim_list_bufs order).
func (p *Process) Modified(ctx context.Context) ([]Buffer, error) {
	var bufs []Buffer
	err := p.execLua(ctx, "modified buffers", &bufs, bufferLua+`
		local out = {}
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.bo[b].buflisted and vim.bo[b].buftype == "" and vim.bo[b].modified then
				table.insert(out, { Path = pathOf(b), Modified = true })
			end
		end
		return out`)
	if err != nil {
		return nil, err
	}
	return bufs, nil
}

// Buffers returns every listed buffer that has a file, in buffer-number order
// (nvim_list_bufs order). Unnamed buffers and those that are not ordinary files
// (help, quickfix, terminal...) are left out.
func (p *Process) Buffers(ctx context.Context) ([]Buffer, error) {
	var bufs []Buffer
	err := p.execLua(ctx, "list buffers", &bufs, bufferLua+`
		local out = {}
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.bo[b].buflisted then
				local name = pathOf(b)
				if name ~= "" then
					table.insert(out, { Path = name, Modified = vim.bo[b].modified })
				end
			end
		end
		return out`)
	if err != nil {
		return nil, err
	}
	return bufs, nil
}

// Wipe removes the buffer that holds the file at path, if there is one, and
// reports whether it was the current buffer. A buffer with unsaved changes is
// not wiped: the error is ErrBufferModified. No buffer for that file: (false, nil).
//
// The buffer is wiped, not just deleted (:help :bwipeout, :help :bdelete):
// :bdelete leaves the buffer in the buffer list as an unlisted one that keeps
// its name, so ":edit #" or an alternate-file mapping would bring the old name
// back; wiping forgets it completely. Wiping the current buffer needs no
// special care: Neovim shows the alternate or another buffer in its window,
// or a new empty one when it was the last, and never closes the last window
// (measured with Neovim 0.12). BufEnter then fires for the buffer that took
// its place, which is how Go learns about it (see init.lua).
//
// The file is found by name, not by what is on disk: it may have been renamed
// or trashed just before the call, so bufnr(path), which compares file
// identities when the file exists, cannot be relied on.
func (p *Process) Wipe(ctx context.Context, path string) (wasCurrent bool, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("nvim: wipe buffer: %w", err)
	}
	var res struct {
		Modified bool
		Current  bool
	}
	err = p.execLua(ctx, "wipe buffer", &res, bufferLua+findLua+`
		-- While a prompt is up the current buffer may be half loaded (see Save).
		if vim.api.nvim_get_mode().mode:sub(1, 1) == "r" then
			error("`+promptMarker+`", 0)
		end

		local current = vim.api.nvim_get_current_buf()
		local hits = buffersOf((...))
		-- Checked for all before anything is removed, and without force: a
		-- buffer with unsaved changes would otherwise be asked about (or lost).
		for _, b in ipairs(hits) do
			if vim.bo[b].modified then
				return { Modified = true, Current = false }
			end
		end
		local wasCurrent = false
		for _, b in ipairs(hits) do
			wasCurrent = wasCurrent or b == current
			vim.api.nvim_buf_delete(b, {})
		end
		return { Modified = false, Current = wasCurrent }`, abs)
	if err != nil {
		return false, err
	}
	if res.Modified {
		return false, fmt.Errorf("%w: %s", ErrBufferModified, path)
	}
	return res.Current, nil
}

// SetReadOnly makes the buffer that holds the file at path read-only or
// writable again. Read-only means 'nomodifiable' and 'readonly': nothing can
// change the text, not even a call to the API (it fails with E21), and :w says
// E45 (:w! still writes the unchanged text, which is harmless). A buffer
// that is not current and one that is hidden are treated alike. No buffer for
// that file: ErrNoBuffer (wrapped).
func (p *Process) SetReadOnly(ctx context.Context, path string, readOnly bool) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("nvim: set read-only: %w", err)
	}
	var found bool
	err = p.execLua(ctx, "set read-only", &found, bufferLua+findLua+`
		local file, readOnly = ...
		local hits = buffersOf(file)
		for _, b in ipairs(hits) do
			vim.bo[b].modifiable = not readOnly
			vim.bo[b].readonly = readOnly
		end
		return #hits > 0`, abs, readOnly)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("nvim: set read-only: %w: %s", ErrNoBuffer, path)
	}
	return nil
}

// Reload reads the file at path again into its buffer and drops the buffer's
// changes, as :edit! does, whether or not the buffer is current: the file was
// replaced behind the buffer's back (the working copy of a Word document that
// was reloaded from the user's file). A read-only buffer is reloaded too and
// stays read-only; Reload does not choose the new state, SetReadOnly does. No
// buffer for that file: ErrNoBuffer (wrapped), which the caller may take as
// "nothing to reload".
func (p *Process) Reload(ctx context.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("nvim: reload: %w", err)
	}
	var found bool
	// The :redraw is there for the same reason as in OpenFile: messages left on
	// the screen must be settled inside the call, or a hit-enter prompt opens
	// after it and freezes Neovim.
	err = p.execLua(ctx, "reload", &found, bufferLua+findLua+`
		-- While a prompt is up the buffer may be half loaded (see Save).
		if vim.api.nvim_get_mode().mode:sub(1, 1) == "r" then
			error("`+promptMarker+`", 0)
		end
		local hits = buffersOf((...))
		-- nvim_buf_call makes a background buffer current for a moment; init.lua
		-- must not announce that as the user entering it. Cleared right after the
		-- pcall, so a failed :edit! cannot leave it set.
		vim.g.pisar_reloading = true
		local ok, err = pcall(function()
			for _, b in ipairs(hits) do
				-- :edit! of a buffer that is not modifiable fails with E21.
				local modifiable, readonly = vim.bo[b].modifiable, vim.bo[b].readonly
				vim.bo[b].modifiable = true
				local eok, eerr = pcall(vim.api.nvim_buf_call, b, function()
					vim.cmd({ cmd = "edit", bang = true })
				end)
				vim.bo[b].modifiable, vim.bo[b].readonly = modifiable, readonly
				if not eok then error(eerr, 0) end
			end
		end)
		vim.g.pisar_reloading = false
		vim.cmd.redraw()
		if not ok then
			error(err, 0)
		end
		return #hits > 0`, abs)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("nvim: reload: %w: %s", ErrNoBuffer, path)
	}
	return nil
}

// messageLua defines msg_width(), fit(text, max) and show(text) for the chunks
// that report errors to the user, and framed(template, name, value), which fills
// in a text. They are the functions of init.lua
// (_G.pisar_msg), taken by name: a message that does not fit on the message line
// opens a hit-enter prompt, which inside an RPC call freezes Neovim, so there is
// one copy of the cutting and its reasons are written down there. Only the
// screen copy is cut; callers hand the full text to Go. This is the only place
// in this package that puts text on Neovim's message line, and it holds no
// sentence of its own: the sentences are Options.Text.
const messageLua = `
	local msg_width, fit, show, framed = pisar_msg.width, pisar_msg.fit, pisar_msg.show, pisar_msg.framed`

// promptGuardLua defines refuseDuringPrompt(), which Save and SaveAll call
// before they write anything. While a prompt is up (mode() starts with "r":
// hit-enter, "-- More --", or a question like the swap file one) the current
// buffer may be a half-loaded one: with the swap file question open, :write
// wrote that empty buffer over the real file (measured with Neovim 0.12; the
// question is asked inside :edit, after the buffer exists but before it has its
// text). Refusing is the only safe answer, and it is the same error
// (ErrPromptOpen) for Save, SaveAll, Wipe and Reload.
//
// Measured for SaveAll too (Neovim 0.12, swap question open, mode "r?"): the
// half-loaded buffer is not 'modified' (one empty line, modified = false), so
// SaveAll would not have written it. It did write every other modified buffer
// in the middle of the question, though, and a question can end in a state
// nobody has looked at, so SaveAll is refused all the same.
const promptGuardLua = `
	local function refuseDuringPrompt()
		if vim.api.nvim_get_mode().mode:sub(1, 1) == "r" then
			error("` + promptMarker + `", 0)
		end
	end`

// Save writes the current buffer to its file (what ⌘S calls). It works in any
// mode and leaves the mode alone. When the write fails (E32 for an unnamed
// buffer, for instance) the error is returned with its full text and a version
// cut to the screen width is shown in Neovim, so the user sees why ⌘S did
// nothing; the shortened text stays in :messages.
func (p *Process) Save(ctx context.Context) error {
	// The success message ("name" 3L, 20B written) is shortened by Neovim itself
	// ('shortmess' t, which init.lua pins); only errors need show. The error is
	// re-raised without a position prefix so Go sees the original text.
	return p.execLua(ctx, "save", nil, messageLua+promptGuardLua+`
		refuseDuringPrompt()
		local ok, err = pcall(vim.cmd.write)
		if ok then
			vim.cmd.redraw()
		else
			show(err)
			error(err, 0)
		end`)
}

// SaveAll writes every modified buffer that has a file, including buffers in
// the background, without changing the current buffer or the mode. A modified
// buffer without a file cannot be written: the others are still saved and the
// returned error satisfies errors.Is(err, ErrUnnamedBuffer). Write failures
// (a read-only file, for instance) are returned too, and every problem is
// shown in Neovim like Save does. While a prompt is up nothing is written and
// the error is Save's (see promptGuardLua).
func (p *Process) SaveAll(ctx context.Context) error {
	var res struct {
		Unnamed int
		Errors  []string
	}
	// :wall is not used: it skips read-only buffers without an error. Writing
	// one buffer at a time with nvim_buf_call needs no BufEnter, and keeps each
	// failure separate. Each problem is shown with show (see messageLua), one
	// at a time; the full texts go to Go in the returned error.
	err := p.execLua(ctx, "save all", &res, messageLua+promptGuardLua+`
		refuseDuringPrompt()
		local errs, unnamed = {}, 0
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.bo[b].buflisted and vim.bo[b].buftype == "" and vim.bo[b].modified then
				local name = vim.api.nvim_buf_get_name(b)
				if name == "" then
					unnamed = unnamed + 1
					-- Without the text (Options.Text) nothing is said here: Go
					-- returns ErrUnnamedBuffer either way.
					local template = ...
					if template and template ~= "" then
						show(framed(template, "buffer", tostring(b)))
					end
				else
					-- pcall inside the callback: the error keeps its own text
					-- instead of being wrapped by nvim_buf_call.
					local err
					local ok, callErr = pcall(vim.api.nvim_buf_call, b, function()
						local wok, werr = pcall(vim.cmd.write)
						if not wok then err = werr end
					end)
					if not ok then err = callErr end
					if err then
						err = tostring(err):gsub("^Vim%(write%):", "")
						table.insert(errs, name .. ": " .. err)
						show(vim.fn.fnamemodify(name, ":t") .. ": " .. err)
					end
				end
			end
		end
		return { Unnamed = unnamed, Errors = errs }`, p.texts["nvimNoFileName"])
	if err != nil {
		return err
	}
	var errs []error
	if res.Unnamed > 0 {
		errs = append(errs, fmt.Errorf("%w (%d)", ErrUnnamedBuffer, res.Unnamed))
	}
	if len(res.Errors) > 0 {
		errs = append(errs, fmt.Errorf("nvim: save all: %s", strings.Join(res.Errors, "; ")))
	}
	return errors.Join(errs...)
}

// CopyVisual yanks the current visual selection into the "+ register (the
// system clipboard, through Neovim's clipboard provider) and leaves visual
// mode, exactly as typing "+y would. It reports false and does nothing when
// Neovim is not in a visual mode (v, V or CTRL-V).
func (p *Process) CopyVisual(ctx context.Context) (bool, error) {
	var copied bool
	// The first character of the mode string is enough: select mode is "s",
	// "S" or CTRL-S and operator-pending modes start with "n", so neither
	// counts. "normal!" ignores mappings, and the yank itself ends visual mode.
	err := p.execLua(ctx, "copy visual", &copied, `
		local mode = vim.api.nvim_get_mode().mode:sub(1, 1)
		if mode ~= "v" and mode ~= "V" and mode ~= "\22" then
			return false
		end
		vim.cmd.normal({ args = { '"+y' }, bang = true })
		return true`)
	if err != nil {
		return false, err
	}
	return copied, nil
}

// Redraw makes Neovim send the whole screen again (what a re-attached terminal
// needs). It uses ":mode" ("Clears and redraws the screen", :help :mode), the
// documented way to clear and repaint. ":redraw!" is not enough: measured with
// Neovim 0.12, it sends no byte at all when a file is open and nothing on the
// screen changed, so a reloaded page could stay blank. nvim__redraw() is
// documented as experimental. ":mode" does not change the size and leaves the
// buffers alone; it only wipes the message line, like CTRL-L does.
func (p *Process) Redraw(ctx context.Context) error {
	return p.execLua(ctx, "redraw", nil, `vim.cmd.mode()`)
}

// Events returns the notification stream. It is buffered (eventBuffer
// entries); if the consumer does not keep up, the oldest events are dropped
// so Neovim and the RPC loop never block. It is closed when the process is gone.
func (p *Process) Events() <-chan Event { return p.events }

// Done is closed when the nvim process has exited, for any reason.
func (p *Process) Done() <-chan struct{} { return p.done }

// Pid returns the process id of the nvim process.
func (p *Process) Pid() int { return p.cmd.Process.Pid }

func (p *Process) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// execLua runs a Lua chunk; args are available as `...` inside it.
func (p *Process) execLua(ctx context.Context, op string, result interface{}, code string, args ...interface{}) error {
	return p.call(ctx, op, func(nv *nvimrpc.Nvim) error {
		return nv.ExecLua(code, result, args...)
	})
}

// call runs one RPC call. go-client calls take no context, so the call runs
// in its own goroutine and we stop waiting when ctx ends or Neovim goes away.
// That goroutine ends at the latest when reap closes the connection.
func (p *Process) call(ctx context.Context, op string, fn func(*nvimrpc.Nvim) error) error {
	p.mu.Lock()
	nv := p.nv
	if p.torn || nv == nil {
		p.mu.Unlock()
		return fmt.Errorf("%s: %w", op, ErrNotRunning)
	}
	p.calls.Add(1)
	p.mu.Unlock()

	errc := make(chan error, 1)
	go func() {
		defer p.calls.Done()
		errc <- fn(nv)
	}()

	select {
	case err := <-errc:
		switch {
		case err == nil:
			return nil
		case errors.Is(err, rpc.ErrClosed), errors.Is(err, io.EOF), errors.Is(err, io.ErrClosedPipe),
			errors.Is(err, syscall.EPIPE), errors.Is(err, syscall.ECONNRESET), p.exited():
			// Connection lost: the server is gone even if the TUI is still exiting.
			return fmt.Errorf("%s: %w (%w)", op, ErrNotRunning, err)
		}
		if strings.Contains(err.Error(), promptMarker) {
			return fmt.Errorf("nvim: %s: %w", op, ErrPromptOpen)
		}
		return fmt.Errorf("nvim: %s: %w", op, err)
	case <-ctx.Done():
		return fmt.Errorf("nvim: %s: %w", op, ctx.Err())
	case <-p.done:
		return fmt.Errorf("%s: %w", op, ErrNotRunning)
	}
}

// Close stops Neovim and releases everything. It is idempotent and returns
// nil if Neovim already exited on its own. Stages, each limited to StopTimeout:
// RPC "qa!" (no save), SIGTERM to the process group, SIGKILL to the process
// group. An already cancelled ctx skips the graceful stages.
func (p *Process) Close(ctx context.Context) error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()

	if !p.exited() {
		// If the consumer stopped reading, Neovim may sit in a blocked write
		// to the PTY and never process the quit; keep the PTY empty while stopping.
		p.drainOnce.Do(func() {
			p.drain.Add(1)
			go func() {
				defer p.drain.Done()
				// Ends with an error when the PTY closes; nothing to report.
				_, _ = io.Copy(io.Discard, p.out)
			}()
		})
		if err := p.stop(ctx); err != nil {
			return err
		}
	}
	<-p.reaped

	if !p.closed {
		p.closed = true
		// Closing the master unblocks pump if it is still reading; closing
		// the pipe's read end unblocks it if it is writing to a full pipe.
		errPTY := p.pty.Close()
		errOut := p.out.Close()
		<-p.pumpDone
		p.drain.Wait()
		if err := errors.Join(errPTY, errOut); err != nil {
			return fmt.Errorf("nvim: release pty: %w", err)
		}
	}
	return nil
}

// stop escalates until the process has exited.
func (p *Process) stop(ctx context.Context) error {
	p.mu.Lock()
	rpcUp := p.nv != nil && !p.torn
	p.mu.Unlock()

	if rpcUp && p.stage(ctx, 1, func(sctx context.Context) {
		// The outcome is read from the process exit, not from this call: a
		// successful quit tears the connection down before any reply arrives.
		_ = p.call(sctx, "quit", func(nv *nvimrpc.Nvim) error { return nv.Command("qa!") })
	}) {
		return nil
	}
	if p.stage(ctx, 2, func(context.Context) { p.signalGroup(syscall.SIGTERM) }) {
		return nil
	}
	// SIGKILL must get its full wait even if the caller's ctx is already gone.
	if p.stage(context.WithoutCancel(ctx), 3, func(context.Context) {
		p.signalGroup(syscall.SIGKILL)
		p.mu.Lock()
		pid := p.serverPid
		p.mu.Unlock()
		if pid != 0 && !p.exited() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}) {
		return nil
	}
	return fmt.Errorf("nvim: process %d still running after SIGKILL", p.cmd.Process.Pid)
}

// stage runs act, then waits for exit until StopTimeout or ctx ends.
func (p *Process) stage(ctx context.Context, n int32, act func(context.Context)) bool {
	ctx, cancel := context.WithTimeout(ctx, p.stopTimeout)
	defer cancel()
	act(ctx)
	select {
	case <-p.done:
		p.stoppedBy.Store(n)
		return true
	case <-ctx.Done():
		return false
	}
}

// signalGroup signals the whole process group. ESRCH means it is already gone.
func (p *Process) signalGroup(sig syscall.Signal) {
	if p.exited() {
		return
	}
	// Errors other than ESRCH (EPERM) cannot happen for our own child; if
	// the signal did not land, the next stage times out and escalates.
	_ = syscall.Kill(-p.cmd.Process.Pid, sig)
}
