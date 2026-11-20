//go:build windows

package daemon

import (
	"errors"
	"syscall"
)

const (
	// PROCESS_QUERY_LIMITED_INFORMATION is enough for GetExitCodeProcess and,
	// unlike PROCESS_QUERY_INFORMATION, is granted for most processes.
	processQueryLimitedInformation = 0x1000
	// STILL_ACTIVE is the exit code GetExitCodeProcess reports for a live process.
	stillActive = 259
)

func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// Access denied means the process exists but belongs to someone else,
		// matching EPERM from kill(pid, 0) on Unix.
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer syscall.CloseHandle(h)

	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}
