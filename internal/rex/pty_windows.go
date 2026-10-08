package rex

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procUpdateProcThreadAttribute = kernel32.NewProc("UpdateProcThreadAttribute")
	procSetConsoleCtrlHandler     = kernel32.NewProc("SetConsoleCtrlHandler")
)

// pty is a pseudo console (ConPTY) and the process it runs.
type pty struct {
	mu      sync.Mutex
	console windows.Handle // 0 once closed
	process windows.Handle // 0 once waited for
	// in takes what is typed, and out has what the console shows.
	in, out   *os.File
	processID int
}

// startPTY runs argv (argv[0] as the program sees it, path the file) in a
// new pseudo console of cols×rows.
func startPTY(path string, argv []string, dir string, env []string, cols, rows int) (*pty, error) {
	// The console reads what is typed from inR and writes what it shows to
	// outW; the session writes to inW and reads outR.
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("pty: %w", err)
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		windows.CloseHandle(inR)
		windows.CloseHandle(inW)
		return nil, fmt.Errorf("pty: %w", err)
	}
	var console windows.Handle
	err := windows.CreatePseudoConsole(coord(cols, rows), inR, outW, 0, &console)
	// The console has its own copies of its ends.
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)
	if err != nil {
		windows.CloseHandle(inW)
		windows.CloseHandle(outR)
		return nil, fmt.Errorf("pty: CreatePseudoConsole: %w", err)
	}
	p := &pty{console: console, in: os.NewFile(uintptr(inW), "conpty-in"), out: os.NewFile(uintptr(outR), "conpty-out")}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir, _ = os.UserHomeDir()
	}
	if err := p.spawn(path, argv, dir, env); err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}

func (p *pty) spawn(path string, argv []string, dir string, env []string) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fmt.Errorf("pty: %w", err)
	}
	defer attrs.Delete()
	if r, _, err := procUpdateProcThreadAttribute.Call(uintptr(unsafe.Pointer(attrs.List())), 0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(p.console), unsafe.Sizeof(p.console), 0, 0); r == 0 {
		return fmt.Errorf("pty: UpdateProcThreadAttribute: %w", err)
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	// The console's own standard handles: without invalid ones, the
	// program would write to the server's log.
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput, si.StdOutput, si.StdErr = windows.InvalidHandle, windows.InvalidHandle, windows.InvalidHandle
	cmdline, err := windows.UTF16PtrFromString(commandLine(path, argv))
	if err != nil {
		return err
	}
	wdir, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	block := environmentBlock(env)
	// A process ignores Ctrl-C when the one that started it did, as the
	// server does in a process group of its own: Ctrl-C would interrupt
	// nothing that runs in its consoles.
	procSetConsoleCtrlHandler.Call(0, 0)
	var pi windows.ProcessInformation
	err = windows.CreateProcess(nil, cmdline, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		&block[0], wdir, &si.StartupInfo, &pi)
	if err != nil {
		return fmt.Errorf("pty: starting %s: %w", path, err)
	}
	windows.CloseHandle(pi.Thread)
	p.process, p.processID = pi.Process, int(pi.ProcessId)
	return nil
}

func (p *pty) read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *pty) write(b []byte) (int, error) { return p.in.Write(b) }

// close closes the console, which ends the processes attached to it: read
// ends once it read what the console still had to show.
func (p *pty) close() error {
	p.mu.Lock()
	console := p.console
	p.console = 0
	p.mu.Unlock()
	if console == 0 {
		return nil
	}
	p.in.Close()
	// Before Windows 11 24H2, ClosePseudoConsole waits for its output to be
	// read, which read goes on doing.
	go func() {
		windows.ClosePseudoConsole(console)
		p.out.Close()
	}()
	return nil
}

func (p *pty) resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.console == 0 {
		return os.ErrClosed
	}
	return windows.ResizePseudoConsole(p.console, coord(cols, rows))
}

func (p *pty) pid() int { return p.processID }

