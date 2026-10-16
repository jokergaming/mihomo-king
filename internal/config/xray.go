package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func (s *Settings) WriteXrayConfig(subYAML []byte) error {
	cfg, err := s.XrayConfig(subYAML)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.XrayDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.ConfigPath(), data, 0o644)
}

func (s *Settings) XrayConfig(subYAML []byte) (map[string]any, error) {
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

	outbounds := []map[string]any{}
	proxyTags := map[string]bool{}

	rawProxies := mapSlice(root["proxies"])
	for _, proxy := range rawProxies {
		outbound, ok := clashProxyToXray(proxy)
		if !ok {
			continue
		}
		tag := str(outbound["tag"])
		outbounds = append(outbounds, outbound)
		proxyTags[tag] = true
	}

	if chain := s.ActiveChain(); chain != nil {
		if !proxyTags[chain.Entry] || !proxyTags[ChainNodeName] {
			return nil, fmt.Errorf("chain nodes must use proxy types supported by xray conversion")
		}
		// For the synthetic ChainNodeName, configure sockopt.dialerProxy in Xray
		for _, ob := range outbounds {
			if str(ob["tag"]) == ChainNodeName {
				stream, ok := ob["streamSettings"].(map[string]any)
				if !ok {
					stream = map[string]any{}
					ob["streamSettings"] = stream
				}
				sockopt, ok := stream["sockopt"].(map[string]any)
				if !ok {
					sockopt = map[string]any{}
					stream["sockopt"] = sockopt
				}
				sockopt["dialerProxy"] = chain.Entry
			}
		}
	}

	// Always provide DIRECT and REJECT outbounds
	outbounds = append(outbounds,
		map[string]any{"tag": "DIRECT", "protocol": "freedom"},
		map[string]any{"tag": "REJECT", "protocol": "blackhole"},
	)

	// Determine active proxy target
	activeTag := "DIRECT"
	if s.XrayNode != "" && proxyTags[s.XrayNode] {
		activeTag = s.XrayNode
	} else if len(outbounds) > 2 {
		// Pick first parsed proxy node
		activeTag = str(outbounds[0]["tag"])
	}

	// Routing rules
	routeRules := []map[string]any{
		// Private / local IPs direct
		{
			"type": "field",
			"ip": []string{
				"127.0.0.0/8",
				"10.0.0.0/8",
				"172.16.0.0/12",
				"192.168.0.0/16",
				"::1/128",
				"fc00::/7",
				"fe80::/10",
			},
			"outboundTag": "DIRECT",
		},
	}

	switch s.Mode {
	case "direct":
		routeRules = append(routeRules, map[string]any{
			"type":        "field",
			"network":     "tcp,udp",
			"outboundTag": "DIRECT",
		})
	case "global":
		// Override direct rule: send everything to activeTag
		routeRules = []map[string]any{
			{
				"type":        "field",
				"network":     "tcp,udp",
				"outboundTag": activeTag,
			},
		}
	default: // "rule"
		routeRules = append(routeRules, map[string]any{
			"type":        "field",
			"network":     "tcp,udp",
			"outboundTag": activeTag,
		})
	}

	return map[string]any{
		"log": map[string]any{
			"loglevel": s.LogLevel,
		},
		"inbounds":  xrayInbounds(s),
		"outbounds": outbounds,
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          routeRules,
		},
	}, nil
}

func xrayInbounds(s *Settings) []map[string]any {
	inbounds := []map[string]any{
		{
			"tag":      "mixed-in",
			"listen":   "127.0.0.1",
			"port":     s.MixedPort,
			"protocol": "mixed",
			"sniffing": map[string]any{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
			},
		},
	}
	if s.TunEnable {
		inbounds = append(inbounds, map[string]any{
			"tag":      "tun-in",
			"protocol": "tun",
			"settings": map[string]any{
				"name": s.TunDevice,
				"mtu":  9000,
			},
		})
	}
	return inbounds
}

