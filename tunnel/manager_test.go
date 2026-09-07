package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/aonghus/tunn/config"
	"github.com/aonghus/tunn/executor"
	"github.com/aonghus/tunn/output"
)

type stubPortChecker struct {
	listeners map[string]*processInfo
	err       error
}

func (s *stubPortChecker) findListener(port string) (*processInfo, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.listeners == nil {
		return nil, nil
	}
	return s.listeners[port], nil
}

func TestManagerRunTunnels(t *testing.T) {
	mock := &executor.MockSSHExecutor{}
	display := output.NewDisplay()
	manager := NewManager(mock, display, nil, display.RemoveTunnel)
	manager.checker = &stubPortChecker{}

	tunnels := map[string]config.Tunnel{
		"api": {
			Host:  "server1",
			Ports: []string{"3000:3000"},
		},
		"db": {
			Host:  "server2",
			Ports: []string{"5432:5432"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := manager.RunTunnels(ctx, tunnels)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context deadline exceeded, got %v", err)
	}

	if len(mock.Commands) != 2 {
		t.Errorf("Expected 2 commands (one per tunnel), got %d", len(mock.Commands))
	}

	foundHosts := make(map[string]bool)
	for _, cmd := range mock.Commands {
		for _, arg := range cmd {
			if arg == "server1" || arg == "server2" {
				foundHosts[arg] = true
			}
		}
	}

	if len(foundHosts) != 2 {
		t.Error("Expected both server1 and server2 to be in commands")
	}
}

func TestManagerRunSingleTunnel(t *testing.T) {
	mock := &executor.MockSSHExecutor{}
	display := output.NewDisplay()
	manager := NewManager(mock, display, nil, display.RemoveTunnel)
	manager.checker = &stubPortChecker{}

	tunnels := map[string]config.Tunnel{
		"api": {
			Host:  "server1",
			Ports: []string{"3000:3000", "4000:4000"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := manager.RunTunnels(ctx, tunnels)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context deadline exceeded, got %v", err)
	}

	if len(mock.Commands) != 1 {
		t.Fatalf("Expected 1 command for tunnel, got %d", len(mock.Commands))
	}

	cmd := mock.Commands[0]
	found3000 := false
	found4000 := false
	for i := 0; i < len(cmd)-1; i++ {
		if cmd[i] == "-L" && cmd[i+1] == "3000:localhost:3000" {
			found3000 = true
		}
		if cmd[i] == "-L" && cmd[i+1] == "4000:localhost:4000" {
			found4000 = true
		}
	}
	if !found3000 || !found4000 {
		t.Fatalf("expected aggregated command to include both port mappings, got %v", cmd)
	}
}

func TestManagerCancellation(t *testing.T) {
	mock := &executor.MockSSHExecutor{}
	display := output.NewDisplay()
	manager := NewManager(mock, display, nil, display.RemoveTunnel)
	manager.checker = &stubPortChecker{}

	tunnels := map[string]config.Tunnel{
		"api": {
			Host:  "server1",
			Ports: []string{"3000:3000"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := manager.RunTunnels(ctx, tunnels)
	if err != context.Canceled {
		t.Errorf("Expected context canceled, got %v", err)
	}
}

func TestManagerReload(t *testing.T) {
	mock := &executor.MockSSHExecutor{}
	display := output.NewDisplay()
	manager := NewManager(mock, display, nil, display.RemoveTunnel)
	manager.checker = &stubPortChecker{}

	initial := map[string]config.Tunnel{
		"api": {Host: "server1", Ports: []string{"3000:3000"}},
		"db":  {Host: "server2", Ports: []string{"5432:5432"}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- manager.RunTunnels(ctx, initial)
	}()

	waitForCommandCount(t, mock, 2)

	updated := map[string]config.Tunnel{
		"api":   {Host: "server1", Ports: []string{"3000:3000"}}, // unchanged
		"cache": {Host: "server3", Ports: []string{"6379:6379"}}, // new
		// db intentionally omitted: should be stopped
	}

	if err := manager.Reload(updated); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	// api unchanged (no restart) + db stopped + cache started = 3 total commands ever issued
	waitForCommandCount(t, mock, 3)

	foundServer3 := false
	for _, cmd := range mock.CommandsSnapshot() {
		for _, arg := range cmd {
			if arg == "server3" {
				foundServer3 = true
			}
		}
	}
	if !foundServer3 {
		t.Error("expected new 'cache' tunnel targeting server3 to have been started")
	}

	cancel()
	if err := <-runDone; err != context.Canceled {
		t.Errorf("expected context canceled, got %v", err)
	}
}

func waitForCommandCount(t *testing.T, mock *executor.MockSSHExecutor, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(mock.CommandsSnapshot()) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d commands, got %d", want, len(mock.CommandsSnapshot()))
}

func TestManagerRunTunnelPortInUse(t *testing.T) {
	mock := &executor.MockSSHExecutor{}
	display := output.NewDisplay()
	manager := NewManager(mock, display, nil, display.RemoveTunnel)
	manager.checker = &stubPortChecker{
		listeners: map[string]*processInfo{
			"3000": {
				command: "node",
				pid:     3342,
			},
		},
	}

	tunnelCfg := config.Tunnel{
		Host:  "server1",
		Ports: []string{"3000:3000"},
	}

	err := manager.runTunnel(context.Background(), "api", tunnelCfg)
	if err == nil {
		t.Fatal("expected error when port is in use, got nil")
	}

	expected := "port 3000 is being used by \"node\" (pid: 3342)"
	if err.Error() != expected {
		t.Fatalf("unexpected error message: %v", err)
	}

	if len(mock.Commands) != 0 {
		t.Fatalf("expected executor not to run, but got %d commands", len(mock.Commands))
	}
}
