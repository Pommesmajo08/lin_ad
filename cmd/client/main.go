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
		return fmt.Errorf("commands: login NAME, me, logout, password-change, users, user-add NAME, user-delete NAME, user-disable NAME, user-enable NAME, user-password NAME, groups, group-add NAME, group-delete NAME, member-add GROUP USER, member-delete GROUP USER, devices, device-register [NAME], device-heartbeat ID, device-delete ID, device-access ID [USER...], device-access-groups ID [GROUP...], device-access-all ID, device-enrollment ID, device-revoke ID")
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
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, b)
	}
	fmt.Print(string(b))
	return nil
}
