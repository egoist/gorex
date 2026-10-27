//go:build darwin || linux || windows

package rex

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

// scrollback is about how many bytes of output a session keeps above its
// screen, for the windows that attach to it.
const scrollback = 8 << 20

// session is a pseudo-terminal of the server and its shell.
type session struct {
	id      string
	shell   string
	created time.Time
	p       *pty

	mu sync.Mutex
	// vt is the session's screen, kept by the terminal emulator of the
	// app's terminals without a view: a window attaching gets a snapshot
	// of it at its size, as the session shows it now.
	vt      *terminal.Terminal
	vtIn    *io.PipeWriter
	output  uint64
	bells   atomic.Int64
	clients map[*attached]struct{}
	// typist is the window typed in last: with several windows attached,
	// only its terminal answers the program's queries.
	typist     *attached
	lastOutput time.Time
	lastInput  time.Time
	resync     *time.Timer
	// reaped tells that the process ended, with code; exited, that what
	// it printed was read too.
	reaped bool
	exited bool
	code   int
	// screen and title are what the session showed as it exited, its
	// emulator closed.
	screen     []byte
	title      string
	cols, rows int
	done       chan struct{}
	// reading is closed when the goroutine reading the terminal returns,
	// and readEnd when the terminal's output ends: a pause returns before.
	reading, readEnd chan struct{}
	// hold keeps the terminal open while the server hands it to another
	// executable (upgrade).
	hold sync.RWMutex
}

// attached is a connection attached to a session, whose output a
// goroutine writes so that a slow client never holds up the others.
type attached struct {
	conn net.Conn
	// first is the snapshot it gets first, out what the session printed
	// since.
	first []byte
	out   chan []byte
	once  sync.Once
	// queued is how many bytes wait in out; remote tells a remote
	// connection, whose writes have a deadline.
	queued atomic.Int64
	remote bool
}

// The most chunks of output that may wait for a client, and bytes for a
// remote one: one further behind is dropped, and attaches again to a
// snapshot. Without a bound, a client that stopped reading would hold
// all the session printed since.
const (
	maxQueue       = 2048
	maxQueuedBytes = 8 << 20
)

func (a *attached) send(p []byte) bool {
	// The bytes are capped for remote clients only: a local one dropped
	// closes its pane, and reads what it is sent quick enough.
	if a.queued.Add(int64(len(p))) > maxQueuedBytes && a.remote {
		a.queued.Add(-int64(len(p)))
		return false
	}
	select {
	case a.out <- p:
		return true
	default:
		a.queued.Add(-int64(len(p)))
		return false // too far behind: drop it, it attaches again
	}
}

func (a *attached) finish() { a.once.Do(func() { close(a.out) }) }

// writeChunk is the most the writer writes under one deadline: a remote
// client is dropped once it takes nothing for writeTimeout, not when a
// snapshot takes it longer than that on a slow network.
const writeChunk = 32 << 10

// writer writes the client what it gets until a write fails, then closes
// the connection at once: the attach's read fails with it, and drops the
// client, though the session prints nothing more.
func (a *attached) writer() {
	defer func() {
		a.conn.Close()
		for range a.out {
		}
	}()
	first := a.first
	a.first = nil
	if a.write(first) != nil {
		return
	}
	for p := range a.out {
		err := a.write(p)
		a.queued.Add(-int64(len(p)))
		if err != nil {
			return
		}
	}
}

