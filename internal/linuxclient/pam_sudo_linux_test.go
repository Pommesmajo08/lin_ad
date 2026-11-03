package linuxclient

import (
	"bytes"
	"strings"
	"testing"
)

func TestSudoPAMBlockPreservesOriginalStack(t *testing.T) {
	original := []byte("# Local administrator configuration\n@include common-auth\naccount required pam_unix.so\n")
	installed := append([]byte(sudoPAMBlock), original...)
	restored, err := stripSudoPAM(installed)
	if err != nil || !bytes.Equal(restored, original) {
		t.Fatal("PAM stack changed outside managed block", err)
	}
	again := append([]byte(sudoPAMBlock), restored...)
	if !bytes.Equal(again, installed) {
		t.Fatal("reinstall is not idempotent")
	}
	for _, bad := range []string{sudoPAMBegin, sudoPAMEnd, sudoPAMEnd + sudoPAMBegin, sudoPAMBlock + sudoPAMBlock} {
		if _, err := stripSudoPAM([]byte(bad)); err == nil {
			t.Fatal("ambiguous managed block accepted")
		}
	}
	if strings.Contains(sudoPAMBlock, "\nauth ") {
		t.Fatal("managed block must not bypass original authentication rules")
	}
	if !strings.Contains(sudoPAMBlock, "account requisite") || !strings.Contains(sudoPAMBlock, "pam-sudo-check") {
		t.Fatal("mandatory role validation missing")
	}
}
