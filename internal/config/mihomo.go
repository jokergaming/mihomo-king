package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// MergeConfig parses a subscription's YAML and overlays the keys this tool owns
// (external-controller, secret, ports, mode, log-level, tun). The subscription
// supplies proxies / proxy-groups / rules; the managed keys are always enforced
// so the running config has a reachable API and the desired TUN state.
func (s *Settings) MergeConfig(subYAML []byte) ([]byte, error) {
	root := map[string]any{}
	if len(subYAML) > 0 {
		if err := yaml.Unmarshal(subYAML, &root); err != nil {
			return nil, fmt.Errorf("parse subscription yaml: %w", err)
		}
	}

	root["external-controller"] = s.Controller
	root["secret"] = s.Secret
	root["mixed-port"] = s.MixedPort
	root["allow-lan"] = true
	root["mode"] = s.Mode
	root["log-level"] = s.LogLevel
	root["tun"] = tunBlock(s.TunEnable, s.TunDevice)

	out, err := yaml.Marshal(root)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// WriteActiveConfig merges subYAML and writes mihomo's runtime config.yaml.
func (s *Settings) WriteActiveConfig(subYAML []byte) error {
	merged, err := s.MergeConfig(subYAML)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.MihomoDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.ConfigPath(), merged, 0o644)
}

// WriteActiveFromStore writes the runtime config from the active subscription's
// stored body (or a proxy-less config if no subscription is active).
func (s *Settings) WriteActiveFromStore() error {
	body, err := s.ActiveYAML()
	if err != nil {
		return err
	}
	return s.WriteActiveConfig(body)
}

func tunBlock(enable bool, device string) map[string]any {
	return map[string]any{
		"enable":                enable,
		"device":                device,
		"stack":                 "system",
		"auto-route":            true,
		"auto-detect-interface": true,
		"strict-route":          true,
		"dns-hijack":            []string{"any:53", "tcp://any:53"},
		"mtu":                   9000,
	}
}
