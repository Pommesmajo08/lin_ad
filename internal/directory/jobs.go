package directory

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxJobRuns = 2000
const MaxJobOutput = 12000
const JobLease = time.Hour

type Job struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	DeviceIDs       []string   `json:"device_ids"`
	Enabled         bool       `json:"enabled"`
	IntervalMinutes int        `json:"interval_minutes"`
	NextRunAt       *time.Time `json:"next_run_at"`
	CreatedAt       time.Time  `json:"created_at"`
	CreatedBy       string     `json:"created_by"`
}
type JobRun struct {
	ID         string     `json:"id"`
	JobID      string     `json:"job_id"`
	JobName    string     `json:"job_name"`
	Kind       string     `json:"kind"`
	DeviceID   string     `json:"device_id"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	LeaseUntil *time.Time `json:"lease_until"`
	ExitCode   *int       `json:"exit_code"`
	Output     string     `json:"output"`
	Resolved   bool       `json:"resolved"`
	ClaimHash  string     `json:"claim_hash,omitempty"`
}

func ValidJobKind(kind string) bool {
	return kind == "system-info" || kind == "apt-refresh" || kind == "apt-upgrade"
}
func copyJobState(st State) State {
	data, _ := json.Marshal(st)
	var next State
	_ = json.Unmarshal(data, &next)
	if next.Jobs == nil {
		next.Jobs = map[string]Job{}
	}
	if next.Runs == nil {
		next.Runs = map[string]JobRun{}
	}
	return next
}
func publicRun(run JobRun) JobRun { run.ClaimHash = ""; return run }
func jobActive(run JobRun) bool {
	return run.Status == "queued" || run.Status == "running" || (run.Status == "unknown" && !run.Resolved)
}
func availableRunSlots(st *State, needed int) bool {
	if len(st.Runs)+needed <= MaxJobRuns {
		return true
	}
	removable := []JobRun{}
	for _, r := range st.Runs {
		if !jobActive(r) {
			removable = append(removable, r)
		}
	}
	sort.Slice(removable, func(i, j int) bool { return removable[i].CreatedAt.Before(removable[j].CreatedAt) })
	for _, r := range removable {
		if len(st.Runs)+needed <= MaxJobRuns {
			break
		}
		delete(st.Runs, r.ID)
	}
	return len(st.Runs)+needed <= MaxJobRuns
}
func queueJob(st *State, job Job, now time.Time, manual bool) error {
	targets := []string{}
	for _, id := range job.DeviceIDs {
		d, ok := st.Devices[id]
		if !ok || d.OS != "linux" {
			continue
		}
		active := false
		for _, r := range st.Runs {
			if r.JobID == job.ID && r.DeviceID == id && jobActive(r) {
				active = true
				break
			}
		}
		if active {
			if manual {
				return errors.New("a run is already pending for this job and device")
			}
			continue
		}
		targets = append(targets, id)
	}
	if !availableRunSlots(st, len(targets)) {
		return errors.New("job history capacity reached; finish or cancel pending runs")
	}
	for _, id := range targets {
		runID, err := randomToken(16)
		if err != nil {
			return err
		}
		st.Runs[runID] = JobRun{ID: runID, JobID: job.ID, JobName: job.Name, Kind: job.Kind, DeviceID: id, Status: "queued", CreatedAt: now}
	}
	return nil
}

// Called during polling/administration: missed intervals coalesce into one run.
func advanceJobs(st *State, now time.Time) bool {
	changed := false
	for id, r := range st.Runs {
		if r.Status == "running" && r.LeaseUntil != nil && !now.Before(*r.LeaseUntil) {
			r.Status = "unknown"
			r.Output = "No result received before the execution lease expired. Inspect the device before resolving."
			st.Runs[id] = r
			changed = true
		}
	}
	ids := []string{}
	for id := range st.Jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		job := st.Jobs[id]
		if !job.Enabled || job.NextRunAt == nil || now.Before(*job.NextRunAt) {
			continue
		}
		if err := queueJob(st, job, now, false); err != nil {
			continue
		}
		if job.IntervalMinutes == 0 {
			job.NextRunAt = nil
			job.Enabled = false
		} else {
			step := time.Duration(job.IntervalMinutes) * time.Minute
			next := job.NextRunAt.Add((now.Sub(*job.NextRunAt)/step + 1) * step)
			job.NextRunAt = &next
		}
		st.Jobs[id] = job
		changed = true
	}
	return changed
}
func validateJobTargets(st State, ids []string) error {
	if len(ids) == 0 || len(ids) > 100 {
		return errors.New("assign between 1 and 100 Linux devices")
	}
	for _, id := range ids {
		d, ok := st.Devices[id]
		if !ok || d.OS != "linux" {
			return errors.New("jobs require existing Linux devices")
		}
	}
	return nil
}
func (s *Server) jobs(w http.ResponseWriter, r *http.Request, u User) {
	if !u.Admin {
		fail(w, 403, "administrator required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	next := copyJobState(s.state)
	now := time.Now().UTC()
	var result any
	status := 200
	if r.Method == "GET" && (r.URL.Path == "/v1/jobs" || r.URL.Path == "/v1/job-runs") {
		if advanceJobs(&next, now) {
			if err := s.save(next); err != nil {
				fail(w, 500, "job persistence failed")
				return
			}
			s.state = next
		}
		if r.URL.Path == "/v1/jobs" {
			list := []Job{}
			for _, j := range next.Jobs {
				list = append(list, j)
			}
			sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
			reply(w, 200, list)
		} else {
			list := []JobRun{}
			for _, run := range next.Runs {
				if id := r.URL.Query().Get("device_id"); id != "" && run.DeviceID != id {
					continue
				}
				if id := r.URL.Query().Get("job_id"); id != "" && run.JobID != id {
					continue
				}
				list = append(list, publicRun(run))
			}
			sort.Slice(list, func(i, j int) bool {
				if list[i].CreatedAt.Equal(list[j].CreatedAt) {
					return list[i].ID < list[j].ID
				}
				return list[i].CreatedAt.After(list[j].CreatedAt)
			})
			if len(list) > 200 {
				list = list[:200]
			}
			reply(w, 200, list)
		}
		return
	}
	switch {
	case r.URL.Path == "/v1/jobs" && r.Method == "POST":
		var b struct {
			Name      string     `json:"name"`
			Kind      string     `json:"kind"`
			DeviceIDs []string   `json:"device_ids"`
			StartAt   *time.Time `json:"start_at"`
			Interval  int        `json:"interval_minutes"`
			Enabled   *bool      `json:"enabled"`
		}
		if !decode(w, r, &b) {
			return
		}
		b.Name = strings.TrimSpace(b.Name)
		if b.Name == "" || len(b.Name) > 80 || !ValidJobKind(b.Kind) || b.Interval < 0 || (b.Interval > 0 && b.Interval < 5) || b.Interval > 525600 {
			fail(w, 400, "invalid job name, kind or interval (0 or 5–525600 minutes)")
			return
		}
		if len(next.Jobs) >= 200 {
			fail(w, 409, "maximum number of jobs reached")
			return
		}
		if err := validateJobTargets(next, b.DeviceIDs); err != nil {
			fail(w, 400, err.Error())
			return
		}
		id, err := randomToken(16)
		if err != nil {
			fail(w, 500, "job creation failed")
			return
		}
		start := now
		if b.StartAt != nil {
			start = b.StartAt.UTC()
		}
		if start.Before(time.Unix(0, 0)) || start.After(now.AddDate(10, 0, 0)) {
			fail(w, 400, "start_at must be between 1970 and ten years in the future")
			return
		}
		enabled := b.Enabled == nil || *b.Enabled
		job := Job{ID: id, Name: b.Name, Kind: b.Kind, DeviceIDs: uniqueNames(b.DeviceIDs), Enabled: enabled, IntervalMinutes: b.Interval, NextRunAt: &start, CreatedAt: now, CreatedBy: u.Name}
		next.Jobs[id] = job
		advanceJobs(&next, now)
		result = next.Jobs[id]
		status = 201
	case len(parts) == 3 && parts[1] == "jobs" && r.Method == "PATCH":
		job, ok := next.Jobs[parts[2]]
		if !ok {
			fail(w, 404, "job not found")
			return
		}
		var b struct {
			Enabled   *bool      `json:"enabled"`
			DeviceIDs *[]string  `json:"device_ids"`
			StartAt   *time.Time `json:"start_at"`
			Interval  *int       `json:"interval_minutes"`
		}
		if !decode(w, r, &b) {
			return
		}
		if b.Enabled == nil && b.DeviceIDs == nil && b.StartAt == nil && b.Interval == nil {
			fail(w, 400, "job configuration field required")
			return
		}
		if b.DeviceIDs != nil {
			if err := validateJobTargets(next, *b.DeviceIDs); err != nil {
				fail(w, 400, err.Error())
				return
			}
			job.DeviceIDs = uniqueNames(*b.DeviceIDs)
		}
		if b.Interval != nil {
			if *b.Interval < 0 || (*b.Interval > 0 && *b.Interval < 5) || *b.Interval > 525600 {
				fail(w, 400, "invalid interval (0 or 5–525600 minutes)")
				return
			}
			job.IntervalMinutes = *b.Interval
		}
		if b.StartAt != nil {
			start := b.StartAt.UTC()
			if start.Before(time.Unix(0, 0)) || start.After(now.AddDate(10, 0, 0)) {
				fail(w, 400, "invalid start_at")
				return
			}
			job.NextRunAt = &start
		}
		if b.Enabled != nil {
			job.Enabled = *b.Enabled
			if job.Enabled && job.NextRunAt == nil {
				job.NextRunAt = &now
			}
		}
		allowed := map[string]bool{}
		for _, id := range job.DeviceIDs {
			allowed[id] = true
		}
		for id, run := range next.Runs {
			if run.JobID == job.ID && run.Status == "queued" && ((b.Enabled != nil && !*b.Enabled) || !allowed[run.DeviceID]) {
				run.Status = "cancelled"
				run.FinishedAt = &now
				run.Output = "Cancelled by job configuration change."
				next.Runs[id] = run
			}
		}
		next.Jobs[job.ID] = job
		result = job
	case len(parts) == 4 && parts[1] == "jobs" && parts[3] == "run" && r.Method == "POST":
		job, ok := next.Jobs[parts[2]]
		if !ok {
			fail(w, 404, "job not found")
			return
		}
		if err := validateJobTargets(next, job.DeviceIDs); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err := queueJob(&next, job, now, true); err != nil {
			fail(w, 409, err.Error())
			return
		}
		result = map[string]bool{"ok": true}
	case len(parts) == 3 && parts[1] == "jobs" && r.Method == "DELETE":
		if _, ok := next.Jobs[parts[2]]; !ok {
			fail(w, 404, "job not found")
			return
		}
		delete(next.Jobs, parts[2])
		for id, run := range next.Runs {
			if run.JobID == parts[2] && run.Status == "queued" {
				run.Status = "cancelled"
				run.FinishedAt = &now
				next.Runs[id] = run
			}
		}
		result = map[string]bool{"ok": true}
	case len(parts) == 4 && parts[1] == "job-runs" && r.Method == "POST" && (parts[3] == "cancel" || parts[3] == "resolve"):
		run, ok := next.Runs[parts[2]]
		if !ok {
			fail(w, 404, "run not found")
			return
		}
		if parts[3] == "cancel" {
			if run.Status != "queued" {
				fail(w, 409, "only queued runs can be cancelled")
				return
			}
			run.Status = "cancelled"
			run.FinishedAt = &now
		} else {
			if run.Status != "unknown" {
				fail(w, 409, "only unknown runs need resolution")
				return
			}
			run.Resolved = true
		}
		next.Runs[run.ID] = run
		result = publicRun(run)
	default:
		fail(w, 404, "not found")
		return
	}
	if err := s.save(next); err != nil {
		fail(w, 500, "job persistence failed")
		return
	}
	s.state = next
	reply(w, status, result)
}
func boundedJobOutput(text string) string {
	if len(text) > MaxJobOutput {
		text = text[:MaxJobOutput]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		text += "\n[output truncated]"
	}
	return strings.ToValidUTF8(text, "�")
}
