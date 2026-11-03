package directory

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type User struct {
	Name      string `json:"name"`
	Admin     bool   `json:"admin"`
	Disabled  bool   `json:"disabled"`
	UnixID    int    `json:"unix_id"`
	LinuxName string `json:"linux_name"`
	Salt      string `json:"salt,omitempty"`
	Hash      string `json:"hash,omitempty"`
}
type State struct {
	NextUnixID int                 `json:"next_unix_id"`
	Users      map[string]User     `json:"users"`
	Groups     map[string][]string `json:"groups"`
	Devices    map[string]Device   `json:"devices"`
}
type session struct {
	User    string
	Expires time.Time
}
type Server struct {
	mu       sync.Mutex
	path     string
	state    State
	sessions map[string]session
	attempts map[string][]time.Time
	dummy    User
}

var validName = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

func passwordUser(name, password string, admin bool) User {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	u := User{Name: name, Admin: admin, Salt: hex.EncodeToString(salt)}
	u.Hash = derive(password, u.Salt)
	return u
}
func derive(password, salt string) string {
	key, err := pbkdf2.Key(sha256.New, password, []byte(salt), 600000, 32)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(key)
}
func New(path, password string) (*Server, error) {
	s := &Server{path: path, sessions: map[string]session{}, attempts: map[string][]time.Time{}, dummy: passwordUser("dummy", "unusable", false)}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &s.state); err != nil {
			return nil, err
		}
		if s.state.Users == nil || s.state.Groups == nil {
			return nil, errors.New("invalid directory data")
		}
		if s.state.Devices == nil {
			s.state.Devices = map[string]Device{}
		}
		changed, err := assignUnixIDs(&s.state)
		if err != nil {
			return nil, err
		}
		if changed {
			if err = s.save(s.state); err != nil {
				return nil, err
			}
		}
		return s, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(password) < 16 {
		return nil, errors.New("initial LINAD_ADMIN_PASSWORD must contain at least 16 characters")
	}
	s.state = State{Users: map[string]User{"admin": passwordUser("admin", password, true)}, Groups: map[string][]string{}, Devices: map[string]Device{}}
	if _, err = assignUnixIDs(&s.state); err != nil {
		return nil, err
	}
	if err = s.save(s.state); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Server) save(st State) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".directory-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(st); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), s.path)
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		fail(w, 400, "invalid JSON body")
		return false
	}
	return true
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("POST /v1/login", s.login)
	m.HandleFunc("POST /v1/enroll", s.enroll)
	m.HandleFunc("/v1/machine/", s.machine)
	m.HandleFunc("/v1/", s.authorized)
	web := webHandler()
	m.Handle("GET /{$}", web)
	m.Handle("GET /app.js", web)
	m.Handle("GET /style.css", web)
	m.Handle("GET /favicon.svg", web)
	return securityHeaders(m)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	// A bounded global budget prevents attacker-controlled names from growing memory.
	attempts := s.attempts["global"][:0]
	for _, t := range s.attempts["global"] {
		if now.Sub(t) < time.Minute {
			attempts = append(attempts, t)
		}
	}
	if len(attempts) >= 30 {
		s.attempts["global"] = attempts
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "login rate limit exceeded")
		return
	}
	s.attempts["global"] = append(attempts, now)
	u, ok := s.state.Users[body.Name]
	if !ok {
		u = s.dummy
	}
	hash := derive(body.Password, u.Salt)
	if subtle.ConstantTimeCompare([]byte(hash), []byte(u.Hash)) != 1 || !ok || u.Disabled {
		fail(w, 401, "invalid credentials")
		return
	}
	for k, v := range s.sessions {
		if now.After(v.Expires) {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 10000 {
		fail(w, 503, "session capacity reached")
		return
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		fail(w, 500, "session creation failed")
		return
	}
	raw := hex.EncodeToString(token)
	expires := now.Add(8 * time.Hour)
	s.sessions[tokenKey(raw)] = session{u.Name, expires}
	reply(w, 200, map[string]any{"token": raw, "expires_at": expires})
}
func tokenKey(raw string) string { h := sha256.Sum256([]byte(raw)); return hex.EncodeToString(h[:]) }
func (s *Server) authorized(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		fail(w, 401, "bearer token required")
		return
	}
	key := tokenKey(strings.TrimPrefix(auth, "Bearer "))
	session, ok := s.sessions[key]
	u, exists := s.state.Users[session.User]
	if !ok || !exists || u.Disabled || time.Now().After(session.Expires) {
		delete(s.sessions, key)
		fail(w, 401, "session expired or invalid")
		return
	}
	if r.URL.Path == "/v1/me" && r.Method == "GET" {
		u.Hash = ""
		u.Salt = ""
		reply(w, 200, u)
		return
	}
	if r.URL.Path == "/v1/me/groups" && r.Method == "GET" {
		groups := []string{}
		for name, members := range s.state.Groups {
			for _, member := range members {
				if member == u.Name {
					groups = append(groups, name)
					break
				}
			}
		}
		sort.Strings(groups)
		reply(w, 200, groups)
		return
	}
	if r.URL.Path == "/v1/logout" && r.Method == "POST" {
		delete(s.sessions, key)
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.URL.Path == "/v1/password" && r.Method == "POST" {
		s.changePassword(w, r, u)
		return
	}
	if r.URL.Path == "/v1/devices" || strings.HasPrefix(r.URL.Path, "/v1/devices/") {
		s.devices(w, r, u)
		return
	}
	if !u.Admin {
		fail(w, 403, "administrator required")
		return
	}
	// Work on a copy: failed writes never mutate the in-memory committed directory.
	data, _ := json.Marshal(s.state)
	var next State
	_ = json.Unmarshal(data, &next)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		fail(w, 404, "not found")
		return
	}
	switch parts[1] {
	case "users":
		if len(parts) == 2 && r.Method == "GET" {
			users := make([]User, 0, len(next.Users))
			for _, v := range next.Users {
				v.Hash = ""
				v.Salt = ""
				users = append(users, v)
			}
			sort.Slice(users, func(i, j int) bool { return users[i].Name < users[j].Name })
			reply(w, 200, users)
			return
		}
		if len(parts) == 2 && r.Method == "POST" {
			var b struct {
				Name     string `json:"name"`
				Password string `json:"password"`
			}
			if !decode(w, r, &b) {
				return
			}
			if !validName.MatchString(b.Name) || len(b.Password) < 16 {
				fail(w, 400, "valid name and password of at least 16 characters required")
				return
			}
			if _, ok := next.Users[b.Name]; ok {
				fail(w, 409, "user exists")
				return
			}
			next.Users[b.Name] = passwordUser(b.Name, b.Password, false)
			if _, err := assignUnixIDs(&next); err != nil {
				fail(w, 500, "Unix identity allocation failed")
				return
			}
		} else if len(parts) == 3 && r.Method == "PATCH" {
			target, ok := next.Users[parts[2]]
			if !ok {
				fail(w, 404, "user not found")
				return
			}
			var body struct {
				Disabled *bool `json:"disabled"`
				Admin    *bool `json:"admin"`
			}
			if !decode(w, r, &body) {
				return
			}
			if body.Disabled == nil && body.Admin == nil {
				fail(w, 400, "disabled or admin field required")
				return
			}
			if target.Name == "admin" && body.Disabled != nil && *body.Disabled {
				fail(w, 400, "bootstrap administrator cannot be disabled")
				return
			}
			if target.Name == "admin" && body.Admin != nil && !*body.Admin {
				fail(w, 400, "bootstrap administrator cannot be demoted")
				return
			}
			if body.Disabled != nil {
				target.Disabled = *body.Disabled
			}
			if body.Admin != nil {
				target.Admin = *body.Admin
			}
			next.Users[target.Name] = target
		} else if len(parts) == 4 && parts[3] == "password" && r.Method == "POST" {
			target, ok := next.Users[parts[2]]
			if !ok {
				fail(w, 404, "user not found")
				return
			}
			var body struct {
				Password string `json:"password"`
			}
			if !decode(w, r, &body) {
				return
			}
			if len(body.Password) < 16 {
				fail(w, 400, "password must contain at least 16 characters")
				return
			}
			updated := passwordUser(target.Name, body.Password, target.Admin)
			updated.Disabled = target.Disabled
			updated.UnixID = target.UnixID
			updated.LinuxName = target.LinuxName
			next.Users[target.Name] = updated
		} else if len(parts) == 3 && r.Method == "DELETE" {
			if parts[2] == "admin" {
				fail(w, 400, "bootstrap administrator cannot be deleted")
				return
			}
			if _, ok := next.Users[parts[2]]; !ok {
				fail(w, 404, "user not found")
				return
			}
			delete(next.Users, parts[2])
			for id, device := range next.Devices {
				users := []string{}
				for _, name := range device.AllowedUsers {
					if name != parts[2] {
						users = append(users, name)
					}
				}
				device.AllowedUsers = users
				next.Devices[id] = device
			}
			for g, members := range next.Groups {
				filtered := []string{}
				for _, m := range members {
					if m != parts[2] {
						filtered = append(filtered, m)
					}
				}
				next.Groups[g] = filtered
			}
		} else {
			fail(w, 404, "not found")
			return
		}
	case "groups":
		if len(parts) == 2 && r.Method == "GET" {
			reply(w, 200, next.Groups)
			return
		}
		if len(parts) == 2 && r.Method == "POST" {
			var b struct {
				Name string `json:"name"`
			}
			if !decode(w, r, &b) {
				return
			}
			if !validName.MatchString(b.Name) {
				fail(w, 400, "invalid group name")
				return
			}
			if _, ok := next.Groups[b.Name]; ok {
				fail(w, 409, "group exists")
				return
			}
			next.Groups[b.Name] = []string{}
		} else if len(parts) == 3 && r.Method == "DELETE" {
			if _, ok := next.Groups[parts[2]]; !ok {
				fail(w, 404, "group not found")
				return
			}
			delete(next.Groups, parts[2])
			for id, device := range next.Devices {
				groups := []string{}
				for _, name := range device.AllowedGroups {
					if name != parts[2] {
						groups = append(groups, name)
					}
				}
				device.AllowedGroups = groups
				next.Devices[id] = device
			}
		} else if len(parts) == 5 && parts[3] == "members" && (r.Method == "PUT" || r.Method == "DELETE") {
			members, ok := next.Groups[parts[2]]
			if !ok {
				fail(w, 404, "group not found")
				return
			}
			if _, ok := next.Users[parts[4]]; !ok {
				fail(w, 404, "user not found")
				return
			}
			filtered := []string{}
			for _, m := range members {
				if m != parts[4] {
					filtered = append(filtered, m)
				}
			}
			if r.Method == "PUT" {
				filtered = append(filtered, parts[4])
			}
			next.Groups[parts[2]] = filtered
		} else {
			fail(w, 404, "not found")
			return
		}
	default:
		fail(w, 404, "not found")
		return
	}
	if err := s.save(next); err != nil {
		fail(w, 500, "directory persistence failed")
		return
	}
	s.state = next
	if parts[1] == "users" && len(parts) >= 3 {
		s.revokeSessions(parts[2])
	}
	reply(w, 200, map[string]bool{"ok": true})
}

