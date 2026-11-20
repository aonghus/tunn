package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
	spec, err := LocalForward(portMapping)
	if err != nil {
		if e.OnStatusChange != nil {
			e.OnStatusChange(tunnelName, portMapping, fmt.Sprintf("error - %s", err))
		}
		return err
	}
	return e.executeSSH(ctx, tunnelName, tunnel, portMapping, []string{"-L", spec})
}

// DefaultRemoteHost is where forwards connect on the remote side when the
// mapping does not name a host. It is 127.0.0.1 rather than "localhost"
// because some sshd builds (notably Windows OpenSSH) resolve localhost to ::1
// only, which fails against services that listen on IPv4 alone.
const DefaultRemoteHost = "127.0.0.1"

// LocalForward converts a port mapping from the config into the argument for
// ssh -L. Accepted forms:
//
//	3000              -> 3000:127.0.0.1:3000
//	3000:4000         -> 3000:127.0.0.1:4000
//	3000:db:5432      -> 3000:db:5432
//	3000:[::1]:3000   -> 3000:[::1]:3000 (IPv6 hosts may also be written bare)
func LocalForward(mapping string) (string, error) {
	parts := strings.Split(strings.TrimSpace(mapping), ":")
	var local, host, remote string
	switch len(parts) {
	case 1:
		local, host, remote = parts[0], DefaultRemoteHost, parts[0]
	case 2:
		local, host, remote = parts[0], DefaultRemoteHost, parts[1]
	default:
		local, remote = parts[0], parts[len(parts)-1]
		host = strings.Join(parts[1:len(parts)-1], ":")
	}

	for _, p := range []string{local, remote} {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("invalid port mapping %q: %q is not a port number", mapping, p)
		}
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "" {
		return "", fmt.Errorf("invalid port mapping %q: empty remote host", mapping)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return local + ":" + host + ":" + remote, nil
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
		spec, err := LocalForward(portMapping)
		if err != nil {
			return err
		}
		args = append(args, "-L", spec)
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
