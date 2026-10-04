package panel

import (
	"encoding/json"
	"fmt"

	"github.com/HoshinoNeko/XrayR/common/porthopping"
	"github.com/xtls/xray-core/infra/conf"
)

type hoppingFirewall interface {
	Apply(ports string, target uint32, tag, listenIP string) error
	Remove()
}

type customHoppingPlan struct {
	ports, tag, listenIP string
	target               uint32
}

// Decode deployment extensions separately: they are not xray-core QUIC fields.
func parseCustomInbounds(data []byte) ([]conf.InboundDetourConfig, []customHoppingPlan, error) {
	var inbounds []conf.InboundDetourConfig
	if err := json.Unmarshal(data, &inbounds); err != nil {
		return nil, nil, err
	}
	var extras []json.RawMessage
	if err := json.Unmarshal(data, &extras); err != nil {
		return nil, nil, err
	}
	var plans []customHoppingPlan
	seenTags := make(map[string]bool)
	for i := range inbounds {
		in := &inbounds[i]
		if in.Tag != "" {
			if seenTags[in.Tag] {
				return nil, nil, fmt.Errorf("custom inbound %d: duplicate tag %q", i, in.Tag)
			}
			seenTags[in.Tag] = true
		}
		hop, err := porthopping.ResolveConfig(extras[i], []string{"portHopping"}, []string{"streamSettings", "finalmask", "quicParams", "udpHop"})
		if err != nil {
			return nil, nil, fmt.Errorf("custom inbound %d: %w", i, err)
		}
		if hop != nil {
			if in.Protocol != "hysteria" {
				return nil, nil, fmt.Errorf("custom inbound %d: port hopping is only supported for hysteria", i)
			}
			if hop.Enabled && hop.AutoConfigureFirewall {
				plan, err := buildCustomHoppingPlan(in, hop)
				if err != nil {
					return nil, nil, fmt.Errorf("custom inbound %d: %w", i, err)
				}
				plans = append(plans, plan)
			}
		}
		// Like panel nodes, a server must never install the client-only udphop mask.
		if in.Protocol == "hysteria" && in.StreamSetting != nil && in.StreamSetting.FinalMask != nil {
			mask := in.StreamSetting.FinalMask
			kept := mask.Udp[:0]
			for _, item := range mask.Udp {
				if item.Type != "udphop" {
					kept = append(kept, item)
				}
			}
			mask.Udp = kept
		}
	}
	return inbounds, plans, nil
}

func buildCustomHoppingPlan(in *conf.InboundDetourConfig, hop *porthopping.Config) (customHoppingPlan, error) {
	if in.StreamSetting == nil || in.StreamSetting.Network == nil || string(*in.StreamSetting.Network) != "hysteria" {
		return customHoppingPlan{}, fmt.Errorf("automatic port hopping requires network=hysteria")
	}
	if in.Tag == "" {
		return customHoppingPlan{}, fmt.Errorf("automatic port hopping requires a non-empty tag")
	}
	if in.PortList == nil || len(in.PortList.Range) != 1 || in.PortList.Range[0].From != in.PortList.Range[0].To || in.PortList.Range[0].From == 0 || in.PortList.Range[0].From > 65535 {
		return customHoppingPlan{}, fmt.Errorf("automatic port hopping requires a single listen port between 1 and 65535")
	}
	if err := porthopping.ValidatePorts(hop.Ports); err != nil {
		return customHoppingPlan{}, err
	}
	listen := ""
	if in.ListenOn != nil {
		if !in.ListenOn.Address.Family().IsIP() {
			return customHoppingPlan{}, fmt.Errorf("automatic port hopping requires an IP listen address")
		}
		listen = in.ListenOn.Address.IP().String()
	}
	return customHoppingPlan{ports: hop.Ports, target: in.PortList.Range[0].From, tag: "custom-inbound:" + in.Tag, listenIP: listen}, nil
}

func (p *Panel) applyCustomPortHopping() error {
	for _, plan := range p.customHoppingPlans {
		var manager hoppingFirewall = porthopping.New()
		if p.newHoppingFirewall != nil {
			manager = p.newHoppingFirewall()
		}
		// Track before Apply so even a partially failed manager is cleaned up.
		p.customHoppingFirewalls = append(p.customHoppingFirewalls, manager)
		if err := manager.Apply(plan.ports, plan.target, plan.tag, plan.listenIP); err != nil {
			p.removeCustomPortHopping()
			return fmt.Errorf("%s firewall setup failed: %w", plan.tag, err)
		}
	}
	return nil
}

func (p *Panel) removeCustomPortHopping() {
	for i := len(p.customHoppingFirewalls) - 1; i >= 0; i-- {
		p.customHoppingFirewalls[i].Remove()
	}
	p.customHoppingFirewalls = nil
}
