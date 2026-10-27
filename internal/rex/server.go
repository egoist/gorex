//go:build darwin || linux || windows

package rex

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Server owns the sessions.
type Server struct {
	mu       sync.Mutex
	sessions map[string]*session
	order    []string
	layout   json.RawMessage
	started  time.Time
	host     HostInfo
	hostDone chan struct{}
	ln       net.Listener
	lock     *os.File
	exe      string
	exeTime  time.Time
	controls int
	idleFrom time.Time
	quit     chan struct{}
	quitOnce sync.Once
	rem      remote
	// ops is held by the ops that add or end sessions, and an upgrade
	// takes it alone; acceptMu holds Accept while an upgrade stops it.
	ops      sync.RWMutex
	acceptMu sync.Mutex
	// kept are the control connections kept alive (Request.Keepalive), by
	// Hello.Conn, with the connections attached through them.
	kept map[string]*keptConn
}

// keptConn is a control connection kept alive, and the connections
// attached through it, which close with it.
type keptConn struct {
	id       string
	mu       sync.Mutex
	attached map[net.Conn]struct{}
	gone     bool
}

// keepaliveTimeout is how long a control connection kept alive may send
// nothing before the server drops it, and the sessions attached through
// it: a peer gone without closing them, over a network that dropped or
// a NAT that forgot it, leaves them open, and only silence tells.
var keepaliveTimeout = newDuration(30 * time.Second)

// writeTimeout is how long a remote connection may take to take any of
// what the server writes it before the server drops it: a peer gone
// takes nothing.
var writeTimeout = newDuration(30 * time.Second)

// keep keeps a control connection alive, under a new id.
func (s *Server) keep() *keptConn {
	k := &keptConn{id: newID(), attached: map[net.Conn]struct{}{}}
	s.mu.Lock()
	if s.kept == nil {
		s.kept = map[string]*keptConn{}
	}
	s.kept[k.id] = k
	s.mu.Unlock()
	return k
}

// unkeep forgets a control connection gone, and closes the connections
// attached through it.
func (s *Server) unkeep(k *keptConn) {
	s.mu.Lock()
	delete(s.kept, k.id)
	s.mu.Unlock()
	k.mu.Lock()
	k.gone = true
	conns := k.attached
	k.attached = nil
	k.mu.Unlock()
	for c := range conns {
		c.Close()
	}
}

// bind attaches conn through the control connection id; false when that
// is gone.
func (s *Server) bind(id string, conn net.Conn) (*keptConn, bool) {
	s.mu.Lock()
	k := s.kept[id]
	s.mu.Unlock()
	if k == nil {
		return nil, false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.gone {
		return nil, false
	}
	k.attached[conn] = struct{}{}
	return k, true
}

func (k *keptConn) unbind(conn net.Conn) {
	k.mu.Lock()
	delete(k.attached, conn)
	k.mu.Unlock()
}

// idleTimeout is how long a server with no sessions and no app waits
// before it exits.
const idleTimeout = 10 * time.Minute

// Serve runs the server until it is shut down: it returns an error at
// once when another server already runs.
func Serve() error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "server.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := lockFile(lock); err != nil {
		return errors.New("rex: a server already runs")
	}
	defer lock.Close()
	sock := SocketPath()
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	os.Chmod(sock, 0o600)
	s := newServer(ln, lock)
	if b, err := os.ReadFile(filepath.Join(dir, "layout.json")); err == nil && json.Valid(b) {
		s.layout = b
	}
	return s.serve()
}

func newServer(ln net.Listener, lock *os.File) *Server {
	s := &Server{
		sessions: map[string]*session{}, started: time.Now(), ln: ln, lock: lock,
		hostDone: make(chan struct{}), idleFrom: time.Now(), quit: make(chan struct{}),
	}
	s.exe, s.exeTime = Executable()
	return s
}

// serve accepts connections until the server is shut down.
func (s *Server) serve() error {
	ln, sock := s.ln, SocketPath()
	go func() {
		s.host = hostInfo()
		close(s.hostDone)
	}()
	go s.watchIdle()
	s.remoteLoad()
	go func() {
		<-s.quit
		ln.Close()
	}()
	log.Printf("rex: serving on %s (pid %d)", sock, os.Getpid())
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				s.mu.Lock()
				all := make([]*session, 0, len(s.sessions))
				for _, ss := range s.sessions {
					all = append(all, ss)
				}
				s.mu.Unlock()
				s.remoteClose()
				for _, ss := range all {
					ss.kill()
				}
				os.Remove(sock)
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				// An upgrade stopped it: wait for it to fail.
				s.acceptMu.Lock()
				s.acceptMu.Unlock()
				continue
			}
			return err
		}
		go s.handle(conn, false)
	}
}

