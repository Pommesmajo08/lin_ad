package directory

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCustomScriptPermissionsSnapshotsAndEditing(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "db.json"), "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	s.state.Users["alice"] = User{Name: "alice"}
	s.sessions[tokenKey("admin")] = session{"admin", time.Now().Add(time.Hour)}
	s.sessions[tokenKey("alice")] = session{"alice", time.Now().Add(time.Hour)}
	device := strings.Repeat("a", 32)
	secret := strings.Repeat("b", 64)
	s.state.Devices[device] = Device{ID: device, OS: "linux", CredentialHash: tokenKey(secret)}
	request := func(method, path, auth string, body any, want int) string {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(data)))
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s; want %d", method, path, w.Code, w.Body.String(), want)
		}
		return w.Body.String()
	}
	admin := "Bearer admin"
	alice := "Bearer alice"
	machine := "Device " + device + "." + secret
	body := map[string]any{"name": "Own script", "kind": "custom-script", "interpreter": "bash", "script": "\ufeffprintf 'first\\n'\r\n", "device_ids": []string{device}}
	request("POST", "/v1/jobs", alice, body, 403)
	var job Job
	_ = json.Unmarshal([]byte(request("POST", "/v1/jobs", admin, body, 201)), &job)
	if job.Script != "printf 'first\\n'\n" || len(job.ScriptSHA256) != 64 {
		t.Fatal("script normalization/hash missing")
	}
	var original JobRun
	for _, r := range s.state.Runs {
		original = r
	}
	if original.Script != job.Script || original.ScriptSHA256 != job.ScriptSHA256 || original.Interpreter != "bash" {
		t.Fatal("run did not snapshot script")
	}
	request("GET", "/v1/jobs/"+job.ID, alice, nil, 403)
	request("GET", "/v1/job-runs/"+original.ID, alice, nil, 403)
	var listed []Job
	_ = json.Unmarshal([]byte(request("GET", "/v1/jobs", admin, nil, 200)), &listed)
	if listed[0].Script != "" || listed[0].ScriptSHA256 != job.ScriptSHA256 {
		t.Fatal("list must contain metadata without source")
	}
	var history []JobRun
	_ = json.Unmarshal([]byte(request("GET", "/v1/job-runs", admin, nil, 200)), &history)
	if history[0].Script != "" {
		t.Fatal("history list leaked source")
	}
	oldPath := s.path
	s.path = impossibleStatePath(t)
	request("PATCH", "/v1/jobs/"+job.ID, admin, map[string]string{"script": "echo changed\n"}, 500)
	s.path = oldPath
	if s.state.Jobs[job.ID].Script != job.Script {
		t.Fatal("failed edit committed")
	}
	request("PATCH", "/v1/jobs/"+job.ID, admin, map[string]string{"script": "print('second')\n", "interpreter": "python3"}, 200)
	if s.state.Jobs[job.ID].ScriptSHA256 == job.ScriptSHA256 || s.state.Runs[original.ID].ScriptSHA256 != job.ScriptSHA256 {
		t.Fatal("edit mutated queued snapshot")
	}
	var claim struct {
		Run *JobRun `json:"run"`
	}
	_ = json.Unmarshal([]byte(request("POST", "/v1/machine/jobs/claim", machine, map[string]string{"request_id": strings.Repeat("c", 64)}, 200)), &claim)
	if claim.Run == nil || claim.Run.Script != original.Script || claim.Run.ScriptSHA256 != original.ScriptSHA256 {
		t.Fatal("worker received wrong script version")
	}
	var detail JobRun
	_ = json.Unmarshal([]byte(request("GET", "/v1/job-runs/"+original.ID, admin, nil, 200)), &detail)
	if detail.Script != original.Script || detail.ClaimHash != "" {
		t.Fatal("snapshot inspection failed")
	}
	for _, bad := range []map[string]any{
		{"name": "bad", "kind": "custom-script", "device_ids": []string{device}, "script": ""},
		{"name": "bad", "kind": "custom-script", "device_ids": []string{device}, "script": strings.Repeat("x", MaxJobScript+1)},
		{"name": "bad", "kind": "custom-script", "device_ids": []string{device}, "script": "echo x\x00"},
		{"name": "bad", "kind": "custom-script", "device_ids": []string{device}, "script": "echo x", "interpreter": "/bin/arbitrary"},
		{"name": "bad", "kind": "system-info", "device_ids": []string{device}, "script": "echo hidden"},
	} {
		request("POST", "/v1/jobs", admin, bad, 400)
	}
	request("PATCH", "/v1/jobs/"+job.ID, admin, map[string]string{"script": ""}, 400)
	// The JSON size allowance must accommodate valid source with escaped HTML characters.
	large := map[string]any{"name": "HTML script", "kind": "custom-script", "interpreter": "sh", "script": "#" + strings.Repeat("<", MaxJobScript-2) + "\n", "device_ids": []string{device}, "enabled": false}
	request("POST", "/v1/jobs", admin, large, 201)
}
