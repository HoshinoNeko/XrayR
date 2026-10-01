package porthopping

import (
	"crypto/sha256"
	"fmt"
	"net"
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
	// Injectable command execution keeps firewall lifecycle tests unprivileged.
	commands firewallCommands
	goos     string
}

func New() *Manager { return &Manager{} }

type firewallCommands interface {
	LookPath(string) (string, error)
	Run(string, ...string) ([]byte, error)
}

type systemCommands struct{}

func (systemCommands) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (systemCommands) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func (m *Manager) executor() firewallCommands {
	if m.commands != nil {
		return m.commands
	}
	return systemCommands{}
}

func firewallFamilies(listenIP string) ([]string, error) {
	// Xray-core uses net.ListenPacket("udp", ...), not udp4/udp6. Go uses
	// a dual-stack socket for wildcard addresses when the OS supports it,
	// including 0.0.0.0. A concrete address only accepts its own family.
	if listenIP == "" {
		return []string{"iptables", "ip6tables"}, nil
	}
	ip := net.ParseIP(listenIP)
	if ip == nil {
		return nil, fmt.Errorf("automatic port hopping requires an IP listen address: %q", listenIP)
	}
	if ip.IsUnspecified() {
		return []string{"iptables", "ip6tables"}, nil
	}
	if ip.To4() != nil {
		return []string{"iptables"}, nil
	}
	return []string{"ip6tables"}, nil
}

// Apply checks firewall availability and privileges before adding idempotent
// UDP REDIRECT rules. Existing rules owned by this manager are removed first.
func (m *Manager) Apply(ports string, target uint32, tag, listenIP string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	goos := m.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "linux" {
		return fmt.Errorf("automatic port hopping firewall setup is supported on Linux only")
	}
	if target == 0 || target > 65535 {
		return fmt.Errorf("invalid redirect target port: %d", target)
	}
	ranges, err := parsePorts(ports)
	if err != nil {
		return err
	}
	families, err := firewallFamilies(listenIP)
	if err != nil {
		return err
	}
	executor := m.executor()
	commands := make([]string, 0, len(families))
	// Check every required family before changing any existing rules.
	for _, name := range families {
		command, err := executor.LookPath(name)
		if err != nil {
			return fmt.Errorf("%s is required for automatic port hopping: %w", name, err)
		}
		if output, err := executor.Run(command, "-w", "-t", "nat", "-S", "PREROUTING"); err != nil {
			return fmt.Errorf("%s preflight failed (NAT support and root/CAP_NET_ADMIN required): %w: %s", name, err, strings.TrimSpace(string(output)))
		}
		commands = append(commands, command)
	}

	m.removeLocked()
	comment := fmt.Sprintf("XrayR-hy2-%x", sha256.Sum256([]byte(tag)))[:27]
	for _, command := range commands {
		for _, portRange := range ranges {
			spec := []string{"PREROUTING", "-p", "udp", "--dport", portRange, "-m", "comment", "--comment", comment, "-j", "REDIRECT", "--to-ports", strconv.FormatUint(uint64(target), 10)}
			check := append([]string{"-w", "-t", "nat", "-C"}, spec...)
			if _, err := executor.Run(command, check...); err == nil {
				m.rules = append(m.rules, rule{command: command, args: append([]string{"-w", "-t", "nat", "-D"}, spec...)})
				continue
			}
			add := append([]string{"-w", "-t", "nat", "-A"}, spec...)
			if output, err := executor.Run(command, add...); err != nil {
				m.removeLocked()
				return fmt.Errorf("%s add port hopping rule %s failed: %w: %s", command, portRange, err, strings.TrimSpace(string(output)))
			}
			m.rules = append(m.rules, rule{command: command, args: append([]string{"-w", "-t", "nat", "-D"}, spec...)})
		}
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
		_, _ = m.executor().Run(m.rules[i].command, m.rules[i].args...)
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
