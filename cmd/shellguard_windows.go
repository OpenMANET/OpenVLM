//go:build windows

package cmd

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// parentIsPowerShell reports whether this process was launched by
// powershell.exe (Windows PowerShell 5.x) or pwsh.exe (PowerShell 7+).
//
// PowerShell's `>` redirect encodes stdout as UTF-16LE (with BOM), and its
// `|` pipe re-encodes through $OutputEncoding (ASCII by default) — both
// silently corrupt binary streams. Detecting this at runtime lets us
// refuse `openvlm read` to stdout before producing a broken backup file.
//
// Using parent-process name (not the PSModulePath env var) avoids false
// positives when the user wraps the call in `cmd /c "openvlm read > x.bin"`
// from a PowerShell prompt: cmd.exe inherits the env var but its own
// `>` is binary-safe, and the immediate parent is cmd.exe, not PowerShell.
//
// Returns false on any error — the guard is best-effort.
func parentIsPowerShell() bool {
	ppid := uint32(os.Getppid())
	if ppid == 0 {
		return false
	}

	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}

	defer windows.CloseHandle(snap) //nolint:errcheck // best-effort cleanup

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := windows.Process32First(snap, &entry); err != nil {
		return false
	}

	for {
		if entry.ProcessID == ppid {
			name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))

			return name == "powershell.exe" || name == "pwsh.exe"
		}

		if err := windows.Process32Next(snap, &entry); err != nil {
			return false
		}
	}
}

// stdoutIsPipe reports whether stdout is connected to a pipe or regular
// file (anything that is not a TTY/console). When stdout is a TTY the
// PowerShell guard does not need to fire: nothing is being captured into a
// file, so the only consequence of writing binary is garbled terminal
// output, not silently corrupted data.
func stdoutIsPipe() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}

	return (fi.Mode() & os.ModeCharDevice) == 0
}
