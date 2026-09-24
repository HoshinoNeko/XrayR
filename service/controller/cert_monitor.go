package controller

import (
	"time"

	"github.com/HoshinoNeko/XrayR/common/mylego"
)

// All accesses, including timer callbacks, are serialized by stateMu.
func (c *Controller) needsCertMonitorLocked() bool {
	if !mylego.AutoCertificatesAvailable || c.closed || c.suspended ||
		c.nodeInfo == nil || c.nodeInfo.Disabled || !c.nodeInfo.EnableTLS ||
		c.nodeInfo.EnableREALITY || c.config == nil || c.config.EnableREALITY ||
		c.config.CertConfig == nil {
		return false
	}
	switch c.config.CertConfig.CertMode {
	case "dns", "http", "tls":
		return true
	default:
		return false
	}
}

func (c *Controller) syncCertMonitorLocked() {
	if !c.needsCertMonitorLocked() {
		if c.certTimer != nil {
			c.certTimer.Stop()
			c.certTimer = nil
			c.certGeneration++
		}
		return
	}
	if c.certTimer != nil {
		return
	}
	interval := time.Duration(c.config.UpdatePeriodic) * time.Minute
	if interval <= 0 {
		interval = time.Minute
	}
	c.certGeneration++
	generation := c.certGeneration
	c.certTimer = time.AfterFunc(interval, func() {
		c.stateMu.Lock()
		defer c.stateMu.Unlock()
		// A stopped callback may already be waiting for the lock. Never let it
		// renew or rearm a replacement timer after disable, reload, or Close.
		if generation != c.certGeneration || c.certTimer == nil {
			return
		}
		c.certTimer = nil
		if c.needsCertMonitorLocked() {
			_ = c.certMonitorLocked()
		}
		c.syncCertMonitorLocked()
	})
}
