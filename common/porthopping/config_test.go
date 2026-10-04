package porthopping

import "testing"

func TestResolveConfigPrecedence(t *testing.T) {
	paths := [][]string{{"portHopping"}, {"hysteria2", "portHopping"}, {"hysteria2", "finalmask", "quicParams", "udpHop"}}
	for _, tc := range []struct {
		raw, ports string
		enabled    bool
	}{
		{`{"portHopping":{"enabled":true,"ports":"50000"},"hysteria2":{"portHopping":{"enabled":true,"ports":"51000"},"finalmask":{"quicParams":{"udpHop":{"enable":true,"ports":"52000"}}}}}`, "50000", true},
		{`{"portHopping":{"enabled":false},"hysteria2":{"portHopping":{"enabled":true,"ports":"51000"}}}`, "", false},
		{`{"portHopping":{},"hysteria2":{"portHopping":{"enabled":true,"ports":"51000"}}}`, "", false},
		{`{"hysteria2":{"portHopping":{"enabled":true,"ports":"51000"},"finalmask":{"quicParams":{"udpHop":{"enable":true,"ports":"52000"}}}}}`, "51000", true},
		{`{"hysteria2":{"finalmask":{"quicParams":{"udpHop":{"enable":true,"ports":"52000"}}}}}`, "52000", true},
		{`{"portHopping":{"enabled":true,"ports":"50000"},"hysteria2":{"portHopping":"invalid overridden value"}}`, "50000", true},
	} {
		config, err := ResolveConfig([]byte(tc.raw), paths...)
		if err != nil || config == nil || config.Enabled != tc.enabled || config.Ports != tc.ports {
			t.Fatalf("got %+v %v for %s", config, err, tc.raw)
		}
	}
}
