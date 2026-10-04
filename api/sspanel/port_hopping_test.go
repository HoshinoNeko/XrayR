package sspanel

import (
	"encoding/json"
	"testing"

	"github.com/HoshinoNeko/XrayR/api"
)

func TestPanelPortHoppingPrecedence(t *testing.T) {
	for _, tc := range []struct {
		top, nested, legacy map[string]any
		want                string
		enabled             bool
	}{
		{nil, nil, map[string]any{"enable": true, "ports": "52000"}, "52000", true},
		{nil, map[string]any{"enabled": true, "ports": "51000"}, map[string]any{"enable": true, "ports": "52000"}, "51000", true},
		{map[string]any{"enabled": true, "ports": "50000"}, map[string]any{"enabled": true, "ports": "51000"}, map[string]any{"enable": true, "ports": "52000"}, "50000", true},
		{map[string]any{"enabled": false}, map[string]any{"enabled": true, "ports": "51000"}, map[string]any{"enable": true, "ports": "52000"}, "", false},
	} {
		custom := map[string]any{"offset_port_node": "443", "portHopping": tc.top, "hysteria2": map[string]any{"version": 2, "portHopping": tc.nested, "finalmask": map[string]any{"quicParams": map[string]any{"udpHop": tc.legacy}}}}
		raw, _ := json.Marshal(custom)
		node, err := New(&api.Config{NodeType: "V2ray"}).ParseSSPanelNodeInfo(&NodeInfoResponse{Sort: 15, CustomConfig: raw})
		if err != nil {
			t.Fatal(err)
		}
		hop := node.Hysteria2.PortHopping
		if hop == nil || hop.Ports != tc.want || hop.Enabled != tc.enabled {
			t.Fatalf("wrong selected config: %+v", hop)
		}
	}
}
