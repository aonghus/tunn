package daemon

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/strandnerd/tunn/status"
)

func TestServerStatusHandshake(t *testing.T) {
	store := status.NewStore()
	store.EnsureTunnel("db", []string{"5432"})
	store.Update("db", "5432", "active")

	s := NewServer(Paths{}, store, 1234, nil, nil)
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
	})

	done := make(chan struct{})
	go func() {
		s.handleConnection(serverConn)
		close(done)
	}()

	enc := json.NewEncoder(clientConn)
	dec := json.NewDecoder(clientConn)

	if err := enc.Encode(StatusRequest{Command: "status"}); err != nil {
		t.Fatalf("failed to encode request: %v", err)
	}

	var resp StatusResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	<-done

	if !resp.Running {
		t.Fatalf("expected running status, got %+v", resp)
	}
	if resp.Mode != "daemon" {
		t.Fatalf("expected mode daemon, got %s", resp.Mode)
	}
	if resp.PID != 1234 {
		t.Fatalf("expected pid 1234, got %d", resp.PID)
	}
	if len(resp.Tunnels) != 1 {
		t.Fatalf("expected 1 tunnel in response, got %d", len(resp.Tunnels))
	}
	if resp.Tunnels[0].Ports["5432"] != "active" {
		t.Fatalf("expected port status active, got %s", resp.Tunnels[0].Ports["5432"])
	}
}

func TestServerStopCommand(t *testing.T) {
	store := status.NewStore()
	store.EnsureTunnel("db", []string{"5432"})
	triggered := make(chan struct{}, 1)

	s := NewServer(Paths{}, store, 99, func() {
		triggered <- struct{}{}
	}, nil)

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
	})

	go s.handleConnection(serverConn)

	enc := json.NewEncoder(clientConn)
	dec := json.NewDecoder(clientConn)

	if err := enc.Encode(StatusRequest{Command: "stop"}); err != nil {
		t.Fatalf("failed to encode request: %v", err)
	}

	var resp StatusResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Running {
		t.Fatalf("expected running=false response, got %+v", resp)
	}
	if resp.PID != 99 {
		t.Fatalf("expected pid 99, got %d", resp.PID)
	}
	if resp.Message != "stopping" {
		t.Fatalf("expected message 'stopping', got %q", resp.Message)
	}

	select {
	case <-triggered:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected stop callback to be invoked")
	}
}

func TestServerReloadCommand(t *testing.T) {
	store := status.NewStore()

	s := NewServer(Paths{}, store, 42, nil, func() (string, error) {
		return "reloaded (1 tunnel(s))", nil
	})

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
	})

	go s.handleConnection(serverConn)

	enc := json.NewEncoder(clientConn)
	dec := json.NewDecoder(clientConn)

	if err := enc.Encode(StatusRequest{Command: "reload"}); err != nil {
		t.Fatalf("failed to encode request: %v", err)
	}

	var resp StatusResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Running {
		t.Fatalf("expected running=true response, got %+v", resp)
	}
	if resp.Message != "reloaded (1 tunnel(s))" {
		t.Fatalf("expected reload summary message, got %q", resp.Message)
	}
}

func TestServerReloadCommandUnsupported(t *testing.T) {
	store := status.NewStore()

	s := NewServer(Paths{}, store, 42, nil, nil)

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
	})

	go s.handleConnection(serverConn)

	enc := json.NewEncoder(clientConn)
	dec := json.NewDecoder(clientConn)

	if err := enc.Encode(StatusRequest{Command: "reload"}); err != nil {
		t.Fatalf("failed to encode request: %v", err)
	}

	var resp StatusResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Message != "reload not supported" {
		t.Fatalf("expected 'reload not supported' message, got %q", resp.Message)
	}
}
