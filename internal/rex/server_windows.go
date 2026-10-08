package rex

import (
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// lockFile locks f for this process, or fails at once when another holds
// it.
func lockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
}

// detached starts the server without a console, out of the app's process
// group and, when it may leave it, its job, so that it outlives the app.
func detached() *syscall.SysProcAttr {
	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	var job windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&job)), uint32(unsafe.Sizeof(job)), nil) == nil &&
		job.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK != 0 {
		flags |= windows.CREATE_BREAKAWAY_FROM_JOB
	}
	return &syscall.SysProcAttr{CreationFlags: flags}
}

var (
	kernel32                               = windows.NewLazySystemDLL("kernel32.dll")
	procGetPhysicallyInstalledSystemMemory = kernel32.NewProc("GetPhysicallyInstalledSystemMemory")
	procGetSystemPowerStatus               = kernel32.NewProc("GetSystemPowerStatus")
)

// hostInfo describes this machine.
func hostInfo() HostInfo {
	h := HostInfo{}
	if u, err := user.Current(); err == nil {
		h.User = u.Username[strings.LastIndexByte(u.Username, '\\')+1:] // without the domain
		h.Home = u.HomeDir
	}
	h.Name, _ = os.Hostname()
	value := func(path, name string) string {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err != nil {
			return ""
		}
		defer k.Close()
		v, _, _ := k.GetStringValue(name)
		return strings.TrimSpace(v)
	}
	const version = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	h.OS = value(version, "ProductName")
	if build, _ := strconv.Atoi(value(version, "CurrentBuildNumber")); build >= 22000 {
		// Windows 11 still names itself Windows 10 there.
		h.OS = strings.Replace(h.OS, "Windows 10", "Windows 11", 1)
	}
	if v := value(version, "DisplayVersion"); v != "" {
		h.OS += " " + v
	}
	h.Chip = value(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString")
	h.Model = value(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemProductName")
	if placeholder(h.Model) {
		h.Model = "Windows PC"
	}
	var kb uint64
	if r, _, _ := procGetPhysicallyInstalledSystemMemory.Call(uintptr(unsafe.Pointer(&kb))); r != 0 {
		h.Memory = kb << 10
	}
	// SYSTEM_POWER_STATUS: a BatteryFlag of 128 is no battery, 255 unknown.
	var power struct {
		ACLineStatus, BatteryFlag, BatteryLifePercent, SystemStatusFlag uint8
		BatteryLifeTime, BatteryFullLifeTime                            uint32
	}
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&power))); r != 0 {
		h.Portable = power.BatteryFlag != 128 && power.BatteryFlag != 255
	}
	return h
}

// placeholder tells the names that boards without a model of their own
// give in its place.
func placeholder(model string) bool {
	switch strings.ToLower(model) {
	case "", "system product name", "to be filled by o.e.m.", "default string", "not applicable", "none", "o.e.m.":
		return true
	}
	return false
}
