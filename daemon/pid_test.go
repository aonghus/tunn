package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// exitedPID returns the PID of a child process that has already exited.
func exitedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to run child process: %v", err)
	}
	return cmd.Process.Pid
}

func TestIsProcessRunning(t *testing.T) {
	if !isProcessRunning(os.Getpid()) {
		t.Fatal("expected the current process to be running")
	}
	if pid := exitedPID(t); isProcessRunning(pid) {
		t.Fatalf("expected exited process %d not to be running", pid)
	}
	if isProcessRunning(0) || isProcessRunning(-1) {
		t.Fatal("expected non-positive PIDs not to be running")
	}
}

func TestPIDLifecycle(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{
		RuntimeDir: dir,
		PIDFile:    filepath.Join(dir, "daemon.pid"),
		SocketFile: filepath.Join(dir, "daemon.sock"),
	}

	if err := WritePID(paths, os.Getpid()); err != nil {
		t.Fatalf("WritePID failed: %v", err)
	}

	pid, err := ReadPID(paths)
	if err != nil {
		t.Fatalf("ReadPID failed: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("expected pid %d, got %d", os.Getpid(), pid)
	}

	pid, running, err := CheckRunning(paths)
	if err != nil {
		t.Fatalf("CheckRunning failed: %v", err)
	}
	if !running {
		t.Fatalf("expected current process to be considered running")
	}
	if pid != os.Getpid() {
		t.Fatalf("expected pid %d, got %d", os.Getpid(), pid)
	}

	// Write the PID of a process that has exited.
	if err := WritePID(paths, exitedPID(t)); err != nil {
		t.Fatalf("WritePID failed: %v", err)
	}
	if err := os.WriteFile(paths.SocketFile, []byte("stub"), 0o600); err != nil {
		t.Fatalf("failed to write socket stub: %v", err)
	}

	pid, running, err = CheckRunning(paths)
	if err != nil {
		t.Fatalf("CheckRunning failed: %v", err)
	}
	if running {
		t.Fatalf("expected stale pid to be treated as not running")
	}
	if pid != 0 {
		t.Fatalf("expected pid to be cleared, got %d", pid)
	}

	if _, err := os.Stat(paths.PIDFile); !os.IsNotExist(err) {
		t.Fatalf("expected pid file to be removed, got err %v", err)
	}
	if _, err := os.Stat(paths.SocketFile); !os.IsNotExist(err) {
		t.Fatalf("expected socket file to be removed, got err %v", err)
	}
}
