package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Pommesmajo08/lin_ad/internal/linuxclient"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func readEnrollmentToken() (string, error) {
	fmt.Fprint(os.Stderr, "Einmaligen Enrollment-Token eingeben: ")
	var term syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&term)))
	if errno == 0 {
		old := term
		term.Lflag &^= syscall.ECHO
		syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&term)))
		defer syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
	}
	token, err := bufio.NewReader(io.LimitReader(os.Stdin, 130)).ReadString('\n')
	fmt.Fprintln(os.Stderr)
	if err != nil && err != io.EOF {
		return "", err
	}
	token = strings.TrimSpace(token)
	if len(token) != 64 || strings.Trim(token, "0123456789abcdef") != "" {
		return "", errors.New("invalid enrollment token")
	}
	return token, nil
}
func run() error {
	if os.Geteuid() != 0 {
		return errors.New("linad-linux must run as root")
	}
	if len(os.Args) < 2 {
		return errors.New("commands: enroll, sync, daemon, pam-auth, pam-account, deactivate")
	}
	mode := os.Args[1]
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	config := flags.String("config", linuxclient.DefaultConfig, "root-owned configuration file")
	state := flags.String("state", linuxclient.DefaultState, "root-owned account registry")
	server := flags.String("server", "", "HTTPS directory server")
	deviceID := flags.String("device-id", "", "administrator-created device ID")
	caFile := flags.String("ca-file", "", "optional CA PEM file")
	allowHTTP := flags.Bool("allow-http", false, "explicitly allow unencrypted HTTP")
	replace := flags.Bool("replace", false, "replace credentials for the same device and server")
	interval := flags.Duration("interval", 30*time.Second, "synchronization interval")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	// Local accounts, including root, must work even if linad or the network is down.
	login := os.Getenv("PAM_USER")
	if mode == "pam-account" && !strings.HasPrefix(login, "linad_") {
		return nil
	}
	if (mode == "pam-auth" || mode == "pam-sudo-auth") && !strings.HasPrefix(login, "linad_") {
		return errors.New("not a linad account")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if mode == "enroll" {
		if _, err := os.Lstat(*config); err == nil {
			if !*replace {
				return errors.New("configuration already exists; use --replace for credential rotation")
			}
			old, err := linuxclient.Load(*config)
			if err != nil {
				return err
			}
			if old.Config.Server != *server || old.Config.DeviceID != *deviceID {
				return errors.New("credential rotation must keep the same server and device")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		c, err := linuxclient.New(linuxclient.Config{Server: *server, DeviceID: *deviceID, CAFile: *caFile, AllowHTTP: *allowHTTP})
		if err != nil {
			return err
		}
		token, err := readEnrollmentToken()
		if err != nil {
			return err
		}
		enrollCtx, enrollCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer enrollCancel()
		if err = c.Enroll(enrollCtx, token, *config); err != nil {
			return err
		}
		fmt.Println("Gerät provisioniert. Jetzt linad-linux sync ausführen.")
		return nil
	}
	c, err := linuxclient.Load(*config)
	if err != nil {
		return err
	}
	switch mode {
	case "sync":
		return linuxclient.Sync(ctx, c, *state)
	case "deactivate":
		return linuxclient.Provision(ctx, c.Config.DeviceID, *state, nil)
	case "pam-auth", "pam-account", "pam-sudo-auth", "pam-sudo-account":
		password := ""
		if mode == "pam-auth" || mode == "pam-sudo-auth" {
			data, err := io.ReadAll(io.LimitReader(os.Stdin, 513))
			if err != nil || len(data) > 512 {
				return errors.New("invalid PAM password length")
			}
			password = strings.TrimRight(string(data), "\x00")
		}
		if err = linuxclient.PAMCheck(ctx, c, *state, login, password, (mode == "pam-auth" || mode == "pam-sudo-auth"), strings.HasPrefix(mode, "pam-sudo-")); err != nil {
			return errors.New("linad authentication or device access denied")
		}
		return nil
	case "daemon":
		if *interval < 5*time.Second || *interval > time.Hour {
			return errors.New("interval must be between 5s and 1h")
		}
		daemonCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		ticker := time.NewTicker(*interval)
		defer ticker.Stop()
		for {
			op, done := context.WithTimeout(daemonCtx, 25*time.Second)
			err = linuxclient.Sync(op, c, *state)
			done()
			if err != nil {
				log.Printf("synchronization failed: %v", err)
			}
			select {
			case <-daemonCtx.Done():
				return nil
			case <-ticker.C:
			}
		}
	default:
		return errors.New("unknown command")
	}
}
