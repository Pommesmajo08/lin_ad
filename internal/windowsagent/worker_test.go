package windowsagent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHTTPServerAcceptedByDefault(t *testing.T) {
	for _, server := range []string{"http://127.0.0.1:8080", "http://192.168.1.10:8080", "http://directory.example:8080", "https://directory.example"} {
		if _, e := New(Config{Server: server, DeviceID: strings.Repeat("a", 32)}); e != nil {
			t.Fatalf("default connection rejected for %s: %v", server, e)
		}
	}
	if _, e := New(Config{Server: "ftp://directory.example", DeviceID: strings.Repeat("a", 32)}); e == nil {
		t.Fatal("unsupported URL scheme accepted")
	}
}

func TestWorkerProtocolDurabilityAndInterruptedRun(t *testing.T) {
	s, e := directory.New(filepath.Join(t.TempDir(), "server.json"), "initial-admin-password")
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	auth := ""
	request := func(method, path string, body any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(string(b)))
		r.Header.Set("Authorization", auth)
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			t.Fatal(method, path, resp.Status)
		}
		var result map[string]any
		if e = json.NewDecoder(resp.Body).Decode(&result); e != nil {
			t.Fatal(e)
		}
		return result
	}
	auth = "Bearer " + request("POST", "/v1/login", map[string]string{"name": "admin", "password": "initial-admin-password"})["token"].(string)
	d := request("POST", "/v1/devices", map[string]string{"name": "windows-test", "os": "windows"})
	id := d["id"].(string)
	enrollment := request("POST", "/v1/devices/"+id+"/enrollment", nil)
	dir := privateTestDir(t)
	config := filepath.Join(dir, "client.json")
	journal := filepath.Join(dir, "jobs.json")
	c, e := New(Config{Server: server.URL, DeviceID: id, AllowHTTP: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Enroll(context.Background(), enrollment["enrollment_token"].(string), config); e != nil {
		t.Fatal(e)
	}
	c, e = LoadConfig(config)
	if e != nil {
		t.Fatal(e)
	}
	job := request("POST", "/v1/jobs", map[string]any{"name": "PowerShell", "kind": "custom-script", "interpreter": "powershell", "script": "Write-Output 'hello'\n", "device_ids": []string{id}})
	calls := 0
	execute := func(ctx context.Context, r directory.JobRun) (int, string) {
		calls++
		if r.Interpreter != "powershell" || r.ScriptSHA256 == "" {
			t.Fatal("invalid snapshot")
		}
		return 7, "expected failure"
	}
	base := c.HTTP.Transport
	fail := true
	c.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/result") && fail {
			fail = false
			return nil, errors.New("simulated lost upload")
		}
		return base.RoundTrip(r)
	})
	if _, e = Once(context.Background(), c, journal, execute); e == nil {
		t.Fatal("lost upload not propagated")
	}
	var saved Ledger
	if e = Read(journal, &saved); e != nil || saved.Current == nil || saved.Current.Status != "failed" {
		t.Fatal("result not durable", e)
	}
	if _, e = Once(context.Background(), c, journal, execute); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("job executed twice")
	}
	if e = Read(journal, &saved); e != nil || saved.Current != nil || saved.RequestID != "" {
		t.Fatal("result not cleared", e)
	}
	request("POST", "/v1/jobs/"+job["id"].(string)+"/run", nil)
	nonce := strings.Repeat("f", 64)
	run, e := c.Claim(context.Background(), nonce)
	if e != nil || run == nil {
		t.Fatal(e)
	}
	if e = Save(journal, Ledger{RequestID: nonce, Current: &Record{Run: *run, RequestID: nonce, Status: "running"}}); e != nil {
		t.Fatal(e)
	}
	if _, e = Once(context.Background(), c, journal, execute); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("interrupted job repeated")
	}
	detail := request("GET", "/v1/job-runs/"+run.ID, nil)
	if detail["status"] != "unknown" {
		t.Fatal("interrupted result not unknown")
	}
}
