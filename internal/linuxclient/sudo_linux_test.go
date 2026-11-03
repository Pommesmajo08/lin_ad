package linuxclient

import (
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"strings"
	"testing"
)

func TestSudoPolicyUsesOnlyAuthorizedAdmins(t *testing.T) {
	admin := directory.UnixAccount{Name: "admin", Login: directory.LinuxLogin("admin", 60001), UID: 60001, GID: 60001, Admin: true}
	alice := directory.UnixAccount{Name: "alice", Login: directory.LinuxLogin("alice", 60002), UID: 60002, GID: 60002}
	policy := sudoPolicy("test-device", map[string]directory.UnixAccount{admin.Login: admin, alice.Login: alice}, false)
	if !strings.Contains(policy, admin.Login+" ALL=(ALL:ALL) PASSWD: ALL") || strings.Contains(policy, alice.Login) || strings.Contains(policy, "NOPASSWD") {
		t.Fatal("incorrect sudo policy")
	}
	if strings.Contains(policy, "pam_service") || strings.Contains(policy, "pam_login_service") || strings.Contains(policy, "authenticate") || strings.Contains(policy, "runaspw") || !strings.Contains(policy, "timestamp_timeout=0") {
		t.Fatal("sudo lacks current-role checks")
	}
	if sudoPolicy("test-device", map[string]directory.UnixAccount{alice.Login: alice}, false) != "" {
		t.Fatal("normal users got sudo policy")
	}
	promoted := alice
	promoted.Admin = true
	if !sameIdentity(alice, promoted) {
		t.Fatal("role change modified identity comparison")
	}
}
