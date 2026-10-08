package rex

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// inspect returns the name, arguments and working directory of a process.
func inspect(pid int) (p procInfo) {
	if pid <= 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		// An elevated process tells its name alone.
		if h, err = windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)); err != nil {
			return
		}
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
		p.name = exeName(windows.UTF16ToString(buf[:n]))
	}
	p.args = processArgs(h)
	p.dir = processDir(h)
	return p
}

// exeName names a program by its file, without .exe.
func exeName(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); strings.EqualFold(ext, ".exe") {
		base = base[:len(base)-len(ext)]
	}
	return base
}

// unicodeString is a UNICODE_STRING of another process, whose Buffer is
// an address there.
type unicodeString struct {
	Length, MaximumLength uint16
	Buffer                uintptr
}

// processArgs returns the arguments of a process, from its command line.
func processArgs(h windows.Handle) []string {
	buf := make([]byte, 4096)
	for {
		var n uint32
		err := windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, unsafe.Pointer(&buf[0]), uint32(len(buf)), &n)
		if err == windows.STATUS_INFO_LENGTH_MISMATCH && int(n) > len(buf) && n < 1<<20 {
			buf = make([]byte, n)
			continue
		}
		if err != nil {
			return nil
		}
		break
	}
	// A UNICODE_STRING, whose text follows it in buf.
	us := (*unicodeString)(unsafe.Pointer(&buf[0]))
	off := unsafe.Sizeof(*us)
	if us.Length == 0 || int(off)+int(us.Length) > len(buf) {
		return nil
	}
	line := windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[off])), us.Length/2))
	args, err := windows.DecomposeCommandLine(line)
	if err != nil {
		return nil
	}
	return args
}

// processBasicInformation is PROCESS_BASIC_INFORMATION, with the addresses
// of another process as numbers.
type processBasicInformation struct {
	ExitStatus                   uintptr
	PebBaseAddress               uintptr
	AffinityMask                 uintptr
	BasePriority                 uintptr
	UniqueProcessID              uintptr
	InheritedFromUniqueProcessID uintptr
}

// processDir returns the working directory of a process, from its
// parameters in its memory.
func processDir(h windows.Handle) string {
	var wow bool
	if windows.IsWow64Process(h, &wow) != nil || wow {
		// The parameters of a 32-bit process are elsewhere.
		return ""
	}
	var pbi processBasicInformation
	if windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), nil) != nil || pbi.PebBaseAddress == 0 {
		return ""
	}
	read := func(addr uintptr, p unsafe.Pointer, size uintptr) bool {
		return windows.ReadProcessMemory(h, addr, (*byte)(p), size, nil) == nil
	}
	var params uintptr
	if !read(pbi.PebBaseAddress+unsafe.Offsetof(windows.PEB{}.ProcessParameters), unsafe.Pointer(&params), unsafe.Sizeof(params)) || params == 0 {
		return ""
	}
	// CurrentDirectory starts with its path, DosPath.
	var dir unicodeString
	if !read(params+unsafe.Offsetof(windows.RTL_USER_PROCESS_PARAMETERS{}.CurrentDirectory), unsafe.Pointer(&dir), unsafe.Sizeof(dir)) || dir.Length == 0 {
		return ""
	}
	text := make([]uint16, dir.Length/2)
	if !read(dir.Buffer, unsafe.Pointer(&text[0]), uintptr(len(text))*2) {
		return ""
	}
	d := windows.UTF16ToString(text)
	if len(d) > 3 {
		d = strings.TrimSuffix(d, `\`) // C:\ keeps it
	}
	return d
}

// procTree is the processes of the system by their parents.
type procTree struct {
	children map[uint32][]uint32
	name     map[uint32]string
}

var procTreeCache struct {
	sync.Mutex
	at   time.Time
	tree procTree
}

// processTree returns the processes of the system as they were a moment
// ago: the sessions of a "list" share them.
func processTree() procTree {
	procTreeCache.Lock()
	defer procTreeCache.Unlock()
	if time.Since(procTreeCache.at) < 100*time.Millisecond {
		return procTreeCache.tree
	}
	t := procTree{children: map[uint32][]uint32{}, name: map[uint32]string{}}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return t
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID != e.ParentProcessID {
			t.children[e.ParentProcessID] = append(t.children[e.ParentProcessID], e.ProcessID)
		}
		t.name[e.ProcessID] = windows.UTF16ToString(e.ExeFile[:])
	}
	procTreeCache.at, procTreeCache.tree = time.Now(), t
	return t
}

// newestChild returns the child of parent that started last, or 0. A
// process that started before parent only has the id parent had before it.
func (t procTree) newestChild(parent uint32) uint32 {
	born, ok := started(parent)
	if !ok {
		return 0
	}
	var newest uint32
	var newestAt int64
	for _, c := range t.children[parent] {
		if at, ok := started(c); ok && at >= born && at >= newestAt {
			newest, newestAt = c, at
		}
	}
	return newest
}

// started returns when a process started, in 100-nanosecond intervals.
func started(pid uint32) (int64, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return 0, false
	}
	return created.Nanoseconds() / 100, true
}
