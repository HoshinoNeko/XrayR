package panel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HoshinoNeko/XrayR/service"
)

func hoppingInbound(extension string) string {
	return `[{"tag":"static-hy2","port":55444,"protocol":"hysteria","settings":{"version":2,"users":[{"auth":"example:example-password"}]},"streamSettings":{"network":"hysteria","hysteriaSettings":{"version":2},"finalmask":{"quicParams":{"congestion":"force-brutal","brutalUp":"60 mbps","brutalDown":"0"}}}` + extension + `}]`
}

func TestParseCustomPortHopping(t *testing.T) {
	root := hoppingInbound(`,"portHopping":{"enabled":true,"autoConfigureFirewall":true,"ports":"55001-60000"}`)
	legacy := strings.Replace(hoppingInbound(""), `"brutalDown":"0"`, `"brutalDown":"0","udpHop":{"enable":true,"autoConfigureFirewall":true,"ports":"55001-60000","interval":"5-10"}`, 1)
	for _, raw := range []string{root, legacy} {
		inbounds, plans, err := parseCustomInbounds([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		want := []customHoppingPlan{{ports: "55001-60000", target: 55444, tag: "custom-inbound:static-hy2"}}
		if !reflect.DeepEqual(plans, want) {
			t.Fatalf("got %+v, want %+v", plans, want)
		}
		// Deployment settings must not alter core QUIC settings or native auth.
		if inbounds[0].StreamSetting.FinalMask.QuicParams.BrutalUp != "60 mbps" {
			t.Fatal("QUIC settings changed")
		}
		if _, err := inbounds[0].Build(); err != nil {
			t.Fatalf("core configuration no longer builds: %v", err)
		}
	}
	for _, listen := range []string{"0.0.0.0", "::", "127.0.0.1", "2001:db8::1"} {
		raw := strings.Replace(root, `"tag":"static-hy2"`, `"listen":"`+listen+`","tag":"static-hy2"`, 1)
		_, plans, err := parseCustomInbounds([]byte(raw))
		if err != nil || len(plans) != 1 || plans[0].listenIP != listen {
			t.Fatalf("listen %s: plans=%+v, error=%v", listen, plans, err)
		}
	}
}

func TestCustomPortHoppingSwitchesAndValidation(t *testing.T) {
	for _, flags := range []string{
		`"enabled":false,"autoConfigureFirewall":true`,
		`"enabled":true,"autoConfigureFirewall":false`,
		`"autoConfigureFirewall":true`,
	} {
		_, plans, err := parseCustomInbounds([]byte(hoppingInbound(`,"portHopping":{` + flags + `}`)))
		if err != nil || len(plans) != 0 {
			t.Fatalf("disabled switches created plans: %s: %+v %v", flags, plans, err)
		}
	}
	valid := hoppingInbound(`,"portHopping":{"enabled":true,"autoConfigureFirewall":true,"ports":"55001-60000"}`)
	for _, raw := range []string{
		strings.Replace(valid, `"enabled":true`, `"enabled":"true"`, 1),
		strings.Replace(valid, `"autoConfigureFirewall":true`, `"autoConfigureFirewall":"true"`, 1),
		strings.Replace(valid, `"enabled":true`, `"enabled":true,"enable":false`, 1),
		strings.Replace(valid, "55001-60000", "60000-55001", 1),
		strings.Replace(valid, "55001-60000", "65536", 1),
		strings.Replace(valid, `"port":55444`, `"port":"55444-55445"`, 1),
		strings.Replace(valid, `"port":55444`, `"port":0`, 1),
		strings.Replace(valid, `"tag":"static-hy2",`, "", 1),
		strings.Replace(valid, `"protocol":"hysteria"`, `"protocol":"socks"`, 1),
		strings.Replace(valid, `"network":"hysteria"`, `"network":"tcp"`, 1),
		strings.Replace(valid, `"tag":"static-hy2"`, `"listen":"example.invalid","tag":"static-hy2"`, 1),
		strings.Replace(valid, `"brutalDown":"0"`, `"brutalDown":0`, 1),
		valid[:len(valid)-1] + `,{"tag":"static-hy2","port":1081,"protocol":"socks"}]`,
	} {
		if _, _, err := parseCustomInbounds([]byte(raw)); err == nil {
			t.Fatal("invalid/conflicting configuration was accepted")
		}
	}
}

func TestCustomServerStripsClientUDPHopOnly(t *testing.T) {
	raw := strings.Replace(hoppingInbound(""), `"quicParams":`, `"udp":[{"type":"salamander","settings":{"password":"example-obfs"}},{"type":"udphop","settings":{"mode":"intervalremote","remotePorts":"55001-60000"}}],"quicParams":`, 1)
	inbounds, plans, err := parseCustomInbounds([]byte(raw))
	if err != nil || len(plans) != 0 {
		t.Fatalf("parse failed: %v", err)
	}
	masks := inbounds[0].StreamSetting.FinalMask.Udp
	if len(masks) != 1 || masks[0].Type != "salamander" {
		t.Fatalf("wrong server masks: %+v", masks)
	}
	if _, err := inbounds[0].Build(); err != nil {
		t.Fatal(err)
	}
}

func TestCustomTopLevelHoppingOverridesLegacy(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		raw := hoppingInbound(`,"portHopping":{"enabled":` + enabled + `,"autoConfigureFirewall":true,"ports":"50000-50100"}`)
		raw = strings.Replace(raw, `"brutalDown":"0"`, `"brutalDown":"0","udpHop":{"enable":true,"autoConfigureFirewall":true,"ports":"52000-52100"}`, 1)
		_, plans, err := parseCustomInbounds([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if enabled == "true" {
			if len(plans) != 1 || plans[0].ports != "50000-50100" {
				t.Fatalf("wrong precedence: %+v", plans)
			}
		} else if len(plans) != 0 {
			t.Fatalf("disabled top-level fell back to legacy: %+v", plans)
		}
	}
}

type testHoppingFirewall struct {
	plan    customHoppingPlan
	fail    bool
	removed bool
}

func (f *testHoppingFirewall) Apply(ports string, target uint32, tag, listen string) error {
	f.plan = customHoppingPlan{ports: ports, target: target, tag: tag, listenIP: listen}
	if f.fail {
		return errors.New("simulated IPv6 capability failure")
	}
	return nil
}
func (f *testHoppingFirewall) Remove() { f.removed = true }

func TestCustomFirewallRollback(t *testing.T) {
	first, second := &testHoppingFirewall{}, &testHoppingFirewall{fail: true}
	managers := []*testHoppingFirewall{first, second}
	i := 0
	p := &Panel{customHoppingPlans: []customHoppingPlan{{ports: "55001-60000", target: 55444, tag: "custom-inbound:one"}, {ports: "60001-61000", target: 55445, tag: "custom-inbound:two"}}}
	p.newHoppingFirewall = func() hoppingFirewall { f := managers[i]; i++; return f }
	if err := p.applyCustomPortHopping(); err == nil || !first.removed || !second.removed || len(p.customHoppingFirewalls) != 0 {
		t.Fatalf("partial installation was not rolled back: %v", err)
	}
}

func customHoppingTestPanel(t *testing.T) (*Panel, *[]*testHoppingFirewall) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	socket, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := socket.LocalAddr().(*net.UDPAddr).Port
	socket.Close()
	inbound := []map[string]any{{
		"tag": "static-hy2", "listen": "127.0.0.1", "port": port, "protocol": "hysteria",
		"settings": map[string]any{"version": 2, "users": []any{map[string]any{"auth": "example:example-password"}}},
		"streamSettings": map[string]any{"network": "hysteria", "hysteriaSettings": map[string]any{"version": 2}, "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{
			"certificate": strings.Split(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), "\n"),
			"key":         strings.Split(string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), "\n"),
		}}}},
		"portHopping": map[string]any{"enabled": true, "autoConfigureFirewall": true, "ports": "55001-60000"},
	}}
	data, err := json.Marshal(inbound)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "inbounds.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	p := New(&Config{InboundConfigPath: file})
	created := &[]*testHoppingFirewall{}
	p.newHoppingFirewall = func() hoppingFirewall {
		f := &testHoppingFirewall{}
		*created = append(*created, f)
		return f
	}
	return p, created
}

