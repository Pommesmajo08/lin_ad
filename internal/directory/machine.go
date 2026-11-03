package directory

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	var b struct {
		DeviceID string `json:"device_id"`
		Token    string `json:"enrollment_token"`
	}
	if !decode(w, r, &b) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.state.Devices[b.DeviceID]
	if !ok || d.OS != "linux" || d.EnrollmentHash == "" || time.Now().After(d.EnrollmentExpires) || len(b.Token) != 64 || subtle.ConstantTimeCompare([]byte(tokenKey(b.Token)), []byte(d.EnrollmentHash)) != 1 {
		fail(w, 401, "invalid or expired enrollment token")
		return
	}
	secret, err := randomToken(32)
	if err != nil {
		fail(w, 500, "enrollment failed")
		return
	}
	data, _ := json.Marshal(s.state)
	var next State
	_ = json.Unmarshal(data, &next)
	d.CredentialHash = tokenKey(secret)
	d.EnrollmentHash = ""
	d.EnrollmentExpires = time.Time{}
	d.LastSeen = time.Now().UTC()
	d.LastJobPoll = nil
	next.Devices[d.ID] = d
	if err := s.save(next); err != nil {
		fail(w, 500, "directory persistence failed")
		return
	}
	s.state = next
	reply(w, 200, map[string]string{"device_id": d.ID, "secret": secret})
}
func (s *Server) machine(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Device ") {
		fail(w, 401, "device credential required")
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(auth, "Device "), ".", 2)
	if len(parts) != 2 || len(parts[1]) != 64 {
		fail(w, 401, "invalid device credential")
		return
	}
	d, ok := s.state.Devices[parts[0]]
	if !ok || d.OS != "linux" || d.CredentialHash == "" || subtle.ConstantTimeCompare([]byte(tokenKey(parts[1])), []byte(d.CredentialHash)) != 1 {
		fail(w, 401, "invalid device credential")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/machine/jobs/") {
		s.machineJobs(w, r, d)
		return
	}
	if r.URL.Path == "/v1/machine/users" && r.Method == "GET" {
		accounts := []UnixAccount{}
		for _, u := range s.state.Users {
			if deviceAllows(s.state, d, u) {
				accounts = append(accounts, UnixAccount{Name: u.Name, Login: u.LinuxName, UID: u.UnixID, GID: u.UnixID, Admin: u.Admin})
			}
		}
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].UID < accounts[j].UID })
		// A successful synchronization is a machine heartbeat; persist before acknowledging.
		next := s.state
		next.Devices = make(map[string]Device, len(s.state.Devices))
		for id, device := range s.state.Devices {
			next.Devices[id] = device
		}
		d.LastSeen = time.Now().UTC()
		next.Devices[d.ID] = d
		if err := s.save(next); err != nil {
			fail(w, 500, "directory persistence failed")
			return
		}
		s.state = next
		reply(w, 200, accounts)
		return
	}
	if (r.URL.Path == "/v1/machine/authenticate" || r.URL.Path == "/v1/machine/access") && r.Method == "POST" {
		var b struct {
			Login    string `json:"login"`
			Password string `json:"password,omitempty"`
		}
		if !decode(w, r, &b) {
			return
		}
		u := s.dummy
		exists := false
		for _, candidate := range s.state.Users {
			if candidate.LinuxName == b.Login {
				u = candidate
				exists = true
				break
			}
		}
		if r.URL.Path == "/v1/machine/authenticate" {
			key := "machine:" + d.ID
			recent := s.attempts[key][:0]
			now := time.Now()
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
			matches := subtle.ConstantTimeCompare([]byte(derive(b.Password, u.Salt)), []byte(u.Hash)) == 1
			if !matches || !exists || !deviceAllows(s.state, d, u) {
				fail(w, 401, "invalid credentials or device access denied")
				return
			}
		} else if !exists || !deviceAllows(s.state, d, u) {
			fail(w, 403, "device access denied")
			return
		}
		reply(w, 200, UnixAccount{Name: u.Name, Login: u.LinuxName, UID: u.UnixID, GID: u.UnixID, Admin: u.Admin})
		return
	}
	fail(w, 404, "not found")
}
