package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	server := flag.String("server", "https://localhost:8443", "server URL")
	local := flag.Bool("allow-http", false, "allow unencrypted HTTP for local development")
	flag.Parse()
	u, err := url.Parse(*server)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid server URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && *local) {
		return fmt.Errorf("HTTPS required; use --allow-http only for local development")
	}
	a := flag.Args()
	if len(a) == 0 {
		return fmt.Errorf("commands: jobs, job-runs, job-create NAME KIND DEVICE..., job-schedule NAME KIND START_RFC3339 INTERVAL_MINUTES DEVICE..., job-run ID, job-pause ID, job-resume ID, job-delete ID, job-cancel RUN_ID, job-resolve RUN_ID, user-admin NAME, user-standard NAME, my-groups, login NAME, me, logout, password-change, users, user-add NAME, user-delete NAME, user-disable NAME, user-enable NAME, user-password NAME, groups, group-add NAME, group-delete NAME, member-add GROUP USER, member-delete GROUP USER, devices, device-register [NAME], device-heartbeat ID, device-delete ID, device-access ID [USER...], device-access-groups ID [GROUP...], device-access-all ID, device-enrollment ID, device-revoke ID")
	}
	method, path := "GET", ""
	var body any
	switch {
	case a[0] == "login" && len(a) == 2:
		method, path = "POST", "/v1/login"
		body = map[string]string{"name": a[1], "password": os.Getenv("LINAD_PASSWORD")}
	case a[0] == "password-change" && len(a) == 1:
		method, path = "POST", "/v1/password"
		body = map[string]string{"current_password": os.Getenv("LINAD_PASSWORD"), "new_password": os.Getenv("LINAD_NEW_PASSWORD")}
	case a[0] == "user-password" && len(a) == 2:
		method, path = "POST", "/v1/users/"+url.PathEscape(a[1])+"/password"
		body = map[string]string{"password": os.Getenv("LINAD_NEW_PASSWORD")}
	case (a[0] == "user-disable" || a[0] == "user-enable") && len(a) == 2:
		method, path = "PATCH", "/v1/users/"+url.PathEscape(a[1])
		body = map[string]bool{"disabled": a[0] == "user-disable"}
	case a[0] == "device-enrollment" && len(a) == 2:
		method, path = "POST", "/v1/devices/"+url.PathEscape(a[1])+"/enrollment"
	case a[0] == "device-revoke" && len(a) == 2:
		method, path = "DELETE", "/v1/devices/"+url.PathEscape(a[1])+"/credential"
	case (a[0] == "device-access" || a[0] == "device-access-groups" || a[0] == "device-access-all") && len(a) >= 2:
		if a[0] == "device-access-all" && len(a) != 2 {
			return fmt.Errorf("device-access-all takes only a device ID")
		}
		method, path = "PUT", "/v1/devices/"+url.PathEscape(a[1])+"/access"
		users, groups := []string{}, []string{}
		if a[0] == "device-access" {
			users = a[2:]
		}
		if a[0] == "device-access-groups" {
			groups = a[2:]
		}
		body = map[string]any{"allow_all": a[0] == "device-access-all", "allowed_users": users, "allowed_groups": groups}
	case a[0] == "devices" && len(a) == 1:
		path = "/v1/devices"
	case a[0] == "device-register" && (len(a) == 1 || len(a) == 2):
		name, err := os.Hostname()
		if err != nil {
			return err
		}
		if len(a) == 2 {
			name = a[1]
		}
		method, path = "POST", "/v1/devices"
		body = map[string]string{"name": strings.ToLower(name), "os": runtime.GOOS}
	case a[0] == "device-heartbeat" && len(a) == 2:
		method, path = "POST", "/v1/devices/"+url.PathEscape(a[1])+"/heartbeat"
	case a[0] == "device-delete" && len(a) == 2:
		method, path = "DELETE", "/v1/devices/"+url.PathEscape(a[1])
	case a[0] == "jobs" && len(a) == 1:
		path = "/v1/jobs"
	case a[0] == "job-runs" && len(a) == 1:
		path = "/v1/job-runs"
	case a[0] == "job-create" && len(a) >= 4:
		method, path = "POST", "/v1/jobs"
		body = map[string]any{"name": a[1], "kind": a[2], "device_ids": a[3:], "interval_minutes": 0, "enabled": true}
	case a[0] == "job-schedule" && len(a) >= 6:
		start, err := time.Parse(time.RFC3339, a[3])
		if err != nil {
			return err
		}
		interval, err := strconv.Atoi(a[4])
		if err != nil {
			return err
		}
		method, path = "POST", "/v1/jobs"
		body = map[string]any{"name": a[1], "kind": a[2], "start_at": start, "interval_minutes": interval, "device_ids": a[5:], "enabled": true}
	case a[0] == "job-run" && len(a) == 2:
		method, path = "POST", "/v1/jobs/"+url.PathEscape(a[1])+"/run"
	case (a[0] == "job-pause" || a[0] == "job-resume") && len(a) == 2:
		method, path = "PATCH", "/v1/jobs/"+url.PathEscape(a[1])
		body = map[string]bool{"enabled": a[0] == "job-resume"}
	case a[0] == "job-delete" && len(a) == 2:
		method, path = "DELETE", "/v1/jobs/"+url.PathEscape(a[1])
	case (a[0] == "job-cancel" || a[0] == "job-resolve") && len(a) == 2:
		action := "cancel"
		if a[0] == "job-resolve" {
			action = "resolve"
		}
		method, path = "POST", "/v1/job-runs/"+url.PathEscape(a[1])+"/"+action
	case a[0] == "my-groups" && len(a) == 1:
		path = "/v1/me/groups"
	case (a[0] == "user-admin" || a[0] == "user-standard") && len(a) == 2:
		method, path = "PATCH", "/v1/users/"+url.PathEscape(a[1])
		body = map[string]bool{"admin": a[0] == "user-admin"}
	case a[0] == "me" && len(a) == 1:
		path = "/v1/me"
	case a[0] == "logout" && len(a) == 1:
		method, path = "POST", "/v1/logout"
	case a[0] == "users" && len(a) == 1:
		path = "/v1/users"
	case a[0] == "groups" && len(a) == 1:
		path = "/v1/groups"
	case a[0] == "user-add" && len(a) == 2:
		method, path = "POST", "/v1/users"
		body = map[string]string{"name": a[1], "password": os.Getenv("LINAD_PASSWORD")}
	case a[0] == "group-add" && len(a) == 2:
		method, path = "POST", "/v1/groups"
		body = map[string]string{"name": a[1]}
	case a[0] == "user-delete" && len(a) == 2:
		method, path = "DELETE", "/v1/users/"+url.PathEscape(a[1])
	case a[0] == "group-delete" && len(a) == 2:
		method, path = "DELETE", "/v1/groups/"+url.PathEscape(a[1])
	case (a[0] == "member-add" || a[0] == "member-delete") && len(a) == 3:
		method = "PUT"
		if a[0] == "member-delete" {
			method = "DELETE"
		}
		path = "/v1/groups/" + url.PathEscape(a[1]) + "/members/" + url.PathEscape(a[2])
	default:
		return fmt.Errorf("invalid command or arguments")
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, strings.TrimRight(*server, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("LINAD_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	c := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 8<<20 {
		return fmt.Errorf("server response exceeds 8 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, b)
	}
	fmt.Print(string(b))
	return nil
}
