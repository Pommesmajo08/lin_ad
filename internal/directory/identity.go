package directory

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const MinUnixID = 60001
const MaxUnixID = 64999

type UnixAccount struct {
	Name  string `json:"name"`
	Login string `json:"login"`
	UID   int    `json:"uid"`
	GID   int    `json:"gid"`
}

// LinuxLogin uses a readable prefix and a unique immutable UID suffix.
func LinuxLogin(name string, uid int) string {
	if len(name) > 16 {
		name = name[:16]
	}
	return fmt.Sprintf("linad_%s_%d", strings.ReplaceAll(name, ".", "_"), uid)
}
func assignUnixIDs(state *State) (bool, error) {
	changed := false
	if state.NextUnixID < MinUnixID {
		state.NextUnixID = MinUnixID
		changed = true
	}
	names := make([]string, 0, len(state.Users))
	used := map[int]bool{}
	for name, u := range state.Users {
		names = append(names, name)
		if u.UnixID != 0 {
			if u.UnixID < MinUnixID || u.UnixID > MaxUnixID || used[u.UnixID] {
				return false, errors.New("invalid or duplicate Unix identity")
			}
			used[u.UnixID] = true
			if state.NextUnixID <= u.UnixID {
				state.NextUnixID = u.UnixID + 1
				changed = true
			}
		}
	}
	sort.Strings(names)
	for _, name := range names {
		u := state.Users[name]
		if u.UnixID == 0 {
			if state.NextUnixID > MaxUnixID {
				return false, errors.New("Unix identity range exhausted")
			}
			u.UnixID = state.NextUnixID
			state.NextUnixID++
			changed = true
		}
		login := LinuxLogin(name, u.UnixID)
		if u.LinuxName != login {
			u.LinuxName = login
			changed = true
		}
		state.Users[name] = u
	}
	return changed, nil
}
