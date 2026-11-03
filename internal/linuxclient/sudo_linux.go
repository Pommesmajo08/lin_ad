package linuxclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/Pommesmajo08/lin_ad/internal/directory"
)

const sudoPolicyPath = "/etc/sudoers.d/linad-linux"

var sudoInclude = regexp.MustCompile(`(?m)^[ \t]*[@#]includedir[ \t]+/etc/sudoers\.d[ \t]*$`)

func sudoPolicy(deviceID string, accounts map[string]directory.UnixAccount, supportsRunasPW bool) string {
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
		// Explicit PAM hooks check the current server role on every invocation,
		// including sudo -i. Never cache authentication or use a target/root password.
		fmt.Fprintf(&b, "Defaults:%s timestamp_timeout=0, !rootpw, !targetpw", name)
		if supportsRunasPW {
			b.WriteString(", !runaspw")
		}
		b.WriteByte('\n')
		fmt.Fprintf(&b, "%s ALL=(ALL:ALL) PASSWD: ALL\n", name)
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
	needsPolicy := false
	for _, account := range accounts {
		if account.Admin {
			needsPolicy = true
			break
		}
	}
	supportsRunasPW := false
	if needsPolicy {
		supportsRunasPW, err = probeRunasPW(ctx)
		if err != nil {
			return err
		}
	}
	policy := sudoPolicy(deviceID, accounts, supportsRunasPW)
	if policy == "" {
		if old != nil {
			return os.Remove(sudoPolicyPath)
		}
		return nil
	}
	if !sudoPAMReady() {
		if old != nil {
			if err = os.Remove(sudoPolicyPath); err != nil {
				return err
			}
		}
		return errors.New("sudo PAM protection missing; Linad sudo grant removed; rerun the updated installer")
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
	if err = os.Rename(f.Name(), sudoPolicyPath); err != nil {
		return err
	}
	if err = run(ctx, "/usr/sbin/visudo", "--check", "--quiet"); err != nil {
		if old == nil {
			if rollback := os.Remove(sudoPolicyPath); rollback != nil {
				return fmt.Errorf("sudo validation failed: %v; rollback failed: %w", err, rollback)
			}
		} else if rollback := atomicPAM(sudoPolicyPath, old, 0440, 0); rollback != nil {
			return fmt.Errorf("sudo validation failed: %v; rollback failed: %w", err, rollback)
		}
		return err
	}
	return nil
}

// sudo-rs rejects runaspw, while classic sudo may need it explicitly unset.
// Only a confirmed unsupported-setting diagnostic is treated as absence.
func probeRunasPW(ctx context.Context) (bool, error) {
	f, err := os.CreateTemp("/etc/sudoers.d", ".linad-probe-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString("Defaults !runaspw\n"); err != nil {
		f.Close()
		return false, err
	}
	if err = f.Close(); err != nil {
		return false, err
	}
	cmd := exec.CommandContext(ctx, "/usr/sbin/visudo", "--check", "--quiet", "--file", f.Name())
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	output, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	message := string(output)
	if strings.Contains(message, "unknown setting") && strings.Contains(message, "runaspw") {
		return false, nil
	}
	return false, fmt.Errorf("sudo capability check failed: %w: %s", err, strings.TrimSpace(message))
}
