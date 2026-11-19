package tunnel

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type portChecker interface {
	findListener(port string) (*processInfo, error)
}

// processInfo describes whatever holds a port. A zero pid means the port is
// taken but the owning process could not be identified.
type processInfo struct {
	command string
	pid     int
}

func newSystemPortChecker() portChecker {
	return &systemPortChecker{}
}

type systemPortChecker struct{}

// findListener reports what is listening on a local TCP port, or nil if it is free.
//
// Where lsof is available it is used first, because it can name the owning
// process. Otherwise (Windows, minimal Linux images), or when lsof sees nothing
// (it may not show other users' sockets), the port is probed by binding it.
func (c *systemPortChecker) findListener(port string) (*processInfo, error) {
	if port == "" {
		return nil, nil
	}
	if runtime.GOOS != "windows" {
		if info := lsofListener(port); info != nil {
			return info, nil
		}
	}
	return probeListener(port), nil
}

// probeListener tries to bind 127.0.0.1:port, which is where ssh -L listens.
// It returns a processInfo with no pid if the bind fails.
func probeListener(port string) *processInfo {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return &processInfo{}
	}
	_ = ln.Close()
	return nil
}

// lsofListener returns the process lsof reports listening on port, or nil if
// lsof finds nothing, is not installed, or fails.
func lsofListener(port string) *processInfo {
	output, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN").Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) <= 1 {
		return nil
	}

	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		command := fields[0]
		if command == "" {
			command = "unknown"
		}

		return &processInfo{command: command, pid: pid}
	}

	return nil
}

func extractLocalPort(mapping string) (string, error) {
	mapping = strings.TrimSpace(mapping)
	if mapping == "" {
		return "", fmt.Errorf("empty port mapping")
	}

	parts := strings.Split(mapping, ":")
	if len(parts) == 0 {
		return "", fmt.Errorf("unable to determine local port for mapping %q", mapping)
	}

	candidate := strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		if _, err := strconv.Atoi(candidate); err != nil {
			candidate = strings.TrimSpace(parts[1])
		}
	}

	if candidate == "" {
		return "", fmt.Errorf("unable to determine local port for mapping %q", mapping)
	}

	if _, err := strconv.Atoi(candidate); err != nil {
		return "", fmt.Errorf("invalid local port %q", candidate)
	}

	return candidate, nil
}