func (a *attached) write(p []byte) error {
	for len(p) > 0 {
		n := min(len(p), writeChunk)
		if a.remote {
			a.conn.SetWriteDeadline(time.Now().Add(writeTimeout.get()))
		}
		if _, err := a.conn.Write(p[:n]); err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

// maxSize is the most columns or rows a terminal may have: the sizes a
// client sends, remote ones included, are clamped to 1..maxSize.
const maxSize = 1000

func clampSize(n int) int { return min(max(n, 1), maxSize) }

func newSession(id string, o CreateOptions) (*session, error) {
	path, argv, name, err := shellCommand(o.Command)
	if err != nil {
		return nil, err
	}
	cols, rows := o.Cols, o.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	cols, rows = clampSize(cols), clampSize(rows)
	dir := o.Dir
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	env := sessionEnv(id, o.Env)
	p, err := startPTY(path, argv, dir, env, cols, rows)
	if err != nil {
		return nil, err
	}
	s := &session{
		id: id, shell: name, created: time.Now(), p: p,
		clients: map[*attached]struct{}{},
		cols:    cols, rows: rows, done: make(chan struct{}),
	}
	if err := s.newVT(); err != nil {
		p.hangup()
		return nil, err
	}
	s.run(p.wait)
	return s, nil
}

// newVT gives the session its emulator, at the session's size.
func (s *session) newVT() error {
	// The emulator's Conn reads nothing, and what it would answer programs
	// is dropped: the windows' terminals answer them.
	pr, pw := io.Pipe()
	vt, err := terminal.New(terminal.Options{Conn: discard{pr}, Scrollback: scrollback, OnBell: func() { s.bells.Add(1) }})
	if err != nil {
		return err
	}
	vt.Resize(s.cols, s.rows)
	s.vt, s.vtIn = vt, pw
	return nil
}

// run reads the terminal, and waits for its process to end: wait waits
// for it and returns its exit code.
func (s *session) run(wait func() int) {
	s.readEnd = make(chan struct{})
	s.startRead()
	go func() {
		code := wait()
		s.mu.Lock()
		s.reaped, s.code = true, code
		s.mu.Unlock()
		// What it printed last is still to read; then hang up whatever
		// else holds the terminal, as terminal apps do.
		select {
		case <-s.readEnd:
		case <-time.After(150 * time.Millisecond):
			s.hold.RLock()
			s.p.close()
			s.hold.RUnlock()
			<-s.readEnd
		}
		s.hold.RLock()
		defer s.hold.RUnlock()
		s.mu.Lock()
		s.screen, s.title = s.vt.Snapshot(), s.vt.Title()
		s.exited = true
		for a := range s.clients {
			a.finish()
		}
		clear(s.clients)
		s.mu.Unlock()
		close(s.done)
		s.vtIn.Close()
		s.vt.Close()
	}()
}

// starterMarkers are the variables that belong to the process that started
// the server (an agent session, another Rex), not to a fresh terminal. Only
// runtime and session markers are listed; user configuration such as
// CLAUDE_CONFIG_DIR or CLAUDE_CODE_USE_BEDROCK stays.
var starterMarkers = map[string]bool{
	"CLAUDECODE": true, "CLAUDE_PID": true, "CLAUDE_EFFORT": true,
	"CLAUDE_CODE_ENTRYPOINT": true, "CLAUDE_CODE_CHILD_SESSION": true,
	"CLAUDE_CODE_SESSION_ID": true, "CLAUDE_CODE_SESSION_ATTENDED": true,
	"CLAUDE_CODE_BRIDGE_SESSION_ID": true, "CLAUDE_CODE_MESSAGING_TOKEN": true,
	"CLAUDE_CODE_MESSAGING_SOCKET": true, "CLAUDE_CODE_EXECPATH": true,
	"CLAUDE_PROJECT_DIR": true, "CLAUDE_ENV_FILE": true, "AI_AGENT": true,
	"CODEX_SANDBOX": true, "CODEX_SANDBOX_NETWORK_DISABLED": true, "CODEX_THREAD_ID": true,
	"REX_SESSION": true, "REX_BLOCK": true, "REX_SERVER": true, "REX_BIN_DIR": true,
}

func starterMarker(k string) bool { return starterMarkers[k] }

// serverEnv returns the environment for a server process: the current one
// without starter markers, so a secret never lives in the server.
func serverEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !starterMarker(k) {
			env = append(env, kv)
		}
	}
	return env
}

