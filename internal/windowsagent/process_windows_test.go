package windowsagent

import (
	"context"
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func privateTestDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	// TempDir can have explicit runner ACEs. Removing inheritance alone does
	// not remove those; replace the descriptor just as the installer does.
	sddl, e := syscall.UTF16PtrFromString("O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if e != nil {
		t.Fatal(e)
	}
	var sd uintptr
	ok, _, e := advapi.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW").Call(
		uintptr(unsafe.Pointer(sddl)), 1, uintptr(unsafe.Pointer(&sd)), 0)
	if ok == 0 {
		t.Fatalf("parse test ACL: %v", e)
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	path, e := syscall.UTF16PtrFromString(d)
	if e != nil {
		t.Fatal(e)
	}
	// OWNER_SECURITY_INFORMATION | DACL_SECURITY_INFORMATION |
	// PROTECTED_DACL_SECURITY_INFORMATION: replace explicit and inherited ACEs.
	ok, _, e = advapi.NewProc("SetFileSecurityW").Call(uintptr(unsafe.Pointer(path)), 0x80000005, sd)
	if ok == 0 {
		t.Fatalf("apply test ACL: %v", e)
	}
	if e := VerifyPrivate(d); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestPrivateDirectoryRejectsAdditionalPrincipal(t *testing.T) {
	d := privateTestDir(t)
	if out, e := exec.Command("icacls.exe", d, "/grant", "*S-1-1-0:(OI)(CI)R").CombinedOutput(); e != nil {
		t.Fatalf("add untrusted ACE: %v %s", e, out)
	}
	if e := VerifyPrivate(d); e == nil || !strings.Contains(e.Error(), "non-administrator access") {
		t.Fatalf("additional principal was not rejected: %v", e)
	}
}

// Isolate host/.NET startup from suspended start and Job Object assignment.
// If this fails, the process runner cannot be the cause of the startup hang.
func TestNativePowerShellStartupWithoutJobObject(t *testing.T) {
	d := privateTestDir(t)
	t.Setenv("LINAD_TEST_SECRET", "must-not-be-inherited")
	t.Setenv("PSModulePath", `C:\not-a-windows-powershell-module-path`)
	path := filepath.Join(d, "startup.ps1")
	source := `$ErrorActionPreference='Stop'
if($env:LINAD_TEST_SECRET){throw 'agent environment leaked'}
Import-Module Microsoft.PowerShell.Security -ErrorAction Stop
[Console]::WriteLine('linad-startup-ok')
exit 7
`
	if e := os.WriteFile(path, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	program := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, program, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "RemoteSigned", "-File", path)
	cmd.Dir = d
	cmd.Env = powerShellEnvironment(d, directory.JobRun{})
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	out, e := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("PowerShell startup without Job Object timed out: %v; output: %s", ctx.Err(), out)
	}
	exit, ok := e.(*exec.ExitError)
	if !ok || exit.ExitCode() != 7 || !strings.Contains(string(out), "linad-startup-ok") {
		t.Fatalf("PowerShell startup/module loading failed: %v; output: %s", e, out)
	}
}

func TestNativePowerShellAndIntegrity(t *testing.T) {
	d := privateTestDir(t)
	_, source, hash, e := directory.NormalizeJobScript("powershell", "Write-Output 'Grüße'; exit 7\n")
	if e != nil {
		t.Fatal(e)
	}
	run := directory.JobRun{Kind: "custom-script", Interpreter: "powershell", Script: source, ScriptSHA256: hash}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code, out := Execute(ctx, run, d)
	if code != 7 || !strings.Contains(out, "Grüße") {
		t.Fatal(code, out)
	}
	run.Script += "Write-Output tampered\n"
	code, out = Execute(ctx, run, d)
	if code == 0 || !strings.Contains(out, "integrity") {
		t.Fatal("tampered script accepted", code, out)
	}
	entries, e := os.ReadDir(d)
	if e != nil || len(entries) != 0 {
		t.Fatal("script temporary files retained", e)
	}
}
func TestNativeExecutableInvocationAndExitCode(t *testing.T) {
	d := privateTestDir(t)
	// The agent must supply its own native executable extensions, even when
	// launched from an environment with an invalid PATHEXT.
	t.Setenv("PATHEXT", ".INVALID")
	_, source, hash, e := directory.NormalizeJobScript("powershell", `
$global:LASTEXITCODE=$null
& $env:ComSpec /d /c 'echo linad-native-executable-ok & exit /b 9'
$nativeExit=$LASTEXITCODE
if($null -eq $nativeExit){throw 'native executable was not invoked synchronously'}
Write-Output "native-exit=$nativeExit"
exit $nativeExit
`)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code, out := Execute(ctx, directory.JobRun{Kind: "custom-script", Interpreter: "powershell", Script: source, ScriptSHA256: hash}, d)
	if code != 9 || !strings.Contains(out, "linad-native-executable-ok") || !strings.Contains(out, "native-exit=9") {
		t.Fatalf("native invocation/output/exit code failed: %d %s", code, out)
	}
}

func TestNativeTimeoutKillsProcessTree(t *testing.T) {
	d := privateTestDir(t)
	_, source, hash, _ := directory.NormalizeJobScript("powershell", `$info=New-Object Diagnostics.ProcessStartInfo
$info.FileName="$PSHOME\powershell.exe"
$info.Arguments='-NoProfile -NonInteractive -Command "Start-Sleep 120"'
$info.UseShellExecute=$false
$child=[Diagnostics.Process]::Start($info)
Write-Output "child=$($child.Id)"
Start-Sleep 120
`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	code, out := Execute(ctx, directory.JobRun{Kind: "custom-script", Interpreter: "powershell", Script: source, ScriptSHA256: hash}, d)
	if code == 0 || time.Since(start) > 110*time.Second {
		t.Fatal("timeout failed", code, out)
	}
	match := regexp.MustCompile(`child=(\d+)`).FindStringSubmatch(out)
	if len(match) != 2 {
		t.Fatal("child did not start", out)
	}
	pid, _ := strconv.Atoi(match[1])
	h, e := syscall.OpenProcess(0x1000, false, uint32(pid))
	if e == nil {
		defer syscall.CloseHandle(h)
		var exit uint32
		if e = syscall.GetExitCodeProcess(h, &exit); e != nil || exit == 259 {
			t.Fatal("child process still alive", exit, e)
		}
	}
	if e := VerifyPrivate(filepath.Join(d, "not-present")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
