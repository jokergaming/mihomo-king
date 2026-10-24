package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeUsesSharedGeoWithoutReplacingSubscriptionRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "GeoIP.dat"), []byte("dat"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geoip.metadb"), []byte("mmdb"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := []byte("rules:\n  - GEOIP,CN,DIRECT\n  - MATCH,PROXY\n")
	s := &Settings{MihomoDir: dir, Controller: "127.0.0.1:9091", Secret: "x", MixedPort: 7890, Mode: "rule", LogLevel: "info"}

	out, err := s.MergeConfig(sub)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["geodata-mode"] != true || got["geo-auto-update"] != false {
		t.Fatalf("shared dat was not selected: %#v", got)
	}
	rules, _ := got["rules"].([]any)
	if len(rules) != 2 || rules[0] != "GEOIP,CN,DIRECT" {
		t.Fatalf("rules changed: %#v", rules)
	}
}

func TestMergeKeepsSubscriptionGeoSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "GeoIP.dat"), []byte("dat"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := []byte("geox-url:\n  geoip: https://provider.example/geoip.dat\nrules:\n  - GEOIP,CN,DIRECT\n")
	s := &Settings{MihomoDir: dir, Controller: "127.0.0.1:9091", Secret: "x", MixedPort: 7890, Mode: "rule", LogLevel: "info"}
	out, err := s.MergeConfig(sub)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["geo-auto-update"]; ok {
		t.Fatalf("subscription geo source was overridden: %#v", got["geox-url"])
	}
	urls, _ := got["geox-url"].(map[string]any)
	if urls["geoip"] != "https://provider.example/geoip.dat" {
		t.Fatalf("geox-url = %#v", got["geox-url"])
	}
}

func TestSingBoxMixinAddsLocalRuleSets(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"geoip-cn.srs", "geoip-private.srs"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SRS"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &Settings{ManagedTool: ToolSingBox, SingBoxDir: dir, Controller: "127.0.0.1:9091", Secret: "x", MixedPort: 7890, Mode: "rule", LogLevel: "info"}
	cfg, err := s.SingBoxConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	route := cfg["route"].(map[string]any)
	sets, _ := route["rule_set"].([]map[string]any)
	if len(sets) != 2 || sets[0]["type"] != "local" || sets[0]["path"] != filepath.Join(dir, "geoip-cn.srs") {
		t.Fatalf("rule_set = %#v", route["rule_set"])
	}
	rules, _ := route["rules"].([]map[string]any)
	if len(rules) != 3 {
		t.Fatalf("mixin added routing rules: %#v", rules)
	}
}
