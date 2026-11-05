package windowsagent

import (
	"context"
	"errors"
	"fmt"
	"github.com/Pommesmajo08/lin_ad/internal/directory"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const inventoryScript = `$ErrorActionPreference='Stop'
Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber,OSArchitecture,LastBootUpTime | Format-List
Get-CimInstance Win32_ComputerSystem | Select-Object Name,Manufacturer,Model,TotalPhysicalMemory | Format-List
`
const updateScript = `$ErrorActionPreference='Stop'
$session=New-Object -ComObject Microsoft.Update.Session
$session.ClientApplicationID='Linad Windows Agent'
$found=$session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
$updates=New-Object -ComObject Microsoft.Update.UpdateColl
foreach($u in $found.Updates){ if(-not $u.EulaAccepted){$u.AcceptEula()}; [void]$updates.Add($u) }
if($updates.Count -eq 0){Write-Output 'No applicable updates'; exit 0}
Write-Output "Applicable updates: $($updates.Count)"
$downloader=$session.CreateUpdateDownloader(); $downloader.Updates=$updates
$download=$downloader.Download()
if($download.ResultCode -ne 2){throw "Download result: $($download.ResultCode)"}
$installer=$session.CreateUpdateInstaller();$installer.Updates=$updates;$installer.AllowSourcePrompts=$false
$result=$installer.Install()
Write-Output "Result: $($result.ResultCode); reboot required: $($result.RebootRequired)"
for($i=0;$i -lt $updates.Count;$i++){Write-Output "$($updates.Item($i).Title): $($result.GetUpdateResult($i).HResult)"}
if($result.ResultCode -ne 2){exit 1}
`

type basicLimits struct {
	ProcessTime, JobTime   int64
	Flags                  uint32
	MinWorking, MaxWorking uintptr
	Active                 uint32
	Affinity               uintptr
	Priority, Scheduling   uint32
}
type extendedLimits struct {
	Basic                                                      basicLimits
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}

// Keep the Windows/.NET runtime environment, but do not pass agent credentials,
// CI secrets or a PowerShell 7 PSModulePath to Windows PowerShell 5.1.
func powerShellEnvironment(dir string, run directory.JobRun) []string {
	env := make([]string, 0, 32)
	for _, name := range []string{
		"SystemRoot", "WINDIR", "SystemDrive", "ProgramData", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432",
		"ALLUSERSPROFILE", "PUBLIC", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "HOMEDRIVE", "HOMEPATH",
		"COMPUTERNAME", "USERNAME", "USERDOMAIN", "OS", "PROCESSOR_ARCHITECTURE", "NUMBER_OF_PROCESSORS",
	} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	root := os.Getenv("SystemRoot")
	home := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0")
	modules := filepath.Join(home, "Modules")
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		modules += ";" + filepath.Join(programFiles, "WindowsPowerShell", "Modules")
	}
	return append(env,
		"PATH="+filepath.Join(root, "System32")+";"+root+";"+home,
		"ComSpec="+filepath.Join(root, "System32", "cmd.exe"),
		"PSModulePath="+modules,
		"TEMP="+dir, "TMP="+dir,
		"LINAD_DEVICE_ID="+run.DeviceID, "LINAD_JOB_ID="+run.JobID, "LINAD_RUN_ID="+run.ID)
}

