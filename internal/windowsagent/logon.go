package windowsagent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type LocalAccount struct {
	SID, Comment   string
	Enabled, Admin bool
}
type AccountBackend interface {
	Lookup(string) (LocalAccount, bool, error)
	Create(string, string, string) error
	Set(string, string, bool, bool) error
	Enumerate() ([]string, error)
}
type ManagedAccount struct {
	Account     directory.WindowsAccount `json:"account"`
	SID         string                   `json:"sid"`
	Reservation string                   `json:"reservation"`
	LeaseHash   string                   `json:"lease_hash,omitempty"`
	LeaseUntil  time.Time                `json:"lease_until"`
}
type AccountRegistry struct {
	DeviceID string                    `json:"device_id"`
	Server   string                    `json:"server"`
	Accounts map[string]ManagedAccount `json:"accounts"`
}
type LogonManager struct {
	mu      sync.Mutex
	Client  *Client
	Dir     string
	Backend AccountBackend
	Now     func() time.Time
}
type Grant struct{ Login, Password, Ticket string }

func randomText() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
func (m *LogonManager) clock() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now().UTC()
}
func (m *LogonManager) path() string { return filepath.Join(m.Dir, "windows-accounts.json") }
func (m *LogonManager) load() (AccountRegistry, error) {
	var registry AccountRegistry
	e := Read(m.path(), &registry)
	if errors.Is(e, os.ErrNotExist) {
		return AccountRegistry{DeviceID: m.Client.Config.DeviceID, Server: m.Client.Config.Server, Accounts: map[string]ManagedAccount{}}, nil
	}
	if e != nil {
		return registry, e
	}
	if registry.DeviceID != m.Client.Config.DeviceID || registry.Server != m.Client.Config.Server || registry.Accounts == nil {
		return registry, errors.New("account registry belongs to another device/server")
	}
	for name, a := range registry.Accounts {
		if name != a.Account.Login || !validWindowsAccount(a.Account) || !Hex(a.Reservation, 64) {
			return registry, errors.New("invalid managed account registry")
		}
	}
	return registry, nil
}
func validWindowsAccount(a directory.WindowsAccount) bool {
	return a.ID >= directory.MinUnixID && a.ID <= directory.MaxUnixID && a.Login == directory.WindowsLogin(a.ID) && a.Name != ""
}
func (m *LogonManager) comment(a ManagedAccount) string {
	return "Linad " + m.Client.Config.DeviceID + " " + a.Reservation
}
func (m *LogonManager) ensure(r *AccountRegistry, a directory.WindowsAccount) (ManagedAccount, error) {
	if !validWindowsAccount(a) {
		return ManagedAccount{}, errors.New("invalid Windows account received")
	}
	saved, managed := r.Accounts[a.Login]
	if managed && (saved.Account.ID != a.ID || saved.Account.Name != a.Name) {
		return saved, errors.New("immutable account identity changed")
	}
	local, exists, e := m.Backend.Lookup(a.Login)
	if e != nil {
		return saved, e
	}
	if exists && (!managed || local.Comment != m.comment(saved) || (saved.SID != "" && saved.SID != local.SID)) {
		return saved, errors.New("refusing to adopt an existing local account")
	}
	if !managed {
		reservation, e := randomText()
		if e != nil {
			return saved, e
		}
		saved = ManagedAccount{Account: a, Reservation: reservation}
		r.Accounts[a.Login] = saved
		if e = Save(m.path(), r); e != nil {
			return saved, e
		}
	}
	if !exists {
		password, e := randomText()
		if e != nil {
			return saved, e
		}
		if e = m.Backend.Create(a.Login, "aA1!"+password, m.comment(saved)); e != nil {
			return saved, e
		}
		local, exists, e = m.Backend.Lookup(a.Login)
		if e != nil || !exists {
			return saved, errors.New("created Windows account could not be verified")
		}
	}
	if local.Comment != m.comment(saved) || (saved.SID != "" && saved.SID != local.SID) {
		return saved, errors.New("local SID/ownership mismatch")
	}
	if saved.SID == local.SID && saved.Account == a {
		return saved, nil
	}
	saved.SID = local.SID
	saved.Account = a
	r.Accounts[a.Login] = saved
	return saved, Save(m.path(), r)
}
func (m *LogonManager) Sync(ctx context.Context) error {
	var accounts []directory.WindowsAccount
	if e := m.Client.Request(ctx, "GET", "/v1/machine/windows/users", nil, &accounts, true); e != nil {
		// Network failure must still expire any short-lived local authentication grants.
		_ = m.Expire()
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, e := m.load()
	if e != nil {
		return e
	}
	names, e := m.Backend.Enumerate()
	if e != nil {
		return e
	}
	for _, name := range names {
		if len(name) >= 6 && name[:6] == "linad_" {
			if _, ok := r.Accounts[name]; !ok {
				return errors.New("reserved Windows account namespace collision")
			}
		}
	}
	allowed := map[string]bool{}
	for _, a := range accounts {
		if !validWindowsAccount(a) {
			return errors.New("invalid account manifest")
		}
		allowed[a.Login] = true
		saved, e := m.ensure(&r, a)
		if e != nil {
			return e
		}
		if !saved.LeaseUntil.After(m.clock()) {
			if e = m.disable(&r, a.Login, a.Admin); e != nil {
				return e
			}
		}
	}
	for name := range r.Accounts {
		if !allowed[name] {
			if e = m.disable(&r, name, false); e != nil {
				return e
			}
		}
	}
	return Save(m.path(), r)
}
func (m *LogonManager) disable(r *AccountRegistry, name string, admin bool) error {
	saved := r.Accounts[name]
	local, exists, e := m.Backend.Lookup(name)
	if e != nil {
		return e
	}
	if !exists {
		return nil
	}
	if (saved.SID != "" && local.SID != saved.SID) || local.Comment != m.comment(saved) {
		return errors.New("managed account SID/ownership changed")
	}
	saved.SID = local.SID
	if !local.Enabled && saved.LeaseHash == "" && saved.LeaseUntil.IsZero() && local.Admin == admin {
		saved.Account.Admin = admin
		r.Accounts[name] = saved
		return nil
	}
	password, e := randomText()
	if e != nil {
		return e
	}
	if e = m.Backend.Set(name, "aA1!"+password, false, admin); e != nil {
		return e
	}
	saved.LeaseHash = ""
	saved.LeaseUntil = time.Time{}
	saved.Account.Admin = admin
	r.Accounts[name] = saved
	return nil
}
func (m *LogonManager) Expire() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, e := m.load()
	if e != nil {
		return e
	}
	changed := false
	for name, a := range r.Accounts {
		if a.LeaseHash != "" && !a.LeaseUntil.After(m.clock()) {
			if e = m.disable(&r, name, a.Account.Admin); e != nil {
				return e
			}
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return Save(m.path(), r)
}
func (m *LogonManager) Authenticate(ctx context.Context, name, password string) (Grant, error) {
	if len(name) > 64 || len(password) > 512 {
		return Grant{}, errors.New("invalid credentials")
	}
	var account directory.WindowsAccount
	if e := m.Client.Request(ctx, "POST", "/v1/machine/windows/authenticate", map[string]string{"name": name, "password": password}, &account, true); e != nil {
		return Grant{}, errors.New("Linad authentication or device access denied")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, e := m.load()
	if e != nil {
		return Grant{}, e
	}
	saved, e := m.ensure(&r, account)
	if e != nil {
		return Grant{}, e
	}
	if saved.LeaseUntil.After(m.clock()) && saved.LeaseHash != "" {
		return Grant{}, errors.New("another sign-in is already pending")
	}
	secret, e := randomText()
	if e != nil {
		return Grant{}, e
	}
	ticket, e := randomText()
	if e != nil {
		return Grant{}, e
	}
	hash := sha256.Sum256([]byte(ticket))
	saved.LeaseHash = hex.EncodeToString(hash[:])
	saved.LeaseUntil = m.clock().Add(30 * time.Second)
	r.Accounts[account.Login] = saved
	// Journal the lease before enabling an account. Directory passwords never enter SAM.
	if e = Save(m.path(), r); e != nil {
		return Grant{}, e
	}
	if e = m.Backend.Set(account.Login, "aA1!"+secret, true, account.Admin); e != nil {
		return Grant{}, e
	}
	return Grant{account.Login, "aA1!" + secret, ticket}, nil
}
func (m *LogonManager) Complete(login, ticket string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, e := m.load()
	if e != nil {
		return e
	}
	a, ok := r.Accounts[login]
	if !ok {
		return errors.New("unknown grant")
	}
	hash := sha256.Sum256([]byte(ticket))
	expected := hex.EncodeToString(hash[:])
	if a.LeaseHash == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(a.LeaseHash)) != 1 {
		return errors.New("invalid grant")
	}
	if e = m.disable(&r, login, a.Account.Admin); e != nil {
		return e
	}
	return Save(m.path(), r)
}
func (m *LogonManager) DisableAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, e := m.load()
	if e != nil {
		return e
	}
	for name := range r.Accounts {
		if e = m.disable(&r, name, false); e != nil {
			return e
		}
	}
	return Save(m.path(), r)
}

// Check validates namespaces and ownership without provisioning or changing passwords.
func (m *LogonManager) Check(accounts []directory.WindowsAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, e := m.load()
	if e != nil {
		return e
	}
	names, e := m.Backend.Enumerate()
	if e != nil {
		return e
	}
	for _, name := range names {
		if len(name) >= 6 && name[:6] == "linad_" {
			if _, known := r.Accounts[name]; !known {
				return errors.New("reserved Windows account namespace collision")
			}
		}
	}
	for name, a := range r.Accounts {
		local, exists, e := m.Backend.Lookup(name)
		if e != nil {
			return e
		}
		if exists && (local.Comment != m.comment(a) || (a.SID != "" && a.SID != local.SID)) {
			return errors.New("managed account ownership changed")
		}
	}
	for _, a := range accounts {
		if !validWindowsAccount(a) {
			return errors.New("invalid Windows account manifest")
		}
		local, exists, e := m.Backend.Lookup(a.Login)
		if e != nil {
			return e
		}
		saved, known := r.Accounts[a.Login]
		if exists && (!known || local.Comment != m.comment(saved) || (saved.SID != "" && saved.SID != local.SID)) {
			return errors.New("refusing to adopt a local Windows account")
		}
	}
	return nil
}