// Called only with the server mutex held.
func (s *Server) revokeSessions(name string) {
	for key, session := range s.sessions {
		if session.User == name {
			delete(s.sessions, key)
		}
	}
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, u User) {
	var body struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &body) {
		return
	}
	now := time.Now()
	attempts := s.attempts["password"][:0]
	for _, t := range s.attempts["password"] {
		if now.Sub(t) < time.Minute {
			attempts = append(attempts, t)
		}
	}
	if len(attempts) >= 30 {
		s.attempts["password"] = attempts
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "password change rate limit exceeded")
		return
	}
	s.attempts["password"] = append(attempts, now)
	if len(body.New) < 16 {
		fail(w, 400, "password must contain at least 16 characters")
		return
	}
	if subtle.ConstantTimeCompare([]byte(derive(body.Current, u.Salt)), []byte(u.Hash)) != 1 {
		fail(w, 403, "current password is incorrect")
		return
	}
	data, _ := json.Marshal(s.state)
	var next State
	_ = json.Unmarshal(data, &next)
	updated := passwordUser(u.Name, body.New, u.Admin)
	updated.Disabled = u.Disabled
	updated.UnixID = u.UnixID
	updated.LinuxName = u.LinuxName
	next.Users[u.Name] = updated
	if err := s.save(next); err != nil {
		fail(w, 500, "directory persistence failed")
		return
	}
	s.state = next
	s.revokeSessions(u.Name)
	reply(w, 200, map[string]bool{"ok": true})
}
