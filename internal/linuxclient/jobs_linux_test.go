package linuxclient

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestJobCommandsAndFailurePropagation(t *testing.T) {
	calls := [][]string{}
	command := func(ctx context.Context, path string, args []string, w io.Writer) error {
		if path != "/usr/bin/apt-get" {
			t.Fatal("unexpected executable")
		}
		calls = append(calls, append([]string{}, args...))
		return nil
	}
	code, _ := executeJob(context.Background(), "apt-upgrade", command)
	if code != 0 || len(calls) != 2 || calls[0][len(calls[0])-1] != "update" || calls[1][len(calls[1])-1] != "upgrade" {
		t.Fatal("incorrect upgrade sequence", calls)
	}
	if !strings.Contains(strings.Join(calls[1], " "), "--force-confold") || strings.Contains(strings.Join(calls[1], " "), "unauthenticated") {
		t.Fatal("package trust/configuration was weakened")
	}
	calls = nil
	code, output := executeJob(context.Background(), "apt-upgrade", func(context.Context, string, []string, io.Writer) error {
		calls = append(calls, nil)
		return errors.New("index failed")
	})
	if code == 0 || len(calls) != 1 || !strings.Contains(output, "failed") {
		t.Fatal("upgrade continued after failed index refresh")
	}
	code, _ = executeJob(context.Background(), "arbitrary-shell", command)
	if code == 0 {
		t.Fatal("unsupported command admitted")
	}
	code, output = executeJob(context.Background(), "system-info", func(context.Context, string, []string, io.Writer) error {
		t.Fatal("system-info unexpectedly executed a command")
		return nil
	})
	if code != 0 || !strings.Contains(output, "Architecture:") {
		t.Fatal("missing system information")
	}
}
func TestBoundedJobLogs(t *testing.T) {
	w := &limitedLog{}
	text := strings.Repeat("x", 20000)
	n, err := w.Write([]byte(text))
	if n != len(text) || err != nil || len(w.String()) > 10100 || !strings.Contains(w.String(), "truncated") {
		t.Fatal("log limit failed")
	}
}