func (s *Server) watchIdle() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
		}
		s.mu.Lock()
		idle := len(s.sessions) == 0 && s.controls == 0 && time.Since(s.idleFrom) > idleTimeout
		s.mu.Unlock()
		if idle && !s.remoteEnabled() {
			log.Print("rex: idle, exiting")
			s.shutdown()
			return
		}
	}
}

func (s *Server) shutdown() { s.quitOnce.Do(func() { close(s.quit) }) }

// handle serves a connection; a remote one may make the ops of
// remoteAllowed only.
func (s *Server) handle(conn net.Conn, remote bool) {
	r := bufio.NewReaderSize(conn, 64<<10)
	first, err := readLine(r)
	if err != nil {
		refuseLine(conn, err)
		conn.Close()
		return
	}
	var a Attach
	if json.Unmarshal(first, &a) == nil && a.Op == "attach" {
		s.mu.Lock()
		ss := s.sessions[a.SID]
		s.mu.Unlock()
		if ss == nil {
			conn.Write([]byte(`{"error":"no such session"}` + "\n"))
			conn.Close()
			return
		}
		if a.Conn != "" {
			k, ok := s.bind(a.Conn, conn)
			if !ok {
				conn.Write([]byte(`{"error":"` + errConnGone + `"}` + "\n"))
				conn.Close()
				return
			}
			defer k.unbind(conn)
		}
		// The session takes the window's size before the answer, after
		// which the client may resize it on its control connection: that
		// size must not be overwritten by this older one.
		if a.Cols > 0 && a.Rows > 0 {
			ss.resize(a.Cols, a.Rows)
		}
		conn.Write([]byte(`{"ok":true}` + "\n"))
		// What the reader buffered past the first line is input.
		if n := r.Buffered(); n > 0 {
			b, _ := r.Peek(n)
			ss.input(nil, b)
		}
		ss.attach(conn, remote)
		return
	}
	s.control(conn, r, first, remote)
}

// control serves a control connection: a request per line.
func (s *Server) control(conn net.Conn, r *bufio.Reader, first []byte, remote bool) {
	s.mu.Lock()
	s.controls++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.controls--
		s.idleFrom = time.Now()
		s.mu.Unlock()
		conn.Close()
	}()
	var kept *keptConn
	defer func() {
		if kept != nil {
			s.unkeep(kept)
		}
	}()
	var wmu sync.Mutex
	reply := func(res Response) {
		b, _ := json.Marshal(res)
		wmu.Lock()
		if remote {
			conn.SetWriteDeadline(time.Now().Add(writeTimeout.get()))
		}
		if _, err := conn.Write(append(b, '\n')); err != nil && remote {
			conn.Close() // gone, or as good as
		}
		wmu.Unlock()
	}
	kills := make(chan struct{}, maxKills)
	line := first
	for {
		var req Request
		if err := json.Unmarshal(line, &req); err == nil {
			if req.Op == "hello" && req.Keepalive && kept == nil {
				kept = s.keep()
			}
			// Kills wait for the session to end: answer them aside, a
			// few at a time; the others wait their turn.
			if req.Op == "kill" {
				kills <- struct{}{}
				kept := kept // a later hello sets it, as this runs
				go func() {
					defer func() { <-kills }()
					if killHook != nil {
						killHook()
					}
					reply(s.request(req, remote, kept))
				}()
			} else {
				reply(s.request(req, remote, kept))
			}
		}
		if kept != nil {
			conn.SetReadDeadline(time.Now().Add(keepaliveTimeout.get()))
		}
		var err error
		line, err = readLine(r)
		if err != nil {
			if errors.Is(err, errLineTooLong) {
				reply(Response{Error: err.Error()})
			}
			return
		}
	}
}

// maxLine is the longest line of control a connection may send, and
// maxKills how many kills of a connection run at once.
const (
	maxLine  = 1 << 20
	maxKills = 8
)

var errLineTooLong = errors.New("rex: the request is too long")

// errConnGone answers an attach through a control connection that is
// gone: the client lost it too, or is about to.
const errConnGone = "rex: the connection attached through is gone"

