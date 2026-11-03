package directory

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdminRolesAndOwnGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	s, err := New(path, "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	s.sessions[tokenKey("admin")] = session{"admin", time.Now().Add(time.Hour)}
	request := func(method, path, token, body string, want int) string {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s; want %d", method, path, w.Code, w.Body.String(), want)
		}
		return w.Body.String()
	}
	request("POST", "/v1/users", "admin", `{"name":"alice","password":"alice-long-password"}`, 200)
	alice := s.state.Users["alice"]
	s.sessions[tokenKey("alice")] = session{"alice", time.Now().Add(time.Hour)}
	request("PATCH", "/v1/users/alice", "alice", `{"admin":true}`, 403)
	request("PATCH", "/v1/users/admin", "admin", `{"admin":false}`, 400)
	request("PATCH", "/v1/users/admin", "admin", `{"admin":false,"disabled":false}`, 400)
	oldPath := s.path
	s.path = "/dev/null/impossible"
	request("PATCH", "/v1/users/alice", "admin", `{"admin":true}`, 500)
	s.path = oldPath
	if s.state.Users["alice"].Admin {
		t.Fatal("failed role write committed")
	}
	request("GET", "/v1/me", "alice", "", 200)
	request("PATCH", "/v1/users/alice", "admin", `{"admin":true}`, 200)
	request("GET", "/v1/me", "alice", "", 401)
	login := func() string {
		t.Helper()
		b := request("POST", "/v1/login", "", `{"name":"alice","password":"alice-long-password"}`, 200)
		var v struct{ Token string }
		_ = json.Unmarshal([]byte(b), &v)
		return v.Token
	}
	token := login()
	request("GET", "/v1/users", token, "", 200)
	if u := s.state.Users["alice"]; u.UnixID != alice.UnixID || u.LinuxName != alice.LinuxName {
		t.Fatal("role change modified Unix identity")
	}
	reloaded, err := New(path, "")
	if err != nil || !reloaded.state.Users["alice"].Admin {
		t.Fatal("role not persisted", err)
	}
	request("POST", "/v1/groups", "admin", `{"name":"team"}`, 200)
	request("PUT", "/v1/groups/team/members/alice", "admin", "", 200)
	if b := request("GET", "/v1/me/groups", token, "", 200); strings.TrimSpace(b) != `["team"]` {
		t.Fatal("incorrect own groups", b)
	}
	request("PATCH", "/v1/users/alice", token, `{"admin":false}`, 200)
	request("GET", "/v1/me", token, "", 401)
	token = login()
	request("GET", "/v1/users", token, "", 403)
	request("GET", "/v1/me/groups", token, "", 200)
}
