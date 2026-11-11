// Package nvim runs a real Neovim in a PTY and controls it over msgpack-RPC.
//
// Two channels connect to the process. The PTY carries the screen and the
// keystrokes (an xterm.js terminal will read and write it). The RPC unix
// socket carries commands and notifications. Both belong to one Process.
package nvim

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/neovim/go-client/msgpack/rpc"
	nvimrpc "github.com/neovim/go-client/nvim"
)

// Defaults for zero Options fields. This is the only place they are defined.
const (
	defaultCols         uint16 = 80
	defaultRows         uint16 = 24
	defaultStartTimeout        = 10 * time.Second
	defaultStopTimeout         = 3 * time.Second
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
	notifyGroup  = "PisarNotify"
	channelVar   = "pisar_channel"
	savedMethod  = "pisar_saved"
	byeMethod    = "pisar_bye"
	socketPrefix = "nvim-"
)

// EventKind identifies the type of an Event.
type EventKind int

const (
	// EventSaved is sent after Neovim wrote a buffer to its file.
	EventSaved EventKind = iota + 1
)

// Event is a notification from Neovim.
type Event struct {
	Kind EventKind
	Path string // absolute path of the buffer's file as Neovim reports it
}

var (
	// ErrNotRunning is returned (wrapped) once the Neovim process is gone.
	ErrNotRunning = errors.New("nvim: process is not running")
	// ErrSocketPathTooLong is returned by Start when the socket path would not fit sun_path.
	ErrSocketPathTooLong = errors.New("nvim: socket path too long")
)

// Options configures Start. Zero Cols, Rows, StartTimeout and StopTimeout
// select the package defaults.
type Options struct {
	Bin          string        // nvim binary name or path; resolved with exec.LookPath
	InitLua      string        // absolute path, passed as `-u`
	AppName      string        // NVIM_APPNAME (isolates config/data/state from the user's own Neovim)
	SocketDir    string        // existing directory owned by the caller; the socket file is created inside it
	Cols, Rows   uint16        // initial terminal size
	Env          []string      // extra "K=V" entries, applied after os.Environ() and after the package's own
	StartTimeout time.Duration // max wait for the RPC socket to accept a connection
	StopTimeout  time.Duration // max wait per shutdown stage
}

