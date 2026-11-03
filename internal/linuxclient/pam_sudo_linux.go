package linuxclient

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const sudoPAMBegin = "# BEGIN linad-linux sudo\n"
const sudoPAMEnd = "# END linad-linux sudo\n"
const sudoPAMBlock = sudoPAMBegin + "account requisite pam_exec.so seteuid /usr/local/libexec/linad-linux pam-sudo-check\n" + sudoPAMEnd

// libpam uses the other stack when sudo-i is absent; preserve that fallback.
const createdSudoI = "# Created by linad-linux: login-shell sudo service\n@include other\n"

func stripSudoPAM(data []byte) ([]byte, error) {
	begin, end := strings.Count(string(data), sudoPAMBegin), strings.Count(string(data), sudoPAMEnd)
	if begin == 0 && end == 0 {
		return data, nil
	}
	if begin != 1 || end != 1 {
		return nil, errors.New("ambiguous linad sudo PAM markers")
	}
	start := bytes.Index(data, []byte(sudoPAMBegin))
	finish := bytes.Index(data, []byte(sudoPAMEnd))
	if finish < start {
		return nil, errors.New("invalid linad sudo PAM marker order")
	}
	base := append([]byte{}, data[:start]...)
	base = append(base, data[finish+len(sudoPAMEnd):]...)
	return base, nil
}
func sudoPAMTarget(path string) (string, os.FileInfo, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, err
	}
	if !strings.HasPrefix(target, "/etc/pam.d/") {
		return "", nil, errors.New("sudo PAM target must remain in /etc/pam.d")
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || owner.Uid != 0 || info.Mode().Perm()&0022 != 0 {
		return "", nil, errors.New("sudo PAM service must be a root-owned regular file without group/other write access")
	}
	return target, info, nil
}
func atomicPAM(path string, data []byte, mode os.FileMode, gid int) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".linad-pam-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Chown(0, gid)
	}
	if err == nil {
		err = f.Chmod(mode)
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
	return os.Rename(f.Name(), path)
}

// ConfigureSudoPAM adds/removes a bounded block and preserves all original PAM
// entries. Purpose is explicit in the helper name, not inferred from PAM_SERVICE.
func ConfigureSudoPAM(install bool) error {
	if os.Geteuid() != 0 {
		return errors.New("sudo PAM configuration requires root")
	}
	for _, name := range []string{"sudo", "sudo-i"} {
		path := "/etc/pam.d/" + name
		target, info, err := sudoPAMTarget(path)
		if errors.Is(err, os.ErrNotExist) {
			if !install {
				continue
			}
			if name == "sudo" {
				return errors.New("sudo PAM stack missing; install a PAM-enabled sudo package")
			}
			// Do not replace a dangling symlink or any pre-existing filesystem entry.
			if _, e := os.Lstat(path); e == nil || !errors.Is(e, os.ErrNotExist) {
				return errors.New("invalid existing sudo-i PAM path")
			}
			if err = atomicPAM(path, []byte(sudoPAMBlock+createdSudoI), 0644, 0); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		base, err := stripSudoPAM(data)
		if err != nil {
			return err
		}
		if install {
			if err = os.MkdirAll("/var/lib/linad-linux/sudo-pam-backup", 0700); err != nil {
				return err
			}
			backup, err := os.OpenFile("/var/lib/linad-linux/sudo-pam-backup/"+name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err == nil {
				_, writeErr := backup.Write(base)
				closeErr := backup.Close()
				if writeErr != nil {
					return writeErr
				}
				if closeErr != nil {
					return closeErr
				}
			} else if !errors.Is(err, os.ErrExist) {
				return err
			}
		}
		output := base
		if install {
			output = append([]byte(sudoPAMBlock), base...)
		}
		if bytes.Equal(output, data) {
			continue
		}
		if !install && string(base) == createdSudoI {
			if err = os.Remove(target); err != nil {
				return err
			}
			continue
		}
		owner := info.Sys().(*syscall.Stat_t)
		if err = atomicPAM(target, output, info.Mode().Perm(), int(owner.Gid)); err != nil {
			return fmt.Errorf("update sudo PAM %s: %w", name, err)
		}
	}
	return nil
}
func sudoPAMReady() bool {
	for _, name := range []string{"sudo", "sudo-i"} {
		path := "/etc/pam.d/" + name
		target, _, err := sudoPAMTarget(path)
		if err != nil {
			return false
		}
		data, err := os.ReadFile(target)
		if err != nil || !bytes.HasPrefix(data, []byte(sudoPAMBlock)) {
			return false
		}
	}
	return true
}
