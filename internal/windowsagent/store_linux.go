// Linux support exists to run protocol/ledger tests; the actual agent is Windows-only.
package windowsagent

import (
	"errors"
	"os"
)

func VerifyPrivate(path string) error {
	i, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if i.Mode()&os.ModeSymlink != 0 || i.Mode().Perm()&0077 != 0 {
		return errors.New("private path required")
	}
	return nil
}
func replaceFile(from, to string) error { return os.Rename(from, to) }

func ProtectNew(path string) error { return os.Chmod(path, 0600) }
