package linuxclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Pommesmajo08/lin_ad/internal/directory"
)

func executeRun(ctx context.Context, run directory.JobRun, command jobCommand) (int, string) {
	if run.Kind != "custom-script" {
		return executeJob(ctx, run.Kind, command)
	}
	interpreter, source, hash, err := directory.NormalizeJobScript(run.Interpreter, run.Script)
	if err != nil || interpreter == "powershell" || hash != run.ScriptSHA256 {
		return 1, "Script validation/integrity check failed; no code executed."
	}
	workdir, err := os.MkdirTemp("", "linad-script-*")
	if err != nil {
		return 1, "Could not create private script directory: " + err.Error()
	}
	defer os.RemoveAll(workdir)
	path := filepath.Join(workdir, "job-script")
	if err = os.WriteFile(path, []byte(source), 0600); err != nil {
		return 1, "Could not write script: " + err.Error()
	}
	program, args := "/bin/sh", []string{path}
	if interpreter == "bash" {
		program = "/bin/bash"
		args = []string{"--noprofile", "--norc", path}
	}
	if interpreter == "python3" {
		program = "/usr/bin/python3"
		args = []string{"-I", "-B", path}
	}
	output := &limitedLog{}
	fmt.Fprintf(output, "Custom script: %s\nSHA-256: %s\n", interpreter, hash)
	if err = command(ctx, program, args, output); err != nil {
		fmt.Fprintln(output, "Script failed:", err)
		return exitCode(err), output.String()
	}
	return 0, output.String()
}
func runScriptCommand(run directory.JobRun) jobCommand {
	return func(ctx context.Context, program string, args []string, w io.Writer) error {
		return runJobProcess(ctx, program, args, w, filepath.Dir(args[len(args)-1]), []string{"LINAD_JOB_ID=" + run.JobID, "LINAD_RUN_ID=" + run.ID, "LINAD_DEVICE_ID=" + run.DeviceID}, true)
	}
}