// Resume the initial CREATE_SUSPENDED thread with the documented Win32 API,
// checking its suspend count instead of relying on process-wide NT resumption.
func resumeInitialThread(pid uint32) error {
	snapshot, _, e := kernel.NewProc("CreateToolhelp32Snapshot").Call(0x4, 0) // TH32CS_SNAPTHREAD
	if snapshot == ^uintptr(0) {
		return fmt.Errorf("snapshot suspended thread: %w", e)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	type threadEntry struct {
		Size, Usage, ID, Owner      uint32
		BasePriority, DeltaPriority int32
		Flags                       uint32
	}
	entry := threadEntry{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	ok, _, e := kernel.NewProc("Thread32First").Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	var threadID uint32
	for ok != 0 {
		if entry.Owner == pid {
			if threadID != 0 {
				return errors.New("suspended process has multiple threads before startup")
			}
			threadID = entry.ID
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		ok, _, e = kernel.NewProc("Thread32Next").Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	if e != syscall.Errno(18) { // ERROR_NO_MORE_FILES
		return fmt.Errorf("enumerate suspended thread: %w", e)
	}
	if threadID == 0 {
		return errors.New("initial suspended thread not found")
	}
	thread, _, e := kernel.NewProc("OpenThread").Call(0x2, 0, uintptr(threadID)) // THREAD_SUSPEND_RESUME
	if thread == 0 {
		return fmt.Errorf("open suspended thread: %w", e)
	}
	defer syscall.CloseHandle(syscall.Handle(thread))
	previous, _, e := kernel.NewProc("ResumeThread").Call(thread)
	if previous == 0xffffffff {
		return fmt.Errorf("resume initial thread: %w", e)
	}
	if previous != 1 {
		return fmt.Errorf("unexpected initial thread suspend count: %d", previous)
	}
	return nil
}

// Suspended start prevents descendants escaping before assignment to the kill-on-close job.
func runPowerShell(ctx context.Context, path, dir string, run directory.JobRun, out *LimitedLog) error {
	h, _, e := kernel.NewProc("CreateJobObjectW").Call(0, 0)
	if h == 0 {
		return e
	}
	var once sync.Once
	closeJob := func() { once.Do(func() { syscall.CloseHandle(syscall.Handle(h)) }) }
	defer closeJob()
	limits := extendedLimits{Basic: basicLimits{Flags: 0x2000}}
	ok, _, e := kernel.NewProc("SetInformationJobObject").Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		return e
	}
	program := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, program, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "RemoteSigned", "-File", path)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = powerShellEnvironment(dir, run)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000004}
	cmd.Cancel = func() error { closeJob(); return nil }
	cmd.WaitDelay = 5 * time.Second
	if e = cmd.Start(); e != nil {
		return e
	}
	process, e := syscall.OpenProcess(0x0100|0x0200|0x0001|0x0800, false, uint32(cmd.Process.Pid))
	if e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return e
	}
	defer syscall.CloseHandle(process)
	ok, _, e = kernel.NewProc("AssignProcessToJobObject").Call(h, uintptr(process))
	if ok == 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return e
	}
	if e = resumeInitialThread(uint32(cmd.Process.Pid)); e != nil {
		closeJob()
		_ = cmd.Wait()
		return e
	}

	fmt.Fprintf(out, "PowerShell process %d: initial thread resumed after job assignment\n", cmd.Process.Pid)
	e = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return e
}
func Execute(ctx context.Context, run directory.JobRun, stateDir string) (int, string) {
	if !directory.JobSupportsOS(run.Kind, run.Interpreter, "windows") {
		return 1, "Unsupported Windows job; no code executed."
	}
	source := inventoryScript
	if run.Kind == "windows-update" {
		source = updateScript
	}
	if run.Kind == "custom-script" {
		interp, canonical, hash, e := directory.NormalizeJobScript(run.Interpreter, run.Script)
		if e != nil || interp != "powershell" || hash != run.ScriptSHA256 {
			return 1, "Script integrity check failed; no code executed."
		}
		source = canonical
	}
	if e := VerifyPrivate(stateDir); e != nil {
		return 1, e.Error()
	}
	dir, e := os.MkdirTemp(stateDir, "script-*")
	if e != nil {
		return 1, e.Error()
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "job.ps1")
	// Windows PowerShell 5 requires a BOM to decode non-ASCII UTF-8 source correctly.
	if e = os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, []byte("[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)\n$OutputEncoding = [Console]::OutputEncoding\n$ErrorActionPreference = 'Stop'\n"+source)...), 0600); e != nil {
		return 1, e.Error()
	}
	out := &LimitedLog{}
	fmt.Fprintf(out, "Windows job: %s; SHA-256: %s\n", run.Kind, run.ScriptSHA256)
	e = runPowerShell(ctx, path, dir, run, out)
	if e == nil {
		return 0, out.String()
	}
	fmt.Fprintln(out, "Execution failed:", e)
	var exit *exec.ExitError
	if errors.As(e, &exit) {
		code := exit.ExitCode()
		if code < -1 || code > 255 {
			code = 1
		}
		return code, out.String()
	}
	return -1, out.String()
}