func (o Options) withDefaults() Options {
	if o.Cols == 0 {
		o.Cols = defaultCols
	}
	if o.Rows == 0 {
		o.Rows = defaultRows
	}
	if o.StartTimeout == 0 {
		o.StartTimeout = defaultStartTimeout
	}
	if o.StopTimeout == 0 {
		o.StopTimeout = defaultStopTimeout
	}
	return o
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
//
// A later duplicate answer from xterm.js is harmless.
var termAnswers = []struct{ query, reply string }{
	{"\x1b[5n", "\x1b[0n"},
	{"\x1b[c", "\x1b[?1;2c"},
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

	done   chan struct{} // closed once the process has been reaped
	reaped chan struct{} // closed when all RPC resources have been released

	mu        sync.Mutex // guards nv, serveDone, serverPid, torn and calls admission
	nv        *nvimrpc.Nvim
	serveDone chan struct{}
	serverPid int // Neovim's server process; 0 until known
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
	opts = opts.withDefaults()
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
	sock := filepath.Join(opts.SocketDir,
		fmt.Sprintf("%s%d-%d.sock", socketPrefix, os.Getpid(), socketSeq.Add(1)))
	if len(sock) > maxSocketPath {
		return nil, fmt.Errorf("%w: %d bytes, limit %d: %s", ErrSocketPathTooLong, len(sock), maxSocketPath, sock)
	}

	// Arguments are separate elements, never a shell string.
	args := []string{"--listen", sock}
	if opts.InitLua != "" {
		args = append(args, "-u", opts.InitLua)
	}
	cmd := exec.Command(bin, args...)
	env := os.Environ()
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

	if err := nv.RegisterHandler(savedMethod, func(path string) {
		p.emit(Event{Kind: EventSaved, Path: path})
	}); err != nil {
		_ = nv.Close()
		return fmt.Errorf("nvim: register handler: %w", err)
	}
	if err := nv.RegisterHandler(byeMethod, p.closeEvents); err != nil {
		_ = nv.Close()
		return fmt.Errorf("nvim: register handler: %w", err)
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

	return p.call(ctx, "register channel", func(nv *nvimrpc.Nvim) error {
		return nv.SetVar(channelVar, nv.ChannelID())
	})
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
	var window []byte // previous tail + new data: a query may arrive split across reads
	for {
		n, err := p.pty.Read(buf)
		if n > 0 {
			tail := len(window)
			window = append(window, buf[:n]...)
			for _, a := range termAnswers {
				for i := 0; i+len(a.query) <= len(window); i++ {
					// Only matches ending in the new data, or the tail would be answered twice.
					if i+len(a.query) > tail && string(window[i:i+len(a.query)]) == a.query {
						// If this fails the PTY is going away, which the pump sees on its next read.
						_, _ = p.pty.Write([]byte(a.reply))
					}
				}
			}
			keep := 0
			for _, a := range termAnswers {
				keep = max(keep, len(a.query)-1)
			}
			if len(window) > keep {
				window = append(window[:0], window[len(window)-keep:]...)
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
	return p.execLua(ctx, "open file", nil, `
		vim.cmd({ cmd = "edit", args = { (...) }, magic = { file = false, bar = false } })`, abs)
}

// CurrentFile returns the absolute path of the current buffer, or "" if it is unnamed.
func (p *Process) CurrentFile(ctx context.Context) (string, error) {
	var name string
	if err := p.execLua(ctx, "current file", &name, `return vim.api.nvim_buf_get_name(0)`); err != nil {
		return "", err
	}
	return name, nil
}

// Screen describes what Neovim shows in the current window.
type Screen struct {
	TopLine    int      // 1-based buffer line at the top of the window (line('w0'))
	Height     int      // window height in rows (winheight(0))
	CursorLine int      // 1-based
	LastLine   int      // number of lines in the buffer
	Lines      []string // buffer lines TopLine .. TopLine+Height-1 (fewer at the end of the buffer)
}

// Screen reports which part of the buffer the current window shows and where
// the cursor is. It is a snapshot taken inside Neovim's main loop, so all
// fields agree with each other. Lines are buffer text, not screen text: a
// wrapped long line is one entry although it fills several rows.
func (p *Process) Screen(ctx context.Context) (Screen, error) {
	var s Screen
	// The keys of the returned table are the field names of Screen, which is
	// how the msgpack decoder fills the struct. line('w0') validates the
	// window first, so the top line is current even in the middle of a scroll.
	err := p.execLua(ctx, "screen", &s, `
		local top = vim.fn.line("w0")
		local height = vim.fn.winheight(0)
		local last = vim.api.nvim_buf_line_count(0)
		return {
			TopLine = top,
			Height = height,
			CursorLine = vim.api.nvim_win_get_cursor(0)[1],
			LastLine = last,
			Lines = vim.api.nvim_buf_get_lines(0, top - 1, math.min(top - 1 + height, last), false),
		}`)
	if err != nil {
		return Screen{}, err
	}
	return s, nil
}

// Save writes the current buffer to its file (what ⌘S calls). It works in any
// mode and leaves the mode alone. When the write fails (E32 for an unnamed
// buffer, for instance) the error is returned and the same message is shown in
// Neovim, so the user sees why ⌘S did nothing; it stays in :messages.
func (p *Process) Save(ctx context.Context) error {
	// The message is echoed with the ErrorMsg highlight instead of err = true,
	// and followed by :redraw. In insert and visual mode Neovim otherwise
	// blocks the main loop, ignoring keys, to keep a message on screen: 1 s
	// for an error message, 3 s for the "written" message of a successful
	// :write (measured with Neovim 0.12). The error is re-raised without a
	// position prefix so Go sees the original text.
	return p.execLua(ctx, "save", nil, `
		local ok, err = pcall(vim.cmd.write)
		if not ok then
			vim.api.nvim_echo({ { tostring(err), "ErrorMsg" } }, true, {})
		end
		vim.cmd.redraw()
		if not ok then
			error(err, 0)
		end`)
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

// Redraw forces Neovim to repaint the whole screen (what a re-attached terminal needs).
func (p *Process) Redraw(ctx context.Context) error {
	// ":redraw!" is the stable command for a full repaint (:help :redraw);
	// nvim__redraw() is documented as experimental.
	return p.execLua(ctx, "redraw", nil, `vim.cmd.redraw({ bang = true })`)
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
