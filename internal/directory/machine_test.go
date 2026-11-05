package directory

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnrollmentAndLinuxAuthentication(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "db.json"), "initial-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	s.sessions[tokenKey("admin")] = session{"admin", time.Now().Add(time.Hour)}
	request := func(method, path, auth, body string, want int) string {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s; want %d", method, path, w.Code, w.Body.String(), want)
		}
		return w.Body.String()
	}
	admin := "Bearer admin"
	request("POST", "/v1/users", admin, `{"name":"alice","password":"alice-long-password"}`, 200)
	alice := s.state.Users["alice"]
	if alice.UnixID < MinUnixID || alice.LinuxName == "" {
		t.Fatal("missing Unix identity")
	}
	var device Device
	_ = json.Unmarshal([]byte(request("POST", "/v1/devices", admin, `{"name":"linux-client","os":"linux"}`, 201)), &device)
	var enrollment struct {
		Token string `json:"enrollment_token"`
	}
	_ = json.Unmarshal([]byte(request("POST", "/v1/devices/"+device.ID+"/enrollment", admin, "", 200)), &enrollment)
	request("POST", "/v1/enroll", "", `{"device_id":"`+device.ID+`","enrollment_token":"invalid"}`, 401)
	body := `{"device_id":"` + device.ID + `","enrollment_token":"` + enrollment.Token + `"}`
	oldPath := s.path
	s.path = impossibleStatePath(t)
	request("POST", "/v1/enroll", "", body, 500)
	s.path = oldPath
	var credential struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal([]byte(request("POST", "/v1/enroll", "", body, 200)), &credential)
	request("POST", "/v1/enroll", "", body, 401)
	machine := "Device " + device.ID + "." + credential.Secret
	request("GET", "/v1/users", machine, "", 401)
	if b := request("GET", "/v1/machine/users", machine, "", 200); strings.TrimSpace(b) != "[]" {
		t.Fatal("default allow policy")
	}
	authBody := `{"login":"` + alice.LinuxName + `","password":"alice-long-password"}`
	request("POST", "/v1/machine/authenticate", machine, authBody, 401)
	request("POST", "/v1/groups", admin, `{"name":"linux-users"}`, 200)
	request("PUT", "/v1/groups/linux-users/members/alice", admin, "", 200)
	request("PUT", "/v1/devices/"+device.ID+"/access", admin, `{"allowed_groups":["linux-users"]}`, 200)
	request("POST", "/v1/machine/authenticate", machine, authBody, 200)
	request("POST", "/v1/machine/authenticate", machine, `{"login":"`+alice.LinuxName+`","password":"wrong"}`, 401)
	if b := request("GET", "/v1/machine/users", machine, "", 200); !strings.Contains(b, alice.LinuxName) || strings.Contains(b, "hash") {
		t.Fatal("invalid machine manifest")
	}
	if b := request("GET", "/v1/devices", admin, "", 200); strings.Contains(b, "credential_hash") || strings.Contains(b, "enrollment_hash") || strings.Contains(b, credential.Secret) {
		t.Fatal("credential material leaked")
	}
	request("PATCH", "/v1/users/alice", admin, `{"disabled":true}`, 200)
	request("POST", "/v1/machine/authenticate", machine, authBody, 401)
	request("POST", "/v1/machine/access", machine, `{"login":"`+alice.LinuxName+`"}`, 403)
	request("PATCH", "/v1/users/alice", admin, `{"disabled":false}`, 200)
	request("POST", "/v1/users/alice/password", admin, `{"password":"changed-long-password"}`, 200)
	if s.state.Users["alice"].UnixID != alice.UnixID || s.state.Users["alice"].LinuxName != alice.LinuxName {
		t.Fatal("password reset changed Unix identity")
	}
	request("POST", "/v1/machine/authenticate", machine, authBody, 401)
	request("DELETE", "/v1/groups/linux-users", admin, "", 200)
	request("POST", "/v1/machine/access", machine, `{"login":"`+alice.LinuxName+`"}`, 403)
	request("PUT", "/v1/devices/"+device.ID+"/access", admin, `{"allow_all":true}`, 200)
	request("POST", "/v1/machine/access", machine, `{"login":"`+alice.LinuxName+`"}`, 200)
	// Re-enrollment rotates the credential; generating a token alone does not revoke it.
	_ = json.Unmarshal([]byte(request("POST", "/v1/devices/"+device.ID+"/enrollment", admin, "", 200)), &enrollment)
	request("GET", "/v1/machine/users", machine, "", 200)
	request("POST", "/v1/enroll", "", `{"device_id":"`+device.ID+`","enrollment_token":"`+enrollment.Token+`"}`, 200)
	request("GET", "/v1/machine/users", machine, "", 401)
	request("DELETE", "/v1/devices/"+device.ID+"/credential", admin, "", 200)
	if s.state.Devices[device.ID].CredentialHash != "" || s.state.Devices[device.ID].EnrollmentHash != "" {
		t.Fatal("revocation failed")
	}
}
func TestStableUnixIdentityAndNoReuse(t *testing.T) {
	state := State{Users: map[string]User{"alice": {Name: "alice"}, "bob": {Name: "bob"}}, Groups: map[string][]string{}}
	changed, err := assignUnixIDs(&state)
	if err != nil || !changed {
		t.Fatal(err)
	}
	original := state.Users["alice"]
	delete(state.Users, "alice")
	state.Users["charlie"] = User{Name: "charlie"}
	if _, err = assignUnixIDs(&state); err != nil {
		t.Fatal(err)
	}
	if state.Users["charlie"].UnixID <= original.UnixID {
		t.Fatal("UID reused")
	}
	state.Users["long.name-with-special_1234567890"] = User{Name: "long.name-with-special_1234567890"}
	_, _ = assignUnixIDs(&state)
	if len(state.Users["long.name-with-special_1234567890"].LinuxName) > 32 {
		t.Fatal("Linux name too long")
	}
}
