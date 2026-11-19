//go:build !windows

package main

import "os/exec"

// detachDaemon is a no-op outside Windows; the daemon already outlives the
// launching shell there.
func detachDaemon(cmd *exec.Cmd) {}
