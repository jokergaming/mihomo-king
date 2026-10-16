package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

func TestXrayConfigConversion(t *testing.T) {
	sub := []byte(`
proxies:
  - {name: vless-node, type: vless, server: 1.2.3.4, port: 443, uuid: 7e550ef0-9601-4968-8d8e-103fab975c5d, flow: xtls-rprx-vision, tls: true, servername: example.com, network: tcp}
  - {name: vmess-node, type: vmess, server: 5.6.7.8, port: 10086, uuid: 7e550ef0-9601-4968-8d8e-103fab975c5d, cipher: auto, network: ws, ws-opts: {path: /ws, headers: {Host: example.com}}}
  - {name: trojan-node, type: trojan, server: 9.10.11.12, port: 443, password: secret, tls: true, servername: example.com}
  - {name: ss-node, type: ss, server: 13.14.15.16, port: 8388, cipher: aes-256-gcm, password: secret}
`)
	s := &Settings{
		ManagedTool: ToolXray,
		MixedPort:   7890,
		Mode:        "rule",
		LogLevel:    "warning",
		XrayNode:    "vless-node",
	}

	cfg, err := s.XrayConfig(sub)
	if err != nil {
		t.Fatalf("XrayConfig: %v", err)
	}

	outbounds := cfg["outbounds"].([]map[string]any)
	if len(outbounds) != 6 { // 4 proxies + DIRECT + REJECT
		t.Fatalf("len(outbounds) = %d, want 6", len(outbounds))
	}

	// Verify against local xray binary if available
	if bin := findXrayBin(); bin != "" {
		if _, err := exec.LookPath(bin); err == nil {
			f, err := os.CreateTemp("", "xray-test-*.json")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(f.Name())

			data, _ := json.MarshalIndent(cfg, "", "  ")
			if err := os.WriteFile(f.Name(), data, 0o644); err != nil {
				t.Fatal(err)
			}

			out, err := exec.Command(bin, "run", "-test", "-c", f.Name()).CombinedOutput()
			if err != nil {
				t.Fatalf("xray validation failed: %v, out:\n%s", err, string(out))
			}
		}
	}
}

func TestXrayConfigChainedProxy(t *testing.T) {
	sub := []byte(`
proxies:
  - {name: entry-node, type: ss, server: 1.1.1.1, port: 8388, cipher: aes-256-gcm, password: pwd1}
  - {name: exit-node, type: trojan, server: 2.2.2.2, port: 443, password: pwd2, tls: true}
proxy-groups:
  - {name: PROXY, type: select, proxies: [entry-node, exit-node]}
`)
	s := &Settings{
		ManagedTool: ToolXray,
		MixedPort:   7890,
		Mode:        "rule",
		LogLevel:    "warning",
		Active:      "mysub",
		Chain: &ProxyChain{
			Subscription: "mysub",
			Entry:        "entry-node",
			Exit:         "exit-node",
		},
	}

	cfg, err := s.XrayConfig(sub)
	if err != nil {
		t.Fatalf("XrayConfig: %v", err)
	}

	outbounds := cfg["outbounds"].([]map[string]any)
	var chainNode map[string]any
	for _, ob := range outbounds {
		if str(ob["tag"]) == ChainNodeName {
			chainNode = ob
			break
		}
	}
	if chainNode == nil {
		t.Fatalf("chain node %q not found in outbounds", ChainNodeName)
	}

	stream := chainNode["streamSettings"].(map[string]any)
	sockopt := stream["sockopt"].(map[string]any)
	if dialerProxy := str(sockopt["dialerProxy"]); dialerProxy != "entry-node" {
		t.Fatalf("sockopt.dialerProxy = %q, want entry-node", dialerProxy)
	}

	// Verify against local xray binary if available
	if bin := findXrayBin(); bin != "" {
		if _, err := exec.LookPath(bin); err == nil {
			f, err := os.CreateTemp("", "xray-test-*.json")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(f.Name())

			data, _ := json.MarshalIndent(cfg, "", "  ")
			if err := os.WriteFile(f.Name(), data, 0o644); err != nil {
				t.Fatal(err)
			}

			out, err := exec.Command(bin, "run", "-test", "-c", f.Name()).CombinedOutput()
			if err != nil {
				t.Fatalf("xray validation failed: %v, out:\n%s", err, string(out))
			}
		}
	}
}
