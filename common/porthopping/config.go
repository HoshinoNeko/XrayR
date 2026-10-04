package porthopping

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type Config struct {
	Enabled               bool   `json:"enabled"`
	AutoConfigureFirewall bool   `json:"autoConfigureFirewall"`
	Ports                 string `json:"ports"`
}

// ResolveConfig selects a whole object by precedence, never merging fields.
// An empty/disabled higher-priority object suppresses enabled lower-priority ones.
func ResolveConfig(raw []byte, paths ...[]string) (*Config, error) {
	for _, path := range paths {
		value := json.RawMessage(raw)
		for _, key := range path {
			if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				value = nil
				break
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(value, &object); err != nil {
				return nil, fmt.Errorf("port hopping configuration must be an object: %w", err)
			}
			value = object[key]
		}
		if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		var config struct {
			Enabled *bool  `json:"enabled"`
			Enable  *bool  `json:"enable"`
			Auto    bool   `json:"autoConfigureFirewall"`
			Ports   string `json:"ports"`
		}
		if err := json.Unmarshal(value, &config); err != nil {
			return nil, fmt.Errorf("invalid port hopping configuration: %w", err)
		}
		if config.Enabled != nil && config.Enable != nil && *config.Enabled != *config.Enable {
			return nil, fmt.Errorf("port hopping enabled and enable disagree")
		}
		enabled := config.Enabled != nil && *config.Enabled || config.Enable != nil && *config.Enable
		// Older subscription-only udpHop objects implied enable by listing ports.
		if config.Enabled == nil && config.Enable == nil && path[len(path)-1] == "udpHop" {
			enabled = config.Ports != ""
		}
		return &Config{Enabled: enabled, AutoConfigureFirewall: config.Auto, Ports: config.Ports}, nil
	}
	return nil, nil
}
