package directory

import (
	"crypto/subtle"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (s *Server) machineJobs(w http.ResponseWriter, r *http.Request, d Device) {
	next := copyJobState(s.state)
	now := time.Now().UTC()
	changed := advanceJobs(&next, now)
	if r.URL.Path == "/v1/machine/jobs/claim" && r.Method == "POST" {
		var b struct {
			RequestID string `json:"request_id"`
		}
		if !decode(w, r, &b) {
			return
		}
		if len(b.RequestID) != 64 || strings.Trim(b.RequestID, "0123456789abcdef") != "" {
			fail(w, 400, "valid claim request_id required")
			return
		}
		if d.LastJobPoll == nil || now.Sub(*d.LastJobPoll) >= time.Minute {
			device := next.Devices[d.ID]
			device.LastJobPoll = &now
			next.Devices[d.ID] = device
			changed = true
		}
		hash := tokenKey(b.RequestID)
		var selected *JobRun
		// Retrying a claim after a lost response returns the same run, never a second one.
		for _, run := range next.Runs {
			if run.DeviceID == d.ID && run.ClaimHash == hash {
				copy := publicRun(run)
				selected = &copy
				break
			}
		}
		if selected == nil {
			blocked := false
			for _, run := range next.Runs {
				if run.DeviceID == d.ID && (run.Status == "running" || (run.Status == "unknown" && !run.Resolved)) {
					blocked = true
					break
				}
			}
			if !blocked {
				queue := []JobRun{}
				for _, run := range next.Runs {
					if run.DeviceID == d.ID && run.Status == "queued" {
						queue = append(queue, run)
					}
				}
				sort.Slice(queue, func(i, j int) bool {
					if queue[i].CreatedAt.Equal(queue[j].CreatedAt) {
						return queue[i].ID < queue[j].ID
					}
					return queue[i].CreatedAt.Before(queue[j].CreatedAt)
				})
				if len(queue) > 0 {
					run := queue[0]
					run.Status = "running"
					run.StartedAt = &now
					until := now.Add(JobLease)
					run.LeaseUntil = &until
					run.ClaimHash = hash
					next.Runs[run.ID] = run
					copy := publicRun(run)
					selected = &copy
					changed = true
				}
			}
		}
		if changed {
			if err := s.save(next); err != nil {
				fail(w, 500, "job persistence failed")
				return
			}
			s.state = next
		}
		reply(w, 200, map[string]any{"run": selected})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 5 && parts[4] == "result" && r.Method == "POST" {
		run, ok := next.Runs[parts[3]]
		if !ok || run.DeviceID != d.ID {
			fail(w, 404, "run not found")
			return
		}
		var b struct {
			RequestID string `json:"request_id"`
			Status    string `json:"status"`
			ExitCode  int    `json:"exit_code"`
			Output    string `json:"output"`
		}
		if !decode(w, r, &b) {
			return
		}
		if run.ClaimHash == "" || subtle.ConstantTimeCompare([]byte(run.ClaimHash), []byte(tokenKey(b.RequestID))) != 1 {
			fail(w, 403, "invalid run claim")
			return
		}
		if (b.Status != "succeeded" && b.Status != "failed" && b.Status != "unknown") || (b.Status == "succeeded" && b.ExitCode != 0) || (b.Status == "failed" && b.ExitCode == 0) || b.ExitCode < -1 || b.ExitCode > 255 {
			fail(w, 400, "invalid job result")
			return
		}
		output := boundedJobOutput(b.Output)
		if run.FinishedAt != nil && run.Status == b.Status && run.ExitCode != nil && *run.ExitCode == b.ExitCode && run.Output == output {
			reply(w, 200, map[string]bool{"ok": true})
			return
		}
		if run.Status == "succeeded" || run.Status == "failed" || run.Status == "cancelled" {
			if run.Status == b.Status && run.ExitCode != nil && *run.ExitCode == b.ExitCode && run.Output == output {
				reply(w, 200, map[string]bool{"ok": true})
				return
			}
			fail(w, 409, "run already finalized")
			return
		}
		if run.Status != "running" && run.Status != "unknown" {
			fail(w, 409, "run was not claimed")
			return
		}
		run.Status = b.Status
		run.ExitCode = &b.ExitCode
		run.Output = output
		run.FinishedAt = &now
		run.Resolved = run.Resolved || b.Status != "unknown"
		next.Runs[run.ID] = run
		if err := s.save(next); err != nil {
			fail(w, 500, "job persistence failed")
			return
		}
		s.state = next
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	fail(w, 404, "not found")
}
