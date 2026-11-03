package linuxclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Pommesmajo08/lin_ad/internal/directory"
)

const DefaultJobsState = "/var/lib/linad-linux/jobs.json"

type JobRecord struct {
	Run       directory.JobRun `json:"run"`
	RequestID string           `json:"request_id"`
	Status    string           `json:"status"`
	ExitCode  int              `json:"exit_code"`
	Output    string           `json:"output"`
}
type JobLedger struct {
	RequestID string     `json:"request_id"`
	Current   *JobRecord `json:"current"`
	Seen      []string   `json:"seen"`
}
type limitedLog struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (w *limitedLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	remaining := 10000 - len(w.data)
	if remaining > 0 {
		if remaining > n {
			remaining = n
		}
		w.data = append(w.data, p[:remaining]...)
	}
	if n > remaining {
		w.truncated = true
	}
	return n, nil
}
func (w *limitedLog) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := strings.ToValidUTF8(string(w.data), "�")
	if w.truncated {
		s += "\n[output truncated]"
	}
	return s
}

type jobCommand func(context.Context, string, []string, io.Writer) error

func executeJob(ctx context.Context, kind string, command jobCommand) (int, string) {
	output := &limitedLog{}
	if !directory.ValidJobKind(kind) {
		return 1, "Unsupported job kind; no command executed."
	}
	if kind == "system-info" {
		hostname, _ := os.Hostname()
		fmt.Fprintf(output, "Host: %s\nOS: %s\nArchitecture: %s\n", hostname, runtime.GOOS, runtime.GOARCH)
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			output.Write(data)
		}
		return 0, output.String()
	}
	fmt.Fprintln(output, "$ apt-get update")
	if err := command(ctx, "/usr/bin/apt-get", []string{"-o", "DPkg::Lock::Timeout=60", "update"}, output); err != nil {
		fmt.Fprintln(output, "Package index update failed:", err)
		return exitCode(err), output.String()
	}
	if kind == "apt-upgrade" {
		fmt.Fprintln(output, "$ apt-get upgrade (noninteractive; existing configuration retained)")
		args := []string{"-o", "DPkg::Lock::Timeout=60", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold", "-y", "upgrade"}
		if err := command(ctx, "/usr/bin/apt-get", args, output); err != nil {
			fmt.Fprintln(output, "Package upgrade failed:", err)
			return exitCode(err), output.String()
		}
	}
	return 0, output.String()
}
func exitCode(err error) int {
	var e *exec.ExitError
	if errors.As(err, &e) && e.ExitCode() > 0 && e.ExitCode() < 256 {
		return e.ExitCode()
	}
	return 1
}
func runJobCommand(ctx context.Context, program string, args []string, w io.Writer) error {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=l", "UCF_FORCE_CONFFOLD=1"}
	// These are the root daemon's configured proxy variables, never PAM/user data.
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
		if value, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return ctx.Err()
	}
	return err
}
func (c *Client) claimJob(ctx context.Context, requestID string) (*directory.JobRun, error) {
	var response struct {
		Run *directory.JobRun `json:"run"`
	}
	err := c.request(ctx, "POST", "/v1/machine/jobs/claim", map[string]string{"request_id": requestID}, &response, true)
	return response.Run, err
}
func (c *Client) reportJob(ctx context.Context, record JobRecord) error {
	return c.request(ctx, "POST", "/v1/machine/jobs/"+record.Run.ID+"/result", map[string]any{"request_id": record.RequestID, "status": record.Status, "exit_code": record.ExitCode, "output": record.Output}, nil, true)
}
func loadJobLedger(path string) (JobLedger, error) {
	data, err := SecureRead(path)
	if errors.Is(err, os.ErrNotExist) {
		return JobLedger{}, nil
	}
	if err != nil {
		return JobLedger{}, err
	}
	var ledger JobLedger
	err = json.Unmarshal(data, &ledger)
	return ledger, err
}
func reportAndFinish(c *Client, path string, ledger *JobLedger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := c.reportJob(ctx, *ledger.Current)
	var apiErr *HTTPError
	if err != nil && !(errors.As(err, &apiErr) && (apiErr.Status == 404 || apiErr.Status == 409)) {
		return err
	}
	if err != nil {
		log.Printf("Job result could not be retained by server; run %s: %v", ledger.Current.Run.ID, err)
	}
	ledger.Seen = append(ledger.Seen, ledger.Current.Run.ID)
	if len(ledger.Seen) > 100 {
		ledger.Seen = ledger.Seen[len(ledger.Seen)-100:]
	}
	ledger.Current = nil
	ledger.RequestID = ""
	return AtomicJSON(path, ledger)
}

// ProcessJobOnce uses a durable claim ID and records execution before starting.
// A restarted worker reports an interrupted run as unknown instead of re-executing it.
func ProcessJobOnce(ctx context.Context, c *Client, path string) (bool, error) {
	if os.Geteuid() != 0 {
		return false, errors.New("job execution requires root")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return false, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ledger, err := loadJobLedger(path)
	if err != nil {
		return false, err
	}
	if ledger.Current != nil {
		if ledger.Current.Status == "running" {
			ledger.Current.Status = "unknown"
			ledger.Current.ExitCode = -1
			ledger.Current.Output = "Worker was interrupted before a durable result was saved. Inspect the device; this run was not repeated."
			if err = AtomicJSON(path, ledger); err != nil {
				return false, err
			}
		}
		return true, reportAndFinish(c, path, &ledger)
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if ledger.RequestID == "" {
		nonce := make([]byte, 32)
		if _, err = rand.Read(nonce); err != nil {
			return false, err
		}
		ledger.RequestID = hex.EncodeToString(nonce)
		if err = AtomicJSON(path, ledger); err != nil {
			return false, err
		}
	}
	run, err := c.claimJob(ctx, ledger.RequestID)
	if err != nil {
		return false, err
	}
	if run == nil {
		return false, nil
	}
	if run.DeviceID != c.Config.DeviceID || len(run.ID) != 32 || strings.Trim(run.ID, "0123456789abcdef") != "" {
		return false, errors.New("invalid job claim response")
	}
	for _, id := range ledger.Seen {
		if id == run.ID {
			return false, errors.New("server attempted to replay a completed run")
		}
	}
	ledger.Current = &JobRecord{Run: *run, RequestID: ledger.RequestID, Status: "running"}
	if err = AtomicJSON(path, ledger); err != nil {
		return false, err
	}
	if run.Status != "running" {
		ledger.Current.Status = "failed"
		ledger.Current.ExitCode = 1
		ledger.Current.Output = "Claim expired or was finalized before the local worker started; no command executed."
	} else {
		// On service stop, drain an accepted update rather than killing dpkg mid-write.
		jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
		code, output := executeJob(jobCtx, run.Kind, runJobCommand)
		cancel()
		ledger.Current.ExitCode = code
		ledger.Current.Output = output
		ledger.Current.Status = "succeeded"
		if code != 0 {
			ledger.Current.Status = "failed"
		}
	}
	if err = AtomicJSON(path, ledger); err != nil {
		return false, err
	}
	return true, reportAndFinish(c, path, &ledger)
}
func RunJobWorker(ctx context.Context, c *Client, path string) {
	for {
		if ctx.Err() != nil {
			return
		}
		worked, err := ProcessJobOnce(ctx, c, path)
		if err != nil {
			log.Printf("Job worker: %v", err)
		}
		if err == nil && worked {
			continue
		}
		timer := time.NewTimer(15 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
