package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func (s *Settings) WriteSingBoxConfig(subYAML []byte) error {
	cfg, err := s.SingBoxConfig(subYAML)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.SingBoxDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.ConfigPath(), data, 0o644)
}

func (s *Settings) SingBoxConfig(subYAML []byte) (map[string]any, error) {
	root := map[string]any{}
	if len(subYAML) > 0 {
		if err := yaml.Unmarshal(subYAML, &root); err != nil {
			return nil, fmt.Errorf("parse subscription yaml: %w", err)
		}
	}
	if err := s.applyLocalNodes(root); err != nil {
		return nil, err
	}

	outbounds := []map[string]any{
		{"type": "direct", "tag": "DIRECT"},
		{"type": "block", "tag": "REJECT"},
	}
	proxyTags := map[string]bool{"DIRECT": true, "REJECT": true}
	for _, proxy := range mapSlice(root["proxies"]) {
		outbound, ok := clashProxyToSingBox(proxy)
		if !ok {
			continue
		}
		outbounds = append(outbounds, outbound)
		proxyTags[str(outbound["tag"])] = true
	}

	rawGroups := mapSlice(root["proxy-groups"])
	candidateGroupTags := map[string]bool{}
	for _, group := range rawGroups {
		if name := str(group["name"]); name != "" {
			candidateGroupTags[name] = true
		}
	}

	convertedGroups := make([]map[string]any, 0, len(rawGroups))
	convertedGroupTags := map[string]bool{}
	for _, group := range rawGroups {
		outbound, ok := clashGroupToSingBox(group, proxyTags, candidateGroupTags)
		if !ok {
			continue
		}
		convertedGroups = append(convertedGroups, outbound)
		convertedGroupTags[str(outbound["tag"])] = true
	}

	groupNames := []string{}
	validRefs := map[string]bool{}
	for tag := range proxyTags {
		validRefs[tag] = true
	}
	for tag := range convertedGroupTags {
		validRefs[tag] = true
	}
	for _, outbound := range convertedGroups {
		filtered := filterOutboundRefs(anyStringList(outbound["outbounds"]), validRefs, str(outbound["tag"]))
		if len(filtered) == 0 {
			continue
		}
		outbound["outbounds"] = filtered
		groupNames = append(groupNames, str(outbound["tag"]))
		outbounds = append(outbounds, outbound)
	}
	final := "DIRECT"
	if len(groupNames) > 0 {
		final = groupNames[0]
	}

	route := map[string]any{
		"rules": []map[string]any{
			{"action": "sniff"},
			{"protocol": "dns", "action": "hijack-dns"},
			{"ip_is_private": true, "outbound": "DIRECT"},
		},
		"default_domain_resolver": "local",
		"auto_detect_interface":   s.TunEnable,
		"final":                   final,
	}

	return map[string]any{
		"log": map[string]any{
			"level": s.LogLevel,
		},
		"inbounds":  singBoxInbounds(s),
		"outbounds": outbounds,
		"dns":       singBoxDNS(final),
		"route":     route,
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": s.Controller,
				"secret":              s.Secret,
				"default_mode":        s.Mode,
			},
			"cache_file": map[string]any{
				"enabled":      true,
				"path":         "cache.db",
				"store_fakeip": true,
			},
		},
	}, nil
}

func singBoxDNS(final string) map[string]any {
	remote := map[string]any{
		"type":   "https",
		"tag":    "remote",
		"server": "cloudflare-dns.com",
	}
	if final != "DIRECT" {
		remote["detour"] = final
	}
	return map[string]any{
		"servers": []map[string]any{
			remote,
			{
				"type":   "udp",
				"tag":    "local",
				"server": "223.5.5.5",
			},
		},
		"final":    "remote",
		"strategy": "ipv4_only",
	}
}

func singBoxInbounds(s *Settings) []map[string]any {
	inbounds := []map[string]any{{
		"type":        "mixed",
		"tag":         "mixed-in",
		"listen":      "127.0.0.1",
		"listen_port": s.MixedPort,
	}}
	if s.TunEnable {
		inbounds = append(inbounds, map[string]any{
			"type":           "tun",
			"tag":            "tun-in",
			"interface_name": s.TunDevice,
			"address":        []string{s.TunAddress},
			"stack":          "system",
			"auto_route":     true,
			"auto_redirect":  true,
			"strict_route":   true,
			"mtu":            9000,
		})
	}
	return inbounds
}

