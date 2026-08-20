package tunnel

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/strandnerd/tunn/config"
	"github.com/strandnerd/tunn/executor"
	"github.com/strandnerd/tunn/output"
)

// runningTunnel tracks a tunnel currently being executed so it can be
// stopped individually (e.g. by a config reload) without affecting others.
type runningTunnel struct {
	tunnel config.Tunnel
	cancel context.CancelFunc
	done   chan struct{}
}

type Manager struct {
	executor executor.SSHExecutor
	display  *output.Display
	checker  portChecker
	notify   func(string, string, string)
	onRemove func(string)

	mu      sync.Mutex
	ctx     context.Context
	running map[string]*runningTunnel
	wg      sync.WaitGroup
}

func NewManager(exec executor.SSHExecutor, display *output.Display, notifier func(string, string, string), onRemove func(string)) *Manager {
	return &Manager{
		executor: exec,
		display:  display,
		checker:  newSystemPortChecker(),
		notify:   notifier,
		onRemove: onRemove,
	}
}

// RunTunnels starts every tunnel in the given map and blocks until ctx is
// cancelled, at which point all tunnels are stopped. While running, the set
// of active tunnels can be changed via Reload.
func (m *Manager) RunTunnels(ctx context.Context, tunnels map[string]config.Tunnel) error {
	m.mu.Lock()
	m.ctx = ctx
	m.running = make(map[string]*runningTunnel, len(tunnels))
	for name, tun := range tunnels {
		m.startTunnelLocked(name, tun)
	}
	m.mu.Unlock()

	// Wait for context cancellation (tunnels run until cancelled)
	<-ctx.Done()
	m.wg.Wait()
	return ctx.Err()
}

// Reload diffs the given tunnels against the currently running set: tunnels
// no longer present are stopped, new tunnels are started, and tunnels whose
// configuration changed are restarted. Tunnels that are unchanged keep
// running undisturbed. It must be called while RunTunnels is active.
func (m *Manager) Reload(tunnels map[string]config.Tunnel) error {
	m.mu.Lock()
	if m.ctx == nil || m.ctx.Err() != nil {
		m.mu.Unlock()
		return fmt.Errorf("cannot reload: tunnel manager is not running")
	}

	toStop := make(map[string]bool)
	for name := range m.running {
		if _, ok := tunnels[name]; !ok {
			toStop[name] = true
		}
	}

	var toStart []string
	for name, tun := range tunnels {
		existing, ok := m.running[name]
		if !ok {
			toStart = append(toStart, name)
			continue
		}
		if !tunnelsEqual(existing.tunnel, tun) {
			toStop[name] = true
			toStart = append(toStart, name)
		}
	}

	type stopping struct {
		name string
		done chan struct{}
	}
	stoppingList := make([]stopping, 0, len(toStop))
	for name := range toStop {
		rt, ok := m.running[name]
		if !ok {
			continue
		}
		delete(m.running, name)
		rt.cancel()
		stoppingList = append(stoppingList, stopping{name: name, done: rt.done})
	}
	m.mu.Unlock()

	for _, s := range stoppingList {
		<-s.done
		if m.onRemove != nil {
			m.onRemove(s.name)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return fmt.Errorf("cannot reload: tunnel manager stopped during reload")
	}
	for _, name := range toStart {
		m.startTunnelLocked(name, tunnels[name])
	}

	return nil
}

// startTunnelLocked launches a tunnel and tracks it for later cancellation.
// Callers must hold m.mu.
func (m *Manager) startTunnelLocked(name string, tun config.Tunnel) {
	subCtx, cancel := context.WithCancel(m.ctx)
	done := make(chan struct{})
	m.running[name] = &runningTunnel{tunnel: tun, cancel: cancel, done: done}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(done)
		_ = m.runTunnel(subCtx, name, tun)
	}()
}

func tunnelsEqual(a, b config.Tunnel) bool {
	return reflect.DeepEqual(a, b)
}

func (m *Manager) runTunnel(ctx context.Context, name string, tunnel config.Tunnel) error {
	if err := m.ensurePortsAvailable(name, tunnel); err != nil {
		return err
	}
	return m.executor.Execute(ctx, name, tunnel)
}

func (m *Manager) ensurePortsAvailable(tunnelName string, tunnel config.Tunnel) error {
	mappings := make([]string, 0, len(tunnel.Ports)+len(tunnel.DynamicPorts))
	mappings = append(mappings, tunnel.Ports...)
	for _, port := range tunnel.DynamicPorts {
		mappings = append(mappings, dynamicLabel(port))
	}

	conflicts := make(map[string]string)
	var conflictMessages []string

	for _, mapping := range mappings {
		localPort, err := extractLocalPort(mapping)
		if err != nil {
			return fmt.Errorf("invalid port mapping %q: %w", mapping, err)
		}

		process, err := m.checker.findListener(localPort)
		if err != nil {
			return err
		}
		if process != nil {
			message := fmt.Sprintf("port %s is being used by \"%s\" (pid: %d)", localPort, process.command, process.pid)
			conflicts[mapping] = message
			conflictMessages = append(conflictMessages, message)
		}
	}

	if len(conflicts) > 0 {
		for _, mapping := range mappings {
			status := "stopped"
			if msg, ok := conflicts[mapping]; ok {
				status = fmt.Sprintf("error - %s", msg)
			}
			m.reportStatus(tunnelName, mapping, status)
		}
		return fmt.Errorf("%s", strings.Join(conflictMessages, "; "))
	}

	return nil
}

func dynamicLabel(port string) string {
	return fmt.Sprintf("%s:socks", port)
}

func (m *Manager) reportStatus(tunnelName, mapping, status string) {
	if m.display != nil {
		m.display.UpdateStatus(tunnelName, mapping, status)
	}
	if m.notify != nil {
		m.notify(tunnelName, mapping, status)
	}
}
