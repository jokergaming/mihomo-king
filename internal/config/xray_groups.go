package config

import (
	"fmt"
	"mihomo-king/internal/api"

	"gopkg.in/yaml.v3"
)

// XrayGroups parses the active subscription and returns a virtual selector group
// containing all usable proxy nodes, reflecting s.XrayNode as Now.
func (s *Settings) XrayGroups() ([]api.Group, error) {
	subYAML, err := s.ActiveYAML()
	if err != nil {
		return nil, err
	}
	root := map[string]any{}
	if len(subYAML) > 0 {
		if err := yaml.Unmarshal(subYAML, &root); err != nil {
			return nil, fmt.Errorf("parse subscription yaml: %w", err)
		}
	}
	if err := s.applyLocalNodes(root); err != nil {
		return nil, err
	}
	if err := s.applyChain(root); err != nil {
		return nil, err
	}

	rawProxies := mapSlice(root["proxies"])
	var members []string
	for _, proxy := range rawProxies {
		outbound, ok := clashProxyToXray(proxy)
		if !ok {
			continue
		}
		members = append(members, str(outbound["tag"]))
	}

	if len(members) == 0 {
		return nil, nil
	}

	now := members[0]
	if s.XrayNode != "" {
		for _, m := range members {
			if m == s.XrayNode {
				now = m
				break
			}
		}
	}

	group := api.Group{
		Name: "PROXY",
		Type: "Selector",
		Now:  now,
		All:  members,
	}

	return []api.Group{group}, nil
}
