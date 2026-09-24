package panel

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"golang.org/x/net/proxy"
)

// Optional local reproduction reads user files but never contacts their panel,
// WARP or TW servers. Only bind ports, TLS certificates and egresses are replaced.
func TestCustomInboundConnectivity(t *testing.T) {
	dir := t.TempDir()
	inbounds := []map[string]any{
		{"listen": "127.0.0.1", "port": 1234, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}},
		{"tag": "socks-warp-in", "listen": "127.0.0.1", "port": 1081, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}},
		{"tag": "socks-tw-in", "listen": "127.0.0.1", "port": 1082, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}},
		{"tag": "hysteria-in", "port": 55444, "protocol": "hysteria", "settings": map[string]any{"version": 2, "users": []any{map[string]any{"auth": "static-test-password", "email": "static-user", "level": 0}}}, "streamSettings": map[string]any{"network": "hysteria", "security": "tls", "hysteriaSettings": map[string]any{"version": 2}}, "sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}}},
	}
	route := json.RawMessage(`{"rules":[{"type":"field","ip":["127.0.0.0/8","10.0.0.0/8"],"outboundTag":"block"},{"type":"field","inboundTag":["socks-warp-in"],"outboundTag":"warp-out"},{"type":"field","inboundTag":["socks-tw-in"],"outboundTag":"tw-out"}]}`)
	if source := os.Getenv("XRAYR_TEST_CONFIG_DIR"); source != "" {
		data, err := os.ReadFile(filepath.Join(source, "custom_inbound.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &inbounds); err != nil {
			t.Fatal(err)
		}
		route, err = os.ReadFile(filepath.Join(source, "route.json"))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("XRAY_LOCATION_ASSET", source)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	ports := make([]int, len(inbounds))
	for i, in := range inbounds {
		if in["protocol"] == "hysteria" {
			socket, e := net.ListenPacket("udp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			ports[i] = socket.LocalAddr().(*net.UDPAddr).Port
			socket.Close()
			stream := in["streamSettings"].(map[string]any)
			stream["tlsSettings"] = map[string]any{"certificates": []any{map[string]any{"certificateFile": certFile, "keyFile": keyFile}}}
		} else {
			socket, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			ports[i] = socket.Addr().(*net.TCPAddr).Port
			socket.Close()
		}
		in["port"], in["listen"] = ports[i], "127.0.0.1"
	}
	outbounds := []any{}
	for _, tag := range []string{"IPv4_out", "warp-out", "tw-out"} {
		tag := tag
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tag) }))
		defer backend.Close()
		outbounds = append(outbounds, map[string]any{"tag": tag, "protocol": "freedom", "settings": map[string]any{"redirect": strings.TrimPrefix(backend.URL, "http://"), "finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.1/32"}}}}})
	}
	outbounds = append(outbounds, map[string]any{"tag": "block", "protocol": "blackhole"})
	writeJSON := func(name string, value any) string {
		t.Helper()
		b, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		p := filepath.Join(dir, name)
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	p := New(&Config{InboundConfigPath: writeJSON("in.json", inbounds), OutboundConfigPath: writeJSON("out.json", outbounds), RouteConfigPath: writeJSON("route.json", route)})
	p.Start()
	defer func() {
		if p.Running {
			p.Close()
		}
	}()
	for i, in := range inbounds {
		t.Run(fmt.Sprintf("%s-%v", in["protocol"], in["tag"]), func(t *testing.T) {
			var dial func(context.Context, string, string) (net.Conn, error)
			expected := "IPv4_out"
			switch in["tag"] {
			case "socks-warp-in":
				expected = "warp-out"
			case "socks-tw-in":
				expected = "tw-out"
			}
			if in["protocol"] == "hysteria" {
				user := in["settings"].(map[string]any)["users"].([]any)[0].(map[string]any)
				raw := fmt.Sprintf(`{"outbounds":[{"protocol":"hysteria","settings":{"version":2,"address":"127.0.0.1","port":%d},"streamSettings":{"network":"hysteria","security":"tls","tlsSettings":{"serverName":"localhost","pinnedPeerCertSha256":"%x"},"hysteriaSettings":{"version":2,"auth":%q}}}]}`, ports[i], sha256.Sum256(der), user["auth"])
				var cfg conf.Config
				if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
					t.Fatal(err)
				}
				built, e := cfg.Build()
				if e != nil {
					t.Fatal(e)
				}
				client, e := core.New(built)
				if e != nil {
					t.Fatal(e)
				}
				defer client.Close()
				if e = client.Start(); e != nil {
					t.Fatal(e)
				}
				dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
					return core.Dial(ctx, client, xnet.TCPDestination(xnet.ParseAddress("1.1.1.1"), 80))
				}
			} else {
				d, e := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", ports[i]), nil, &net.Dialer{Timeout: time.Second})
				if e != nil {
					t.Fatal(e)
				}
				dial = d.(proxy.ContextDialer).DialContext
			}
			tr := &http.Transport{DialContext: dial}
			defer tr.CloseIdleConnections()
			client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
			resp, e := client.Get("http://1.1.1.1/")
			if e != nil {
				t.Fatal(e)
			}
			defer resp.Body.Close()
			body, e := io.ReadAll(resp.Body)
			if e != nil || string(body) != expected {
				t.Fatalf("unexpected route response %q: %v", body, e)
			}
			if in["protocol"] == "socks" {
				if curl, err := exec.LookPath("curl"); err == nil {
					output, err := exec.Command(curl, "--noproxy", "", "--proxy", fmt.Sprintf("socks5h://127.0.0.1:%d", ports[i]), "--max-time", "3", "-sS", "http://1.1.1.1/").CombinedOutput()
					if err != nil || string(output) != expected {
						t.Fatalf("curl route failed: %v %s", err, output)
					}
					output, err = exec.Command(curl, "--noproxy", "", "--proxy", fmt.Sprintf("socks5h://127.0.0.1:%d", ports[i]), "--max-time", "3", "-sS", "http://127.0.0.1/").CombinedOutput()
					if err == nil {
						t.Fatal("private destination unexpectedly allowed")
					}
					t.Logf("private-destination rule rejects request while listener is active: %s", output)
				}
			}
		})
	}
	// Reproduce the listener gap caused by a whole-panel restart, not by routing.
	p.Close()
	for i, in := range inbounds {
		if in["protocol"] != "socks" {
			continue
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ports[i]), time.Second)
		if err == nil {
			conn.Close()
			t.Fatal("listener survived core shutdown")
		}
		t.Logf("closed listener: %v", err)
	}
	p.Start()
	for i, in := range inbounds {
		if in["protocol"] != "socks" {
			continue
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ports[i]), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
}
