// Package gui provides a loopback-only browser interface for the directory client.
package gui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Pommesmajo08/lin_ad/internal/directory"
)

type App struct {
	mu      sync.RWMutex
	target  *url.URL
	secret  string
	host    string
	proxy   *httputil.ReverseProxy
	OnClose func()
}

func New(host, server string, allowHTTP bool) (*App, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, err
	}
	a := &App{secret: hex.EncodeToString(bytes), host: host}
	if server != "" {
		target, err := validateTarget(server, allowHTTP)
		if err != nil {
			return nil, err
		}
		a.target = target
	}
	a.proxy = &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			a.mu.RLock()
			target := *a.target
			a.mu.RUnlock()
			p.SetURL(&target)
			// The local capability must never be forwarded to the directory server.
			p.Out.Header.Del("X-Linad-Client")
			p.Out.Header.Del("Origin")
			p.Out.Header.Del("Referer")
		},
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 60 * time.Second},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeJSON(w, 502, map[string]string{"error": "Verbindung zum Verzeichnisserver fehlgeschlagen. Adresse und TLS-Zertifikat prüfen."})
		},
	}
	return a, nil
}
func validateTarget(raw string, allowHTTP bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" {
		return nil, errors.New("Bitte eine Serveradresse ohne Benutzerinfo, Pfad oder Query angeben")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && allowHTTP) {
		return nil, errors.New("HTTPS erforderlich; HTTP nur ausdrücklich für lokale Entwicklung erlauben")
	}
	u.Path = ""
	return u, nil
}
func (a *App) URL() string { return "http://" + a.host + "/#gui=" + a.secret }
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (a *App) Handler() http.Handler {
	web := directory.WebHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.Host != a.host {
			http.Error(w, "invalid host", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/client/") {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Linad-Client")), []byte(a.secret)) != 1 {
				writeJSON(w, 403, map[string]string{"error": "Lokaler Zugriff verweigert. GUI über die Client-Anwendung öffnen."})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 65536)
			if r.URL.Path == "/client/quit" && r.Method == "POST" {
				writeJSON(w, 200, map[string]bool{"ok": true})
				if a.OnClose != nil {
					go a.OnClose()
				}
				return
			}
			if r.URL.Path == "/client/info" && r.Method == "GET" {
				name, _ := os.Hostname()
				a.mu.RLock()
				server := ""
				if a.target != nil {
					server = a.target.String()
				}
				a.mu.RUnlock()
				writeJSON(w, 200, map[string]string{"name": strings.ToLower(name), "os": runtime.GOOS, "server": server})
				return
			}
			if r.URL.Path == "/client/connect" && r.Method == "POST" {
				var body struct {
					Server    string `json:"server"`
					AllowHTTP bool   `json:"allow_http"`
				}
				d := json.NewDecoder(r.Body)
				d.DisallowUnknownFields()
				if d.Decode(&body) != nil || d.Decode(new(any)) != io.EOF {
					writeJSON(w, 400, map[string]string{"error": "Ungültige Verbindungsdaten"})
					return
				}
				target, err := validateTarget(body.Server, body.AllowHTTP)
				if err != nil {
					writeJSON(w, 400, map[string]string{"error": err.Error()})
					return
				}
				a.mu.Lock()
				if a.target != nil && a.target.String() != target.String() {
					a.mu.Unlock()
					writeJSON(w, 409, map[string]string{"error": "Zum Wechseln des Servers den GUI-Client neu starten."})
					return
				}
				a.target = target
				a.mu.Unlock()
				writeJSON(w, 200, map[string]bool{"ok": true})
				return
			}
			if strings.HasPrefix(r.URL.Path, "/v1/") {
				a.mu.RLock()
				ready := a.target != nil
				a.mu.RUnlock()
				if !ready {
					writeJSON(w, 503, map[string]string{"error": "Bitte zuerst einen Server auswählen."})
					return
				}
				a.proxy.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		web.ServeHTTP(w, r)
	})
}
