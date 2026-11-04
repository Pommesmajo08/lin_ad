// Package linuxclient implements the root-owned provisioning and PAM client.
package linuxclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Pommesmajo08/lin_ad/internal/directory"
)

const DefaultConfig = "/etc/linad-linux/client.json"
const DefaultState = "/var/lib/linad-linux/accounts.json"

type Config struct {
	Server    string `json:"server"`
	DeviceID  string `json:"device_id"`
	Secret    string `json:"secret"`
	CAFile    string `json:"ca_file,omitempty"`
	AllowHTTP bool   `json:"allow_http,omitempty"`
}
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string {
	return fmt.Sprintf("directory request rejected (HTTP %d)", e.Status)
}

type Client struct {
	Config Config
	HTTP   *http.Client
}

func validateConfig(c Config) error {
	u, err := url.Parse(c.Server)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("invalid server URL")
	}
	if u.Scheme != "https" {
		if u.Scheme != "http" || !c.AllowHTTP {
			return errors.New("HTTPS required; use --allow-http to explicitly allow HTTP")
		}
	}
	if len(c.DeviceID) != 32 || strings.Trim(c.DeviceID, "0123456789abcdef") != "" {
		return errors.New("invalid device ID")
	}
	return nil
}
func New(c Config) (*Client, error) {
	if err := validateConfig(c); err != nil {
		return nil, err
	}
	// Fixed system bundle plus an optional root-selected CA. Never trust PAM caller
	// overrides such as SSL_CERT_FILE, SSL_CERT_DIR or HTTPS_PROXY.
	roots := x509.NewCertPool()
	if data, err := os.ReadFile("/etc/ssl/certs/ca-certificates.crt"); err == nil {
		roots.AppendCertsFromPEM(data)
	}
	if c.CAFile != "" {
		data, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, err
		}
		if !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("CA file contains no certificates")
		}
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	return &Client{Config: c, HTTP: &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func SecureRead(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%s must be a regular root-owned file, mode 0600", path)
	}
	return os.ReadFile(path)
}
func Load(path string) (*Client, error) {
	b, err := SecureRead(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if len(c.Secret) != 64 || strings.Trim(c.Secret, "0123456789abcdef") != "" {
		return nil, errors.New("invalid device credential")
	}
	return New(c)
}
func AtomicJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".linad-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(value); err == nil {
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
func (c *Client) request(ctx context.Context, method, path string, body, out any, authenticated bool) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	r, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Config.Server, "/")+path, reader)
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	if authenticated {
		r.Header.Set("Authorization", "Device "+c.Config.DeviceID+"."+c.Config.Secret)
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return &HTTPError{Status: resp.StatusCode}
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
	}
	return nil
}
func (c *Client) Enroll(ctx context.Context, token, configPath string) error {
	var response struct {
		DeviceID string `json:"device_id"`
		Secret   string `json:"secret"`
	}
	if err := c.request(ctx, "POST", "/v1/enroll", map[string]string{"device_id": c.Config.DeviceID, "enrollment_token": token}, &response, false); err != nil {
		return err
	}
	if response.DeviceID != c.Config.DeviceID || len(response.Secret) != 64 || strings.Trim(response.Secret, "0123456789abcdef") != "" {
		return errors.New("invalid enrollment response")
	}
	c.Config.Secret = response.Secret
	return AtomicJSON(configPath, c.Config)
}
func (c *Client) Accounts(ctx context.Context) ([]directory.UnixAccount, error) {
	var accounts []directory.UnixAccount
	err := c.request(ctx, "GET", "/v1/machine/users", nil, &accounts, true)
	return accounts, err
}
func (c *Client) Check(ctx context.Context, login, password string, authenticate bool) (directory.UnixAccount, error) {
	path := "/v1/machine/access"
	body := map[string]string{"login": login}
	if authenticate {
		path = "/v1/machine/authenticate"
		body["password"] = password
	}
	var account directory.UnixAccount
	err := c.request(ctx, "POST", path, body, &account, true)
	return account, err
}

// Health checks the endpoint before consuming an enrollment token or installing files.
func (c *Client) Health(ctx context.Context) error {
	var response struct {
		Status string `json:"status"`
	}
	if err := c.request(ctx, "GET", "/healthz", nil, &response, false); err != nil {
		return err
	}
	if response.Status != "ok" {
		return errors.New("server health check failed")
	}
	return nil
}
