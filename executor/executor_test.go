package executor

import (
	"context"
	"os"
	"path/filepath"
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

	if !strings.Contains(cmdStr, "-L 8080:localhost:8080") {
		t.Error("Command should contain port mapping for 8080")
	}

	if !strings.Contains(cmdStr, "-L 9090:localhost:9091") {
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
	scriptPath := filepath.Join(dir, "ssh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
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

func TestExpandPort(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"8080:8081", []string{"8080", "8081"}},
		{"3000", []string{"3000", "3000"}},
		{"5432:5433", []string{"5432", "5433"}},
	}

	for _, tt := range tests {
		result := expandPort(tt.input, ":")
		if len(result) != 2 {
			t.Errorf("Expected 2 elements, got %d", len(result))
			continue
		}
		if result[0] != tt.expected[0] || result[1] != tt.expected[1] {
			t.Errorf("For %s: expected %v, got %v", tt.input, tt.expected, result)
		}
	}
}