package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"github.com/Pommesmajo08/lin_ad/internal/windowsagent"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func token() (string, error) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	var mode uint32
	ok, _, _ := kernel.NewProc("GetConsoleMode").Call(os.Stdin.Fd(), uintptr(unsafe.Pointer(&mode)))
	if ok != 0 {
		kernel.NewProc("SetConsoleMode").Call(os.Stdin.Fd(), uintptr(mode&^2))
		defer kernel.NewProc("SetConsoleMode").Call(os.Stdin.Fd(), uintptr(mode))
	}
	fmt.Fprint(os.Stderr, "Enrollment-Token (verdeckt): ")
	s, e := bufio.NewReader(io.LimitReader(os.Stdin, 130)).ReadString('\n')
	fmt.Fprintln(os.Stderr)
	if e != nil && e != io.EOF {
		return "", e
	}
	s = strings.TrimSpace(s)
	if !windowsagent.Hex(s, 64) {
		return "", errors.New("invalid enrollment token")
	}
	return s, nil
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("commands: enroll, check-connection, doctor, disable-logon, once, run, service")
	}
	mode := os.Args[1]
	f := flag.NewFlagSet(mode, flag.ContinueOnError)
	dir := f.String("state-dir", filepath.Join(os.Getenv("ProgramData"), "Linad", "agent"), "private SYSTEM/Administrators-only state directory")
	server := f.String("server", "", "directory URL")
	device := f.String("device-id", "", "Windows device ID")
	ca := f.String("ca-file", "", "optional additional CA PEM file")
	replace := f.Bool("replace", false, "rotate credential for the same device/server")
	http := f.Bool("allow-http", true, "deprecated: HTTP is allowed by default")
	if e := f.Parse(os.Args[2:]); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if mode == "check-connection" {
		c, e := windowsagent.New(windowsagent.Config{Server: *server, DeviceID: *device, CAFile: *ca, AllowHTTP: *http})
		if e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if e = c.Health(ctx); e != nil {
			return e
		}
		fmt.Println("OK: Serveradresse, Verbindung und TLS-Konfiguration geprüft.")
		return nil
	}
	if e := windowsagent.VerifyPrivate(*dir); e != nil {
		return e
	}
	if mode == "disable-logon" {
		unlock, e := windowsagent.SingleInstance()
		if e != nil {
			return e
		}
		defer unlock()
		return windowsagent.DisableManagedAccounts(*dir)
	}
	config := filepath.Join(*dir, "client.json")
	journal := filepath.Join(*dir, "jobs.json")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if mode == "enroll" {
		if _, e := os.Lstat(config); e == nil {
			if !*replace {
				return errors.New("already enrolled; existing credential retained")
			}
			old, e := windowsagent.LoadConfig(config)
			if e != nil {
				return e
			}
			if *server == "" {
				*server = old.Config.Server
			}
			if *device == "" {
				*device = old.Config.DeviceID
			}
			if *ca == "" {
				*ca = old.Config.CAFile
			}
			if *server != old.Config.Server || *device != old.Config.DeviceID {
				return errors.New("credential rotation must keep server/device")
			}
			if old.Config.AllowHTTP {
				*http = true
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		c, e := windowsagent.New(windowsagent.Config{Server: *server, DeviceID: *device, CAFile: *ca, AllowHTTP: *http})
		if e != nil {
			return e
		}
		if e = c.Health(ctx); e != nil {
			return e
		}
		s, e := token()
		if e != nil {
			return e
		}
		enrollCtx, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		return c.Enroll(enrollCtx, s, config)
	}
	c, e := windowsagent.LoadConfig(config)
	if e != nil {
		return e
	}
	if mode == "doctor" || mode == "check-logon" {
		if e = c.Health(ctx); e != nil {
			return e
		}
		var identity struct {
			OS string `json:"os"`
		}
		if e = c.Request(ctx, "GET", "/v1/machine/info", nil, &identity, true); e != nil {
			return e
		}
		if identity.OS != "windows" {
			return errors.New("credential belongs to a non-Windows device")
		}
		if mode == "check-logon" {
			var accounts []directory.WindowsAccount
			if e = c.Request(ctx, "GET", "/v1/machine/windows/users", nil, &accounts, true); e != nil {
				return e
			}
			manager := &windowsagent.LogonManager{Client: c, Dir: *dir, Backend: windowsagent.NativeAccounts{}}
			if e = manager.Check(accounts); e != nil {
				return e
			}
			fmt.Printf("Windows-Anmelde-API bereit; freigegebene Konten: %d\n", len(accounts))
		}
		fmt.Printf("OK: Server %s; Windows-Gerät %s; geschützte Konfiguration.\n", c.Config.Server, c.Config.DeviceID)
		return nil
	}
	unlock, e := windowsagent.SingleInstance()
	if e != nil {
		return e
	}
	defer unlock()
	execute := func(ctx context.Context, r directory.JobRun) (int, string) { return windowsagent.Execute(ctx, r, *dir) }
	logFile, e := windowsagent.OpenLog(filepath.Join(*dir, "agent.log"))
	if e != nil {
		return e
	}
	log.SetOutput(io.MultiWriter(logFile, os.Stderr))
	switch mode {
	case "once":
		_, e = windowsagent.Once(context.Background(), c, journal, execute)
		return e
	case "run":
		windowsagent.RunDevice(context.Background(), c, journal, *dir, execute)
		return nil
	case "service":
		return windowsagent.Serve(func(ctx context.Context) { windowsagent.RunDevice(ctx, c, journal, *dir, execute) })
	default:
		return errors.New("unknown command")
	}
}
