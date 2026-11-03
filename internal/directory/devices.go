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

// Device is an inventory entry. It is not a machine authentication credential.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OS        string    `json:"os"`
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

var validDeviceName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// devices is called with the directory mutex held and an authenticated user.
func (s *Server) devices(w http.ResponseWriter, r *http.Request, u User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 2 && r.Method == "GET" {
		result := []Device{}
		for _, device := range s.state.Devices {
			if u.Admin || device.Owner == u.Name {
				result = append(result, device)
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
	data, _ := json.Marshal(s.state)
	var next State
	_ = json.Unmarshal(data, &next)
	if next.Devices == nil {
		next.Devices = map[string]Device{}
	}
	var result any = map[string]bool{"ok": true}
	status := 200
	if len(parts) == 2 && r.Method == "POST" {
		var body struct {
			Name string `json:"name"`
			OS   string `json:"os"`
		}
		if !decode(w, r, &body) {
			return
		}
		if !validDeviceName.MatchString(body.Name) || (body.OS != "linux" && body.OS != "windows") {
			fail(w, 400, "valid device name and OS (linux or windows) required")
			return
		}
		for _, device := range next.Devices {
			if device.Name == body.Name && device.Owner == u.Name {
				fail(w, 409, "device already registered for this user")
				return
			}
		}
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			fail(w, 500, "device registration failed")
			return
		}
		now := time.Now().UTC()
		device := Device{ID: hex.EncodeToString(id), Name: body.Name, OS: body.OS, Owner: u.Name, CreatedAt: now, LastSeen: now}
		next.Devices[device.ID] = device
		result = device
		status = 201
	} else if len(parts) >= 3 {
		device, exists := next.Devices[parts[2]]
		// Hide another user's device identifiers as well as their contents.
		if !exists || (!u.Admin && device.Owner != u.Name) {
			fail(w, 404, "device not found")
			return
		}
		if len(parts) == 3 && r.Method == "DELETE" {
			delete(next.Devices, device.ID)
		} else if len(parts) == 4 && parts[3] == "heartbeat" && r.Method == "POST" {
			device.LastSeen = time.Now().UTC()
			next.Devices[device.ID] = device
			result = device
		} else {
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
