package windowsagent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var netapi = syscall.NewLazyDLL("netapi32.dll")

type NativeAccounts struct{}
type userInfo1 struct {
	Name, Password         *uint16
	PasswordAge, Privilege uint32
	Home, Comment          *uint16
	Flags                  uint32
	Script                 *uint16
}

func accountSID(name string) ([]byte, string, error) {
	computer := make([]uint16, 256)
	length := uint32(len(computer))
	ok, _, err := kernel.NewProc("GetComputerNameW").Call(uintptr(unsafe.Pointer(&computer[0])), uintptr(unsafe.Pointer(&length)))
	if ok == 0 {
		return nil, "", err
	}
	name = syscall.UTF16ToString(computer) + "\\" + name
	n, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return nil, "", e
	}
	sid := make([]byte, 256)
	domain := make([]uint16, 256)
	size, domainSize := uint32(len(sid)), uint32(len(domain))
	var use uint32
	ok, _, e = advapi.NewProc("LookupAccountNameW").Call(0, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(&sid[0])), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&domain[0])), uintptr(unsafe.Pointer(&domainSize)), uintptr(unsafe.Pointer(&use)))
	if ok == 0 {
		return nil, "", e
	}
	var text *uint16
	ok, _, e = advapi.NewProc("ConvertSidToStringSidW").Call(uintptr(unsafe.Pointer(&sid[0])), uintptr(unsafe.Pointer(&text)))
	if ok == 0 {
		return nil, "", e
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(text)))
	return sid[:size], utf16String(text), nil
}
func (NativeAccounts) Lookup(name string) (LocalAccount, bool, error) {
	n, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return LocalAccount{}, false, e
	}
	var data unsafe.Pointer
	code, _, _ := netapi.NewProc("NetUserGetInfo").Call(0, uintptr(unsafe.Pointer(n)), 1, uintptr(unsafe.Pointer(&data)))
	if code == 2221 {
		return LocalAccount{}, false, nil
	}
	if code != 0 {
		return LocalAccount{}, false, fmt.Errorf("NetUserGetInfo: %d", code)
	}
	defer netapi.NewProc("NetApiBufferFree").Call(uintptr(data))
	info := (*userInfo1)(data)
	_, sid, e := accountSID(name)
	if e != nil {
		return LocalAccount{}, false, e
	}
	admin, e := nativeAdministratorMembership(name)
	if e != nil {
		return LocalAccount{}, false, e
	}
	return LocalAccount{SID: sid, Comment: utf16String(info.Comment), Enabled: info.Flags&2 == 0, Admin: admin}, true, nil
}
func (NativeAccounts) Create(name, password, comment string) error {
	n, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return e
	}
	p, e := syscall.UTF16PtrFromString(password)
	if e != nil {
		return e
	}
	defer wipeUTF16(p, len(password)+1)
	c, e := syscall.UTF16PtrFromString(comment)
	if e != nil {
		return e
	}
	info := userInfo1{Name: n, Password: p, Privilege: 1, Comment: c, Flags: 0x200 | 0x10000 | 1 | 2}
	var parameter uint32
	code, _, _ := netapi.NewProc("NetUserAdd").Call(0, 1, uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&parameter)))
	if code != 0 {
		return fmt.Errorf("NetUserAdd: %d (field %d)", code, parameter)
	}
	return nil
}
func wipeUTF16(p *uint16, n int) {
	for i := range unsafe.Slice(p, n) {
		unsafe.Slice(p, n)[i] = 0
	}
}
func setFlags(name *uint16, enabled bool) error {
	var current unsafe.Pointer
	status, _, _ := netapi.NewProc("NetUserGetInfo").Call(0, uintptr(unsafe.Pointer(name)), 1, uintptr(unsafe.Pointer(&current)))
	if status != 0 {
		return fmt.Errorf("NetUserGetInfo flags: %d", status)
	}
	flags := (*userInfo1)(current).Flags
	netapi.NewProc("NetApiBufferFree").Call(uintptr(current))
	// Preserve Windows account restrictions (for example smart-card requirements).
	// PASSWORD_CANT_CHANGE is read-only; its ACL is not modified here.
	flags &^= 2 | 0x40
	if !enabled {
		flags |= 2
	}
	code, _, _ := netapi.NewProc("NetUserSetInfo").Call(0, uintptr(unsafe.Pointer(name)), 1008, uintptr(unsafe.Pointer(&flags)), 0)
	if code != 0 {
		return fmt.Errorf("NetUserSetInfo flags: %d", code)
	}
	return nil
}
func builtinGroupName(sidText string) (string, error) {
	s, _ := syscall.UTF16PtrFromString(sidText)
	var sid uintptr
	ok, _, e := advapi.NewProc("ConvertStringSidToSidW").Call(uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(&sid)))
	if ok == 0 {
		return "", e
	}
	defer kernel.NewProc("LocalFree").Call(sid)
	name, domain := make([]uint16, 256), make([]uint16, 256)
	ns, ds := uint32(len(name)), uint32(len(domain))
	var use uint32
	ok, _, e = advapi.NewProc("LookupAccountSidW").Call(0, sid, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&ns)), uintptr(unsafe.Pointer(&domain[0])), uintptr(unsafe.Pointer(&ds)), uintptr(unsafe.Pointer(&use)))
	if ok == 0 {
		return "", e
	}
	return syscall.UTF16ToString(name), nil
}
func (NativeAccounts) Set(name, password string, enabled, admin bool) error {
	n, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return e
	}
	// Fail closed: disable first, change password and role, then optionally enable.
	if e = setFlags(n, false); e != nil {
		return e
	}
	p, e := syscall.UTF16PtrFromString(password)
	if e != nil {
		return e
	}
	defer wipeUTF16(p, len(password)+1)
	info := struct{ Password *uint16 }{p}
	code, _, _ := netapi.NewProc("NetUserSetInfo").Call(0, uintptr(unsafe.Pointer(n)), 1003, uintptr(unsafe.Pointer(&info)), 0)
	if code != 0 {
		return fmt.Errorf("NetUserSetInfo password: %d", code)
	}
	sid, _, e := accountSID(name)
	if e != nil {
		return e
	}
	for _, membership := range []struct {
		SID string
		Add bool
	}{{"S-1-5-32-545", true}, {"S-1-5-32-544", admin}} {
		group, e := builtinGroupName(membership.SID)
		if e != nil {
			return e
		}
		g, _ := syscall.UTF16PtrFromString(group)
		member := struct{ SID unsafe.Pointer }{unsafe.Pointer(&sid[0])}
		method := "NetLocalGroupDelMembers"
		if membership.Add {
			method = "NetLocalGroupAddMembers"
		}
		code, _, _ = netapi.NewProc(method).Call(0, uintptr(unsafe.Pointer(g)), 0, uintptr(unsafe.Pointer(&member)), 1)
		if code != 0 && !(membership.Add && code == 1378) && !(!membership.Add && code == 1377) {
			return fmt.Errorf("Windows local group membership: %d", code)
		}
	}
	actualAdmin, e := nativeAdministratorMembership(name)
	if e != nil {
		return e
	}
	if actualAdmin != admin {
		return errors.New("unexpected administrator membership; account remains disabled")
	}
	if enabled {
		return setFlags(n, true)
	}
	return nil
}
func (NativeAccounts) Enumerate() ([]string, error) {
	result := []string{}
	var resume uintptr
	for {
		var data unsafe.Pointer
		var read, total uint32
		code, _, _ := netapi.NewProc("NetUserEnum").Call(0, 0, 2, uintptr(unsafe.Pointer(&data)), 0xffffffff, uintptr(unsafe.Pointer(&read)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&resume)))
		if code != 0 && code != 234 {
			return nil, fmt.Errorf("NetUserEnum: %d", code)
		}
		if data != nil {
			users := unsafe.Slice((**uint16)(data), int(read))
			for _, name := range users {
				result = append(result, strings.ToLower(utf16String(name)))
			}
			netapi.NewProc("NetApiBufferFree").Call(uintptr(data))
		}
		if code == 0 {
			break
		}
		if read == 0 {
			return nil, errors.New("NetUserEnum made no progress")
		}
	}
	return result, nil
}

