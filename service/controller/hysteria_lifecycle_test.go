package controller

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HoshinoNeko/XrayR/api"
	"github.com/HoshinoNeko/XrayR/app/mydispatcher"
	_ "github.com/HoshinoNeko/XrayR/cmd/distro/all"
	"github.com/HoshinoNeko/XrayR/common/mylego"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
)

type lifecycleAPI struct {
	api.API
	node    api.NodeInfo
	users   []api.UserInfo
	userErr error
	billed  int64
}

func (a *lifecycleAPI) Describe() api.ClientInfo            { return api.ClientInfo{NodeID: 1} }
func (a *lifecycleAPI) GetNodeInfo() (*api.NodeInfo, error) { n := a.node; return &n, nil }
func (a *lifecycleAPI) GetUserList() (*[]api.UserInfo, error) {
	u := append([]api.UserInfo(nil), a.users...)
	return &u, a.userErr
}
func (a *lifecycleAPI) ReportUserTraffic(u *[]api.UserTraffic) error {
	for _, v := range *u {
		a.billed += v.Upload + v.Download
	}
	return nil
}

func TestHysteriaLifecycleAndTCPForwarding(t *testing.T) {
	for _, mode := range []string{"plain", "salamander", "salamander_udphop"} {
		t.Run(mode, func(t *testing.T) { testHysteriaLifecycle(t, mode, 11, 4096, 16000) })
	}
}

func TestHysteriaBasicConnectivity(t *testing.T) {
	for _, mode := range []string{"plain", "salamander"} {
		t.Run(mode, func(t *testing.T) { testHysteriaLifecycle(t, mode, 11) })
	}
}

