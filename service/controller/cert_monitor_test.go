package controller

import (
	"testing"

	"github.com/HoshinoNeko/XrayR/api"
	"github.com/HoshinoNeko/XrayR/common/mylego"
)

func TestCertMonitorLifecycle(t *testing.T) {
	for _, mode := range []string{"none", "file", "", "dns", "http", "tls"} {
		t.Run(mode, func(t *testing.T) {
			c := &Controller{config: &Config{UpdatePeriodic: 3600, CertConfig: &mylego.CertConfig{CertMode: mode}}, nodeInfo: &api.NodeInfo{EnableTLS: true}}
			c.stateMu.Lock()
			defer c.stateMu.Unlock()
			defer func() { c.closed = true; c.syncCertMonitorLocked() }()
			auto := mylego.AutoCertificatesAvailable && (mode == "dns" || mode == "http" || mode == "tls")
			c.syncCertMonitorLocked()
			if (c.certTimer != nil) != auto {
				t.Fatal("incorrect initial timer")
			}
			for _, disable := range []func(){
				func() { c.nodeInfo.EnableTLS = false },
				func() { c.nodeInfo.EnableREALITY = true },
				func() { c.config.EnableREALITY = true },
				func() { c.suspended = true },
				func() { c.nodeInfo.Disabled = true },
				func() { c.config.CertConfig = nil },
			} {
				disable()
				c.syncCertMonitorLocked()
				if c.certTimer != nil {
					t.Fatal("timer survived non-ACME state")
				}
				c.nodeInfo = &api.NodeInfo{EnableTLS: true}
				c.config.EnableREALITY = false
				c.config.CertConfig = &mylego.CertConfig{CertMode: mode}
				c.suspended = false
				c.syncCertMonitorLocked()
				if (c.certTimer != nil) != auto {
					t.Fatal("timer did not follow re-enable")
				}
				timer := c.certTimer
				c.syncCertMonitorLocked()
				if timer != c.certTimer {
					t.Fatal("unchanged polling replaced timer")
				}
			}
			c.closed = true
			c.syncCertMonitorLocked()
			if c.certTimer != nil {
				t.Fatal("timer survived Close")
			}
		})
	}
}
