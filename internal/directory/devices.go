package directory

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Device struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	OS                string     `json:"os"`
	Owner             string     `json:"owner"`
	CreatedAt         time.Time  `json:"created_at"`
	LastSeen          time.Time  `json:"last_seen"`
	AllowAll          bool       `json:"allow_all"`
	AllowedUsers      []string   `json:"allowed_users"`
	AllowedGroups     []string   `json:"allowed_groups"`
	CredentialHash    string     `json:"credential_hash,omitempty"`
	EnrollmentHash    string     `json:"enrollment_hash,omitempty"`
	EnrollmentExpires time.Time  `json:"enrollment_expires,omitempty"`
	LastJobPoll       *time.Time `json:"last_job_poll"`
	Provisioned       bool       `json:"provisioned"`
}

var validDeviceName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func publicDevice(d Device) Device {
	d.Provisioned = d.CredentialHash != ""
	d.CredentialHash = ""
	d.EnrollmentHash = ""
	if d.AllowedUsers == nil {
		d.AllowedUsers = []string{}
	}
	if d.AllowedGroups == nil {
		d.AllowedGroups = []string{}
	}
	return d
}
func deviceAllows(state State, d Device, u User) bool {
	if u.Disabled {
		return false
	}
	if d.AllowAll {
		return true
	}
	for _, name := range d.AllowedUsers {
		if name == u.Name {
			return true
		}
	}
	for _, g := range d.AllowedGroups {
		for _, name := range state.Groups[g] {
			if name == u.Name {
				return true
			}
		}
	}
	return false
}

// devices runs with the server mutex held. Only administrators may mutate inventory.
func (s *Server) devices(w http.ResponseWriter, r *http.Request, u User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 2 && r.Method == "GET" {
		result := []Device{}
		for _, d := range s.state.Devices {
			if u.Admin || deviceAllows(s.state, d, u) {
				d = publicDevice(d)
				if !u.Admin {
					d.AllowedUsers = nil
					d.AllowedGroups = nil
				}
				result = append(result, d)
			}
		}
		sort.Slice(result, func(i, j int) bool {
			if result[i].Name == result[j].Name {
				return result[i].ID < result[j].ID
			}
			return result[i].Name < result[j].Name
		})
		reply(w, 200, result)
		return
	}
	if !u.Admin {
		fail(w, 403, "administrator required")
		return
	}
	data, _ := json.Marshal(s.state)
	var next State
	_ = json.Unmarshal(data, &next)
	if next.Devices == nil {
		next.Devices = map[string]Device{}
	}
	var result any = map[string]bool{"ok": true}
	status := 200
	if len(parts) == 2 && r.Method == "POST" {
		var b struct {
			Name string `json:"name"`
			OS   string `json:"os"`
		}
		if !decode(w, r, &b) {
			return
		}
		if !validDeviceName.MatchString(b.Name) || (b.OS != "linux" && b.OS != "windows") {
			fail(w, 400, "valid device name and OS (linux or windows) required")
			return
		}
		for _, d := range next.Devices {
			if d.Name == b.Name {
				fail(w, 409, "device name already exists")
				return
			}
		}
		id, err := randomToken(16)
		if err != nil {
			fail(w, 500, "device registration failed")
			return
		}
		now := time.Now().UTC()
		d := Device{ID: id, Name: b.Name, OS: b.OS, Owner: u.Name, CreatedAt: now, LastSeen: now, AllowedUsers: []string{}, AllowedGroups: []string{}}
		next.Devices[id] = d
		result = publicDevice(d)
		status = 201
	} else if len(parts) >= 3 {
		d, ok := next.Devices[parts[2]]
		if !ok {
			fail(w, 404, "device not found")
			return
		}
		switch {
		case len(parts) == 3 && r.Method == "DELETE":
			delete(next.Devices, d.ID)
			now := time.Now().UTC()
			for id, job := range next.Jobs {
				devices := []string{}
				for _, deviceID := range job.DeviceIDs {
					if deviceID != d.ID {
						devices = append(devices, deviceID)
					}
				}
				job.DeviceIDs = devices
				next.Jobs[id] = job
			}
			for id, run := range next.Runs {
				if run.DeviceID == d.ID && run.Status == "queued" {
					run.Status = "cancelled"
					run.FinishedAt = &now
					run.Output = "Device removed."
					next.Runs[id] = run
				}
			}
		case len(parts) == 4 && parts[3] == "heartbeat" && r.Method == "POST":
			d.LastSeen = time.Now().UTC()
			next.Devices[d.ID] = d
			result = publicDevice(d)
		case len(parts) == 4 && parts[3] == "access" && r.Method == "PUT":
			var b struct {
				AllowAll bool     `json:"allow_all"`
				Users    []string `json:"allowed_users"`
				Groups   []string `json:"allowed_groups"`
			}
			if !decode(w, r, &b) {
				return
			}
			for _, name := range b.Users {
				if _, ok := next.Users[name]; !ok {
					fail(w, 400, "unknown user in device policy")
					return
				}
			}
			for _, name := range b.Groups {
				if _, ok := next.Groups[name]; !ok {
					fail(w, 400, "unknown group in device policy")
					return
				}
			}
			d.AllowAll = b.AllowAll
			d.AllowedUsers = uniqueNames(b.Users)
			d.AllowedGroups = uniqueNames(b.Groups)
			next.Devices[d.ID] = d
			result = publicDevice(d)
		case len(parts) == 4 && parts[3] == "enrollment" && r.Method == "POST":
			if d.OS != "linux" {
				fail(w, 400, "Linux provisioning requires a Linux device")
				return
			}
			token, err := randomToken(32)
			if err != nil {
				fail(w, 500, "enrollment failed")
				return
			}
			d.EnrollmentHash = tokenKey(token)
			d.EnrollmentExpires = time.Now().UTC().Add(15 * time.Minute)
			next.Devices[d.ID] = d
			result = map[string]any{"device_id": d.ID, "enrollment_token": token, "expires_at": d.EnrollmentExpires}
		case len(parts) == 4 && parts[3] == "credential" && r.Method == "DELETE":
			d.CredentialHash = ""
			d.EnrollmentHash = ""
			d.EnrollmentExpires = time.Time{}
			next.Devices[d.ID] = d
		default:
			fail(w, 404, "not found")
			return
		}
	} else {
		fail(w, 404, "not found")
		return
	}
	if err := s.save(next); err != nil {
		fail(w, 500, "directory persistence failed")
		return
	}
	s.state = next
	reply(w, status, result)
}
func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func uniqueNames(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, name := range in {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
