package linuxclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/Pommesmajo08/lin_ad/internal/directory"
)

type Registry struct {
	DeviceID string                           `json:"device_id"`
	Accounts map[string]directory.UnixAccount `json:"accounts"`
}
type LocalUser struct {
	Name                 string
	UID, GID             int
	Comment, Home, Shell string
}

var accountName = regexp.MustCompile(`^linad_[a-z0-9_-]{1,16}_[0-9]{5}$`)

func ValidateAccount(a directory.UnixAccount) error {
	if a.UID < directory.MinUnixID || a.UID > directory.MaxUnixID || a.GID != a.UID || !accountName.MatchString(a.Login) || a.Login != directory.LinuxLogin(a.Name, a.UID) {
		return errors.New("unsafe Unix account received from server")
	}
	return nil
}
func ReadRegistry(path, deviceID string) (Registry, error) {
	data, err := SecureRead(path)
	if errors.Is(err, os.ErrNotExist) {
		return Registry{DeviceID: deviceID, Accounts: map[string]directory.UnixAccount{}}, nil
	}
	if err != nil {
		return Registry{}, err
	}
	var registry Registry
	if err = json.Unmarshal(data, &registry); err != nil {
		return registry, err
	}
	if registry.DeviceID != deviceID || registry.Accounts == nil {
		return registry, errors.New("local account registry belongs to another device")
	}
	for login, a := range registry.Accounts {
		if login != a.Login {
			return registry, errors.New("invalid local registry")
		}
		if err = ValidateAccount(a); err != nil {
			return registry, err
		}
	}
	return registry, nil
}
func localUsers() (map[string]LocalUser, error) {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	users := map[string]LocalUser{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) != 7 {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, err
		}
		gid, err := strconv.Atoi(fields[3])
		if err != nil {
			return nil, err
		}
		users[fields[0]] = LocalUser{fields[0], uid, gid, fields[4], fields[5], fields[6]}
	}
	return users, scanner.Err()
}
func groupExists(name string, gid int) (bool, error) {
	b, err := os.ReadFile("/etc/group")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 3 {
			continue
		}
		id, _ := strconv.Atoi(f[2])
		if f[0] == name || id == gid {
			if f[0] == name && id == gid {
				return true, nil
			}
			return false, errors.New("local group name or GID collision")
		}
	}
	return false, nil
}
func owns(local LocalUser, a directory.UnixAccount) bool {
	return local.Name == a.Login && local.UID == a.UID && local.GID == a.GID && local.Home == "/home/"+a.Login
}
func run(ctx context.Context, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", filepath.Base(program), err, strings.TrimSpace(string(output)))
	}
	return nil
}
func setExpired(ctx context.Context, login string, expired bool) error {
	b, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return err
	}
	desired := ""
	if expired {
		desired = "1"
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 7 && f[0] == login {
			if f[7] == desired {
				return nil
			}
			return run(ctx, "/usr/sbin/usermod", "--expiredate", desired, login)
		}
	}
	return errors.New("managed account has no shadow entry")
}

