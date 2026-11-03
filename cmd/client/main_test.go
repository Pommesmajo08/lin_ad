package main

import (
	"flag"
	"os"
	"strings"
	"testing"
)

func TestRejectPlainHTTP(t *testing.T) {
	oldArgs, oldFlags := os.Args, flag.CommandLine
	defer func() { os.Args = oldArgs; flag.CommandLine = oldFlags }()
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"client", "--server", "http://example.com", "me"}
	err := run()
	if err == nil || !strings.Contains(err.Error(), "HTTPS required") {
		t.Fatalf("HTTP accepted: %v", err)
	}
}
