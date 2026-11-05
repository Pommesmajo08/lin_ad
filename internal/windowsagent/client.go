// Package windowsagent implements the Windows device jobs protocol and durable worker.
package windowsagent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Server    string `json:"server"`
	DeviceID  string `json:"device_id"`
	Secret    string `json:"secret"`
	AllowHTTP bool   `json:"allow_http"`
	CAFile    string `json:"ca_file,omitempty"`
}
type Client struct {
	Config Config
	HTTP   *http.Client
}
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("server rejected request (HTTP %d)", e.Status) }
func Hex(value string, n int) bool {
	return len(value) == n && strings.Trim(value, "0123456789abcdef") == ""
}
func New(c Config) (*Client, error) {
	u, e := url.Parse(c.Server)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !Hex(c.DeviceID, 32) {
		return nil, errors.New("invalid server URL/device ID")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("server URL must use HTTP or HTTPS")
	}
	var roots *x509.CertPool
	if c.CAFile != "" {
		roots, e = x509.SystemCertPool()
		if e != nil {
			return nil, e
		}
		b, e := os.ReadFile(c.CAFile)
		if e != nil {
			return nil, e
		}
		if !roots.AppendCertsFromPEM(b) {
			return nil, errors.New("CA file contains no certificates")
		}
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	return &Client{c, &http.Client{Transport: tr, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Request(ctx context.Context, method, path string, body, out any, auth bool) error {
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		reader = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Config.Server, "/")+path, reader)
	if e != nil {
		return e
	}
	r.Header.Set("Content-Type", "application/json")
	if auth {
		r.Header.Set("Authorization", "Device "+c.Config.DeviceID+"."+c.Config.Secret)
	}
	resp, e := c.HTTP.Do(r)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{resp.StatusCode}
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 128*1024)).Decode(out)
	}
	return nil
}
func (c *Client) Health(ctx context.Context) error {
	var health struct {
		Status string `json:"status"`
	}
	if e := c.Request(ctx, "GET", "/healthz", nil, &health, false); e != nil {
		return e
	}
	if health.Status != "ok" {
		return errors.New("invalid health response")
	}
	return nil
}
func (c *Client) Enroll(ctx context.Context, token, path string) error {
	if !Hex(token, 64) {
		return errors.New("invalid enrollment token")
	}
	var response struct {
		DeviceID string `json:"device_id"`
		Secret   string `json:"secret"`
	}
	if e := c.Request(ctx, "POST", "/v1/enroll", map[string]string{"device_id": c.Config.DeviceID, "enrollment_token": token, "os": "windows"}, &response, false); e != nil {
		return e
	}
	if response.DeviceID != c.Config.DeviceID || !Hex(response.Secret, 64) {
		return errors.New("invalid enrollment response")
	}
	c.Config.Secret = response.Secret
	return Save(path, c.Config)
}
func LoadConfig(path string) (*Client, error) {
	var c Config
	if e := Read(path, &c); e != nil {
		return nil, e
	}
	if !Hex(c.Secret, 64) {
		return nil, errors.New("invalid device credential")
	}
	return New(c)
}
func Read(path string, out any) error {
	if e := VerifyPrivate(path); e != nil {
		return e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func Save(path string, value any) error {
	if e := VerifyPrivate(filepath.Dir(path)); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".linad-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = ProtectNew(f.Name()); e != nil {
		_ = f.Close()
		return e
	}
	if e = json.NewEncoder(f).Encode(value); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return replaceFile(f.Name(), path)
}
func (c *Client) Claim(ctx context.Context, nonce string) (*directory.JobRun, error) {
	var response struct {
		Run *directory.JobRun `json:"run"`
	}
	e := c.Request(ctx, "POST", "/v1/machine/jobs/claim", map[string]string{"request_id": nonce}, &response, true)
	return response.Run, e
}
