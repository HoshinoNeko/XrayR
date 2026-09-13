package porthopping

import (
	"crypto/sha256"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type rule struct {
	command string
	args    []string
}

type Manager struct {
	mu    sync.Mutex
	rules []rule
}

func New() *Manager { return &Manager{} }

// Apply checks firewall availability and privileges before adding idempotent
// UDP REDIRECT rules. Existing rules owned by this manager are removed first.
func (m *Manager) Apply(ports string, target uint32, tag, listenIP string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if runtime.GOOS != "linux" {
		return fmt.Errorf("automatic port hopping firewall setup is supported on Linux only")
	}
	if target == 0 || target > 65535 {
		return fmt.Errorf("invalid redirect target port: %d", target)
	}
	ranges, err := parsePorts(ports)
	if err != nil {
		return err
	}
	commandName := "iptables"
	if strings.Contains(listenIP, ":") {
		commandName = "ip6tables"
	}
	command, err := exec.LookPath(commandName)
	if err != nil {
		return fmt.Errorf("%s is required for automatic port hopping: %w", commandName, err)
	}
	if output, err := exec.Command(command, "-w", "-t", "nat", "-S", "PREROUTING").CombinedOutput(); err != nil {
		return fmt.Errorf("%s preflight failed (root/CAP_NET_ADMIN required): %w: %s", commandName, err, strings.TrimSpace(string(output)))
	}

	m.removeLocked()
	comment := fmt.Sprintf("XrayR-hy2-%x", sha256.Sum256([]byte(tag)))[:27]
	for _, portRange := range ranges {
		spec := []string{"PREROUTING", "-p", "udp", "--dport", portRange, "-m", "comment", "--comment", comment, "-j", "REDIRECT", "--to-ports", strconv.FormatUint(uint64(target), 10)}
		check := append([]string{"-w", "-t", "nat", "-C"}, spec...)
		if err := exec.Command(command, check...).Run(); err == nil {
			m.rules = append(m.rules, rule{command: command, args: append([]string{"-w", "-t", "nat", "-D"}, spec...)})
			continue
		}
		add := append([]string{"-w", "-t", "nat", "-A"}, spec...)
		if output, err := exec.Command(command, add...).CombinedOutput(); err != nil {
			m.removeLocked()
			return fmt.Errorf("add port hopping rule %s failed: %w: %s", portRange, err, strings.TrimSpace(string(output)))
		}
		m.rules = append(m.rules, rule{command: command, args: append([]string{"-w", "-t", "nat", "-D"}, spec...)})
	}
	return nil
}

func (m *Manager) Remove() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeLocked()
}

func (m *Manager) removeLocked() {
	for i := len(m.rules) - 1; i >= 0; i-- {
		_ = exec.Command(m.rules[i].command, m.rules[i].args...).Run()
	}
	m.rules = nil
}

func parsePorts(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	ranges := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("invalid port range: %s", part)
		}
		first, err := strconv.Atoi(bounds[0])
		if err != nil || first < 1 || first > 65535 {
			return nil, fmt.Errorf("invalid port: %s", part)
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.Atoi(bounds[1])
			if err != nil || last < first || last > 65535 {
				return nil, fmt.Errorf("invalid port range: %s", part)
			}
		}
		if first == last {
			ranges = append(ranges, strconv.Itoa(first))
		} else {
			ranges = append(ranges, fmt.Sprintf("%d:%d", first, last))
		}
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("port hopping ports are empty")
	}
	return ranges, nil
}
