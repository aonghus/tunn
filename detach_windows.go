//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
	createNoWindow        = 0x08000000 // CREATE_NO_WINDOW
)

// detachDaemon keeps the daemon alive after the launching console closes.
//
// CREATE_NO_WINDOW gives the daemon its own hidden console, so it survives the
// parent console closing and the ssh processes it starts reuse that console
// instead of each opening a window. CREATE_NEW_PROCESS_GROUP stops Ctrl+C in
// the launching terminal from reaching it.
func detachDaemon(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | createNoWindow,
	}
}