func TestCustomFirewallPanelLifecycle(t *testing.T) {
	p, created := customHoppingTestPanel(t)
	p.Start()
	defer p.Close()
	if len(*created) != 1 || (*created)[0].plan.listenIP != "127.0.0.1" || (*created)[0].plan.tag != "custom-inbound:static-hy2" {
		t.Fatal("Start did not apply the parsed inbound plan")
	}
	p.Start() // Idempotent: do not create additional managers/listeners.
	if len(*created) != 1 {
		t.Fatal("repeated Start added firewall rules")
	}
	p.Close()
	if !(*created)[0].removed || p.Running || p.Server != nil {
		t.Fatal("Close leaked resources")
	}
	p.Close()
	p.Start() // Reload the file and recreate rules after a clean shutdown.
	if len(*created) != 2 || (*created)[1].removed {
		t.Fatal("restart did not reapply firewall rules")
	}
	p.Close()
	data, err := os.ReadFile(p.panelConfig.InboundConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "55001-60000", "60001-61000", 1))
	if err := os.WriteFile(p.panelConfig.InboundConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	p.Start()
	if len(*created) != 3 || (*created)[2].plan.ports != "60001-61000" || !(*created)[1].removed {
		t.Fatal("restart failed to replace the previous port range")
	}
}

func TestCustomFirewallFailureClosesCore(t *testing.T) {
	p, _ := customHoppingTestPanel(t)
	f := &testHoppingFirewall{fail: true}
	p.newHoppingFirewall = func() hoppingFirewall { return f }
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected firewall failure")
			}
		}()
		p.Start()
	}()
	if !f.removed || p.Server != nil || p.Running {
		t.Fatal("firewall failure leaked listener/rules")
	}
}

type failStartService struct{}

func (failStartService) Start() error { return errors.New("simulated panel activation failure") }
func (failStartService) Close() error { return nil }

type failCloseService struct{}

func (failCloseService) Start() error { return nil }
func (failCloseService) Close() error { return errors.New("simulated service close failure") }

func TestPanelCloseFailureStillRemovesFirewall(t *testing.T) {
	p, created := customHoppingTestPanel(t)
	p.Service = []service.Service{failCloseService{}}
	p.Start()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected close failure")
			}
		}()
		p.Close()
	}()
	if p.Server != nil || p.Running || len(*created) != 1 || !(*created)[0].removed {
		t.Fatal("service close failure leaked core/firewall")
	}
}

func TestPanelStartupFailureClosesCore(t *testing.T) {
	p, created := customHoppingTestPanel(t)
	p.Service = []service.Service{failStartService{}}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected startup failure")
			}
		}()
		p.Start()
	}()
	if p.Server != nil || p.Running || len(p.customHoppingFirewalls) != 0 || len(*created) != 1 || !(*created)[0].removed {
		t.Fatal("failed startup left resources active")
	}
	p.Close() // Safe after a failed startup.
}