// Sync never deletes homes, adopts local accounts or grants privileged groups.
func Sync(ctx context.Context, c *Client, statePath string) error {
	accounts, err := c.Accounts(ctx)
	if err != nil {
		return err
	}
	return Provision(ctx, c.Config.DeviceID, statePath, accounts)
}
func Provision(ctx context.Context, deviceID, statePath string, accounts []directory.UnixAccount) error {
	if os.Geteuid() != 0 {
		return errors.New("provisioning requires root")
	}
	homeRoot, err := os.Lstat("/home")
	if err != nil {
		return err
	}
	stat, ok := homeRoot.Sys().(*syscall.Stat_t)
	if !ok || !homeRoot.IsDir() || homeRoot.Mode()&os.ModeSymlink != 0 || stat.Uid != 0 || homeRoot.Mode().Perm()&0022 != 0 {
		return errors.New("/home must be a root-owned directory without group/other write access")
	}
	desired := map[string]directory.UnixAccount{}
	uids := map[int]bool{}
	for _, a := range accounts {
		if err := ValidateAccount(a); err != nil {
			return err
		}
		if _, exists := desired[a.Login]; exists || uids[a.UID] {
			return errors.New("duplicate Unix identity in manifest")
		}
		desired[a.Login] = a
		uids[a.UID] = true
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(statePath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	registry, err := ReadRegistry(statePath, deviceID)
	if err != nil {
		return err
	}
	users, err := localUsers()
	if err != nil {
		return err
	}
	// Refuse namespace collisions before enabling PAM or mutating any accounts.
	for login, local := range users {
		if strings.HasPrefix(login, "linad_") {
			a, managed := registry.Accounts[login]
			if !managed || !owns(local, a) {
				return fmt.Errorf("reserved linad_ namespace collision: %s", login)
			}
		}
	}
	// Revoke removed access before creating new accounts, preserving all home data.
	for login, a := range registry.Accounts {
		if _, allowed := desired[login]; !allowed {
			if local, exists := users[login]; exists {
				if !owns(local, a) {
					return errors.New("managed account ownership changed")
				}
				if err = setExpired(ctx, login, true); err != nil {
					return err
				}
			}
		}
	}
	for login, a := range desired {
		recorded, managed := registry.Accounts[login]
		if managed && recorded != a {
			return errors.New("immutable account identity changed")
		}
		local, exists := users[login]
		if exists {
			if !managed || !owns(local, a) {
				return fmt.Errorf("refusing to adopt local account %s", login)
			}
		} else {
			for _, other := range users {
				if other.UID == a.UID {
					return errors.New("local UID collision")
				}
			}
			group, err := groupExists(login, a.GID)
			if err != nil {
				return err
			}
			if group && !managed {
				return errors.New("refusing to adopt local group")
			}
			if _, err := os.Lstat("/home/" + login); err == nil || !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("home path already exists for %s", login)
			}
			// Journal the reservation before account creation so an interrupted run can resume.
			registry.Accounts[login] = a
			if err = AtomicJSON(statePath, registry); err != nil {
				return err
			}
			if !group {
				if err = run(ctx, "/usr/sbin/groupadd", "--gid", strconv.Itoa(a.GID), login); err != nil {
					return err
				}
			}
			if err = run(ctx, "/usr/sbin/useradd", "--no-log-init", "--uid", strconv.Itoa(a.UID), "--gid", strconv.Itoa(a.GID), "--create-home", "--home-dir", "/home/"+login, "--shell", "/bin/bash", "--comment", "linad-"+deviceID, "--password", "!", login); err != nil {
				return err
			}
			home, err := os.Lstat("/home/" + login)
			if err != nil {
				return err
			}
			if !home.IsDir() || home.Mode()&os.ModeSymlink != 0 {
				return errors.New("invalid managed home directory")
			}
			if err = os.Chmod("/home/"+login, 0700); err != nil {
				return err
			}
		}
		home, err := os.Lstat("/home/" + login)
		if err != nil {
			return err
		}
		st, ok := home.Sys().(*syscall.Stat_t)
		if !ok || !home.IsDir() || home.Mode()&os.ModeSymlink != 0 || int(st.Uid) != a.UID {
			return errors.New("managed home ownership mismatch")
		}
		if err = os.Chmod("/home/"+login, 0700); err != nil {
			return err
		}
		if err = setExpired(ctx, login, false); err != nil {
			return err
		}
	}
	return nil
}
func PAMCheck(ctx context.Context, c *Client, statePath, login, password string, auth bool) error {
	if !strings.HasPrefix(login, "linad_") {
		if auth {
			return errors.New("not a linad account")
		}
		return nil
	}
	registry, err := ReadRegistry(statePath, c.Config.DeviceID)
	if err != nil {
		return err
	}
	expected, managed := registry.Accounts[login]
	if !managed {
		return errors.New("unknown managed account")
	}
	users, err := localUsers()
	if err != nil {
		return err
	}
	local, exists := users[login]
	if !exists || !owns(local, expected) {
		return errors.New("local identity mismatch")
	}
	actual, err := c.Check(ctx, login, password, auth)
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("directory identity mismatch")
	}
	return nil
}
