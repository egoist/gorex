//go:build darwin || linux

package rex

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// A server upgrades in place: it runs the new executable in its own
// process, which keeps its children, the sessions' shells, and the
// descriptors it leaves open, the listener's, the lock's and the
// terminals'. The rest of a session, its screen and counters, goes in a
// file the new executable reads (Resume).

const canUpgrade = true

// handoffVersion is the version of the handoff's format: a build reads
// the handoffs of the builds before it.
const handoffVersion = 1

// handoff is what an upgrading server hands the executable it runs.
type handoff struct {
	Version int `json:"version"`
	// Listener and Lock are the descriptors of the listener and of the
	// lock file, which stays locked.
	Listener int             `json:"listener"`
	Lock     int             `json:"lock"`
	Started  time.Time       `json:"started"`
	Layout   json.RawMessage `json:"layout,omitempty"`
	Sessions []sessionState  `json:"sessions"`
}

// sessionState is a session in a handoff.
type sessionState struct {
	ID      string    `json:"id"`
	Shell   string    `json:"shell"`
	Created time.Time `json:"created"`
	PID     int       `json:"pid"`
	// Master is the descriptor of the terminal, -1 once the session
	// exited.
	Master int `json:"master"`
	Cols   int `json:"cols"`
	Rows   int `json:"rows"`
	// Screen is the emulator's snapshot, and Title its title.
	Screen     []byte    `json:"screen,omitempty"`
	Title      string    `json:"title,omitempty"`
	Output     uint64    `json:"output"`
	Bells      int64     `json:"bells"`
	LastOutput time.Time `json:"lastOutput"`
	LastInput  time.Time `json:"lastInput"`
	// Reaped tells that the process ended, with Code; Exited, that what
	// it printed was read too.
	Reaped bool `json:"reaped,omitempty"`
	Exited bool `json:"exited,omitempty"`
	Code   int  `json:"code,omitempty"`
}

func handoffPath() string { return filepath.Join(Dir(), "handoff.json") }

// upgrade runs exe, an absolute path, in this process in the server's
// place, handing it the sessions. It returns only when that fails, and
// the server goes on.
func (s *Server) upgrade(exe string) error {
	if !filepath.IsAbs(exe) {
		return fmt.Errorf("rex: %q is not an absolute path", exe)
	}
	ul, ok := s.ln.(*net.UnixListener)
	if !ok || s.lock == nil {
		return errors.New("rex: this server cannot upgrade")
	}
	s.ops.Lock()
	defer s.ops.Unlock()
	// A connection accepted now would close as exe runs: leave them to
	// it, in the listener's queue.
	s.acceptMu.Lock()
	defer s.acceptMu.Unlock()
	ul.SetDeadline(time.Unix(1, 0))
	defer ul.SetDeadline(time.Time{})
	s.remoteClose()
	defer s.remoteLoad()
	s.mu.Lock()
	all := make([]*session, 0, len(s.order))
	for _, id := range s.order {
		all = append(all, s.sessions[id])
	}
	h := handoff{Version: handoffVersion, Started: s.started, Layout: s.layout}
	s.mu.Unlock()
	for _, ss := range all {
		if err := ss.pause(); err != nil {
			return err
		}
		defer ss.resume()
	}
	lf, err := ul.File()
	if err != nil {
		return err
	}
	defer lf.Close()
	h.Listener, h.Lock = descriptor(lf), descriptor(s.lock)
	fds := []int{h.Listener, h.Lock}
	for _, ss := range all {
		st := ss.state()
		if st.Master >= 0 {
			fds = append(fds, st.Master)
		}
		h.Sessions = append(h.Sessions, st)
	}
	path := handoffPath()
	if err := writeJSON(path, h); err != nil {
		return err
	}
	defer os.Remove(path)
	log.Printf("rex: upgrading to %s", exe)
	// No process starts meanwhile, which would get the descriptors.
	syscall.ForkLock.Lock()
	for _, fd := range fds {
		unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0)
	}
	err = syscall.Exec(exe, []string{exe, "-server", "-resume", path}, os.Environ())
	for _, fd := range fds {
		unix.CloseOnExec(fd)
	}
	syscall.ForkLock.Unlock()
	return fmt.Errorf("rex: running %s: %w", exe, err)
}

// descriptor returns the descriptor of f, leaving it non-blocking, or -1.
func descriptor(f *os.File) int {
	n := -1
	control(f, func(fd int) error {
		n = fd
		return nil
	})
	return n
}

