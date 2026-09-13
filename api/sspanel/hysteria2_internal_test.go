package sspanel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HoshinoNeko/XrayR/api"
)

func TestGetNodeInfoHysteria2Contract(t *testing.T) {
	var capability string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capability = r.Header.Get("X-XrayR-Capabilities")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":1,"data":{"sort":15,"server":"hy.example.com","version":"2026.9.0","enabled":true,"node_speedlimit":100,"custom_config":{"offset_port_node":"443","host":"hy.example.com","allow_insecure":false,"hysteria2":{"version":2,"udpIdleTimeout":60,"finalmask":{"udp":[{"type":"salamander","settings":{"password":"obfs"}}],"quicParams":{"congestion":"bbr"}},"portHopping":{"enabled":true,"autoConfigureFirewall":false,"ports":"20000-20100"}}}}}`))
	}))
	defer server.Close()

	client := New(&api.Config{APIHost: server.URL, NodeID: 7, NodeType: "V2ray"})
	node, err := client.GetNodeInfo()
	if err != nil {
		t.Fatal(err)
	}
	if capability == "" {
		t.Fatal("capability header was not sent")
	}
	if node.NodeType != "Hysteria2" || node.Port != 443 || node.TransportProtocol != "hysteria" {
		t.Fatalf("unexpected node: %#v", node)
	}
	if node.Hysteria2 == nil || node.Hysteria2.PortHopping == nil || node.Hysteria2.PortHopping.Ports != "20000-20100" {
		t.Fatalf("hysteria2 config not preserved: %#v", node.Hysteria2)
	}
}

func TestParseDisabledNode(t *testing.T) {
	enabled := false
	custom, _ := json.Marshal(map[string]any{
		"offset_port_node": "443",
		"hysteria2":        map[string]any{"version": 2},
	})
	client := New(&api.Config{NodeID: 7, NodeType: "V2ray"})
	node, err := client.ParseSSPanelNodeInfo(&NodeInfoResponse{Sort: 15, Enabled: &enabled, CustomConfig: custom})
	if err != nil {
		t.Fatal(err)
	}
	if !node.Disabled {
		t.Fatal("disabled panel state was not preserved")
	}
}

func TestSubtractTrafficAfterIdempotentRetry(t *testing.T) {
	current := []UserTraffic{{UID: 1, Upload: 150, Download: 260}, {UID: 2, Upload: 10}}
	reported := []UserTraffic{{UID: 1, Upload: 100, Download: 200}}
	got := subtractTraffic(current, reported)
	if len(got) != 2 || got[0].Upload != 50 || got[0].Download != 60 || got[1].Upload != 10 {
		t.Fatalf("unexpected residual traffic: %#v", got)
	}
}
