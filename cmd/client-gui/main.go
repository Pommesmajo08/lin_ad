package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/Pommesmajo08/lin_ad/internal/gui"
)

func main() {
	server := flag.String("server", "", "Verzeichnisserver (auch in der GUI wählbar)")
	allowHTTP := flag.Bool("allow-http", true, "Veraltet: HTTP ist standardmäßig erlaubt")
	noBrowser := flag.Bool("no-browser", false, "Browser nicht automatisch öffnen")
	flag.Parse()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	app, err := gui.New(listener.Addr().String(), *server, *allowHTTP)
	if err != nil {
		listener.Close()
		log.Fatal(err)
	}
	srv := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	app.OnClose = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	if !*noBrowser {
		go func() {
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", app.URL())
			} else {
				cmd = exec.Command("xdg-open", app.URL())
			}
			if err := cmd.Run(); err != nil {
				log.Printf("Browser konnte nicht geöffnet werden: %v", err)
			}
		}()
	}
	// The URL contains a local capability; do not publish it or write it to a shared log.
	log.Printf("GUI öffnen (privat): %s", app.URL())
	if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
