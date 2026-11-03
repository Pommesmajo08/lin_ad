package directory

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestJobsClaimsPermissionsAndResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	s, err := New(path, "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	s.state.Users["alice"] = User{Name: "alice", UnixID: 60002, LinuxName: LinuxLogin("alice", 60002)}
	s.state.NextUnixID = 60003
	s.sessions[tokenKey("admin")] = session{"admin", time.Now().Add(time.Hour)}
	s.sessions[tokenKey("alice")] = session{"alice", time.Now().Add(time.Hour)}
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	secret := strings.Repeat("c", 64)
	s.state.Devices[a] = Device{ID: a, Name: "a", OS: "linux", CredentialHash: tokenKey(secret)}
	s.state.Devices[b] = Device{ID: b, Name: "b", OS: "linux", CredentialHash: tokenKey(secret)}
	machineA, machineB := "Device "+a+"."+secret, "Device "+b+"."+secret
	raw := func(method, path, auth, body string) (int, string) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	request := func(method, path, auth, body string, want int) string {
		t.Helper()
		code, data := raw(method, path, auth, body)
		if code != want {
			t.Fatalf("%s %s: %d %s; want %d", method, path, code, data, want)
		}
		return data
	}
	admin := "Bearer admin"
	alice := "Bearer alice"
	request("POST", "/v1/jobs", alice, `{}`, 403)
	request("GET", "/v1/job-runs", alice, "", 403)
	request("GET", "/v1/jobs", machineA, "", 401)
	request("POST", "/v1/jobs", admin, `{"name":"bad","kind":"shell","device_ids":["`+a+`"]}`, 400)
	request("POST", "/v1/jobs", admin, `{"name":"bad","kind":"system-info","device_ids":["missing"]}`, 400)
	create := `{"name":"Inventory","kind":"system-info","device_ids":["` + a + `","` + b + `"],"interval_minutes":0}`
	old := s.path
	s.path = "/dev/null/impossible"
	request("POST", "/v1/jobs", admin, create, 500)
	s.path = old
	if len(s.state.Jobs) != 0 {
		t.Fatal("failed creation committed")
	}
	var job Job
	_ = json.Unmarshal([]byte(request("POST", "/v1/jobs", admin, create, 201)), &job)
	if len(s.state.Runs) != 2 || job.Enabled {
		t.Fatal("one-shot scheduling failed")
	}
	request("POST", "/v1/jobs/"+job.ID+"/run", admin, "", 409)
	nonce := strings.Repeat("1", 64)
	claimBody := `{"request_id":"` + nonce + `"}`
	s.path = "/dev/null/impossible"
	request("POST", "/v1/machine/jobs/claim", machineA, claimBody, 500)
	s.path = old
	for _, run := range s.state.Runs {
		if run.Status != "queued" {
			t.Fatal("failed claim committed")
		}
	}
	var claim struct {
		Run *JobRun `json:"run"`
	}
	_ = json.Unmarshal([]byte(request("POST", "/v1/machine/jobs/claim", machineA, claimBody, 200)), &claim)
	if claim.Run == nil {
		t.Fatal("missing claim")
	}
	id := claim.Run.ID
	var retry struct {
		Run *JobRun `json:"run"`
	}
	_ = json.Unmarshal([]byte(request("POST", "/v1/machine/jobs/claim", machineA, claimBody, 200)), &retry)
	if retry.Run == nil || retry.Run.ID != id {
		t.Fatal("claim retry duplicated run")
	}
	if data := request("POST", "/v1/machine/jobs/claim", machineA, `{"request_id":"`+strings.Repeat("2", 64)+`"}`, 200); !strings.Contains(data, `"run":null`) {
		t.Fatal("parallel device run admitted")
	}
	// Two competing claimers cannot both start a second device's queued run.
	type response struct {
		Code  int
		Body  string
		Nonce string
	}
	results := make(chan response, 2)
	for _, n := range []string{strings.Repeat("3", 64), strings.Repeat("4", 64)} {
		go func(n string) {
			code, data := raw("POST", "/v1/machine/jobs/claim", machineB, `{"request_id":"`+n+`"}`)
			results <- response{code, data, n}
		}(n)
	}
	count := 0
	winnerNonce := ""
	for i := 0; i < 2; i++ {
		r := <-results
		if r.Code != 200 {
			t.Fatal(r)
		}
		var c struct {
			Run *JobRun `json:"run"`
		}
		_ = json.Unmarshal([]byte(r.Body), &c)
		if c.Run != nil {
			count++
			winnerNonce = r.Nonce
		}
	}
	if count != 1 {
		t.Fatal("concurrent claims duplicated execution")
	}
	report := `{"request_id":"` + nonce + `","status":"succeeded","exit_code":0,"output":"<script>plain text</script>"}`
	request("POST", "/v1/machine/jobs/"+id+"/result", machineB, report, 404)
	request("POST", "/v1/machine/jobs/"+id+"/result", machineA, strings.Replace(report, nonce, strings.Repeat("0", 64), 1), 403)
	request("POST", "/v1/machine/jobs/"+id+"/result", machineA, strings.Replace(report, `"exit_code":0`, `"exit_code":1`, 1), 400)
	request("POST", "/v1/machine/jobs/"+id+"/result", machineA, report, 200)
	request("POST", "/v1/machine/jobs/"+id+"/result", machineA, report, 200)
	request("POST", "/v1/machine/jobs/"+id+"/result", machineA, strings.Replace(report, "plain text", "changed", 1), 409)
	if data := request("GET", "/v1/job-runs", admin, "", 200); strings.Contains(data, "claim_hash") || strings.Contains(data, nonce) {
		t.Fatal("claim secret leaked")
	}
	reloaded, err := New(path, "")
	if err != nil || reloaded.state.Runs[id].Status != "succeeded" {
		t.Fatal("job result not persisted", err)
	}
	request("POST", "/v1/job-runs/"+id+"/cancel", admin, "", 409)
	request("POST", "/v1/jobs/"+job.ID+"/run", admin, "", 409) // Other assigned device is still running.
	now := time.Now().UTC()
	for runID, run := range s.state.Runs {
		if run.Status == "running" {
			past := now.Add(-2 * time.Hour)
			run.LeaseUntil = &past
			s.state.Runs[runID] = run
		}
	}
	request("GET", "/v1/job-runs", admin, "", 200)
	for runID, run := range s.state.Runs {
		if run.Status == "unknown" {
			request("POST", "/v1/job-runs/"+runID+"/resolve", alice, "", 403)
			request("POST", "/v1/job-runs/"+runID+"/resolve", admin, "", 200)
			payload := `{"request_id":"` + winnerNonce + `","status":"unknown","exit_code":-1,"output":"interrupted"}`
			request("POST", "/v1/machine/jobs/"+runID+"/result", machineB, payload, 200)
			recorded := s.state.Runs[runID]
			if !recorded.Resolved {
				t.Fatal("late report undid explicit resolution")
			}
			request("POST", "/v1/machine/jobs/"+runID+"/result", machineB, payload, 200)
			if !recorded.FinishedAt.Equal(*s.state.Runs[runID].FinishedAt) {
				t.Fatal("result retry changed timestamp")
			}
		}
	}
	request("POST", "/v1/jobs/"+job.ID+"/run", admin, "", 200)
	request("PATCH", "/v1/jobs/"+job.ID, admin, `{"device_ids":["`+a+`"]}`, 200)
	queuedA := false
	for _, r := range s.state.Runs {
		if r.Status == "queued" && r.DeviceID == a {
			queuedA = true
		}
		if r.Status == "queued" && r.DeviceID == b {
			t.Fatal("removed device still queued")
		}
	}
	if !queuedA {
		t.Fatal("assignment edit incorrectly cancelled retained one-shot")
	}
	request("PATCH", "/v1/jobs/"+job.ID, admin, `{"interval_minutes":1}`, 400)
	request("PATCH", "/v1/jobs/"+job.ID, admin, `{"enabled":false}`, 200)
	for _, r := range s.state.Runs {
		if r.Status == "queued" {
			t.Fatal("pause left queued run")
		}
	}
}
func TestJobSchedulingAndRetention(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	past := now.Add(-24 * time.Hour)
	st := State{Devices: map[string]Device{"d": {ID: "d", OS: "linux"}}, Jobs: map[string]Job{"j": {ID: "j", Name: "Daily", Kind: "system-info", DeviceIDs: []string{"d"}, Enabled: true, IntervalMinutes: 60, NextRunAt: &past}}, Runs: map[string]JobRun{}}
	if !advanceJobs(&st, now) || len(st.Runs) != 1 || !st.Jobs["j"].NextRunAt.After(now) {
		t.Fatal("missed intervals not coalesced")
	}
	if advanceJobs(&st, now) || len(st.Runs) != 1 {
		t.Fatal("repeated poll duplicated schedule")
	}
	later := now.Add(3 * time.Hour)
	advanceJobs(&st, later)
	if len(st.Runs) != 1 {
		t.Fatal("scheduled overlap admitted")
	}
	for id, r := range st.Runs {
		r.Status = "succeeded"
		st.Runs[id] = r
	}
	later = later.Add(2 * time.Hour)
	advanceJobs(&st, later)
	if len(st.Runs) != 2 {
		t.Fatal("next occurrence missing")
	}
	output := boundedJobOutput(strings.Repeat("ä", MaxJobOutput))
	if !utf8.ValidString(output) || len(output) > MaxJobOutput+30 {
		t.Fatal("output not safely bounded")
	}
}
