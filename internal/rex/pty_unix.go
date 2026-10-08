//go:build darwin || linux

package rex

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// pty is a pseudo-terminal and the process it runs.
type pty struct {
	master *os.File
	cmd    *exec.Cmd
}

// startPTY runs argv (argv[0] as the program sees it, path the file) in a
// new pseudo-terminal of cols×rows.
func startPTY(path string, argv []string, dir string, env []string, cols, rows int) (*pty, error) {
	master, slaveName, err := openMaster()
	if err != nil {
		return nil, err
	}
	slave, err := os.OpenFile(slaveName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("pty: opening %s: %w", slaveName, err)
	}
	defer slave.Close()
	if err := setSize(master, cols, rows); err != nil {
		master.Close()
		return nil, err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir, _ = os.UserHomeDir()
	}
	cmd := &exec.Cmd{Path: path, Args: argv, Dir: dir, Env: env, Stdin: slave, Stdout: slave, Stderr: slave}
	// A session of its own whose controlling terminal is the slave, so that
	// the terminal's signals reach the process group in the foreground.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		master.Close()
		return nil, err
	}
	return &pty{master: master, cmd: cmd}, nil
}

func (p *pty) read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *pty) write(b []byte) (int, error) { return p.master.Write(b) }

// close closes the terminal's master: read ends, once it read what the
// terminal had.
func (p *pty) close() error { return p.master.Close() }

func (p *pty) resize(cols, rows int) error { return setSize(p.master, cols, rows) }

func (p *pty) pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// kill kills the process group of the session's process.
func (p *pty) kill() {
	if pid := p.pid(); pid > 0 {
		syscall.Kill(-pid, syscall.SIGKILL)
	}
}

func setSize(f *os.File, cols, rows int) error {
	ws := &unix.Winsize{Col: uint16(clamp(cols)), Row: uint16(clamp(rows))}
	return unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, ws)
}

func clamp(v int) int { return max(1, min(v, 0xFFFF)) }

// foreground returns the process group in the foreground of the terminal.
func (p *pty) foreground() int {
	pg, err := unix.IoctlGetInt(int(p.master.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return 0
	}
	return pg
}

// wait waits for the process and returns its exit code, 128 plus the
// signal's number for a process a signal ended.
func (p *pty) wait() int {
	err := p.cmd.Wait()
	if e, ok := errors.AsType[*exec.ExitError](err); ok {
		if ws, ok := e.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return e.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

// hangup ends the session: its processes get SIGHUP, as when a terminal
// window closes.
func (p *pty) hangup() {
	if p.cmd.Process != nil {
		syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
		if pg := p.foreground(); pg > 0 {
			syscall.Kill(-pg, syscall.SIGHUP)
		}
	}
	p.master.Close()
}

// shellCommand returns the program, its arguments and the shell's name
// for a session: the user's login shell when cmd is empty.
func shellCommand(cmd []string) (path string, argv []string, name string, err error) {
	if len(cmd) == 0 {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = defaultShell
		}
		// A login shell, as terminal apps start it: "-zsh".
		return sh, []string{"-" + filepath.Base(sh)}, filepath.Base(sh), nil
	}
	path, err = exec.LookPath(cmd[0])
	if err != nil {
		return "", nil, "", err
	}
	return path, cmd, filepath.Base(cmd[0]), nil
}

// processAlive reports whether a process runs.
func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
