package config

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSingBoxConfigInjectsRuntimeAndConvertsGroups(t *testing.T) {
	sub := []byte(`
proxies:
  - {name: n1, type: ss, server: example.com, port: 443, cipher: 2022-blake3-aes-128-gcm, password: pass}
proxy-groups:
  - {name: PROXY, type: select, proxies: [n1, DIRECT]}
  - {name: AUTO, type: url-test, proxies: [n1], url: http://www.gstatic.com/generate_204}
rules:
  - MATCH,PROXY
`)
	s := &Settings{
		ManagedTool: ToolSingBox,
		Controller:  "127.0.0.1:9091",
		Secret:      "sekret",
		MixedPort:   7890,
		Mode:        "rule",
		LogLevel:    "info",
		TunEnable:   true,
		TunDevice:   "mihomo-king",
		TunAddress:  "172.19.0.1/30",
	}

	cfg, err := s.SingBoxConfig(sub)
	if err != nil {
		t.Fatalf("SingBoxConfig: %v", err)
	}

	experimental := cfg["experimental"].(map[string]any)
	clashAPI := experimental["clash_api"].(map[string]any)
	if clashAPI["external_controller"] != "127.0.0.1:9091" {
		t.Fatalf("external_controller = %v", clashAPI["external_controller"])
	}
	if clashAPI["secret"] != "sekret" {
		t.Fatalf("secret = %v", clashAPI["secret"])
	}

	inbounds := cfg["inbounds"].([]map[string]any)
	if len(inbounds) != 2 {
		t.Fatalf("inbounds len = %d, want mixed + tun", len(inbounds))
	}
	if inbounds[1]["type"] != "tun" || inbounds[1]["interface_name"] != "mihomo-king" {
		t.Fatalf("tun inbound not injected: %#v", inbounds[1])
	}
	if got := inbounds[1]["address"]; !sameStrings(got, []string{"172.19.0.1/30"}) {
		t.Fatalf("tun address = %#v, want 172.19.0.1/30", got)
	}
	if inbounds[1]["auto_redirect"] != true {
		t.Fatalf("tun auto_redirect = %v, want true", inbounds[1]["auto_redirect"])
	}
	if _, ok := inbounds[1]["auto_detect_interface"]; ok {
		t.Fatalf("auto_detect_interface must not be written to tun inbound: %#v", inbounds[1])
	}

	outbounds := cfg["outbounds"].([]map[string]any)
	if !hasOutbound(outbounds, "n1", "shadowsocks") {
		t.Fatalf("converted shadowsocks outbound missing: %#v", outbounds)
	}
	if !hasOutbound(outbounds, "PROXY", "selector") {
		t.Fatalf("selector group missing: %#v", outbounds)
	}
	if !hasOutbound(outbounds, "AUTO", "urltest") {
		t.Fatalf("urltest group missing: %#v", outbounds)
	}

	route := cfg["route"].(map[string]any)
	if route["final"] != "PROXY" {
		t.Fatalf("route.final = %v, want PROXY", route["final"])
	}
	if route["auto_detect_interface"] != true {
		t.Fatalf("route.auto_detect_interface = %v, want true", route["auto_detect_interface"])
	}
	if route["default_domain_resolver"] != "local" {
		t.Fatalf("route.default_domain_resolver = %v, want local", route["default_domain_resolver"])
	}
	rules := route["rules"].([]map[string]any)
	if len(rules) < 2 || rules[0]["action"] != "sniff" || rules[1]["action"] != "hijack-dns" {
		t.Fatalf("route rules do not sniff and hijack dns: %#v", rules)
	}

	dns := cfg["dns"].(map[string]any)
	if dns["final"] != "remote" || dns["strategy"] != "ipv4_only" {
		t.Fatalf("dns defaults not injected: %#v", dns)
	}
	servers := dns["servers"].([]map[string]any)
	if len(servers) != 2 || servers[0]["tag"] != "remote" || servers[0]["detour"] != "PROXY" || servers[1]["tag"] != "local" {
		t.Fatalf("dns servers not injected correctly: %#v", servers)
	}
	if servers[0]["server"] != "cloudflare-dns.com" {
		t.Fatalf("remote dns server = %v, want cloudflare-dns.com", servers[0]["server"])
	}
	if _, ok := servers[1]["detour"]; ok {
		t.Fatalf("local dns server must not detour through DIRECT: %#v", servers[1])
	}
}

