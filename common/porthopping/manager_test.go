package porthopping

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParsePorts(t *testing.T) {
	got, err := parsePorts("20000-20002, 443,30000-30100")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"20000:20002", "443", "30000:30100"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParsePortsRejectsInvalid(t *testing.T) {
	for _, value := range []string{"", "0", "2-1", "65536", "1-2-3"} {
		if _, err := parsePorts(value); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
}

func TestFirewallFamilies(t *testing.T) {
	for _, tc := range []struct {
		listen string
		want   []string
	}{
		{"", []string{"iptables", "ip6tables"}},
		{"0.0.0.0", []string{"iptables", "ip6tables"}},
		{"::", []string{"iptables", "ip6tables"}},
		{"::ffff:0.0.0.0", []string{"iptables", "ip6tables"}},
		{"127.0.0.1", []string{"iptables"}},
		{"192.0.2.1", []string{"iptables"}},
		{"::ffff:192.0.2.1", []string{"iptables"}},
		{"::1", []string{"ip6tables"}},
		{"2001:db8::1", []string{"ip6tables"}},
	} {
		t.Run(tc.listen, func(t *testing.T) {
			got, err := firewallFamilies(tc.listen)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	if _, err := firewallFamilies("not-an-ip"); err == nil {
		t.Fatal("invalid address accepted")
	}
}

// Emulate separate IPv4/IPv6 rule tables without modifying the host firewall.
type fakeCommands struct {
	tables map[string]bool
	calls  []string
	fail   string
}

func (f *fakeCommands) LookPath(name string) (string, error) {
	if f.fail == name+" lookup" {
		return "", fmt.Errorf("not installed")
	}
	return name, nil
}

func (f *fakeCommands) Run(name string, args ...string) ([]byte, error) {
	op := name + " " + args[3]
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.fail == op || (len(args) > 8 && f.fail == op+" "+args[8]) {
		return []byte("simulated failure"), fmt.Errorf("command failed")
	}
	key := name + " " + strings.Join(args[4:], " ")
	switch args[3] {
	case "-C":
		if !f.tables[key] {
			return nil, fmt.Errorf("rule absent")
		}
	case "-A":
		f.tables[key] = true
	case "-D":
		delete(f.tables, key)
	}
	return nil, nil
}

func testManager() (*Manager, *fakeCommands) {
	f := &fakeCommands{tables: make(map[string]bool)}
	return &Manager{goos: "linux", commands: f}, f
}

func TestDualStackLifecycle(t *testing.T) {
	m, f := testManager()
	// An unrelated rule (e.g. Docker) must survive every lifecycle operation.
	f.tables["unrelated Docker rule"] = true
	if err := m.Apply("50000-55000,56000", 50000, "node", "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	if len(m.rules) != 4 || len(f.tables) != 5 {
		t.Fatalf("expected two ranges per family, got %v", f.tables)
	}
	for _, command := range []string{"iptables", "ip6tables"} {
		want := command + " PREROUTING -p udp --dport 50000:55000 -m comment --comment XrayR-hy2-" // Tag hash follows.
		found := false
		for key := range f.tables {
			if strings.HasPrefix(key, want) && strings.HasSuffix(key, " -j REDIRECT --to-ports 50000") {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing correct %s redirect: %v", command, f.tables)
		}
	}
	// Re-applying must not duplicate rules.
	if err := m.Apply("50000-55000,56000", 50000, "node", "::"); err != nil {
		t.Fatal(err)
	}
	if len(f.tables) != 5 {
		t.Fatalf("duplicate rules: %v", f.tables)
	}
	// Reload replaces both families, including the old port and ranges.
	if err := m.Apply("60000-60100", 60000, "node", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.tables) != 3 {
		t.Fatalf("old rules survived reload: %v", f.tables)
	}
	for key := range f.tables {
		if strings.Contains(key, "50000") || strings.Contains(key, "56000") {
			t.Fatalf("stale rule: %s", key)
		}
	}
	m.Remove()
	m.Remove()
	if len(f.tables) != 1 || !f.tables["unrelated Docker rule"] || len(m.rules) != 0 {
		t.Fatalf("cleanup touched unrelated rules or leaked owned rules: %v", f.tables)
	}
}

func TestDualStackPreflightPreservesExistingRules(t *testing.T) {
	for _, failure := range []string{"ip6tables lookup", "ip6tables -S", "iptables -S"} {
		t.Run(failure, func(t *testing.T) {
			m, f := testManager()
			if err := m.Apply("50000-55000", 50000, "node", "::"); err != nil {
				t.Fatal(err)
			}
			before := len(f.calls)
			f.fail = failure
			if err := m.Apply("60000", 60000, "node", "::"); err == nil {
				t.Fatal("failed capability check accepted")
			}
			if len(f.tables) != 2 || len(m.rules) != 2 {
				t.Fatalf("preflight changed active rules: %v", f.tables)
			}
			for _, call := range f.calls[before:] {
				if strings.Contains(call, " -A ") || strings.Contains(call, " -D ") {
					t.Fatalf("mutation before successful preflight: %s", call)
				}
			}
		})
	}
}

func TestDualStackInstallFailureRollsBackBothFamilies(t *testing.T) {
	for _, failure := range []string{"iptables -A", "ip6tables -A", "ip6tables -A 56000"} {
		t.Run(failure, func(t *testing.T) {
			m, f := testManager()
			f.fail = failure
			if err := m.Apply("50000-55000,56000", 50000, "node", "0.0.0.0"); err == nil || !strings.Contains(err.Error(), strings.Fields(failure)[0]) {
				t.Fatalf("expected family-specific failure, got %v", err)
			}
			if len(f.tables) != 0 || len(m.rules) != 0 {
				t.Fatalf("partial rules survived: %v", f.tables)
			}
		})
	}
}

func TestConcreteListenerInstallsOnlyMatchingFamily(t *testing.T) {
	for _, listen := range []string{"127.0.0.1", "2001:db8::1"} {
		t.Run(listen, func(t *testing.T) {
			m, f := testManager()
			if err := m.Apply("50000-55000", 50000, "node", listen); err != nil {
				t.Fatal(err)
			}
			if len(m.rules) != 1 || len(f.tables) != 1 {
				t.Fatalf("unexpected rules: %v", f.tables)
			}
			m.Remove()
			if len(f.tables) != 0 {
				t.Fatal("rules survived removal")
			}
		})
	}
}

func TestDualStackAdoptsExistingRules(t *testing.T) {
	m, f := testManager()
	if err := m.Apply("50000-55000", 50000, "node", "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	// Simulate a restart leaving matching rules behind in both tables.
	m = &Manager{goos: "linux", commands: f}
	before := len(f.calls)
	if err := m.Apply("50000-55000", 50000, "node", "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	if len(m.rules) != 2 || len(f.tables) != 2 {
		t.Fatalf("existing rules not adopted: %v", f.tables)
	}
	for _, call := range f.calls[before:] {
		if strings.Contains(call, " -A ") {
			t.Fatalf("duplicate rule added: %s", call)
		}
	}
	m.Remove()
	if len(f.tables) != 0 {
		t.Fatalf("adopted rules survived cleanup: %v", f.tables)
	}
}

func TestInvalidConfigurationDoesNotTouchFirewall(t *testing.T) {
	for _, tc := range []struct {
		ports  string
		target uint32
		listen string
		goos   string
	}{
		{"50000", 0, "::", "linux"},
		{"50000", 65536, "::", "linux"},
		{"55000-50000", 50000, "::", "linux"},
		{"50000", 50000, "invalid", "linux"},
		{"50000", 50000, "::", "darwin"},
	} {
		m, f := testManager()
		m.goos = tc.goos
		if err := m.Apply(tc.ports, tc.target, "node", tc.listen); err == nil {
			t.Fatalf("invalid configuration accepted: %+v", tc)
		}
		if len(f.calls) != 0 {
			t.Fatalf("firewall called for invalid configuration: %v", f.calls)
		}
	}
}