// pause stops reading the terminal, leaving what it has not read in the
// kernel, and detaches the windows, for an upgrade; resume reads again.
func (s *session) pause() error {
	s.hold.Lock()
	if s.p.master != nil {
		if err := s.p.master.SetReadDeadline(time.Unix(1, 0)); err != nil && !errors.Is(err, os.ErrClosed) {
			s.hold.Unlock()
			return fmt.Errorf("rex: pausing session %s: %w", s.id, err)
		}
	}
	s.mu.Lock()
	reading := s.reading
	for a := range s.clients {
		a.finish()
	}
	clear(s.clients)
	s.mu.Unlock()
	<-reading
	return nil
}

func (s *session) resume() {
	select {
	case <-s.readEnd:
	default:
		s.p.master.SetReadDeadline(time.Time{})
		s.startRead()
	}
	s.hold.Unlock()
}

// state describes a paused session for a handoff.
func (s *session) state() sessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := sessionState{
		ID: s.id, Shell: s.shell, Created: s.created, PID: s.p.pid(), Master: -1,
		Cols: s.cols, Rows: s.rows, Screen: s.vt.Snapshot(), Title: s.vt.Title(),
		Output: s.output, Bells: s.bells.Load(), LastOutput: s.lastOutput, LastInput: s.lastInput,
		Reaped: s.reaped, Exited: s.exited, Code: s.code,
	}
	select {
	case <-s.readEnd:
		// Its output ended: once its process did too, it exited.
		st.Exited = st.Exited || st.Reaped
	default:
	}
	if !st.Exited {
		if st.Master = descriptor(s.p.master); st.Master < 0 {
			st.Exited, st.Code = true, -1
		}
	}
	return st
}

// Resume serves the sessions a server upgrading to this executable
// handed it in the file path, as Serve does.
func Resume(path string) error {
	b, err := os.ReadFile(path)
	os.Remove(path)
	if err != nil {
		return err
	}
	var h handoff
	if err := json.Unmarshal(b, &h); err != nil {
		return fmt.Errorf("rex: reading the handoff: %w", err)
	}
	if h.Version < 1 {
		return fmt.Errorf("rex: a handoff of version %d", h.Version)
	}
	unix.CloseOnExec(h.Lock)
	lock := os.NewFile(uintptr(h.Lock), "server.lock")
	defer lock.Close()
	lf := os.NewFile(uintptr(h.Listener), "server.sock")
	ln, err := net.FileListener(lf)
	lf.Close()
	if err != nil {
		return fmt.Errorf("rex: resuming the listener: %w", err)
	}
	s := newServer(ln, lock)
	s.started, s.layout = h.Started, h.Layout
	for _, st := range h.Sessions {
		ss, err := resumeSession(st)
		if err != nil {
			log.Printf("rex: session %s is lost: %v", st.ID, err)
			if st.Master >= 0 {
				unix.Close(st.Master)
			}
			continue
		}
		s.sessions[ss.id] = ss
		s.order = append(s.order, ss.id)
	}
	log.Printf("rex: resumed %d of %d sessions", len(s.order), len(h.Sessions))
	return s.serve()
}

// resumeSession makes a session of a handoff's again.
func resumeSession(st sessionState) (*session, error) {
	s := &session{
		id: st.ID, shell: st.Shell, created: st.Created,
		clients: map[*attached]struct{}{},
		output:  st.Output, lastOutput: st.LastOutput, lastInput: st.LastInput,
		cols: clampSize(st.Cols), rows: clampSize(st.Rows), done: make(chan struct{}),
	}
	if err := s.newVT(); err != nil {
		return nil, err
	}
	if st.Exited || st.Master < 0 {
		s.p = &pty{proc: st.PID}
		s.reaped, s.exited, s.code = true, true, st.Code
		s.reading, s.readEnd = make(chan struct{}), make(chan struct{})
		close(s.reading)
		close(s.readEnd)
		close(s.done)
		s.vtIn.Close()
		s.vt.Close()
		s.bells.Store(st.Bells)
		return s, nil
	}
	p, err := resumePTY(st.Master, st.PID)
	if err != nil {
		s.vtIn.Close()
		s.vt.Close()
		return nil, err
	}
	s.p = p
	if st.Title != "" {
		s.vt.Feed([]byte("\x1b]2;" + st.Title + "\x07"))
	}
	s.vt.Feed(st.Screen)
	s.bells.Store(st.Bells)
	wait := p.wait
	if st.Reaped {
		// The executable before this one waited for it.
		wait = func() int { return st.Code }
	}
	s.run(wait)
	return s, nil
}