// wait waits for the process and returns its exit code.
func (p *pty) wait() int {
	if _, err := windows.WaitForSingleObject(p.process, windows.INFINITE); err != nil {
		return -1
	}
	var code uint32
	err := windows.GetExitCodeProcess(p.process, &code)
	p.mu.Lock()
	windows.CloseHandle(p.process)
	p.process = 0
	p.mu.Unlock()
	if err != nil {
		return -1
	}
	return int(code)
}

// hangup ends the session: closing the console ends its processes, as
// when a console window closes.
func (p *pty) hangup() { p.close() }

// kill kills the session's process.
func (p *pty) kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.process != 0 {
		windows.TerminateProcess(p.process, 1)
	}
}

// foreground returns the program the shell runs: the child it started
// last, past the command interpreters that run batch files; 0 when it
// runs none.
func (p *pty) foreground() int {
	tree := processTree()
	fg := 0
	for parent := uint32(p.processID); ; {
		child := tree.newestChild(parent)
		if child == 0 {
			break
		}
		fg, parent = int(child), child
		if !strings.EqualFold(tree.name[child], "cmd.exe") {
			break
		}
	}
	return fg
}

// coord packs the size of a console.
func coord(cols, rows int) windows.Coord {
	return windows.Coord{X: int16(max(1, min(cols, 0x7FFF))), Y: int16(max(1, min(rows, 0x7FFF)))}
}

// commandLine joins a program and its arguments as programs split them;
// argv[0] is the program's name.
func commandLine(path string, argv []string) string {
	parts := []string{windows.EscapeArg(path)}
	for _, a := range argv[min(1, len(argv)):] {
		parts = append(parts, windows.EscapeArg(a))
	}
	return strings.Join(parts, " ")
}

// environmentBlock encodes env as CreateProcess takes it: NUL-terminated
// UTF-16 strings, then another NUL.
func environmentBlock(env []string) []uint16 {
	var b []uint16
	for _, kv := range env {
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	if len(b) == 0 {
		b = append(b, 0)
	}
	return append(b, 0)
}

// shellCommand returns the program, its arguments and the shell's name
// for a session: when cmd is empty, $SHELL when it names a program of
// Windows (not one of MSYS's paths), else PowerShell, else the command
// interpreter.
func shellCommand(cmd []string) (path string, argv []string, name string, err error) {
	if len(cmd) > 0 {
		path, err = exec.LookPath(cmd[0])
		if err != nil {
			return "", nil, "", err
		}
		return path, cmd, exeName(cmd[0]), nil
	}
	for _, sh := range []string{os.Getenv("SHELL"), "pwsh.exe", "powershell.exe", os.Getenv("COMSPEC"), "cmd.exe"} {
		if sh == "" {
			continue
		}
		if path, err = exec.LookPath(sh); err == nil {
			name = exeName(sh)
			argv = []string{filepath.Base(sh)}
			if strings.EqualFold(name, "pwsh") || strings.EqualFold(name, "powershell") {
				argv = append(argv, "-NoExit", "-Command", powershellPrompt)
			}
			return path, argv, name, nil
		}
	}
	return "", nil, "", errors.New("rex: no shell found")
}

// powershellPrompt makes PowerShell's prompt set the process's directory
// to its location, which cd changes alone: the server then finds where it
// is as it does for other shells. $? stays as the command left it, for the
// prompt of the profile, which runs first.
const powershellPrompt = `$global:__GoRexPrompt = $function:prompt; ` +
	`function global:prompt { $ok = $global:?; ` +
	`try { $l = $executionContext.SessionState.Path.CurrentLocation; ` +
	`if ($l.Provider.Name -eq 'FileSystem') { [Environment]::CurrentDirectory = $l.ProviderPath } } catch {}; ` +
	`if (-not $ok) { Write-Error '' -ErrorAction Ignore }; & $global:__GoRexPrompt }`

// processAlive reports whether a process runs.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, 0)
	return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
}
