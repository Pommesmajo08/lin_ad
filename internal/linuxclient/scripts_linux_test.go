package linuxclient

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Pommesmajo08/lin_ad/internal/directory"
)

func TestCustomScriptValidationInterpreterAndPrivateFile(t *testing.T) {
	for _, interpreter := range []string{"sh", "bash", "python3"} {
		t.Run(interpreter, func(t *testing.T) {
			source := "printf 'hello\\n'\n"
			if interpreter == "python3" {
				source = "print('hello')\n"
			}
			interp, script, hash, err := directory.NormalizeJobScript(interpreter, source)
			if err != nil {
				t.Fatal(err)
			}
			run := directory.JobRun{Kind: "custom-script", Interpreter: interp, Script: script, ScriptSHA256: hash}
			path := ""
			code, output := executeRun(context.Background(), run, func(ctx context.Context, program string, args []string, w io.Writer) error {
				expected := "/bin/" + interpreter
				if interpreter == "python3" {
					expected = "/usr/bin/python3"
				}
				if program != expected {
					t.Fatal("wrong interpreter", program)
				}
				path = args[len(args)-1]
				data, err := os.ReadFile(path)
				if err != nil || string(data) != source {
					t.Fatal("incorrect script file", err)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("script permissions", err)
				}
				_, _ = io.WriteString(w, "hello\n")
				return nil
			})
			if code != 0 || !strings.Contains(output, "hello") {
				t.Fatal("script execution failed")
			}
			if _, err = os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("script file not removed")
			}
			run.Script += "tampered"
			called := false
			code, _ = executeRun(context.Background(), run, func(context.Context, string, []string, io.Writer) error { called = true; return nil })
			if code == 0 || called {
				t.Fatal("tampered script executed")
			}
		})
	}
}

func TestCustomScriptTimeout(t *testing.T) {
	interpreter, source, hash, err := directory.NormalizeJobScript("bash", "printf started\\n; sleep 20\n")
	if err != nil {
		t.Fatal(err)
	}
	run := directory.JobRun{ID: strings.Repeat("a", 32), Kind: "custom-script", Interpreter: interpreter, Script: source, ScriptSHA256: hash}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	code, output := executeRun(ctx, run, runScriptCommand(run))
	if code == 0 || time.Since(start) > 5*time.Second || !strings.Contains(output, "started") {
		t.Fatal("script timeout did not terminate started process", code, output)
	}
}
