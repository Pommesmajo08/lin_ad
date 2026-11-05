package windowsagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type Record struct {
	Run       directory.JobRun `json:"run"`
	RequestID string           `json:"request_id"`
	Status    string           `json:"status"`
	ExitCode  int              `json:"exit_code"`
	Output    string           `json:"output"`
}
type Ledger struct {
	RequestID string   `json:"request_id"`
	Current   *Record  `json:"current"`
	Seen      []string `json:"seen"`
}
type Executor func(context.Context, directory.JobRun) (int, string)
type LimitedLog struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (w *LimitedLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	space := 10000 - len(w.data)
	take := n
	if take > space {
		take = space
		w.truncated = true
	}
	w.data = append(w.data, p[:take]...)
	return n, nil
}
func (w *LimitedLog) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := strings.ToValidUTF8(string(w.data), "�")
	if w.truncated {
		s += "\n[output truncated]"
	}
	return s
}

var _ io.Writer = (*LimitedLog)(nil)

func Once(ctx context.Context, c *Client, path string, execute Executor) (bool, error) {
	var ledger Ledger
	e := Read(path, &ledger)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return false, e
	}
	if ledger.Current != nil {
		if ledger.Current.Status == "running" {
			ledger.Current.Status = "unknown"
			ledger.Current.ExitCode = -1
			ledger.Current.Output = "Worker interrupted; inspect device. This run was not repeated."
			if e := Save(path, ledger); e != nil {
				return false, e
			}
		}
		return true, finish(c, path, &ledger)
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if ledger.RequestID == "" {
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			return false, e
		}
		ledger.RequestID = hex.EncodeToString(b)
		if e := Save(path, ledger); e != nil {
			return false, e
		}
	}
	run, e := c.Claim(ctx, ledger.RequestID)
	if e != nil {
		return false, e
	}
	if run == nil {
		return false, nil
	}
	if run.DeviceID != c.Config.DeviceID || !Hex(run.ID, 32) {
		return false, errors.New("invalid claimed run")
	}
	for _, id := range ledger.Seen {
		if run.ID == id {
			return false, errors.New("completed run replay refused")
		}
	}
	ledger.Current = &Record{Run: *run, RequestID: ledger.RequestID, Status: "running"}
	if e := Save(path, ledger); e != nil {
		return false, e
	}
	if run.Status != "running" {
		ledger.Current.Status = "failed"
		ledger.Current.ExitCode = 1
		ledger.Current.Output = "Expired/finalized claim; no command executed."
	} else {
		jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
		code, output := execute(jobCtx, *run)
		ledger.Current.Status = "succeeded"
		if code != 0 {
			ledger.Current.Status = "failed"
		}
		if jobCtx.Err() != nil {
			ledger.Current.Status = "unknown"
			output += "\nExecution timed out; inspect device before allowing further jobs."
		}
		cancel()
		ledger.Current.ExitCode = code
		ledger.Current.Output = output
	}
	if e := Save(path, ledger); e != nil {
		return false, e
	}
	return true, finish(c, path, &ledger)
}
func finish(c *Client, path string, l *Ledger) error {
	r := l.Current
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	e := c.Request(ctx, "POST", "/v1/machine/jobs/"+r.Run.ID+"/result", map[string]any{"request_id": r.RequestID, "status": r.Status, "exit_code": r.ExitCode, "output": r.Output}, nil, true)
	var api *HTTPError
	if e != nil && !(errors.As(e, &api) && (api.Status == 404 || api.Status == 409)) {
		return e
	}
	if e != nil {
		log.Printf("Result no longer accepted for run %s: %v", r.Run.ID, e)
	}
	l.Seen = append(l.Seen, r.Run.ID)
	if len(l.Seen) > 100 {
		l.Seen = l.Seen[len(l.Seen)-100:]
	}
	l.Current = nil
	l.RequestID = ""
	return Save(path, l)
}
func Run(ctx context.Context, c *Client, path string, execute Executor) {
	for ctx.Err() == nil {
		worked, e := Once(ctx, c, path, execute)
		if e != nil {
			log.Printf("Job worker: %v", e)
		}
		if worked && e == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
}
