package directory

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type WindowsAccount struct {
	Name  string `json:"name"`
	Login string `json:"login"`
	ID    int    `json:"id"`
	Admin bool   `json:"admin"`
}

func WindowsLogin(id int) string { return fmt.Sprintf("linad_%d", id) }
func windowsAccount(u User) WindowsAccount {
	return WindowsAccount{u.Name, WindowsLogin(u.UnixID), u.UnixID, u.Admin}
}

// Called with the directory mutex held, after device credential verification.
func (s *Server) windowsLogon(w http.ResponseWriter, r *http.Request, d Device) {
	if d.OS != "windows" {
		fail(w, 403, "Windows device required")
		return
	}
	if r.URL.Path == "/v1/machine/windows/users" && r.Method == "GET" {
		accounts := []WindowsAccount{}
		for _, u := range s.state.Users {
			if deviceAllows(s.state, d, u) {
				accounts = append(accounts, windowsAccount(u))
			}
		}
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
		reply(w, 200, accounts)
		return
	}
	if r.URL.Path != "/v1/machine/windows/authenticate" || r.Method != "POST" {
		fail(w, 404, "not found")
		return
	}
	var b struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &b) {
		return
	}
	b.Name = strings.ToLower(strings.TrimSpace(b.Name))
	key := "windows-logon:" + d.ID
	now := time.Now()
	recent := s.attempts[key][:0]
	for _, t := range s.attempts[key] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 120 {
		s.attempts[key] = recent
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "machine login rate limit exceeded")
		return
	}
	s.attempts[key] = append(recent, now)
	u, exists := s.state.Users[b.Name]
	if !exists {
		u = s.dummy
	}
	// Always perform the password derivation, including for unknown names.
	matches := subtle.ConstantTimeCompare([]byte(derive(b.Password, u.Salt)), []byte(u.Hash)) == 1
	if !matches || !exists || len(b.Password) > 512 || !deviceAllows(s.state, d, u) {
		fail(w, 401, "invalid credentials or device access denied")
		return
	}
	reply(w, 200, windowsAccount(u))
}
