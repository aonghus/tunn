package tunnel

import (
	"net"
	"testing"
)

// listenLocal holds a free 127.0.0.1 port for the duration of the test.
func listenLocal(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

// freePort returns a port that was free a moment ago.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	return port
}

func TestProbeListener(t *testing.T) {
	if info := probeListener(listenLocal(t)); info == nil {
		t.Fatal("expected a held port to be reported busy")
	}
	if info := probeListener(freePort(t)); info != nil {
		t.Fatalf("expected a free port to be reported free, got %+v", info)
	}
}

func TestSystemPortCheckerFindsListener(t *testing.T) {
	checker := newSystemPortChecker()

	info, err := checker.findListener(listenLocal(t))
	if err != nil {
		t.Fatalf("findListener failed: %v", err)
	}
	if info == nil {
		t.Fatal("expected a held port to be reported busy")
	}

	info, err = checker.findListener(freePort(t))
	if err != nil {
		t.Fatalf("findListener failed: %v", err)
	}
	if info != nil {
		t.Fatalf("expected a free port to be reported free, got %+v", info)
	}
}
