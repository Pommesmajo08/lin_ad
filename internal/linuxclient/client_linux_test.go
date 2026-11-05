package linuxclient

import (
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"testing"
)

func TestAccountValidation(t *testing.T) {
	a := directory.UnixAccount{Name: "alice", Login: directory.LinuxLogin("alice", 60002), UID: 60002, GID: 60002}
	if err := ValidateAccount(a); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []directory.UnixAccount{{Name: "root", Login: "root", UID: 0, GID: 0}, {Name: "alice", Login: "../../etc/shadow", UID: 60002, GID: 60002}, {Name: "alice", Login: a.Login, UID: 60002, GID: 0}, {Name: "bob", Login: a.Login, UID: 60002, GID: 60002}} {
		if ValidateAccount(bad) == nil {
			t.Fatal("unsafe account accepted", bad)
		}
	}
}
func TestServerValidation(t *testing.T) {
	for _, server := range []string{"https://u:p@example.com", "https://example.com/path", "file:///etc/shadow"} {
		if _, err := New(Config{Server: server, DeviceID: "0123456789abcdef0123456789abcdef", AllowHTTP: true}); err == nil {
			t.Fatal("unsafe server accepted", server)
		}
	}
	for _, server := range []string{"http://127.0.0.1:8080", "http://192.168.1.10:8080", "http://directory.example:8080", "http://[2001:db8::1]:8080"} {
		t.Run(server, func(t *testing.T) {
			config := Config{Server: server, DeviceID: "0123456789abcdef0123456789abcdef"}
			if _, err := New(config); err != nil {
				t.Fatal("HTTP rejected by default", err)
			}
			config.AllowHTTP = true
			if _, err := New(config); err != nil {
				t.Fatal(err)
			}
		})
	}
}
