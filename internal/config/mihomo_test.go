package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeConfigInjectsManagedKeysAndPreservesSubscription(t *testing.T) {
	sub := []byte(`
proxies:
  - {name: n1, type: ss, server: example.com, port: 443}
proxy-groups:
  - {name: PROXY, type: select, proxies: [n1]}
rules:
  - MATCH,PROXY
`)
	s := &Settings{
		Controller: "127.0.0.1:9090",
		Secret:     "sekret",
		MixedPort:  7890,
		Mode:       "rule",
		LogLevel:   "info",
		TunEnable:  true,
	}

	out, err := s.MergeConfig(sub)
	if err != nil {
		t.Fatalf("MergeConfig: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("result is not valid yaml: %v", err)
	}

	if got["external-controller"] != "127.0.0.1:9090" {
		t.Errorf("external-controller = %v, want 127.0.0.1:9090", got["external-controller"])
	}
	if got["secret"] != "sekret" {
		t.Errorf("secret = %v, want sekret", got["secret"])
	}
	if tun, ok := got["tun"].(map[string]any); !ok || tun["enable"] != true {
		t.Errorf("tun.enable not true: %v", got["tun"])
	}
	if proxies, ok := got["proxies"].([]any); !ok || len(proxies) != 1 {
		t.Errorf("subscription proxies not preserved: %v", got["proxies"])
	}
	if _, ok := got["rules"].([]any); !ok {
		t.Errorf("subscription rules not preserved: %v", got["rules"])
	}
}

func TestMergeConfigEmptySubscriptionStillValid(t *testing.T) {
	s := &Settings{Controller: "127.0.0.1:9090", Secret: "x", MixedPort: 7890, Mode: "rule", LogLevel: "info"}
	out, err := s.MergeConfig(nil)
	if err != nil {
		t.Fatalf("MergeConfig(nil): %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("not valid yaml: %v", err)
	}
	if got["mode"] != "rule" {
		t.Errorf("mode = %v, want rule", got["mode"])
	}
	if _, ok := got["tun"].(map[string]any); !ok {
		t.Errorf("tun block missing")
	}
}
