package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeConfigAnyTLSPassesInstalledChecker(t *testing.T) {
	bin, err := exec.LookPath("mihomo")
	if err != nil {
		t.Skip("mihomo not installed")
	}
	dir := t.TempDir()
	s := &Settings{Controller: "127.0.0.1:19091", Secret: "test", MixedPort: 17890, Mode: "rule", LogLevel: "info"}
	sub := []byte("proxies:\n  - {name: node, type: anytls, server: example.com, port: 443, password: pass, tls: true, servername: edge.example.com, skip-cert-verify: true, tfo: true, udp: true}\nproxy-groups:\n  - {name: PROXY, type: select, proxies: [node]}\nrules:\n  - MATCH,PROXY\n")
	config, err := s.MergeConfig(sub)
	if err != nil {
		t.Fatalf("MergeConfig: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, config, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), bin, "-t", "-d", dir, "-f", path).CombinedOutput()
	if err != nil {
		t.Fatalf("mihomo check failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}
}

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

func TestMergeConfigStripsForeignListeners(t *testing.T) {
	// Providers generate standalone configs with their own listeners; passing
	// them through would bind ports owned by a coexisting clash instance.
	sub := []byte(`
port: 6152
socks-port: 6153
mixed-port: 8899
external-ui: ui
dns:
  enable: true
  listen: 0.0.0.0:1053
  nameserver: [8.8.8.8]
proxies: []
`)
	s := &Settings{Controller: "127.0.0.1:9091", Secret: "x", MixedPort: 7890, Mode: "rule", LogLevel: "info"}
	out, err := s.MergeConfig(sub)
	if err != nil {
		t.Fatalf("MergeConfig: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("not valid yaml: %v", err)
	}
	for _, k := range []string{"port", "socks-port", "redir-port", "tproxy-port", "external-ui"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s leaked through from subscription", k)
		}
	}
	if got["mixed-port"] != 7890 {
		t.Errorf("mixed-port = %v, want 7890", got["mixed-port"])
	}
	dns, ok := got["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns block lost: %v", got["dns"])
	}
	if _, ok := dns["listen"]; ok {
		t.Errorf("dns.listen leaked through from subscription")
	}
	if dns["enable"] != true {
		t.Errorf("dns.enable not preserved")
	}
}

func TestMergeConfigAddsLocalNodesWithoutReplacingRules(t *testing.T) {
	sub := []byte(`
proxies:
  - {name: n1, type: ss, server: example.com, port: 443}
proxy-groups:
  - {name: PROXY, type: select, proxies: [n1]}
rules:
  - DOMAIN-SUFFIX,example.org,PROXY
  - MATCH,DIRECT
`)
	s := &Settings{
		Controller: "127.0.0.1:9091",
		Secret:     "x",
		MixedPort:  7890,
		Mode:       "rule",
		LogLevel:   "info",
		LocalNodes: []LocalNode{{
			Name: "local-ss",
			Link: "ss://YWVzLTI1Ni1nY206cGFzcw@local.example:8388#ignored",
		}},
	}
	out, err := s.MergeConfig(sub)
	if err != nil {
		t.Fatalf("MergeConfig: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("not valid yaml: %v", err)
	}
	if proxies := got["proxies"].([]any); len(proxies) != 2 {
		t.Fatalf("proxies len = %d, want subscription + local", len(proxies))
	}
	groups := got["proxy-groups"].([]any)
	proxyGroup := findYAMLGroup(groups, "PROXY")
	if proxyGroup == nil {
		t.Fatalf("PROXY group missing: %#v", groups)
	}
	if !stringListContains(stringList(proxyGroup["proxies"]), LocalGroupName) {
		t.Fatalf("PROXY group does not include Local: %#v", proxyGroup["proxies"])
	}
	localGroup := findYAMLGroup(groups, LocalGroupName)
	if localGroup == nil {
		t.Fatalf("Local group missing: %#v", groups)
	}
	if got := stringList(localGroup["proxies"]); len(got) != 1 || got[0] != "local-ss" {
		t.Fatalf("Local proxies = %#v, want local-ss", got)
	}
	if rules := got["rules"].([]any); len(rules) != 2 {
		t.Fatalf("rules len = %d, want original rules preserved", len(rules))
	}
}

func TestMergeConfigLocalOnlyAddsMatchRule(t *testing.T) {
	s := &Settings{
		Controller: "127.0.0.1:9091",
		Secret:     "x",
		MixedPort:  7890,
		Mode:       "rule",
		LogLevel:   "info",
		LocalNodes: []LocalNode{{
			Name: "local-ss",
			Link: "ss://YWVzLTI1Ni1nY206cGFzcw@local.example:8388#ignored",
		}},
	}
	out, err := s.MergeConfig(nil)
	if err != nil {
		t.Fatalf("MergeConfig: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("not valid yaml: %v", err)
	}
	rules := got["rules"].([]any)
	if len(rules) != 1 || rules[0] != "MATCH,Local" {
		t.Fatalf("rules = %#v, want MATCH,Local", rules)
	}
}

func findYAMLGroup(groups []any, name string) map[string]any {
	for _, item := range groups {
		group, ok := item.(map[string]any)
		if ok && group["name"] == name {
			return group
		}
	}
	return nil
}

func stringListContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