func TestSingBoxConfigDropsDanglingGroupRefs(t *testing.T) {
	sub := []byte(`
proxies:
  - {name: n1, type: ss, server: example.com, port: 443, cipher: aes-128-gcm, password: pass}
proxy-groups:
  - {name: 自动选择, type: url-test, proxies: [自动选择, "Expire: 2026-09-26", n1, DIRECT]}
  - {name: 手动选择, type: select, proxies: [自动选择, MissingNode, n1]}
`)
	s := &Settings{
		ManagedTool: ToolSingBox,
		Controller:  "127.0.0.1:9091",
		Secret:      "sekret",
		MixedPort:   7890,
		Mode:        "rule",
		LogLevel:    "info",
	}

	cfg, err := s.SingBoxConfig(sub)
	if err != nil {
		t.Fatalf("SingBoxConfig: %v", err)
	}
	outbounds := cfg["outbounds"].([]map[string]any)
	auto := findOutbound(outbounds, "自动选择")
	if auto == nil {
		t.Fatalf("自动选择 group missing: %#v", outbounds)
	}
	if got := auto["outbounds"]; !sameStrings(got, []string{"n1", "DIRECT"}) {
		t.Fatalf("自动选择 outbounds = %#v, want n1 + DIRECT", got)
	}
	manual := findOutbound(outbounds, "手动选择")
	if manual == nil {
		t.Fatalf("手动选择 group missing: %#v", outbounds)
	}
	if got := manual["outbounds"]; !sameStrings(got, []string{"自动选择", "n1"}) {
		t.Fatalf("手动选择 outbounds = %#v, want 自动选择 + n1", got)
	}
}

func TestSingBoxConfigConvertsTLSWebSocket(t *testing.T) {
	sub := []byte(`
proxies:
  - {name: cf, type: vless, server: 162.159.38.208, port: 2053, uuid: 7e550ef0-9601-4968-8d8e-103fab975c5d, tls: true, skip-cert-verify: false, servername: illmatic94943.pages.dev, client-fingerprint: chrome, network: ws, ws-opts: {path: /, headers: {Host: illmatic94943.pages.dev}}}
proxy-groups:
  - {name: PROXY, type: select, proxies: [cf]}
`)
	s := &Settings{
		ManagedTool: ToolSingBox,
		Controller:  "127.0.0.1:9091",
		Secret:      "sekret",
		MixedPort:   7890,
		Mode:        "rule",
		LogLevel:    "info",
	}

	cfg, err := s.SingBoxConfig(sub)
	if err != nil {
		t.Fatalf("SingBoxConfig: %v", err)
	}
	cf := findOutbound(cfg["outbounds"].([]map[string]any), "cf")
	if cf == nil {
		t.Fatalf("cf outbound missing")
	}
	tls, ok := cf["tls"].(map[string]any)
	if !ok || tls["enabled"] != true || tls["server_name"] != "illmatic94943.pages.dev" {
		t.Fatalf("tls not converted: %#v", cf["tls"])
	}
	transport, ok := cf["transport"].(map[string]any)
	if !ok || transport["type"] != "ws" || transport["path"] != "/" {
		t.Fatalf("ws transport not converted: %#v", cf["transport"])
	}
	headers, ok := transport["headers"].(map[string]any)
	if !ok || headers["Host"] != "illmatic94943.pages.dev" {
		t.Fatalf("ws headers not converted: %#v", transport["headers"])
	}
}

func TestSingBoxConfigIncludesLocalNodes(t *testing.T) {
	sub := []byte(`
proxies:
  - {name: n1, type: ss, server: example.com, port: 443, cipher: aes-128-gcm, password: pass}
proxy-groups:
  - {name: PROXY, type: select, proxies: [n1]}
rules:
  - MATCH,PROXY
`)
	s := &Settings{
		ManagedTool: ToolSingBox,
		Controller:  "127.0.0.1:9091",
		Secret:      "sekret",
		MixedPort:   7890,
		Mode:        "rule",
		LogLevel:    "info",
		LocalNodes: []LocalNode{{
			Name: "local-ss",
			Link: "ss://YWVzLTI1Ni1nY206cGFzcw@local.example:8388#ignored",
		}},
	}
	cfg, err := s.SingBoxConfig(sub)
	if err != nil {
		t.Fatalf("SingBoxConfig: %v", err)
	}
	outbounds := cfg["outbounds"].([]map[string]any)
	if !hasOutbound(outbounds, "local-ss", "shadowsocks") {
		t.Fatalf("local ss outbound missing: %#v", outbounds)
	}
	proxy := findOutbound(outbounds, "PROXY")
	if proxy == nil || !containsString(anyStringList(proxy["outbounds"]), LocalGroupName) {
		t.Fatalf("PROXY does not reference Local: %#v", proxy)
	}
	local := findOutbound(outbounds, LocalGroupName)
	if local == nil || !sameStrings(local["outbounds"], []string{"local-ss"}) {
		t.Fatalf("Local group not converted: %#v", local)
	}
}

