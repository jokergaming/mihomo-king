package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"mihomo-king/internal/api"
)

// Rule mode can use different nodes for different destinations, so show each
// group's selection rather than inventing a single system-wide active node.
func proxyNodeRoutes(proxies map[string]api.Proxy, mode string) []text {
	if mode == "direct" {
		return []text{message("%s", "DIRECT")}
	}
	if mode == "global" {
		return []text{proxyNodeRoute(proxies, "GLOBAL")}
	}
	var names []string
	for name, p := range proxies {
		if name != "GLOBAL" && len(p.All) > 0 {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	routes := make([]text, 0, len(names))
	for _, name := range names {
		routes = append(routes, proxyNodeRoute(proxies, name))
	}
	return routes
}

func proxyNodeRoute(proxies map[string]api.Proxy, name string) text {
	var path []string
	seen := map[string]bool{}
	for {
		path = append(path, name)
		if seen[name] {
			return message("%s (cycle)", strings.Join(path, " → "))
		}
		seen[name] = true
		p, ok := proxies[name]
		if !ok {
			return message("%s (unavailable)", strings.Join(path, " → "))
		}
		if strings.EqualFold(p.Type, "LoadBalance") {
			return message("%s (LoadBalance: per connection)", strings.Join(path, " → "))
		}
		if p.Now != "" {
			name = p.Now
			continue
		}
		if len(p.All) > 0 {
			return message("%s (no selection)", strings.Join(path, " → "))
		}
		result := strings.Join(path, " → ")
		if p.Type != "" {
			result += " (" + p.Type + ")"
		}
		return message("%s", result)
	}
}

// Xray has no Clash controller. Read the generated runtime config so a saved
// selection that has not been applied cannot be shown as the active node.
func xrayNodeRoutes(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg struct {
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
	for _, rule := range cfg.Routing.Rules {
		if rule.Network != "tcp,udp" {
			continue
		}
		for _, outbound := range cfg.Outbounds {
			if outbound.Tag == rule.OutboundTag {
				return []string{outbound.Tag + " (" + outbound.Protocol + ")"}, nil
			}
		}
		return nil, fmt.Errorf("xray outbound %q is missing", rule.OutboundTag)
	}
	return nil, fmt.Errorf("xray runtime config has no default route")
}
