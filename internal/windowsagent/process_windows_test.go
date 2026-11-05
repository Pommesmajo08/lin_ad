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
)

func privateTestDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	// TempDir can have explicit runner ACEs. Removing inheritance alone does
	// not remove those; replace the descriptor just as the installer does.
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `
$ErrorActionPreference='Stop'
$acl=New-Object Security.AccessControl.DirectorySecurity
$acl.SetSecurityDescriptorSddlForm('O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)')
Set-Acl -LiteralPath $env:LINAD_TEST_PRIVATE_PATH -AclObject $acl
`)
	cmd.Env = append(os.Environ(), "LINAD_TEST_PRIVATE_PATH="+d)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("ACL setup: %v %s", e, out)
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
