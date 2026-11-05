package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyAuthorizationAndServerSelection(t *testing.T) {
	var forwarded bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = true
		if r.Header.Get("X-Linad-Client") != "" {
			t.Error("local secret leaked upstream")
		}
		if r.Header.Get("Authorization") != "Bearer test-user-token" {
			t.Error("user token not forwarded")
		}
		if r.URL.Path != "/v1/me" || r.URL.RawQuery != "x=1" {
			t.Error("incorrect upstream path", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"alice"}`))
	}))
	defer upstream.Close()
	a, err := New("127.0.0.1:12345", "", false)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, secret, host, body string, want int) string {
		t.Helper()
		r := httptest.NewRequest(method, "http://"+a.host+path, strings.NewReader(body))
		r.Host = host
		r.Header.Set("X-Linad-Client", secret)
		r.Header.Set("Authorization", "Bearer test-user-token")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s; want %d", method, path, w.Code, w.Body.String(), want)
		}
		return w.Body.String()
	}
	request("GET", "/", "", a.host, "", 200)
	request("GET", "/client/info", "", a.host, "", 403)
	request("POST", "/client/connect", "", a.host, `{"server":"`+upstream.URL+`"}`, 403)
	request("GET", "/v1/me", a.secret, "attacker.example", "", 403)
	request("GET", "/v1/me", a.secret, a.host, "", 503)
	request("POST", "/client/connect", a.secret, a.host, `{"server":"ftp://example.com"}`, 400)
	request("POST", "/client/connect", a.secret, a.host, `{"server":"`+upstream.URL+`"}`, 200)
	if b := request("GET", "/client/info", a.secret, a.host, "", 200); !strings.Contains(b, upstream.URL) {
		t.Fatal("selected server missing")
	}
	if b := request("GET", "/v1/me?x=1", a.secret, a.host, "", 200); !strings.Contains(b, "alice") || !forwarded {
		t.Fatal("proxy failed")
	}
	request("POST", "/client/connect", a.secret, a.host, `{"server":"https://other.example"}`, 409)
	closed := make(chan struct{})
	a.OnClose = func() { close(closed) }
	request("POST", "/client/quit", a.secret, a.host, "", 200)
	<-closed
}
func TestTargetValidation(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "https://user:password@example.com", "https://example.com/path", "https://example.com?q=secret", "https://example.com/#fragment", "ftp://example.com", "not-a-url"} {
		if _, err := validateTarget(raw, false); err == nil {
			t.Error("invalid URL accepted", raw)
		}
	}
	if _, err := validateTarget("http://example.com:8080", false); err != nil {
		t.Fatal(err)
	}
	if _, err := validateTarget("https://example.com:8443", false); err != nil {
		t.Fatal(err)
	}
}
func TestUntrustedTLSIsRejected(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted endpoint reached") }))
	defer upstream.Close()
	a, err := New("127.0.0.1:12345", upstream.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://"+a.host+"/v1/me", nil)
	r.Header.Set("X-Linad-Client", a.secret)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 502 {
		t.Fatal("untrusted TLS was accepted", w.Code)
	}
}
