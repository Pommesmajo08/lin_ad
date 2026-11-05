package windowsagent

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const LogonPipe = `\\.\pipe\LinadLogon-v1`

func pipeIO(ctx context.Context, h syscall.Handle, b []byte, write bool) error {
	for len(b) > 0 {
		event, e := createPipeEvent()
		if e != nil {
			return e
		}
		overlapped := syscall.Overlapped{HEvent: event}
		var n uint32
		method := "ReadFile"
		if write {
			method = "WriteFile"
		}
		ok, _, callErr := kernel.NewProc(method).Call(uintptr(h), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&overlapped)))
		if ok == 0 && callErr != syscall.ERROR_IO_PENDING {
			syscall.CloseHandle(event)
			return callErr
		}
		if ok == 0 {
			for {
				state, e := syscall.WaitForSingleObject(event, 100)
				if e != nil {
					syscall.CancelIoEx(h, &overlapped)
					syscall.WaitForSingleObject(event, syscall.INFINITE)
					syscall.CloseHandle(event)
					return e
				}
				if state == 0 {
					break
				}
				if ctx.Err() != nil {
					syscall.CancelIoEx(h, &overlapped)
					syscall.WaitForSingleObject(event, syscall.INFINITE)
					syscall.CloseHandle(event)
					return ctx.Err()
				}
			}
			ok, _, callErr = kernel.NewProc("GetOverlappedResult").Call(uintptr(h), uintptr(unsafe.Pointer(&overlapped)), uintptr(unsafe.Pointer(&n)), 0)
			if ok == 0 {
				syscall.CloseHandle(event)
				return callErr
			}
		}
		syscall.CloseHandle(event)
		if n == 0 {
			return errors.New("pipe disconnected")
		}
		b = b[n:]
	}
	return nil
}
func pipeResponse(ctx context.Context, h syscall.Handle, grant Grant, success bool) error {
	status := uint32(1)
	if success {
		status = 0
	}
	buf := binary.LittleEndian.AppendUint32(nil, status)
	for _, s := range []string{grant.Login, grant.Password, grant.Ticket} {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(s)))
		buf = append(buf, []byte(s)...)
	}
	defer clear(buf)
	if e := pipeIO(ctx, h, buf, true); e != nil {
		return e
	}
	// Wait for consumption before disconnecting: DisconnectNamedPipe discards unread data.
	ack := make([]byte, 1)
	if e := pipeIO(ctx, h, ack, false); e != nil {
		return e
	}
	if ack[0] != 0x7f {
		return errors.New("invalid response acknowledgement")
	}
	return nil
}
func (m *LogonManager) ServePipe(ctx context.Context) error {
	sddl, _ := syscall.UTF16PtrFromString("O:SYG:SYD:P(A;;GA;;;SY)")
	var descriptor uintptr
	ok, _, e := advapi.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW").Call(uintptr(unsafe.Pointer(sddl)), 1, uintptr(unsafe.Pointer(&descriptor)), 0)
	if ok == 0 {
		return e
	}
	defer kernel.NewProc("LocalFree").Call(descriptor)
	attrs := struct {
		Length     uint32
		Descriptor uintptr
		Inherit    uint32
	}{Descriptor: descriptor}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	name, _ := syscall.UTF16PtrFromString(LogonPipe)
	raw, _, e := kernel.NewProc("CreateNamedPipeW").Call(uintptr(unsafe.Pointer(name)), 3|0x40000000|0x80000, 8, 1, 4096, 4096, 30000, uintptr(unsafe.Pointer(&attrs)))
	if raw == uintptr(syscall.InvalidHandle) {
		return fmt.Errorf("create protected logon pipe: %w", e)
	}
	h := syscall.Handle(raw)
	defer syscall.CloseHandle(h)
	for ctx.Err() == nil {
		event, e := createPipeEvent()
		if e != nil {
			return e
		}
		overlapped := syscall.Overlapped{HEvent: event}
		ok, _, e = kernel.NewProc("ConnectNamedPipe").Call(raw, uintptr(unsafe.Pointer(&overlapped)))
		if ok == 0 && e != syscall.ERROR_IO_PENDING && e != syscall.Errno(535) {
			syscall.CloseHandle(event)
			return e
		}
		if ok == 0 && e == syscall.ERROR_IO_PENDING {
			for {
				state, err := syscall.WaitForSingleObject(event, 100)
				if err != nil {
					syscall.CancelIoEx(h, &overlapped)
					syscall.WaitForSingleObject(event, syscall.INFINITE)
					syscall.CloseHandle(event)
					return err
				}
				if state == 0 {
					break
				}
				if ctx.Err() != nil {
					syscall.CancelIoEx(h, &overlapped)
					syscall.WaitForSingleObject(event, syscall.INFINITE)
					syscall.CloseHandle(event)
					return nil
				}
			}
		}
		syscall.CloseHandle(event)
		requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if e = m.serveLogon(requestCtx, h); e != nil && ctx.Err() == nil {
			log.Print("Windows logon request failed")
		}
		cancel()
		kernel.NewProc("DisconnectNamedPipe").Call(raw)
	}
	return nil
}
func (m *LogonManager) serveLogon(ctx context.Context, h syscall.Handle) error {
	header := make([]byte, 16)
	if e := pipeIO(ctx, h, header, false); e != nil {
		return e
	}
	code := binary.LittleEndian.Uint32(header[4:])
	n, p := binary.LittleEndian.Uint32(header[8:]), binary.LittleEndian.Uint32(header[12:])
	if binary.LittleEndian.Uint32(header) != 0x4c414431 || n == 0 || n > 64 || p > 512 || (code != 1 && code != 2) {
		return errors.New("invalid logon request")
	}
	buf := make([]byte, int(n+p))
	defer clear(buf)
	if e := pipeIO(ctx, h, buf, false); e != nil {
		return e
	}
	if e := VerifyPrivate(filepath.Join(m.Dir, "logon.enabled")); e != nil {
		return pipeResponse(ctx, h, Grant{}, false)
	}
	first, second := string(buf[:n]), string(buf[n:])
	defer func() { first = ""; second = "" }()
	if code == 2 {
		e := m.Complete(first, second)
		return pipeResponse(ctx, h, Grant{}, e == nil)
	}
	grant, e := m.Authenticate(ctx, first, second)
	first = ""
	second = ""
	if e != nil {
		return pipeResponse(ctx, h, Grant{}, false)
	}
	if e = pipeResponse(ctx, h, grant, true); e != nil {
		_ = m.Complete(grant.Login, grant.Ticket)
		return e
	}
	return nil
}