// DisableManagedAccounts needs no live server or usable device key during recovery.
func DisableManagedAccounts(dir string) error {
	var registry AccountRegistry
	if e := Read(filepath.Join(dir, "windows-accounts.json"), &registry); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	manager := &LogonManager{Client: &Client{Config: Config{DeviceID: registry.DeviceID, Server: registry.Server}}, Dir: dir, Backend: NativeAccounts{}}
	return manager.DisableAll()
}

func nativeAdministratorMembership(name string) (bool, error) {
	n, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return false, e
	}
	var data unsafe.Pointer
	var read, total uint32
	code, _, _ := netapi.NewProc("NetUserGetLocalGroups").Call(0, uintptr(unsafe.Pointer(n)), 0, 1, uintptr(unsafe.Pointer(&data)), 0xffffffff, uintptr(unsafe.Pointer(&read)), uintptr(unsafe.Pointer(&total)))
	if code != 0 {
		return false, fmt.Errorf("NetUserGetLocalGroups: %d", code)
	}
	if data != nil {
		defer netapi.NewProc("NetApiBufferFree").Call(uintptr(data))
	}
	group, e := builtinGroupName("S-1-5-32-544")
	if e != nil {
		return false, e
	}
	if read > 0 && data != nil {
		for _, p := range unsafe.Slice((**uint16)(data), int(read)) {
			if strings.EqualFold(utf16String(p), group) {
				return true, nil
			}
		}
	}
	return false, nil
}
