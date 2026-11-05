package windowsagent

import (
	"context"
	"errors"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const ServiceName = "LinadAgent"

type serviceStatus struct{ Type, State, Accepted, Win32Exit, SpecificExit, Checkpoint, WaitHint uint32 }
type serviceEntry struct {
	Name *uint16
	Main uintptr
}

// Serve connects to SCM, drains an accepted job on stop, and reports stop checkpoints.
func Serve(work func(context.Context)) error {
	name, _ := syscall.UTF16PtrFromString(ServiceName)
	var mu sync.Mutex
	var handle uintptr
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	status := serviceStatus{Type: 0x10, State: 2, WaitHint: 30000}
	publish := func() { advapi.NewProc("SetServiceStatus").Call(handle, uintptr(unsafe.Pointer(&status))) }
	handler := syscall.NewCallback(func(control, event, data, user uintptr) uintptr {
		mu.Lock()
		defer mu.Unlock()
		switch control {
		case 1, 5:
			status.State = 3
			status.Accepted = 0
			status.WaitHint = 30000
			publish()
			cancel()
		case 4:
			publish()
		}
		return 0
	})
	var mainErr error
	main := syscall.NewCallback(func(argc uintptr, argv uintptr) uintptr {
		h, _, e := advapi.NewProc("RegisterServiceCtrlHandlerExW").Call(uintptr(unsafe.Pointer(name)), handler, 0)
		if h == 0 {
			mainErr = e
			return 0
		}
		mu.Lock()
		handle = h
		status.State = 4
		status.Accepted = 5
		status.WaitHint = 0
		publish()
		mu.Unlock()
		done := make(chan struct{})
		go func() { defer close(done); work(ctx) }()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				mu.Lock()
				status.State = 1
				status.Accepted = 0
				status.WaitHint = 0
				publish()
				mu.Unlock()
				return 0
			case <-ticker.C:
				mu.Lock()
				if status.State == 3 {
					status.Checkpoint++
					publish()
				}
				mu.Unlock()
			}
		}
	})
	table := [2]serviceEntry{{Name: name, Main: main}, {}}
	ok, _, e := advapi.NewProc("StartServiceCtrlDispatcherW").Call(uintptr(unsafe.Pointer(&table[0])))
	if ok == 0 {
		if e == syscall.Errno(1063) {
			return errors.New("service mode must be started by Windows Service Control Manager")
		}
		return e
	}
	return mainErr
}