func testHysteriaLifecycle(t *testing.T, mode string, sizes ...int) {
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	socket, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := socket.LocalAddr().(*net.UDPAddr).Port
	socket.Close()
	a := &lifecycleAPI{node: api.NodeInfo{NodeType: "Hysteria2", NodeID: 1, Port: uint32(port), TransportProtocol: "hysteria", EnableTLS: true, Hysteria2: &api.Hysteria2Config{Version: 2}}, users: []api.UserInfo{{UID: 1, UUID: "123e4567-e89b-12d3-a456-426614174000"}}}
	maskJSON := `null`
	if mode != "plain" {
		maskJSON = `{"udp":[{"type":"salamander","settings":{"password":"change-this-obfs-password"}}]}`
		if mode == "salamander_udphop" {
			maskJSON = fmt.Sprintf(`{"udp":[{"type":"salamander","settings":{"password":"change-this-obfs-password"}},{"type":"udphop","settings":{"mode":"intervalremote","interval":30,"remotePorts":"%d"}}]}`, port)
		}
		if err := json.Unmarshal([]byte(maskJSON), &a.node.Hysteria2.FinalMask); err != nil {
			t.Fatal(err)
		}
	}
	base := &conf.Config{LogConfig: &conf.LogConfig{LogLevel: "debug"}, Stats: &conf.StatsConfig{}, Policy: &conf.PolicyConfig{Levels: map[uint32]*conf.Policy{0: {StatsUserUplink: true, StatsUserDownlink: true}}}}
	built, err := base.Build()
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range built.App {
		if item.Type == "xray.app.dispatcher.Config" {
			built.App[i] = serial.ToTypedMessage(&mydispatcher.Config{})
		}
	}
	server, err := core.New(built)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	c := New(server, a, &Config{ListenIP: "127.0.0.1", SendIP: "0.0.0.0", UpdatePeriodic: 3600, DisableGetRule: true, CertConfig: &mylego.CertConfig{CertMode: "file", CertFile: certFile, KeyFile: keyFile}}, "SSpanel")
	if err = c.Start(); err != nil {
		t.Fatal(err)
	}
	// Let initial periodic callbacks finish their startup delay before manual ticks.
	time.Sleep(20 * time.Millisecond)
	for _, task := range c.tasks {
		task.Close()
	}
	c.startAt = time.Now().Add(-2 * time.Hour)
	defer c.Close()
	// v26.9.9 blocks private destinations by default. Permit only this test's
	// loopback echo targets; keep the production outbound security policy intact.
	loopbackSettings := json.RawMessage(`{"finalRules":[{"action":"allow","ip":["127.0.0.1/32"]}]}`)
	loopbackOutbound, err := (&conf.OutboundDetourConfig{Tag: c.Tag, Protocol: "freedom", Settings: &loopbackSettings}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.removeOutbound(c.Tag); err != nil {
		t.Fatal(err)
	}
	if err := c.addOutbound(loopbackOutbound); err != nil {
		t.Fatal(err)
	}

	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		conn, e := echo.Accept()
		if e == nil {
			defer conn.Close()
			io.Copy(conn, conn)
		}
	}()
	var clientConfig conf.Config
	raw := fmt.Sprintf(`{"outbounds":[{"protocol":"hysteria","settings":{"version":2,"address":"127.0.0.1","port":%d},"streamSettings":{"network":"hysteria","security":"tls","tlsSettings":{"pinnedPeerCertSha256":"%x","serverName":"localhost"},"finalmask":%s,"hysteriaSettings":{"version":2,"auth":"%s"}}}]}`, port, sha256.Sum256(der), maskJSON, a.users[0].UUID)
	if err = json.Unmarshal([]byte(raw), &clientConfig); err != nil {
		t.Fatal(err)
	}
	clientConfig.LogConfig = &conf.LogConfig{LogLevel: "debug"}
	cb, err := clientConfig.Build()
	if err != nil {
		t.Fatal(err)
	}
	client, err := core.New(cb)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err = client.Start(); err != nil {
		t.Fatal(err)
	}
	dest := xnet.TCPDestination(xnet.ParseAddress("127.0.0.1"), xnet.Port(echo.Addr().(*net.TCPAddr).Port))
	conn, err := core.Dial(context.Background(), client, dest)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	payload := []byte("GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")
	if _, err = conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err = io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if string(got) != string(payload) {
		t.Fatal("payload mismatch")
	}
	udpEcho, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udpEcho.Close()
	receivedDatagrams := make(chan int, 16)
	go func() {
		data := make([]byte, 65535)
		for {
			n, addr, e := udpEcho.ReadFrom(data)
			if e != nil {
				return
			}
			receivedDatagrams <- n
			udpEcho.WriteTo(data[:n], addr)
		}
	}()
	udpDest := xnet.UDPDestination(xnet.ParseAddress("127.0.0.1"), xnet.Port(udpEcho.LocalAddr().(*net.UDPAddr).Port))
	udpConn, err := core.Dial(context.Background(), client, udpDest)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range sizes {
		udpConn.SetDeadline(time.Now().Add(5 * time.Second))
		payload := bytes.Repeat([]byte{byte(size % 251)}, size)
		if _, err = udpConn.Write(payload); err != nil {
			t.Fatal(err)
		}
		udpData := make([]byte, 65535)
		n, err := udpConn.Read(udpData)
		if err != nil || !bytes.Equal(udpData[:n], payload) {
			var echoSizes []int
			for len(receivedDatagrams) > 0 {
				echoSizes = append(echoSizes, <-receivedDatagrams)
			}
			t.Fatalf("UDP echo size %d: received %d bytes, error %v; echo server datagram sizes %v", size, n, err, echoSizes)
		}
	}
	udpConn.Close()
	if err = c.flushTrafficCounters(); err != nil {
		t.Fatal(err)
	}
	if a.billed == 0 {
		t.Fatal("native traffic not counted")
	}
	billed := a.billed
	a.node.Disabled = true
	c.nodeInfoMonitor()
	if !c.suspended {
		t.Fatal("disable failed")
	}
	a.node.Disabled = false
	a.userErr = errors.New("temporary users failure")
	c.nodeInfoMonitor()
	if !c.suspended {
		t.Fatal("failed activation should stay suspended")
	}
	a.userErr = nil
	c.nodeInfoMonitor()
	if c.suspended {
		t.Fatal("recovery failed")
	}
	if err = c.flushTrafficCounters(); err != nil {
		t.Fatal(err)
	}
	if a.billed != billed {
		t.Fatalf("historical traffic rebilled: %d -> %d", billed, a.billed)
	}
	// Invalid new configuration must preserve the current inbound.
	a.node.Hysteria2 = &api.Hysteria2Config{Version: 2, UDPIdleTimeout: 1}
	c.nodeInfoMonitor()
	if c.suspended || c.nodeInfo.Hysteria2.UDPIdleTimeout != 0 {
		t.Fatal("invalid config replaced running node")
	}
	a.node.Hysteria2 = &api.Hysteria2Config{Version: 2}
	// A bind failure happens after preflight; the old node must be restored.
	occupied, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	a.node.Port = uint32(occupied.LocalAddr().(*net.UDPAddr).Port)
	c.nodeInfoMonitor()
	if c.suspended || c.nodeInfo.Port != uint32(port) {
		t.Fatal("failed activation did not roll back to old node")
	}
	a.node.Port = uint32(port)
	a.node.SpeedLimit = 125000
	c.nodeInfoMonitor()
	if c.suspended || c.nodeInfo.SpeedLimit != 125000 {
		t.Fatal("valid config not applied")
	}
	if err = c.flushTrafficCounters(); err != nil {
		t.Fatal(err)
	}
	if a.billed != billed {
		t.Fatalf("reload rebilled counters: %d -> %d", billed, a.billed)
	}
}
