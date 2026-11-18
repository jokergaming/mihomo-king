package tui

import (
	"encoding/json"
	"fmt"
	"os"

	"strings"

	"mihomo-king/internal/api"
)

type dashboardNode struct {
	text
	target    string // outbound to test; empty for blocked or unresolved routes
	proxyPort int    // Xray's runtime mixed port
}

const directTarget = "DIRECT"

// The summary and the scheduler share the same visible routes.
func (m Model) visibleDashboardNodes() []dashboardNode {
	nodes := m.liveNodes
	if !m.dashboardExpanded && len(nodes) > 1 {
		nodes = nodes[:1]
	}
	if !m.settings.DashboardLatencyEnabled || (len(nodes) > 0 && nodes[0].target == directTarget) {
		return nodes
	}
	direct := dashboardNode{text: message("direct connection"), target: directTarget}
	if len(nodes) == 0 {
		return []dashboardNode{direct}
	}
	visible := make([]dashboardNode, 0, len(nodes)+1)
	visible = append(visible, nodes[0], direct)
	return append(visible, nodes[1:]...)
}

func proxyNodeRoutes(proxies map[string]api.Proxy, mode, primary string) []dashboardNode {
	if mode == "direct" {
		return []dashboardNode{{text: message("%s", directTarget), target: directTarget}}
	}
	if mode == "global" {
		return []dashboardNode{proxyNodeRoute(proxies, "GLOBAL")}
	}
	groups := api.OrderedGroups(proxies)
	var routes []dashboardNode
	if primary != "" {
		routes = append(routes, proxyNodeRoute(proxies, primary))
	}
	for _, group := range groups {
		if group.Name != primary {
			routes = append(routes, proxyNodeRoute(proxies, group.Name))
		}
	}
	return routes
}

func singBoxFinal(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var cfg struct{ Route struct{ Final string } }
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse sing-box runtime config: %w", err)
	}
	return cfg.Route.Final, nil
}

func proxyNodeRoute(proxies map[string]api.Proxy, name string) dashboardNode {
	var path []string
	seen := map[string]bool{}
	for {
		path = append(path, name)
		if seen[name] {
			return dashboardNode{text: message("%s (cycle)", strings.Join(path, " → "))}
		}
		seen[name] = true
		p, ok := proxies[name]
		if !ok {
			return dashboardNode{text: message("%s (unavailable)", strings.Join(path, " → "))}
		}
		if strings.EqualFold(p.Type, "LoadBalance") {
			return dashboardNode{text: message("%s (LoadBalance: per connection)", strings.Join(path, " → "))}
		}
		if p.Now != "" {
			name = p.Now
			continue
		}
		if len(p.All) > 0 {
			return dashboardNode{text: message("%s (no selection)", strings.Join(path, " → "))}
		}
		result := strings.Join(path, " → ")
		if p.Type != "" {
			result += " (" + p.Type + ")"
		}
		target := name
		switch strings.ToLower(p.Type) {
		case "direct":
			target = directTarget
		case "reject", "rejectdrop", "pass":
			target = ""
		}
		return dashboardNode{text: message("%s", result), target: target}
	}
}

// Xray has no Clash controller. Read the generated runtime config so a saved
// selection that has not been applied cannot be shown as the active node.
func xrayNodeRoutes(path string) ([]dashboardNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Inbounds []struct {
			Protocol string `json:"protocol"`
			Port     int    `json:"port"`
		} `json:"inbounds"`
		Outbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
		} `json:"outbounds"`
		Routing struct {
			Rules []struct {
				Network     string `json:"network"`
				OutboundTag string `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse xray runtime config: %w", err)
	}
	var proxyPort int
	for _, inbound := range cfg.Inbounds {
		if inbound.Protocol == "mixed" {
			proxyPort = inbound.Port
			break
		}
	}
	for _, rule := range cfg.Routing.Rules {
		if rule.Network != "tcp,udp" {
			continue
		}
		for _, outbound := range cfg.Outbounds {
			if outbound.Tag == rule.OutboundTag {
				target := outbound.Tag
				if outbound.Protocol == "freedom" {
					target = directTarget
				} else if outbound.Protocol == "blackhole" {
					target = ""
				}
				return []dashboardNode{{text: message("%s", outbound.Tag+" ("+outbound.Protocol+")"), target: target, proxyPort: proxyPort}}, nil
			}
		}
		return nil, fmt.Errorf("xray outbound %q is missing", rule.OutboundTag)
	}
	return nil, fmt.Errorf("xray runtime config has no default route")
}
