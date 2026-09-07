package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/aonghus/tunn/config"
)

type SSHExecutor interface {
	Execute(ctx context.Context, name string, tunnel config.Tunnel) error
}

type RealSSHExecutor struct {
	OnStatusChange func(tunnelName string, port string, status string)
}

func (e *RealSSHExecutor) Execute(ctx context.Context, name string, tunnel config.Tunnel) error {
	var wg sync.WaitGroup

	// Update all ports to connecting status synchronously
	for _, portMapping := range tunnel.Ports {
		if e.OnStatusChange != nil {
			e.OnStatusChange(name, portMapping, "connecting")
		}
	}
	for _, port := range tunnel.DynamicPorts {
		if e.OnStatusChange != nil {
			e.OnStatusChange(name, dynamicLabel(port), "connecting")
		}
	}

	// Start SSH processes for each port
	for _, portMapping := range tunnel.Ports {
		wg.Add(1)
		go func(port string) {
			defer wg.Done()
			e.executePortSSH(ctx, name, tunnel, port)
		}(portMapping)
	}
	for _, dynPort := range tunnel.DynamicPorts {
		wg.Add(1)
		go func(port string) {
			defer wg.Done()
			e.executeDynamicSSH(ctx, name, tunnel, port)
		}(dynPort)
	}

	// Wait for context cancellation (tunnels run until cancelled)
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

func dynamicLabel(port string) string {
	return fmt.Sprintf("%s:socks", port)
}

func (e *RealSSHExecutor) executePortSSH(ctx context.Context, tunnelName string, tunnel config.Tunnel, portMapping string) error {
	ports := expandPort(portMapping, ":")
	local, remote := ports[0], ports[1]
	forwardArgs := []string{"-L", fmt.Sprintf("%s:localhost:%s", local, remote)}
	return e.executeSSH(ctx, tunnelName, tunnel, portMapping, forwardArgs)
}

func (e *RealSSHExecutor) executeDynamicSSH(ctx context.Context, tunnelName string, tunnel config.Tunnel, port string) error {
	forwardArgs := []string{"-D", port}
	return e.executeSSH(ctx, tunnelName, tunnel, dynamicLabel(port), forwardArgs)
}

const (
	initialReconnectBackoff = 1 * time.Second
	maxReconnectBackoff     = 30 * time.Second
)

// executeSSH runs the given forward and, if the connection drops on its own
// (rather than being stopped via ctx cancellation), retries it with
// exponential backoff until it either reconnects or the context is done.
func (e *RealSSHExecutor) executeSSH(ctx context.Context, tunnelName string, tunnel config.Tunnel, portMapping string, forwardArgs []string) error {
	backoff := initialReconnectBackoff

	for {
		wasActive, err := e.runSSHOnce(ctx, tunnelName, tunnel, portMapping, forwardArgs)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if wasActive {
			backoff = initialReconnectBackoff
		}

		if e.OnStatusChange != nil {
			e.OnStatusChange(tunnelName, portMapping, fmt.Sprintf("reconnecting in %s", backoff))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxReconnectBackoff {
			backoff = maxReconnectBackoff
		}

		_ = err // already surfaced via OnStatusChange in runSSHOnce
	}
}

// runSSHOnce starts a single ssh process for the given forward and blocks
// until it exits, is cancelled via ctx, or fails to start. It reports
// whether the connection ever reached the "active" state, which the caller
// uses to decide whether to reset the reconnect backoff.
func (e *RealSSHExecutor) runSSHOnce(ctx context.Context, tunnelName string, tunnel config.Tunnel, portMapping string, forwardArgs []string) (bool, error) {
	// Build SSH command for this specific forward
	args := []string{"-N"}
	args = append(args, forwardArgs...)

	if tunnel.IdentityFile != "" {
		args = append(args, "-i", os.ExpandEnv(tunnel.IdentityFile))
	}

	if tunnel.User != "" {
		args = append(args, "-l", tunnel.User)
	}

	args = append(args, tunnel.Host)

	cmd := exec.Command("ssh", args...)

	// Start the SSH command
	if err := cmd.Start(); err != nil {
		if e.OnStatusChange != nil {
			e.OnStatusChange(tunnelName, portMapping, fmt.Sprintf("error - %s", err.Error()))
		}
		return false, err
	}

	activeTimer := time.NewTimer(500 * time.Millisecond)
	activeC := activeTimer.C

	stopActiveTimer := func() {
		if activeC == nil {
			return
		}
		if !activeTimer.Stop() {
			select {
			case <-activeC:
			default:
			}
		}
		activeC = nil
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	wasActive := false
	for {
		select {
		case <-activeC:
			wasActive = true
			if e.OnStatusChange != nil {
				e.OnStatusChange(tunnelName, portMapping, "active")
			}
			activeC = nil
		case err := <-done:
			stopActiveTimer()
			if err != nil {
				if e.OnStatusChange != nil {
					e.OnStatusChange(tunnelName, portMapping, fmt.Sprintf("error - %s", err.Error()))
				}
				return wasActive, err
			}
			if e.OnStatusChange != nil {
				e.OnStatusChange(tunnelName, portMapping, "stopped")
			}
			return wasActive, nil
		case <-ctx.Done():
			stopActiveTimer()
			if e.OnStatusChange != nil {
				e.OnStatusChange(tunnelName, portMapping, "stopping")
			}
			if cmd.Process != nil {
				_ = cmd.Process.Signal(os.Interrupt)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				<-done
			}
			if e.OnStatusChange != nil {
				e.OnStatusChange(tunnelName, portMapping, "stopped")
			}
			return wasActive, ctx.Err()
		}
	}
}

func expandPort(mapping string, sep string) []string {
	parts := strings.Split(mapping, sep)
	if len(parts) == 1 {
		return []string{parts[0], parts[0]}
	}
	return parts[:2]
}

type MockSSHExecutor struct {
	mu             sync.Mutex
	Commands       [][]string
	OnStatusChange func(tunnelName string, port string, status string)
}

// CommandsSnapshot returns a copy of the commands recorded so far, safe to
// call concurrently with in-flight Execute calls.
func (m *MockSSHExecutor) CommandsSnapshot() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := make([][]string, len(m.Commands))
	copy(snapshot, m.Commands)
	return snapshot
}

func (m *MockSSHExecutor) Execute(ctx context.Context, name string, tunnel config.Tunnel) error {
	args := []string{"ssh", "-N"}
	for _, portMapping := range tunnel.Ports {
		ports := expandPort(portMapping, ":")
		local, remote := ports[0], ports[1]
		args = append(args, "-L", fmt.Sprintf("%s:localhost:%s", local, remote))
	}
	for _, port := range tunnel.DynamicPorts {
		args = append(args, "-D", port)
	}

	if tunnel.IdentityFile != "" {
		args = append(args, "-i", os.ExpandEnv(tunnel.IdentityFile))
	}

	if tunnel.User != "" {
		args = append(args, "-l", tunnel.User)
	}

	args = append(args, tunnel.Host)
	m.mu.Lock()
	m.Commands = append(m.Commands, args)
	m.mu.Unlock()

	if m.OnStatusChange != nil {
		for _, portMapping := range tunnel.Ports {
			m.OnStatusChange(name, portMapping, "connecting")
			m.OnStatusChange(name, portMapping, "active")
		}
		for _, port := range tunnel.DynamicPorts {
			m.OnStatusChange(name, dynamicLabel(port), "connecting")
			m.OnStatusChange(name, dynamicLabel(port), "active")
		}
	}

	<-ctx.Done()
	return ctx.Err()
}
