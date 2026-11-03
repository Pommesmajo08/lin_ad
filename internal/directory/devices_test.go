package directory

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeviceLifecycleAndOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	s, err := New(path, "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "bob"} {
		s.state.Users[name] = passwordUser(name, "initial-user-password", false)
	}
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
			t.Fatalf("%s %s: %d %s (want %d)", method, path, w.Code, w.Body.String(), want)
		}
		return w.Body.String()
	}
	request("GET", "/v1/devices", "", "", 401)
	request("POST", "/v1/devices", "alice", `{"name":"pc","os":"linux","owner":"admin"}`, 400)
	request("POST", "/v1/devices", "alice", `{"name":"bad/name","os":"linux"}`, 400)
	request("POST", "/v1/devices", "alice", `{"name":"pc","os":"unsupported"}`, 400)
	b := request("POST", "/v1/devices", "alice", `{"name":"01-workstation","os":"windows"}`, 201)
	var d Device
	if err := json.Unmarshal([]byte(b), &d); err != nil {
		t.Fatal(err)
	}
	if d.Owner != "alice" || d.ID == "" || d.LastSeen.IsZero() {
		t.Fatal("invalid device", d)
	}
	request("POST", "/v1/devices", "alice", `{"name":"01-workstation","os":"windows"}`, 409)
	if b := request("GET", "/v1/devices", "bob", "", 200); strings.TrimSpace(b) != "[]" {
		t.Fatal("foreign inventory disclosed", b)
	}
	request("DELETE", "/v1/devices/"+d.ID, "bob", "", 404)
	request("POST", "/v1/devices/"+d.ID+"/heartbeat", "bob", "", 404)
	request("POST", "/v1/devices/"+d.ID+"/heartbeat", "alice", "", 200)
	if !s.state.Devices[d.ID].LastSeen.After(d.LastSeen) {
		t.Fatal("heartbeat did not advance")
	}
	if b := request("GET", "/v1/devices", "admin", "", 200); !strings.Contains(b, d.ID) {
		t.Fatal("admin inventory missing")
	}
	reloaded, err := New(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.state.Devices[d.ID].Owner != "alice" {
		t.Fatal("device not persisted")
	}
	oldPath := s.path
	s.path = "/dev/null/impossible"
	request("DELETE", "/v1/devices/"+d.ID, "alice", "", 500)
	if _, ok := s.state.Devices[d.ID]; !ok {
		t.Fatal("failed deletion committed")
	}
	s.path = oldPath
	request("PATCH", "/v1/users/alice", "admin", `{"disabled":true}`, 200)
	request("GET", "/v1/devices", "alice", "", 401)
	request("DELETE", "/v1/users/alice", "admin", "", 200)
	if len(s.state.Devices) != 0 {
		t.Fatal("orphan device after user deletion")
	}
	request("POST", "/v1/devices", "bob", `{"name":"bob-pc","os":"linux"}`, 201)
	for id := range s.state.Devices {
		request("DELETE", "/v1/devices/"+id, "admin", "", 200)
	}
	if len(s.state.Devices) != 0 {
		t.Fatal("admin deletion failed")
	}
}
