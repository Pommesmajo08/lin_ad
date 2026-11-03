package main

import "testing"

func TestSudoPAMPurposeIsExplicitAndServiceAware(t *testing.T) {
	for _, test := range []struct {
		mode, service string
		want          bool
	}{
		{"pam-sudo-auth", "", true}, {"pam-sudo-check", "", true}, {"pam-account", "sudo", true}, {"pam-account", "sudo-i", true}, {"pam-account", "sudo-rs", true}, {"pam-account", "sudo-rs-i", true}, {"pam-account", "login", false}, {"pam-account", "sshd", false}, {"pam-auth", "sudo", false}, {"sync", "sudo", false},
	} {
		if got := sudoPAMPurpose(test.mode, test.service); got != test.want {
			t.Errorf("%s/%s: %v; want %v", test.mode, test.service, got, test.want)
		}
	}
}
