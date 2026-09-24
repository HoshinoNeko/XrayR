package sspanel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HoshinoNeko/XrayR/api"
)

func TestTrafficMultipleFailuresDoNotDoubleBill(t *testing.T) {
	phase, billed := 0, int64(0)
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Data []UserTraffic `json:"data"`
			ID   string        `json:"report_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		amount := p.Data[0].Upload
		if phase == 0 || (phase == 1 && amount == 50) {
			fmt.Fprint(w, `{"ret":0}`)
			return
		}
		if !seen[p.ID] {
			billed += amount
			seen[p.ID] = true
		}
		fmt.Fprint(w, `{"ret":1}`)
	}))
	defer server.Close()
	client := New(&api.Config{APIHost: server.URL})
	for i, n := range []int64{100, 150, 200} {
		phase = i
		err := client.ReportUserTraffic(&[]api.UserTraffic{{UID: 1, Upload: n}})
		if (i < 2) != (err != nil) {
			t.Fatalf("phase %d: %v", i, err)
		}
	}
	if billed != 200 {
		t.Fatalf("billed %d, want 200", billed)
	}
}

func TestNodeConfigPortForms(t *testing.T) {
	for _, port := range []string{`443`, `"443"`} {
		client := New(&api.Config{})
		node, err := client.ParseSSPanelNodeInfo(&NodeInfoResponse{Sort: 15, CustomConfig: json.RawMessage(fmt.Sprintf(`{"offset_port_node":%s,"hysteria2":{"version":2}}`, port))})
		if err != nil || node.Port != 443 {
			t.Fatalf("%s: node=%v err=%v", port, node, err)
		}
	}
	for _, port := range []string{`0`, `65536`, `-1`, `443.5`, `null`} {
		var config CustomConfig
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"offset_port_node":%s}`, port)), &config); err == nil {
			t.Errorf("accepted invalid port %s", port)
		}
	}
}

func TestNodeAndUsersAlwaysRefetchedForRecovery(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-None-Match") != "" {
			t.Error("conditional request could hide unapplied state")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"unchanged"`)
		if r.URL.Path == "/mod_mu/users" {
			fmt.Fprint(w, `{"ret":1,"data":[]}`)
			return
		}
		fmt.Fprint(w, `{"ret":1,"data":{"sort":15,"version":"2026.9.0","custom_config":{"offset_port_node":443,"hysteria2":{"version":2}}}}`)
	}))
	defer server.Close()
	c := New(&api.Config{APIHost: server.URL})
	for i := 0; i < 2; i++ {
		if _, err := c.GetNodeInfo(); err != nil {
			t.Fatal(err)
		}
		if _, err := c.GetUserList(); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 {
		t.Fatal(calls)
	}
}
