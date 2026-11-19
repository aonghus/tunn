package executor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aonghus/tunn/config"
)

func TestMockSSHExecutor(t *testing.T) {
	mock := &MockSSHExecutor{}

	tunnel := config.Tunnel{
		Host:  "testserver",
		Ports: []string{"8080:8080", "9090:9091"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := mock.Execute(ctx, "test", tunnel)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context deadline exceeded, got %v", err)
	}

	if len(mock.Commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(mock.Commands))
	}

	cmd := mock.Commands[0]
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "ssh -N") {
		t.Error("Command should contain 'ssh -N'")
	}

	if !strings.Contains(cmdStr, "-L 8080:127.0.0.1:8080") {
		t.Error("Command should contain port mapping for 8080")
	}

	if !strings.Contains(cmdStr, "-L 9090:127.0.0.1:9091") {
		t.Error("Command should contain port mapping for 9090")
	}

	if !strings.Contains(cmdStr, "testserver") {
		t.Error("Command should contain the host")
	}
}

func TestMockSSHExecutorWithIdentityFile(t *testing.T) {
	mock := &MockSSHExecutor{}

	tunnel := config.Tunnel{
		Host:         "testserver",
		Ports:        []string{"8080:8080"},
		IdentityFile: "~/.ssh/custom_key",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	mock.Execute(ctx, "test", tunnel)

	if len(mock.Commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(mock.Commands))
	}

	cmd := mock.Commands[0]
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "-i") {
		t.Error("Command should contain '-i' flag for identity file")
	}

	foundIdentityFile := false
	for i, arg := range cmd {
		if arg == "-i" && i+1 < len(cmd) {
			if strings.Contains(cmd[i+1], "custom_key") {
				foundIdentityFile = true
				break
			}
		}
	}

	if !foundIdentityFile {
		t.Error("Command should contain the identity file path")
	}
}

func TestMockSSHExecutorStatusCallbacks(t *testing.T) {
	statusChanges := []struct {
		tunnelName string
		port       string
		status     string
	}{}

	mock := &MockSSHExecutor{
		OnStatusChange: func(name, port, status string) {
			statusChanges = append(statusChanges, struct {
				tunnelName string
				port       string
				status     string
			}{name, port, status})
		},
	}

	tunnel := config.Tunnel{
		Host:  "testserver",
		Ports: []string{"8080:8080", "9090:9091"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	mock.Execute(ctx, "test", tunnel)

	expectedChanges := 4
	if len(statusChanges) != expectedChanges {
		t.Errorf("Expected %d status changes, got %d", expectedChanges, len(statusChanges))
	}

	expectedStatuses := []string{"connecting", "active", "connecting", "active"}
	for i, expected := range expectedStatuses {
		if i < len(statusChanges) && statusChanges[i].status != expected {
			t.Errorf("Status change %d: expected %s, got %s", i, expected, statusChanges[i].status)
		}
	}
}

func TestMockSSHExecutorWithDynamicPorts(t *testing.T) {
	mock := &MockSSHExecutor{}

	tunnel := config.Tunnel{
		Host:         "testserver",
		DynamicPorts: []string{"1080"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := mock.Execute(ctx, "test", tunnel)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context deadline exceeded, got %v", err)
	}

	if len(mock.Commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(mock.Commands))
	}

	cmdStr := strings.Join(mock.Commands[0], " ")
	if !strings.Contains(cmdStr, "-D 1080") {
		t.Error("Command should contain '-D 1080' for dynamic forwarding")
	}
}

func TestRealSSHExecutorReconnectsOnDrop(t *testing.T) {
	dir := t.TempDir()
	// A fake ssh that exits immediately, simulating a dropped connection.
	scriptPath, script := filepath.Join(dir, "ssh"), "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		scriptPath, script = filepath.Join(dir, "ssh.bat"), "@exit /b 0\r\n"
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake ssh script: %v", err)
	}

	origPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+origPath); err != nil {
		t.Fatalf("failed to set PATH: %v", err)
	}
	defer os.Setenv("PATH", origPath)

	var mu sync.Mutex
	var statuses []string
	realExec := &RealSSHExecutor{
		OnStatusChange: func(name, port, status string) {
			mu.Lock()
			statuses = append(statuses, status)
			mu.Unlock()
		},
	}

	tunnel := config.Tunnel{Host: "testserver", Ports: []string{"8080:8080"}}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	realExec.Execute(ctx, "test", tunnel)

	mu.Lock()
	defer mu.Unlock()

	reconnectAttempts := 0
	for _, s := range statuses {
		if strings.HasPrefix(s, "reconnecting") {
			reconnectAttempts++
		}
	}

	if reconnectAttempts < 1 {
		t.Errorf("expected at least one reconnect attempt after the connection dropped, got statuses: %v", statuses)
	}
}

func TestLocalForward(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"3000", "3000:127.0.0.1:3000"},
		{"8080:8081", "8080:127.0.0.1:8081"},
		{" 5432:5433 ", "5432:127.0.0.1:5433"},
		{"3000:127.0.0.1:3000", "3000:127.0.0.1:3000"},
		{"3000:localhost:3000", "3000:localhost:3000"},
		{"5432:db.internal:5432", "5432:db.internal:5432"},
		{"3000:[::1]:3000", "3000:[::1]:3000"},
		{"3000:::1:3000", "3000:[::1]:3000"},
	}
	for _, tt := range tests {
		got, err := LocalForward(tt.input)
		if err != nil {
			t.Errorf("LocalForward(%q) returned error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("LocalForward(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLocalForwardRejectsInvalid(t *testing.T) {
	for _, input := range []string{"", "abc", "3000:abc", "0:3000", "3000:70000", "3000::3000", "3000:[]:3000", "db:3000:3000"} {
		if got, err := LocalForward(input); err == nil {
			t.Errorf("LocalForward(%q) = %q, expected an error", input, got)
		}
	}
}