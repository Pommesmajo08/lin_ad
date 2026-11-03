package linuxclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/Pommesmajo08/lin_ad/internal/directory"
)

const sudoPolicyPath = "/etc/sudoers.d/linad-linux"

var sudoInclude = regexp.MustCompile(`(?m)^[ \t]*[@#]includedir[ \t]+/etc/sudoers\.d[ \t]*$`)

func sudoPolicy(deviceID string, accounts map[string]directory.UnixAccount) string {
	names := []string{}
	for login, a := range accounts {
		if a.Admin {
			names = append(names, login)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by linad-linux; device %s\n", deviceID)
	for _, name := range names {
		// A dedicated PAM service checks the current server role on every invocation,
		// including sudo -i. Never cache authentication or use a target/root password.
		fmt.Fprintf(&b, "Defaults:%s pam_service=\"linad-sudo\", pam_login_service=\"linad-sudo\", timestamp_timeout=0, authenticate, !rootpw, !targetpw, !runaspw\n", name)
		fmt.Fprintf(&b, "%s ALL=(ALL:ALL) ALL\n", name)
	}
	return b.String()
}
func configureSudo(ctx context.Context, deviceID string, accounts map[string]directory.UnixAccount) error {
	marker := "# Managed by linad-linux; device " + deviceID + "\n"
	old, err := os.ReadFile(sudoPolicyPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		st, err := os.Lstat(sudoPolicyPath)
		if err != nil {
			return err
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !st.Mode().IsRegular() || owner.Uid != 0 || st.Mode().Perm()&0022 != 0 || !strings.HasPrefix(string(old), marker) {
			return errors.New("refusing to replace an unmanaged sudo policy")
		}
	}
	policy := sudoPolicy(deviceID, accounts)
	if policy == "" {
		if old != nil {
			return os.Remove(sudoPolicyPath)
		}
		return nil
	}
	pam, err := os.ReadFile("/etc/pam.d/linad-sudo")
	if err != nil || !strings.HasPrefix(string(pam), "# Managed by linad-linux\n") {
		return errors.New("sudo PAM service missing; rerun the updated installer")
	}
	base, err := os.ReadFile("/etc/sudoers")
	if err != nil {
		return errors.New("sudo is required for administrator accounts; install sudo first")
	}
	if !sudoInclude.Match(base) {
		return errors.New("/etc/sudoers must include /etc/sudoers.d")
	}
	if string(old) == policy {
		return nil
	}
	if err = run(ctx, "/usr/sbin/visudo", "--check", "--quiet"); err != nil {
		return err
	}
	f, err := os.CreateTemp("/etc/sudoers.d", ".linad-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(policy); err == nil {
		err = f.Chmod(0440)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = run(ctx, "/usr/sbin/visudo", "--check", "--quiet", "--file", f.Name()); err != nil {
		return err
	}
	return os.Rename(f.Name(), sudoPolicyPath)
}
