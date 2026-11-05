package windowsagent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeAccounts struct {
	users     map[string]LocalAccount
	passwords map[string]string
	fail      bool
}

func (f *fakeAccounts) Lookup(name string) (LocalAccount, bool, error) {
	a, ok := f.users[name]
	return a, ok, nil
}
func (f *fakeAccounts) Create(name, password, comment string) error {
	if _, ok := f.users[name]; ok {
		return errors.New("exists")
	}
	f.users[name] = LocalAccount{SID: "S-1-5-21-100-" + name, Comment: comment}
	f.passwords[name] = password
	return nil
}
func (f *fakeAccounts) Set(name, password string, enabled, admin bool) error {
	a, ok := f.users[name]
	if !ok {
		return errors.New("not found")
	}
	a.Enabled = false
	f.users[name] = a
	if f.fail {
		return errors.New("simulated SAM failure")
	}
	a.Enabled = enabled
	a.Admin = admin
	f.users[name] = a
	f.passwords[name] = password
	return nil
}
func (f *fakeAccounts) Enumerate() ([]string, error) {
	names := []string{}
	for name := range f.users {
		names = append(names, name)
	}
	return names, nil
}
func TestWindowsOnlineLogonAndRevocation(t *testing.T) {
	s, e := directory.New(filepath.Join(t.TempDir(), "server.json"), "initial-admin-password")
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	auth := ""
	request := func(method, path string, body any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(string(b)))
		r.Header.Set("Authorization", auth)
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			t.Fatal(method, path, resp.Status)
		}
		if method == "PUT" || method == "PATCH" || method == "POST" {
			var out map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&out)
			return out
		}
		return nil
	}
	auth = "Bearer " + request("POST", "/v1/login", map[string]string{"name": "admin", "password": "initial-admin-password"})["token"].(string)
	password := "Linad-password-never-SAM"
	request("POST", "/v1/users", map[string]string{"name": "alice", "password": password})
	device := request("POST", "/v1/devices", map[string]string{"name": "windows-logon", "os": "windows"})["id"].(string)
	enrollment := request("POST", "/v1/devices/"+device+"/enrollment", nil)
	dir := privateTestDir(t)
	c, e := New(Config{Server: server.URL, DeviceID: device, AllowHTTP: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Enroll(context.Background(), enrollment["enrollment_token"].(string), filepath.Join(dir, "client.json")); e != nil {
		t.Fatal(e)
	}
	backend := &fakeAccounts{users: map[string]LocalAccount{}, passwords: map[string]string{}}
	now := time.Now().UTC()
	m := &LogonManager{Client: c, Dir: dir, Backend: backend, Now: func() time.Time { return now }}
	if _, e = m.Authenticate(context.Background(), "alice", password); e == nil {
		t.Fatal("default deny bypassed")
	}
	if len(backend.users) != 0 {
		t.Fatal("denied authentication created account")
	}
	request("POST", "/v1/groups", map[string]string{"name": "windows-users"})
	request("PUT", "/v1/groups/windows-users/members/alice", nil)
	request("PUT", "/v1/devices/"+device+"/access", map[string]any{"allowed_groups": []string{"windows-users"}})
	if e = m.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(backend.users) != 1 {
		t.Fatal("manifest not provisioned")
	}
	for _, a := range backend.users {
		if a.Enabled {
			t.Fatal("account enabled without online authentication")
		}
	}
	if _, e = m.Authenticate(context.Background(), "alice", "wrong"); e == nil {
		t.Fatal("wrong password accepted")
	}
	grant, e := m.Authenticate(context.Background(), "ALICE", password)
	if e != nil {
		t.Fatal(e)
	}
	if grant.Password == password || len(grant.Password) < 32 || !backend.users[grant.Login].Enabled || backend.users[grant.Login].Admin {
		t.Fatal("invalid temporary local grant")
	}
	if e = m.Complete(grant.Login, "wrong-ticket"); e == nil {
		t.Fatal("wrong completion ticket accepted")
	}
	if _, e = m.Authenticate(context.Background(), "alice", password); e == nil {
		t.Fatal("concurrent grant accepted")
	}
	sid := backend.users[grant.Login].SID
	if e = m.Complete(grant.Login, grant.Ticket); e != nil {
		t.Fatal(e)
	}
	if backend.users[grant.Login].Enabled || backend.passwords[grant.Login] == grant.Password {
		t.Fatal("temporary credential not revoked")
	}
	request("PATCH", "/v1/users/alice", map[string]bool{"admin": true})
	grant, e = m.Authenticate(context.Background(), "alice", password)
	if e != nil || !backend.users[grant.Login].Admin || backend.users[grant.Login].SID != sid {
		t.Fatal("role/stable SID failed", e)
	}
	now = now.Add(time.Minute)
	if e = m.Expire(); e != nil {
		t.Fatal(e)
	}
	if backend.users[grant.Login].Enabled {
		t.Fatal("expired grant remains active")
	}
	request("PATCH", "/v1/users/alice", map[string]bool{"disabled": true})
	if _, e = m.Authenticate(context.Background(), "alice", password); e == nil {
		t.Fatal("disabled directory user accepted")
	}
	if e = m.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	if backend.users[grant.Login].Admin {
		t.Fatal("revoked role remains")
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, entry.Name()))
		if strings.Contains(string(data), password) || strings.Contains(string(data), grant.Password) || strings.Contains(string(data), grant.Ticket) {
			t.Fatal("password/ticket persisted", entry.Name())
		}
	}
	// Offline failure may not reopen a managed account.
	c.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
	if _, e = m.Authenticate(context.Background(), "alice", password); e == nil {
		t.Fatal("offline login accepted")
	}
}
func TestWindowsAccountCollisionAndInterruptedReservation(t *testing.T) {
	dir := privateTestDir(t)
	backend := &fakeAccounts{users: map[string]LocalAccount{}, passwords: map[string]string{}}
	m := &LogonManager{Client: &Client{Config: Config{Server: "https://linad.example", DeviceID: strings.Repeat("a", 32)}}, Dir: dir, Backend: backend}
	r, e := m.load()
	if e != nil {
		t.Fatal(e)
	}
	a := directory.WindowsAccount{Name: "alice", Login: directory.WindowsLogin(directory.MinUnixID), ID: directory.MinUnixID}
	backend.users[a.Login] = LocalAccount{SID: "existing-account", Comment: "local user"}
	if _, e = m.ensure(&r, a); e == nil {
		t.Fatal("existing account adopted")
	}
	if backend.users[a.Login].Comment != "local user" {
		t.Fatal("local account changed")
	}
	delete(backend.users, a.Login)
	saved, e := m.ensure(&r, a)
	if e != nil {
		t.Fatal(e)
	}
	// Reservation before the SID write must recover without adopting foreign accounts.
	saved.SID = ""
	r.Accounts[a.Login] = saved
	if e = Save(m.path(), r); e != nil {
		t.Fatal(e)
	}
	if e = m.DisableAll(); e != nil {
		t.Fatal("interrupted creation did not recover", e)
	}
	backend.users[a.Login] = LocalAccount{SID: "different-account", Comment: m.comment(saved)}
	if e = m.DisableAll(); e == nil {
		t.Fatal("SID replacement adopted")
	}
}
