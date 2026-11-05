package directory

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeviceAdministrationAndPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	s, err := New(path, "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "bob"} {
		s.state.Users[name] = passwordUser(name, "initial-user-password", false)
	}
	_, _ = assignUnixIDs(&s.state)
	for _, name := range []string{"admin", "alice", "bob"} {
		s.sessions[tokenKey(name)] = session{name, time.Now().Add(time.Hour)}
	}
	request := func(method, path, token, body string, want int) string {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s; want %d", method, path, w.Code, w.Body.String(), want)
		}
		return w.Body.String()
	}
	request("GET", "/v1/devices", "", "", 401)
	request("POST", "/v1/devices", "alice", `{"name":"pc","os":"linux"}`, 403)
	request("POST", "/v1/devices", "admin", `{"name":"pc","os":"linux","owner":"alice"}`, 400)
	request("POST", "/v1/devices", "admin", `{"name":"bad/name","os":"linux"}`, 400)
	var d Device
	_ = json.Unmarshal([]byte(request("POST", "/v1/devices", "admin", `{"name":"01-workstation","os":"linux"}`, 201)), &d)
	request("POST", "/v1/devices", "admin", `{"name":"01-workstation","os":"windows"}`, 409)
	request("DELETE", "/v1/devices/"+d.ID, "alice", "", 403)
	request("POST", "/v1/devices/"+d.ID+"/heartbeat", "alice", "", 403)
	request("PUT", "/v1/devices/"+d.ID+"/access", "alice", `{"allow_all":true}`, 403)
	if b := request("GET", "/v1/devices", "alice", "", 200); strings.TrimSpace(b) != "[]" {
		t.Fatal("default device access was not denied")
	}
	request("PUT", "/v1/devices/"+d.ID+"/access", "admin", `{"allowed_users":["unknown"]}`, 400)
	request("PUT", "/v1/devices/"+d.ID+"/access", "admin", `{"allowed_users":["alice"]}`, 200)
	if b := request("GET", "/v1/devices", "alice", "", 200); !strings.Contains(b, d.ID) {
		t.Fatal("allowed device missing")
	}
	if b := request("GET", "/v1/devices", "bob", "", 200); strings.TrimSpace(b) != "[]" {
		t.Fatal("unauthorized inventory leaked")
	}
	request("POST", "/v1/devices/"+d.ID+"/heartbeat", "admin", "", 200)
	reloaded, err := New(path, "")
	if err != nil || !deviceAllows(reloaded.state, reloaded.state.Devices[d.ID], reloaded.state.Users["alice"]) {
		t.Fatal("policy persistence failed", err)
	}
	oldPath := s.path
	s.path = impossibleStatePath(t)
	request("PUT", "/v1/devices/"+d.ID+"/access", "admin", `{"allow_all":true}`, 500)
	if s.state.Devices[d.ID].AllowAll {
		t.Fatal("failed policy committed")
	}
	s.path = oldPath
	request("DELETE", "/v1/users/alice", "admin", "", 200)
	if len(s.state.Devices[d.ID].AllowedUsers) != 0 {
		t.Fatal("dangling user ACL")
	}
	request("DELETE", "/v1/devices/"+d.ID, "admin", "", 200)
	if len(s.state.Devices) != 0 {
		t.Fatal("device not deleted")
	}
}
