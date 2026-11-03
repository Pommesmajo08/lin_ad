package main

import (
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	path := os.Getenv("LINAD_DATA")
	if path == "" {
		path = "data/directory.json"
	}
	password := os.Getenv("LINAD_ADMIN_PASSWORD")
	if f := os.Getenv("LINAD_ADMIN_PASSWORD_FILE"); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		password = string(b)
	}
	s, err := directory.New(path, password)
	if err != nil {
		log.Fatal(err)
	}
	addr := os.Getenv("LINAD_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	h := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	cert, key := os.Getenv("LINAD_TLS_CERT"), os.Getenv("LINAD_TLS_KEY")
	log.Printf("listening on %s", addr)
	if cert != "" || key != "" {
		log.Fatal(h.ListenAndServeTLS(cert, key))
	}
	log.Fatal(h.ListenAndServe())
}