func clashProxyToXray(proxy map[string]any) (map[string]any, bool) {
	name := str(proxy["name"])
	proxyType := strings.ToLower(str(proxy["type"]))
	server := str(proxy["server"])
	port := intValue(proxy["port"])
	if name == "" || proxyType == "" || server == "" || port == 0 {
		return nil, false
	}

	out := map[string]any{
		"tag": name,
	}

	stream := map[string]any{}
	copyXrayTransport(stream, proxy)
	copyXrayTLS(stream, proxy)
	if len(stream) > 0 {
		out["streamSettings"] = stream
	}

	switch proxyType {
	case "vless":
		uuid := str(proxy["uuid"])
		if uuid == "" {
			return nil, false
		}
		user := map[string]any{
			"id":         uuid,
			"encryption": "none",
		}
		if flow := str(proxy["flow"]); flow != "" {
			user["flow"] = flow
		}
		out["protocol"] = "vless"
		out["settings"] = map[string]any{
			"vnext": []map[string]any{
				{
					"address": server,
					"port":    port,
					"users":   []map[string]any{user},
				},
			},
		}
		return out, true

	case "vmess":
		uuid := str(proxy["uuid"])
		if uuid == "" {
			return nil, false
		}
		security := str(proxy["cipher"])
		if security == "" {
			security = "auto"
		}
		alterID := 0
		if aid, ok := proxy["alterId"]; ok {
			alterID = intValue(aid)
		}
		out["protocol"] = "vmess"
		out["settings"] = map[string]any{
			"vnext": []map[string]any{
				{
					"address": server,
					"port":    port,
					"users": []map[string]any{
						{
							"id":       uuid,
							"alterId":  alterID,
							"security": security,
						},
					},
				},
			},
		}
		return out, true

	case "trojan":
		password := str(proxy["password"])
		if password == "" {
			return nil, false
		}
		out["protocol"] = "trojan"
		out["settings"] = map[string]any{
			"servers": []map[string]any{
				{
					"address":  server,
					"port":     port,
					"password": password,
				},
			},
		}
		return out, true

	case "ss", "shadowsocks":
		password := str(proxy["password"])
		cipher := str(proxy["cipher"])
		if cipher == "" {
			cipher = str(proxy["method"])
		}
		if password == "" || cipher == "" {
			return nil, false
		}
		out["protocol"] = "shadowsocks"
		out["settings"] = map[string]any{
			"servers": []map[string]any{
				{
					"address":  server,
					"port":     port,
					"method":   cipher,
					"password": password,
				},
			},
		}
		return out, true

	case "socks", "socks5":
		serverObj := map[string]any{
			"address": server,
			"port":    port,
		}
		if user := str(proxy["username"]); user != "" {
			serverObj["users"] = []map[string]any{
				{
					"user": user,
					"pass": str(proxy["password"]),
				},
			}
		}
		out["protocol"] = "socks"
		out["settings"] = map[string]any{
			"servers": []map[string]any{serverObj},
		}
		return out, true

	case "http":
		serverObj := map[string]any{
			"address": server,
			"port":    port,
		}
		if user := str(proxy["username"]); user != "" {
			serverObj["users"] = []map[string]any{
				{
					"user": user,
					"pass": str(proxy["password"]),
				},
			}
		}
		out["protocol"] = "http"
		out["settings"] = map[string]any{
			"servers": []map[string]any{serverObj},
		}
		return out, true

	default:
		return nil, false
	}
}

func copyXrayTLS(stream, src map[string]any) {
	reality, isReality := src["reality-opts"].(map[string]any)
	if isReality {
		realitySettings := map[string]any{}
		copyStringAs(realitySettings, "serverName", src, "servername", "sni")
		copyStringAs(realitySettings, "publicKey", reality, "public-key", "publicKey")
		copyStringAs(realitySettings, "shortId", reality, "short-id", "shortId")
		if fp := str(src["client-fingerprint"]); fp != "" {
			realitySettings["fingerprint"] = fp
		}
		stream["security"] = "reality"
		stream["realitySettings"] = realitySettings
		return
	}

	enabled, ok := boolValue(src["tls"])
	if !ok || !enabled {
		return
	}
	tlsSettings := map[string]any{}
	copyStringAs(tlsSettings, "serverName", src, "servername", "sni")
	if insecure, ok := boolValue(src["skip-cert-verify"]); ok {
		tlsSettings["allowInsecure"] = insecure
	}
	if fp := str(src["client-fingerprint"]); fp != "" {
		tlsSettings["fingerprint"] = fp
	}
	if alpn := stringList(src["alpn"]); len(alpn) > 0 {
		tlsSettings["alpn"] = alpn
	}
	stream["security"] = "tls"
	stream["tlsSettings"] = tlsSettings
}

func copyXrayTransport(stream, src map[string]any) {
	network := strings.ToLower(str(src["network"]))
	if network == "" {
		network = "tcp"
	}
	stream["network"] = network
	switch network {
	case "ws", "websocket":
		stream["network"] = "ws"
		wsSettings := map[string]any{}
		if opts, ok := src["ws-opts"].(map[string]any); ok {
			copyString(wsSettings, opts, "path")
			if headers, ok := opts["headers"].(map[string]any); ok && len(headers) > 0 {
				wsSettings["headers"] = headers
			}
		}
		stream["wsSettings"] = wsSettings
	case "grpc":
		grpcSettings := map[string]any{}
		if opts, ok := src["grpc-opts"].(map[string]any); ok {
			copyStringAs(grpcSettings, "serviceName", opts, "grpc-service-name", "serviceName")
		}
		stream["grpcSettings"] = grpcSettings
	case "h2", "http":
		httpSettings := map[string]any{}
		if opts, ok := src["h2-opts"].(map[string]any); ok {
			copyString(httpSettings, opts, "path")
			if host := stringList(opts["host"]); len(host) > 0 {
				httpSettings["host"] = host
			}
		}
		stream["httpSettings"] = httpSettings
	}
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
