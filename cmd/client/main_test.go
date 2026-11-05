package main

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAcceptPlainHTTPByDefault(t *testing.T) {
	oldArgs, oldFlags := os.Args, flag.CommandLine
	defer func() { os.Args = oldArgs; flag.CommandLine = oldFlags }()
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	os.Args = []string{"client", "--server", server.URL, "me"}
	err := run()
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("HTTP request did not reach server: %v", err)
	}
}
