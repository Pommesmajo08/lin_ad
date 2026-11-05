package directory

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirectoryLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "directory.json")
	s, err := New(path, "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, token, body string) (int, string) {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	check := func(method, path, token, body string, want int) string {
		t.Helper()
		code, b := request(method, path, token, body)
		if code != want {
			t.Fatalf("%s %s: %d %s; want %d", method, path, code, b, want)
		}
		return b
	}
	check("GET", "/v1/users", "", "", 401)
	check("POST", "/v1/login", "", `{"name":"admin","password":"wrong"}`, 401)
	login := func(name, password string) string {
		t.Helper()
		b := check("POST", "/v1/login", "", `{"name":"`+name+`","password":"`+password+`"}`, 200)
		var v struct{ Token string }
		if err := json.Unmarshal([]byte(b), &v); err != nil || len(v.Token) != 64 {
			t.Fatal("missing session token")
		}
		return v.Token
	}
	admin := login("admin", "initial-admin-password")
	check("POST", "/v1/users", admin, `{"name":"alice","password":"alice-long-password"}`, 200)
	check("POST", "/v1/users", admin, `{"name":"alice","password":"alice-long-password"}`, 409)
	check("POST", "/v1/users", admin, `{"name":"bob","password":"short"}`, 400)
	check("POST", "/v1/users", admin, `{"name":"bob","password":"long-enough-password","admin":true}`, 400)
	alice := login("alice", "alice-long-password")
	check("GET", "/v1/users", alice, "", 403)
	check("GET", "/v1/me", alice, "", 200)
	check("POST", "/v1/groups", admin, `{"name":"engineering"}`, 200)
	check("PUT", "/v1/groups/engineering/members/alice", admin, "", 200)
	check("PUT", "/v1/groups/engineering/members/alice", admin, "", 200)
	b := check("GET", "/v1/groups", admin, "", 200)
	if strings.Count(b, "alice") != 1 {
		t.Fatal(b)
	}
	b = check("GET", "/v1/users", admin, "", 200)
	if strings.Contains(b, "hash") || strings.Contains(b, "salt") {
		t.Fatal("credential leak")
	}
	reloaded, err := New(path, "")
	if err != nil || len(reloaded.state.Users) != 2 || len(reloaded.state.Groups["engineering"]) != 1 {
		t.Fatalf("persistence: %v", err)
	}
	check("DELETE", "/v1/users/admin", admin, "", 400)
	check("DELETE", "/v1/users/alice", admin, "", 200)
	check("GET", "/v1/me", alice, "", 401)
	b = check("GET", "/v1/groups", admin, "", 200)
	if strings.Contains(b, "alice") {
		t.Fatal("dangling membership")
	}
	check("POST", "/v1/logout", admin, "", 200)
	check("GET", "/v1/me", admin, "", 401)
}
func TestFailedPersistenceDoesNotCommit(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "db.json"), "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	s.path = impossibleStatePath(t)
	s.sessions[tokenKey("test")] = session{User: "admin", Expires: time.Now().Add(time.Hour)}
	r := httptest.NewRequest("POST", "/v1/groups", strings.NewReader(`{"name":"team"}`))
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 500 || len(s.state.Groups) != 0 {
		t.Fatal("failed write committed", w.Code)
	}
}

func TestSessionAndRateLimits(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "db.json"), "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	s.sessions[tokenKey("expired")] = session{User: "admin", Expires: time.Now().Add(-time.Second)}
	r := httptest.NewRequest("GET", "/v1/me", nil)
	r.Header.Set("Authorization", "Bearer expired")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 30; i++ {
		s.attempts["global"] = append(s.attempts["global"], time.Now())
	}
	r = httptest.NewRequest("POST", "/v1/login", strings.NewReader(`{"name":"admin","password":"initial-admin-password"}`))
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 429 {
		t.Fatal(w.Code)
	}
}

// Fail persistence on every OS by placing a regular file where a parent directory is required.
func impossibleStatePath(t *testing.T) string {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(blocker, "state.json")
}
