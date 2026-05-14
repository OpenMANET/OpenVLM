//go:build !windows

package cmd

// parentIsPowerShell is a Windows-only concern. On every other OS the
// system shell handles binary stdout correctly, so the guard is a no-op.
func parentIsPowerShell() bool { return false }

// stdoutIsPipe is only consulted by the PowerShell guard, which is
// Windows-only. The non-Windows stub keeps the call site portable.
func stdoutIsPipe() bool { return false }