// killHook, when set, runs in each kill before it starts: for tests.
var killHook func()

// readLine reads a line of at most maxLine bytes, the newline included.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > maxLine {
			return nil, errLineTooLong
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return nil, err
		}
		return line, nil
	}
}

// refuseLine tells a connection its first line was too long.
func refuseLine(conn net.Conn, err error) {
	if errors.Is(err, errLineTooLong) {
		conn.Write([]byte(`{"error":"` + err.Error() + `"}` + "\n"))
	}
}

func (s *Server) request(req Request, remote bool, kept *keptConn) Response {
	var data any
	var err error
	if remote && req.Op == "remote.pair" {
		err = errors.New("rex: this machine is allowed there already")
	} else if remote && !remoteAllowed(req.Op) {
		log.Printf("rex: denied %q to a remote connection", req.Op)
		err = fmt.Errorf("%s is not allowed remotely", req.Op)
	} else {
		data, err = s.do(req, kept)
	}
	res := Response{ID: req.ID}
	if err != nil {
		res.Error = err.Error()
	} else if data != nil {
		res.Data, _ = json.Marshal(data)
	}
	return res
}

func (s *Server) session(id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ss := s.sessions[id]; ss != nil {
		return ss, nil
	}
	return nil, fmt.Errorf("no session %q", id)
}

// do serves a request of a control connection, kept alive when kept is
// not nil.
func (s *Server) do(req Request, kept *keptConn) (any, error) {
	if strings.HasPrefix(req.Op, "remote.") {
		return s.remoteDo(req)
	}
	switch req.Op {
	case "hello":
		select {
		case <-s.hostDone:
		case <-time.After(3 * time.Second):
		}
		h := Hello{Version: ProtocolVersion, PID: os.Getpid(), Started: s.started, Host: s.host, Exe: s.exe, ExeTime: s.exeTime, Upgrade: canUpgrade}
		if kept != nil {
			h.Keepalive, h.Conn = keepaliveTimeout.get(), kept.id
		}
		return h, nil
	case "ping":
		return nil, nil
	case "list":
		s.mu.Lock()
		all := make([]*session, 0, len(s.order))
		for _, id := range s.order {
			all = append(all, s.sessions[id])
		}
		s.mu.Unlock()
		infos := make([]SessionInfo, len(all))
		for i, ss := range all {
			infos[i] = ss.info()
		}
		return infos, nil
	case "create":
		s.ops.RLock()
		defer s.ops.RUnlock()
		o := CreateOptions{}
		if req.Create != nil {
			o = *req.Create
		}
		id := newID()
		ss, err := newSession(id, o)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.sessions[id] = ss
		s.order = append(s.order, id)
		s.mu.Unlock()
		return ss.info(), nil
	case "kill":
		s.ops.RLock()
		defer s.ops.RUnlock()
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, nil // already gone
		}
		ss.kill()
		s.mu.Lock()
		delete(s.sessions, req.SID)
		s.order = slices.DeleteFunc(s.order, func(id string) bool { return id == req.SID })
		if len(s.sessions) == 0 {
			s.idleFrom = time.Now()
		}
		s.mu.Unlock()
		return nil, nil
	case "resize":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		ss.resize(req.Cols, req.Rows)
		return nil, nil
	case "getLayout":
		s.mu.Lock()
		l := s.layout
		s.mu.Unlock()
		if l == nil {
			return nil, nil
		}
		return l, nil
	case "setLayout":
		if !json.Valid(req.Layout) {
			return nil, errors.New("layout is not JSON")
		}
		l := bytes.Clone(req.Layout)
		s.mu.Lock()
		s.layout = l
		s.mu.Unlock()
		path := filepath.Join(Dir(), "layout.json")
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, l, 0o600); err == nil {
			os.Rename(tmp, path)
		}
		return nil, nil
	case "upgrade":
		// Once it runs, the connection closes with this executable.
		return nil, s.upgrade(req.Exe)
	case "shutdown":
		go func() {
			time.Sleep(50 * time.Millisecond)
			s.shutdown()
		}()
		return nil, nil
	}
	return nil, fmt.Errorf("unknown op %q", req.Op)
}

func newID() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Spawn starts a server in the background, as a process of its own that
// outlives the app: the executable run with -server.
func Spawn() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(Dir(), "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd := exec.Command(exe, "-server")
	cmd.Env = serverEnv()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, logf, logf
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
