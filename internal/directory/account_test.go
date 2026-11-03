package directory

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccountSecurity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	s, err := New(path, "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	s.state.Users["alice"] = passwordUser("alice", "alice-long-password", false)
	s.sessions[tokenKey("admin")] = session{"admin", time.Now().Add(time.Hour)}
	s.sessions[tokenKey("alice")] = session{"alice", time.Now().Add(time.Hour)}
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
	request("PATCH", "/v1/users/alice", "alice", `{"disabled":true}`, 403)
	request("POST", "/v1/users/admin/password", "alice", `{"password":"attacker-long-password"}`, 403)
	request("PATCH", "/v1/users/admin", "admin", `{"disabled":true}`, 400)
	request("PATCH", "/v1/users/alice", "admin", `{}`, 400)
	request("POST", "/v1/password", "alice", `{"current_password":"wrong","new_password":"new-long-password"}`, 403)
	request("POST", "/v1/password", "alice", `{"current_password":"alice-long-password","new_password":"short"}`, 400)
	request("GET", "/v1/me", "alice", "", 200)
	request("POST", "/v1/password", "alice", `{"current_password":"alice-long-password","new_password":"new-long-password"}`, 200)
	request("GET", "/v1/me", "alice", "", 401)
	request("POST", "/v1/login", "", `{"name":"alice","password":"alice-long-password"}`, 401)
	login := func(password string) string {
		t.Helper()
		b := request("POST", "/v1/login", "", `{"name":"alice","password":"`+password+`"}`, 200)
		var v struct{ Token string }
		if err := json.Unmarshal([]byte(b), &v); err != nil {
			t.Fatal(err)
		}
		return v.Token
	}
	alice := login("new-long-password")
	request("PATCH", "/v1/users/alice", "admin", `{"disabled":true}`, 200)
	if _, ok := s.sessions[tokenKey(alice)]; ok {
		t.Fatal("disabled session retained")
	}
	request("GET", "/v1/me", alice, "", 401)
	request("POST", "/v1/login", "", `{"name":"alice","password":"new-long-password"}`, 401)
	reloaded, err := New(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.state.Users["alice"].Disabled {
		t.Fatal("disabled state not persisted")
	}
	request("POST", "/v1/users/alice/password", "admin", `{"password":"reset-long-password"}`, 200)
	if !s.state.Users["alice"].Disabled {
		t.Fatal("password reset enabled user")
	}
	request("PATCH", "/v1/users/alice", "admin", `{"disabled":false}`, 200)
	alice = login("reset-long-password")
	request("POST", "/v1/users/alice/password", "admin", `{"password":"another-long-password"}`, 200)
	request("GET", "/v1/me", alice, "", 401)
	// A failed self-service password write must preserve both the password and sessions.
	alice = login("another-long-password")
	s.path = "/dev/null/impossible"
	request("POST", "/v1/password", alice, `{"current_password":"another-long-password","new_password":"never-committed-password"}`, 500)
	request("GET", "/v1/me", alice, "", 200)
	if s.state.Users["alice"].Hash != derive("another-long-password", s.state.Users["alice"].Salt) {
		t.Fatal("failed password update committed")
	}
}
func TestExistingDirectoryMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	legacy := `{"users":{"admin":{"name":"admin","admin":true,"salt":"salt","hash":"hash"}},"groups":{}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.state.Users["admin"].Disabled {
		t.Fatal("legacy user disabled")
	}
}
func TestWebAssetsAndHeaders(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "db.json"), "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(path, w.Code, w.Header())
		}
		if path == "/" && !strings.Contains(w.Body.String(), "login-form") {
			t.Fatal("missing login form")
		}
	}
	r := httptest.NewRequest("GET", "/server.go", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("unexpected file access")
	}
}