func clashGroupToSingBox(group map[string]any, proxyTags, groupTags map[string]bool) (map[string]any, bool) {
	name := str(group["name"])
	if name == "" {
		return nil, false
	}
	groupType := strings.ToLower(str(group["type"]))
	validRefs := map[string]bool{}
	for tag := range proxyTags {
		validRefs[tag] = true
	}
	for tag := range groupTags {
		validRefs[tag] = true
	}
	outbounds := filterOutboundRefs(stringList(group["proxies"]), validRefs, name)
	if len(outbounds) == 0 {
		return nil, false
	}
	switch groupType {
	case "select", "selector":
		return map[string]any{"type": "selector", "tag": name, "outbounds": outbounds}, true
	case "url-test", "urltest", "fallback":
		url := str(group["url"])
		if url == "" {
			url = "https://www.gstatic.com/generate_204"
		}
		interval := str(group["interval"])
		if interval == "" {
			interval = "10m"
		}
		return map[string]any{"type": "urltest", "tag": name, "outbounds": outbounds, "url": url, "interval": interval}, true
	default:
		return map[string]any{"type": "selector", "tag": name, "outbounds": outbounds}, true
	}
}

func filterOutboundRefs(refs []string, valid map[string]bool, self string) []string {
	out := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref == "" || ref == self || !valid[ref] || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

func clashProxyToSingBox(proxy map[string]any) (map[string]any, bool) {
	name := str(proxy["name"])
	proxyType := strings.ToLower(str(proxy["type"]))
	if name == "" || proxyType == "" {
		return nil, false
	}
	out := map[string]any{"type": proxyType, "tag": name}
	copyString(out, proxy, "server")
	copyInt(out, proxy, "server_port", "port")
	copyTLS(out, proxy)
	copyTransport(out, proxy)
	switch proxyType {
	case "ss", "shadowsocks":
		out["type"] = "shadowsocks"
		copyStringAs(out, "method", proxy, "method", "cipher")
		copyString(out, proxy, "password")
	case "vmess":
		copyString(out, proxy, "uuid")
		copyStringAs(out, "security", proxy, "security", "cipher")
		copyInt(out, proxy, "alter_id", "alterId")
	case "vless":
		copyString(out, proxy, "uuid")
		copyString(out, proxy, "flow")
	case "trojan":
		copyString(out, proxy, "password")
	case "hysteria2", "hy2":
		out["type"] = "hysteria2"
		copyString(out, proxy, "password")
	case "socks", "socks5":
		out["type"] = "socks"
		copyString(out, proxy, "username")
		copyString(out, proxy, "password")
	case "http":
		copyString(out, proxy, "username")
		copyString(out, proxy, "password")
	default:
		return nil, false
	}
	return out, true
}

func copyTLS(dst, src map[string]any) {
	enabled, ok := boolValue(src["tls"])
	if !ok || !enabled {
		return
	}
	tls := map[string]any{"enabled": true}
	copyStringAs(tls, "server_name", src, "servername", "sni")
	if insecure, ok := boolValue(src["skip-cert-verify"]); ok {
		tls["insecure"] = insecure
	}
	if fingerprint := str(src["client-fingerprint"]); fingerprint != "" {
		tls["utls"] = map[string]any{
			"enabled":     true,
			"fingerprint": fingerprint,
		}
	}
	dst["tls"] = tls
}

func copyTransport(dst, src map[string]any) {
	switch strings.ToLower(str(src["network"])) {
	case "ws", "websocket":
		transport := map[string]any{"type": "ws"}
		if opts, ok := src["ws-opts"].(map[string]any); ok {
			copyString(transport, opts, "path")
			if headers, ok := opts["headers"].(map[string]any); ok && len(headers) > 0 {
				transport["headers"] = headers
			}
		}
		dst["transport"] = transport
	case "grpc":
		transport := map[string]any{"type": "grpc"}
		if opts, ok := src["grpc-opts"].(map[string]any); ok {
			copyStringAs(transport, "service_name", opts, "grpc-service-name", "serviceName")
		}
		dst["transport"] = transport
	}
}

func mapSlice(v any) []map[string]any {
	var out []map[string]any
	for _, item := range anySlice(v) {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func anySlice(v any) []any {
	switch vv := v.(type) {
	case []any:
		return vv
	default:
		return nil
	}
}

func stringList(v any) []string {
	if refs, ok := v.([]string); ok {
		return refs
	}
	var out []string
	for _, item := range anySlice(v) {
		if s := str(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func anyStringList(v any) []string {
	if refs, ok := v.([]string); ok {
		return refs
	}
	return stringList(v)
}

func str(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case fmt.Stringer:
		return vv.String()
	default:
		return ""
	}
}

func copyString(dst, src map[string]any, keys ...string) {
	for _, key := range keys {
		if v := str(src[key]); v != "" {
			dst[key] = v
			return
		}
	}
}

func copyStringAs(dst map[string]any, dstKey string, src map[string]any, keys ...string) {
	for _, key := range keys {
		if v := str(src[key]); v != "" {
			dst[dstKey] = v
			return
		}
	}
}

func copyInt(dst, src map[string]any, dstKey, srcKey string) {
	switch v := src[srcKey].(type) {
	case int:
		dst[dstKey] = v
	case int64:
		dst[dstKey] = v
	case float64:
		dst[dstKey] = int(v)
	}
}

func boolValue(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}