// sessionEnv returns the environment of a session's shell: the server's,
// without what other terminals set, and what terminal apps set.
func sessionEnv(id string, extra []string) []string {
	var env []string
	hasLang := false
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "TERM", k == "COLORTERM", k == "TERM_PROGRAM", k == "TERM_PROGRAM_VERSION",
			k == "TERM_SESSION_ID", k == "TMUX", k == "TMUX_PANE", k == "STY", k == "WINDOWID",
			k == "COLUMNS", k == "LINES", k == "SHLVL", k == "OLDPWD", k == "PWD", k == "_",
			strings.HasPrefix(k, "ITERM_"), strings.HasPrefix(k, "GHOSTTY_"), strings.HasPrefix(k, "KITTY_"),
			strings.HasPrefix(k, "VSCODE_"), strings.HasPrefix(k, "WEZTERM_"), strings.HasPrefix(k, "ALACRITTY_"),
			strings.HasPrefix(k, "GOREX_"), strings.HasPrefix(k, "MYGO_"),
			starterMarker(k):
			continue
		case k == "LANG" || k == "LC_ALL" || k == "LC_CTYPE":
			hasLang = true
		}
		env = append(env, kv)
	}
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=GoRex",
		"TERM_PROGRAM_VERSION=0.1.0",
		"GOREX_SESSION="+id,
	)
	if !hasLang {
		env = append(env, "LANG=en_US.UTF-8")
	}
	return append(env, extra...)
}

// startRead starts reading the terminal, until its output ends or a pause.
func (s *session) startRead() {
	reading := make(chan struct{})
	s.mu.Lock()
	s.reading = reading
	s.mu.Unlock()
	go func() {
		defer close(reading)
		s.read()
	}()
}

func (s *session) read() {
	buf := make([]byte, 64<<10)
	for {
		n, err := s.p.read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.vt.Feed(data)
			s.output += uint64(n)
			s.lastOutput = time.Now()
			for a := range s.clients {
				if !a.send(data) {
					a.finish()
					delete(s.clients, a)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			// A deadline is a pause: what the terminal has stays unread.
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				close(s.readEnd)
			}
			return
		}
	}
}

// attach attaches a connection: it gets a snapshot of the session's
// screen and scrollback, at the size the server gave the session for it
// (resize), then what the session prints, and what it sends is typed.
func (s *session) attach(conn net.Conn, remote bool) {
	a := &attached{conn: conn, out: make(chan []byte, maxQueue), remote: remote}
	s.mu.Lock()
	snap, title := s.view()
	if title != "" {
		snap = append([]byte("\x1b]2;"+title+"\x07"), snap...)
	}
	a.first = snap
	exited := s.exited
	if exited {
		a.finish()
	} else {
		s.clients[a] = struct{}{}
	}
	s.mu.Unlock()
	go a.writer()
	if exited {
		return
	}
	buf := make([]byte, 32<<10)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			s.input(a, buf[:n])
		}
		if err != nil {
			break
		}
	}
	s.mu.Lock()
	if _, ok := s.clients[a]; ok {
		delete(s.clients, a)
		a.finish()
	}
	if s.typist == a {
		s.typist = nil
	}
	s.mu.Unlock()
}

// input types what window a sent; a is nil for what came with the
// attach request. Every window's terminal answers a program's queries
// (background colour, cursor position…): the program reads one answer,
// and the others would land on the shell's prompt. So answers count from
// the window typed in last only, or, none being, from the first to answer.
func (s *session) input(a *attached, p []byte) {
	s.mu.Lock()
	if !s.takes(a, p) {
		s.mu.Unlock()
		return
	}
	s.lastInput = time.Now()
	exited := s.exited
	s.mu.Unlock()
	if !exited {
		s.p.write(p)
	}
}

// takes tells whether input from a goes to the program; s.mu is held.
func (s *session) takes(a *attached, p []byte) bool {
	if a == nil {
		return true
	}
	if !isReply(p) || s.typist == nil {
		s.typist = a
	}
	return s.typist == a
}

// isReply tells input made of a terminal's answers to queries only: OSC,
// DCS and APC strings, and the CSI reports (cursor position, device
// attributes, status, modes, window size, keyboard flags). Keys and mouse
// make none of them, but for a modified F3, alike a cursor position.
// ponytail: a terminal writes each answer at once, but a network may join
// one to keys; joined, it counts as typing and passes.
func isReply(p []byte) bool {
	if len(p) == 0 {
		return false
	}
	for len(p) > 0 {
		if len(p) < 3 || p[0] != 0x1b {
			return false
		}
		switch p[1] {
		case ']', 'P', '_':
			n := stringEnd(p[2:])
			if n < 0 {
				return false
			}
			p = p[2+n:]
		case '[':
			i := 2
			for i < len(p) && p[i] >= 0x20 && p[i] <= 0x3f {
				i++
			}
			if i == len(p) {
				return false
			}
			params, final := p[2:i], p[i]
			switch {
			case final == 'R' || final == 'c' || final == 'n' || final == 't':
			case final == 'y' && bytes.HasSuffix(params, []byte("$")):
			case final == 'u' && bytes.HasPrefix(params, []byte("?")):
			default:
				return false
			}
			p = p[i+1:]
		default:
			return false
		}
	}
	return true
}