func TestSingBoxConfigPreservesLocalHysteria2Options(t *testing.T) {
	s := &Settings{
		ManagedTool: ToolSingBox,
		Controller:  "127.0.0.1:9091",
		Secret:      "sekret",
		MixedPort:   7890,
		Mode:        "rule",
		LogLevel:    "info",
		LocalNodes: []LocalNode{{
			Name: "local-hy2",
			Link: "hy2://secret@example.com:8443?sni=edge.example.com&insecure=1&alpn=h3,h2&obfs=salamander&obfs-password=obfs-pass#ignored",
		}},
	}
	cfg, err := s.SingBoxConfig(nil)
	if err != nil {
		t.Fatalf("SingBoxConfig: %v", err)
	}
	outbound := findOutbound(cfg["outbounds"].([]map[string]any), "local-hy2")
	if outbound == nil || outbound["type"] != "hysteria2" {
		t.Fatalf("local hysteria2 outbound missing: %#v", cfg["outbounds"])
	}
	tls, ok := outbound["tls"].(map[string]any)
	if !ok || tls["enabled"] != true || tls["server_name"] != "edge.example.com" || tls["insecure"] != true {
		t.Fatalf("hysteria2 tls not converted: %#v", outbound["tls"])
	}
	if !sameStrings(tls["alpn"], []string{"h3", "h2"}) {
		t.Fatalf("hysteria2 alpn not converted: %#v", tls["alpn"])
	}
	obfs, ok := outbound["obfs"].(map[string]any)
	if !ok || obfs["type"] != "salamander" || obfs["password"] != "obfs-pass" {
		t.Fatalf("hysteria2 obfs not converted: %#v", outbound["obfs"])
	}
}

func TestSingBoxConfigConvertsReality(t *testing.T) {
	sub := []byte(`
proxies:
  - {name: r1, type: vless, server: example.com, port: 443, uuid: 7e550ef0-9601-4968-8d8e-103fab975c5d, flow: xtls-rprx-vision, tls: true, servername: www.microsoft.com, client-fingerprint: ios, reality-opts: {public-key: PUBKEY, short-id: 6ba85179}}
  - {name: r2, type: vless, server: example.com, port: 443, uuid: 7e550ef0-9601-4968-8d8e-103fab975c5d, tls: true, servername: www.microsoft.com, reality-opts: {public-key: PUBKEY}}
proxy-groups:
  - {name: PROXY, type: select, proxies: [r1, r2]}
`)
	s := &Settings{ManagedTool: ToolSingBox, Controller: "127.0.0.1:9091", Secret: "x", MixedPort: 7890, Mode: "rule", LogLevel: "info"}
	cfg, err := s.SingBoxConfig(sub)
	if err != nil {
		t.Fatalf("SingBoxConfig: %v", err)
	}
	outbounds := cfg["outbounds"].([]map[string]any)
	for tag, fingerprint := range map[string]string{"r1": "ios", "r2": "chrome"} {
		outbound := findOutbound(outbounds, tag)
		if outbound == nil {
			t.Fatalf("outbound %s missing: %#v", tag, outbounds)
		}
		tls := outbound["tls"].(map[string]any)
		reality, ok := tls["reality"].(map[string]any)
		if !ok || reality["enabled"] != true || reality["public_key"] != "PUBKEY" {
			t.Fatalf("%s reality = %#v", tag, tls["reality"])
		}
		utls, ok := tls["utls"].(map[string]any)
		if !ok || utls["enabled"] != true || utls["fingerprint"] != fingerprint {
			t.Fatalf("%s utls = %#v, want fingerprint %s", tag, tls["utls"], fingerprint)
		}
	}
	if reality := findOutbound(outbounds, "r1")["tls"].(map[string]any)["reality"].(map[string]any); reality["short_id"] != "6ba85179" {
		t.Fatalf("r1 short_id = %v, want 6ba85179", reality["short_id"])
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasOutbound(outbounds []map[string]any, tag, typ string) bool {
	for _, outbound := range outbounds {
		if outbound["tag"] == tag && outbound["type"] == typ {
			return true
		}
	}
	return false
}

func findOutbound(outbounds []map[string]any, tag string) map[string]any {
	for _, outbound := range outbounds {
		if outbound["tag"] == tag {
			return outbound
		}
	}
	return nil
}

func sameStrings(got any, want []string) bool {
	gotStrings, ok := got.([]string)
	if !ok || len(gotStrings) != len(want) {
		return false
	}
	for i := range want {
		if gotStrings[i] != want[i] {
			return false
		}
	}
	return true
}

func TestSingBoxConfigPassesInstalledChecker(t *testing.T) {
	bin, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box not installed")
	}
	dir := t.TempDir()
	s := &Settings{
		ManagedTool: ToolSingBox,
		SingBoxDir:  dir,
		Controller:  "127.0.0.1:9091",
		Secret:      "sekret",
		MixedPort:   7890,
		Mode:        "rule",
		LogLevel:    "info",
		TunEnable:   true,
		TunDevice:   "mihomo-king-test",
		TunAddress:  "172.19.0.1/30",
	}
	sub := []byte(`
proxies:
  - {name: n1, type: ss, server: example.com, port: 443, cipher: aes-128-gcm, password: pass}
proxy-groups:
  - {name: PROXY, type: select, proxies: [n1, DIRECT]}
rules:
  - MATCH,PROXY
`)
	if err := s.WriteSingBoxConfig(sub); err != nil {
		t.Fatalf("WriteSingBoxConfig: %v", err)
	}
	out, err := exec.Command(bin, "check", "-D", dir, "-c", s.ConfigPath()).CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}
}
