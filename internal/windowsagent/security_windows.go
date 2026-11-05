package windowsagent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var advapi = syscall.NewLazyDLL("advapi32.dll")
var kernel = syscall.NewLazyDLL("kernel32.dll")

func VerifyPrivate(path string) error {
	if _, e := os.Lstat(path); e != nil {
		return e
	}
	for part := filepath.Clean(path); ; part = filepath.Dir(part) {
		p, e := syscall.UTF16PtrFromString(part)
		if e != nil {
			return e
		}
		attr, e := syscall.GetFileAttributes(p)
		if e != nil {
			return e
		}
		if attr&0x400 != 0 {
			return errors.New("agent paths must not contain reparse points")
		}
		if filepath.Dir(part) == part {
			break
		}
	}
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	var owner, dacl, sd uintptr
	code, _, _ := advapi.NewProc("GetNamedSecurityInfoW").Call(uintptr(unsafe.Pointer(p)), 1, 5, uintptr(unsafe.Pointer(&owner)), 0, uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 {
		return syscall.Errno(code)
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	if dacl == 0 {
		return errors.New("agent path has unrestricted ACL")
	}
	var ownerText *uint16
	ok, _, e := advapi.NewProc("ConvertSidToStringSidW").Call(owner, uintptr(unsafe.Pointer(&ownerText)))
	if ok == 0 {
		return e
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(ownerText)))
	sid := utf16String(ownerText)
	if sid != "S-1-5-18" && sid != "S-1-5-32-544" {
		return errors.New("agent paths must be owned by SYSTEM or Administrators")
	}
	var text *uint16
	ok, _, e = advapi.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW").Call(sd, 1, 4, uintptr(unsafe.Pointer(&text)), 0)
	if ok == 0 {
		return e
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(text)))
	sddl := utf16String(text)
	count := 0
	for _, piece := range strings.Split(sddl, "(")[1:] {
		entry := strings.Split(strings.SplitN(piece, ")", 2)[0], ";")
		if len(entry) != 6 || entry[0] != "A" || (entry[5] != "SY" && entry[5] != "BA") {
			return fmt.Errorf("agent path permits non-administrator access: %s", path)
		}
		count++
	}
	if count == 0 {
		return errors.New("agent ACL has no allowed principals")
	}
	return nil
}
func utf16String(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), uintptr(n)*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}
func replaceFile(from, to string) error {
	a, e := syscall.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	b, e := syscall.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	ok, _, e := kernel.NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 9)
	if ok == 0 {
		return e
	}
	return nil
}
func SingleInstance() (func(), error) {
	runtime.LockOSThread()
	name, _ := syscall.UTF16PtrFromString("Global\\LinadWindowsAgent")
	h, _, e := kernel.NewProc("CreateMutexW").Call(0, 1, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		runtime.UnlockOSThread()
		return nil, e
	}
	if e == syscall.Errno(183) {
		syscall.CloseHandle(syscall.Handle(h))
		runtime.UnlockOSThread()
		return nil, errors.New("another agent instance is running")
	}
	return func() {
		kernel.NewProc("ReleaseMutex").Call(h)
		syscall.CloseHandle(syscall.Handle(h))
		runtime.UnlockOSThread()
	}, nil
}

func ProtectNew(path string) error {
	text, _ := syscall.UTF16PtrFromString("S-1-5-32-544")
	var sid uintptr
	ok, _, e := advapi.NewProc("ConvertStringSidToSidW").Call(uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(&sid)))
	if ok == 0 {
		return e
	}
	defer kernel.NewProc("LocalFree").Call(sid)
	name, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	code, _, _ := advapi.NewProc("SetNamedSecurityInfoW").Call(uintptr(unsafe.Pointer(name)), 1, 1, sid, 0, 0, 0)
	if code != 0 {
		return syscall.Errno(code)
	}
	return nil
}