// stringEnd returns the length of a string's body with its terminator,
// BEL or ST, or -1 without one.
func stringEnd(p []byte) int {
	for i, c := range p {
		if c == 0x07 {
			return i + 1
		}
		if c == 0x1b && i+1 < len(p) && p[i+1] == '\\' {
			return i + 2
		}
	}
	return -1
}

// resize sets the size of the terminal and reports whether it changed.
func (s *session) resize(cols, rows int) bool {
	cols, rows = clampSize(cols), clampSize(rows)
	s.mu.Lock()
	changed := cols != s.cols || rows != s.rows
	s.cols, s.rows = cols, rows
	exited := s.exited
	if changed {
		s.vt.Resize(cols, rows)
		// Once the program drew its screen at the new size, the windows
		// get it again.
		if s.resync == nil {
			s.resync = time.AfterFunc(resyncDelay, s.resyncScreen)
		} else {
			s.resync.Reset(resyncDelay)
		}
	}
	s.mu.Unlock()
	if changed && !exited {
		s.p.resize(cols, rows)
	}
	return changed
}

// resyncDelay is how long after the last resize the screen of a
// full-screen program is sent again.
const resyncDelay = 150 * time.Millisecond

// resyncScreen sends the windows the screen of a full-screen program as
// the session has it. A window resizes its terminal as it draws, and the
// session as the request reaches it, while the program's output is on its
// way: what the program drew for one size may land in a screen of
// another, which full-screen programs, redrawing only what changed, do
// not mend. Shells' scrollback reflows alike in both and needs nothing.
func (s *session) resyncScreen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited || len(s.clients) == 0 {
		return
	}
	snap := s.vt.Snapshot()
	if !bytes.Contains(snap, []byte("\x1b[?1049h")) {
		return
	}
	// Clear the alternate screen the window shows, then draw it anew.
	msg := append([]byte("\x1b[?1049h\x1b[H\x1b[2J"), snap...)
	for a := range s.clients {
		if !a.send(msg) {
			a.finish()
			delete(s.clients, a)
		}
	}
}

// kill hangs up the session and waits a little for it to end.
func (s *session) kill() {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if !exited {
		s.p.hangup()
	}
	select {
	case <-s.done:
	case <-time.After(time.Second):
		s.p.kill()
	}
}

// view returns the session's screen, as escape sequences, and its
// title; s.mu is held.
func (s *session) view() ([]byte, string) {
	if s.exited {
		return s.screen, s.title
	}
	return s.vt.Snapshot(), s.vt.Title()
}

// info describes the session and what runs in its foreground.
func (s *session) info() SessionInfo {
	s.mu.Lock()
	title := s.title
	if !s.exited {
		title = s.vt.Title()
	}
	in := SessionInfo{
		ID: s.id, Shell: s.shell, Created: s.created,
		Title: title, LastOutput: s.lastOutput, LastInput: s.lastInput,
		Output: s.output, Bells: int(s.bells.Load()), Exited: s.exited, ExitCode: s.code,
		Attached: len(s.clients), Cols: s.cols, Rows: s.rows,
	}
	s.mu.Unlock()
	in.PID = s.p.pid()
	if in.Exited {
		return in
	}
	fg := s.p.foreground()
	if fg <= 0 {
		fg = in.PID
	}
	in.Idle = fg == in.PID
	p := inspect(fg)
	in.Program, in.Args, in.Dir = p.programName(), p.args, p.dir
	if in.Program == "" {
		in.Program = s.shell
	}
	if in.Dir == "" && !in.Idle {
		in.Dir = inspect(in.PID).dir
	}
	return in
}

// discard is the Conn of a session's emulator: it reads from a pipe that
// closes with the session, and drops what it writes.
type discard struct{ r *io.PipeReader }

func (d discard) Read(p []byte) (int, error)  { return d.r.Read(p) }
func (d discard) Write(p []byte) (int, error) { return len(p), nil }
func (d discard) Close() error                { return d.r.Close() }
