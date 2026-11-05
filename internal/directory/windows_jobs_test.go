package directory

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsJobsAndEnrollmentIsolation(t *testing.T) {
	s, e := New(filepath.Join(t.TempDir(), "db.json"), "initial-admin-password")
	if e != nil {
		t.Fatal(e)
	}
	s.sessions[tokenKey("admin")] = session{"admin", time.Now().Add(time.Hour)}
	id := strings.Repeat("a", 32)
	linux := strings.Repeat("b", 32)
	token := strings.Repeat("c", 64)
	s.state.Devices[id] = Device{ID: id, OS: "windows", EnrollmentHash: tokenKey(token), EnrollmentExpires: time.Now().Add(time.Minute)}
	s.state.Devices[linux] = Device{ID: linux, OS: "linux"}
	request := func(method, path, auth string, body any, want int) string {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(data)))
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal(method, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	request("POST", "/v1/enroll", "", map[string]string{"device_id": id, "enrollment_token": token, "os": "linux"}, 401)
	var identity struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal([]byte(request("POST", "/v1/enroll", "", map[string]string{"device_id": id, "enrollment_token": token, "os": "windows"}, 200)), &identity)
	machine := "Device " + id + "." + identity.Secret
	request("GET", "/v1/machine/users", machine, nil, 403)
	request("GET", "/v1/machine/info", machine, nil, 200)
	for _, item := range []struct {
		kind, interp, target string
		want                 int
	}{{"system-info", "", id, 201}, {"apt-upgrade", "", id, 400}, {"windows-update", "", linux, 400}, {"custom-script", "bash", id, 400}, {"custom-script", "powershell", linux, 400}, {"custom-script", "powershell", id, 201}} {
		body := map[string]any{"name": "Test", "kind": item.kind, "device_ids": []string{item.target}, "enabled": false}
		if item.interp != "" {
			body["script"] = "echo hello\n"
			body["interpreter"] = item.interp
		}
		request("POST", "/v1/jobs", "Bearer admin", body, item.want)
	}
}