// RunDevice runs jobs and the optional logon broker independently; accepted jobs drain on stop.
func RunDevice(ctx context.Context, c *Client, journal, dir string, execute Executor) {
	if _, e := os.Stat(filepath.Join(dir, "logon.enabled")); e != nil {
		Run(ctx, c, journal, execute)
		return
	}
	manager := &LogonManager{Client: c, Dir: dir, Backend: NativeAccounts{}}
	if e := manager.DisableAll(); e != nil {
		log.Printf("Windows logon disabled: %v", e)
		Run(ctx, c, journal, execute)
		return
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if e := manager.ServePipe(ctx); e != nil {
			log.Printf("Windows logon broker: %v", e)
		}
	}()
	go func() {
		defer wg.Done()
		syncTimer := time.NewTicker(30 * time.Second)
		defer syncTimer.Stop()
		expire := time.NewTicker(time.Second)
		defer expire.Stop()
		if e := manager.Sync(ctx); e != nil {
			log.Printf("Windows account synchronization failed: %v", e)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-syncTimer.C:
				if e := manager.Sync(ctx); e != nil {
					log.Printf("Windows account synchronization failed: %v", e)
				}
			case <-expire.C:
				if e := manager.Expire(); e != nil {
					log.Printf("Windows logon lease cleanup failed: %v", e)
				}
			}
		}
	}()
	Run(ctx, c, journal, execute)
	wg.Wait()
	if e := manager.DisableAll(); e != nil {
		log.Printf("Windows accounts could not all be disabled on stop: %v", e)
	}
}

func createPipeEvent() (syscall.Handle, error) {
	h, _, e := kernel.NewProc("CreateEventW").Call(0, 1, 0, 0)
	if h == 0 {
		return 0, e
	}
	return syscall.Handle(h), nil
}
