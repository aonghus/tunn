//go:build !unix && !windows

package daemon

func isProcessRunning(pid int) bool {
	return false
}
