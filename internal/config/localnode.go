package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const LocalGroupName = "Local"

func ParseProxyLink(rawLink string) (map[string]any, error) {
	rawLink = strings.TrimSpace(rawLink)
	if rawLink == "" {
		return nil, fmt.Errorf("node link required")
	}
	u, err := url.Parse(rawLink)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(u.Scheme) {
	case "vmess":
		return parseVMessLink(rawLink, u)
	case "vless":
		return parseVLESSLink(u)
	case "ss":
		return parseSSLink(rawLink, u)
	case "trojan":
		return parseTrojanLink(u)
	case "hysteria2", "hy2":
		return parseHysteria2Link(u)
	case "socks", "socks5":
		return parseSocksLink(u)
	case "http", "https":
		return parseHTTPProxyLink(u)
	default:
		return nil, fmt.Errorf("unsupported node link scheme %q", u.Scheme)
	}
}

func (s *Settings) applyLocalNodes(root map[string]any) error {
	if len(s.LocalNodes) == 0 {
		return nil
	}
	proxies := anySlice(root["proxies"])
	existingNames := map[string]bool{}
	for _, proxy := range proxies {
		if m, ok := proxy.(map[string]any); ok {
			if name := str(m["name"]); name != "" {
				existingNames[name] = true
			}
		}
	}

	localNames := make([]string, 0, len(s.LocalNodes))
	for _, node := range s.LocalNodes {
		proxy, err := ParseProxyLink(node.Link)
		if err != nil {
			return fmt.Errorf("parse local node %s: %w", node.Name, err)
		}
		if node.Name != "" {
			proxy["name"] = node.Name
		}
		name := str(proxy["name"])
		if name == "" {
			return fmt.Errorf("local node has no name")
		}
		if existingNames[name] {
			proxies = dropProxyByName(proxies, name)
		}
		existingNames[name] = true
		proxies = append(proxies, proxy)
		localNames = append(localNames, name)
	}
	root["proxies"] = proxies
	mergeLocalGroup(root, localNames)
	return nil
}

func parseVLESSLink(u *url.URL) (map[string]any, error) {
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("vless link missing uuid")
	}
	host, port, err := splitHostPort(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	proxy := map[string]any{
		"name":   nodeName(u, "vless-"+host),
		"type":   "vless",
		"server": host,
		"port":   port,
		"uuid":   u.User.Username(),
		"udp":    true,
	}
	if flow := q.Get("flow"); flow != "" {
		proxy["flow"] = flow
	}
	applyURLTLS(proxy, q, false)
	applyURLTransport(proxy, q)
	return proxy, nil
}

func parseVMessLink(rawLink string, u *url.URL) (map[string]any, error) {
	payload := strings.TrimPrefix(rawLink, u.Scheme+"://")
	if i := strings.IndexAny(payload, "?#"); i >= 0 {
		payload = payload[:i]
	}
	decoded, err := decodeBase64(payload)
	if err != nil {
		return nil, fmt.Errorf("decode vmess payload: %w", err)
	}
	var vm map[string]any
	if err := json.Unmarshal([]byte(decoded), &vm); err != nil {
		return nil, fmt.Errorf("decode vmess json: %w", err)
	}
	host := firstNonEmpty(str(vm["add"]), str(vm["server"]))
	if host == "" {
		return nil, fmt.Errorf("vmess link missing server")
	}
	port, err := intField(vm, "port")
	if err != nil {
		return nil, fmt.Errorf("vmess port: %w", err)
	}
	uuid := firstNonEmpty(str(vm["id"]), str(vm["uuid"]))
	if uuid == "" {
		return nil, fmt.Errorf("vmess link missing uuid")
	}
	name := firstNonEmpty(nodeName(u, ""), str(vm["ps"]), str(vm["name"]), "vmess-"+host)
	proxy := map[string]any{
		"name":   name,
		"type":   "vmess",
		"server": host,
		"port":   port,
		"uuid":   uuid,
		"udp":    true,
	}
	if aid, err := intField(vm, "aid"); err == nil {
		proxy["alterId"] = aid
	}
	if cipher := firstNonEmpty(str(vm["scy"]), str(vm["cipher"])); cipher != "" {
		proxy["cipher"] = cipher
	}
	tls := strings.ToLower(firstNonEmpty(str(vm["tls"]), str(vm["security"])))
	if tls == "tls" || tls == "true" || tls == "1" {
		proxy["tls"] = true
	}
	if sni := firstNonEmpty(str(vm["sni"]), str(vm["servername"])); sni != "" {
		proxy["servername"] = sni
	}
	if fp := str(vm["fp"]); fp != "" {
		proxy["client-fingerprint"] = fp
	}
	network := strings.ToLower(str(vm["net"]))
	switch network {
	case "ws", "websocket":
		proxy["network"] = "ws"
		wsOpts := map[string]any{}
		if path := str(vm["path"]); path != "" {
			wsOpts["path"] = path
		}
		if host := str(vm["host"]); host != "" {
			wsOpts["headers"] = map[string]any{"Host": host}
		}
		if len(wsOpts) > 0 {
			proxy["ws-opts"] = wsOpts
		}
	case "grpc":
		proxy["network"] = "grpc"
		if service := firstNonEmpty(str(vm["path"]), str(vm["serviceName"]), str(vm["service_name"])); service != "" {
			proxy["grpc-opts"] = map[string]any{"grpc-service-name": service}
		}
	}
	return proxy, nil
}

func parseSSLink(rawLink string, u *url.URL) (map[string]any, error) {
	name := nodeName(u, "")
	method, password, host, port, err := ssParts(rawLink, u)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = "ss-" + host
	}
	return map[string]any{
		"name":     name,
		"type":     "ss",
		"server":   host,
		"port":     port,
		"cipher":   method,
		"password": password,
		"udp":      true,
	}, nil
}

func parseTrojanLink(u *url.URL) (map[string]any, error) {
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("trojan link missing password")
	}
	host, port, err := splitHostPort(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	proxy := map[string]any{
		"name":     nodeName(u, "trojan-"+host),
		"type":     "trojan",
		"server":   host,
		"port":     port,
		"password": u.User.Username(),
		"udp":      true,
	}
	applyURLTLS(proxy, q, true)
	network := strings.ToLower(firstNonEmpty(q.Get("type"), q.Get("network")))
	switch network {
	case "", "tcp":
	default:
		applyURLTransport(proxy, q)
	}
	return proxy, nil
}

func parseHysteria2Link(u *url.URL) (map[string]any, error) {
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("hysteria2 link missing password")
	}
	host, port, err := splitHostPort(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	proxy := map[string]any{
		"name":     nodeName(u, "hy2-"+host),
		"type":     "hysteria2",
		"server":   host,
		"port":     port,
		"password": u.User.Username(),
		"udp":      true,
		"tls":      true,
	}
	if sni := firstNonEmpty(q.Get("sni"), q.Get("servername"), q.Get("peer")); sni != "" {
		proxy["sni"] = sni
		proxy["servername"] = sni
	}
	if queryBool(q, "insecure", "allowInsecure", "skip-cert-verify") {
		proxy["skip-cert-verify"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		proxy["alpn"] = splitCSV(alpn)
	}
	if obfs := q.Get("obfs"); obfs != "" {
		proxy["obfs"] = obfs
	}
	if obfsPassword := firstNonEmpty(q.Get("obfs-password"), q.Get("obfs_password"), q.Get("obfsPassword")); obfsPassword != "" {
		proxy["obfs-password"] = obfsPassword
	}
	return proxy, nil
}

func parseSocksLink(u *url.URL) (map[string]any, error) {
	host, port, err := splitHostPort(u)
	if err != nil {
		return nil, err
	}
	proxy := map[string]any{
		"name":   nodeName(u, "socks-"+host),
		"type":   "socks5",
		"server": host,
		"port":   port,
		"udp":    true,
	}
	applyUserPass(proxy, u)
	return proxy, nil
}

func parseHTTPProxyLink(u *url.URL) (map[string]any, error) {
	host, port, err := splitHostPort(u)
	if err != nil {
		return nil, err
	}
	proxy := map[string]any{
		"name":   nodeName(u, "http-"+host),
		"type":   "http",
		"server": host,
		"port":   port,
	}
	if strings.EqualFold(u.Scheme, "https") {
		proxy["tls"] = true
	}
	applyURLTLS(proxy, u.Query(), false)
	applyUserPass(proxy, u)
	return proxy, nil
}

func applyURLTLS(proxy map[string]any, q url.Values, defaultTLS bool) {
	security := strings.ToLower(firstNonEmpty(q.Get("security"), q.Get("tls")))
	if defaultTLS || security == "tls" || security == "reality" || security == "true" || security == "1" {
		proxy["tls"] = true
	}
	if sni := firstNonEmpty(q.Get("sni"), q.Get("servername"), q.Get("peer")); sni != "" {
		proxy["servername"] = sni
	}
	if fp := q.Get("fp"); fp != "" {
		proxy["client-fingerprint"] = fp
	}
	if queryBool(q, "allowInsecure", "insecure", "skip-cert-verify") {
		proxy["skip-cert-verify"] = true
	}
	if security == "reality" {
		reality := map[string]any{}
		if pbk := q.Get("pbk"); pbk != "" {
			reality["public-key"] = pbk
		}
		if sid := q.Get("sid"); sid != "" {
			reality["short-id"] = sid
		}
		proxy["reality-opts"] = reality
		if proxy["client-fingerprint"] == nil {
			// REALITY runs over uTLS, so it needs a fingerprint even when the link has none.
			proxy["client-fingerprint"] = "chrome"
		}
	}
}

func applyURLTransport(proxy map[string]any, q url.Values) {
	network := strings.ToLower(firstNonEmpty(q.Get("type"), q.Get("network")))
	switch network {
	case "ws", "websocket":
		proxy["network"] = "ws"
		wsOpts := map[string]any{}
		if path := q.Get("path"); path != "" {
			wsOpts["path"] = path
		}
		if host := q.Get("host"); host != "" {
			wsOpts["headers"] = map[string]any{"Host": host}
		}
		if len(wsOpts) > 0 {
			proxy["ws-opts"] = wsOpts
		}
	case "grpc":
		proxy["network"] = "grpc"
		if service := firstNonEmpty(q.Get("serviceName"), q.Get("service_name")); service != "" {
			proxy["grpc-opts"] = map[string]any{"grpc-service-name": service}
		}
	}
}

func applyUserPass(proxy map[string]any, u *url.URL) {
	if u.User == nil {
		return
	}
	if username := u.User.Username(); username != "" {
		proxy["username"] = username
	}
	if password, ok := u.User.Password(); ok {
		proxy["password"] = password
	}
}

func ssParts(rawLink string, u *url.URL) (string, string, string, int, error) {
	if u.Host != "" && u.User != nil {
		host, port, err := splitHostPort(u)
		if err != nil {
			return "", "", "", 0, err
		}
		user := u.User.Username()
		if pass, ok := u.User.Password(); ok {
			return user, pass, host, port, nil
		}
		decoded, err := decodeBase64(user)
		if err != nil {
			return "", "", "", 0, fmt.Errorf("decode ss credentials: %w", err)
		}
		method, password, ok := strings.Cut(decoded, ":")
		if !ok || method == "" || password == "" {
			return "", "", "", 0, fmt.Errorf("ss credentials must be method:password")
		}
		return method, password, host, port, nil
	}

	payload := strings.TrimPrefix(rawLink, "ss://")
	if i := strings.IndexAny(payload, "?#"); i >= 0 {
		payload = payload[:i]
	}
	decoded, err := decodeBase64(payload)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("decode ss payload: %w", err)
	}
	creds, addr, ok := strings.Cut(decoded, "@")
	if !ok {
		return "", "", "", 0, fmt.Errorf("ss payload must contain credentials and host")
	}
	method, password, ok := strings.Cut(creds, ":")
	if !ok || method == "" || password == "" {
		return "", "", "", 0, fmt.Errorf("ss credentials must be method:password")
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("ss address: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("ss port: %w", err)
	}
	return method, password, host, port, nil
}

func splitHostPort(u *url.URL) (string, int, error) {
	host := u.Hostname()
	if host == "" {
		return "", 0, fmt.Errorf("%s link missing server", u.Scheme)
	}
	portText := u.Port()
	if portText == "" {
		return "", 0, fmt.Errorf("%s link missing port", u.Scheme)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, fmt.Errorf("%s port: %w", u.Scheme, err)
	}
	return host, port, nil
}

func nodeName(u *url.URL, fallback string) string {
	if u.Fragment != "" {
		return strings.TrimSpace(u.Fragment)
	}
	if u.RawFragment != "" {
		if name, err := url.QueryUnescape(u.RawFragment); err == nil {
			return strings.TrimSpace(name)
		}
	}
	return fallback
}

func intField(values map[string]any, key string) (int, error) {
	switch v := values[key].(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		port, err := strconv.Atoi(v)
		if err != nil {
			return 0, err
		}
		return port, nil
	default:
		return 0, fmt.Errorf("missing %s", key)
	}
}

func decodeBase64(s string) (string, error) {
	s = strings.TrimSpace(s)
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(s)
		if err == nil {
			return string(decoded), nil
		}
		lastErr = err
	}
	return "", lastErr
}

func queryBool(q url.Values, keys ...string) bool {
	for _, key := range keys {
		value := q.Get(key)
		if value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") {
			return true
		}
	}
	return false
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func mergeLocalGroup(root map[string]any, localNames []string) {
	if len(localNames) == 0 {
		return
	}
	groups := anySlice(root["proxy-groups"])
	hasLocal := false
	for _, item := range groups {
		group, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := str(group["name"])
		if name == LocalGroupName {
			group["type"] = "select"
			group["proxies"] = appendUniqueStrings(stringList(group["proxies"]), localNames...)
			hasLocal = true
			continue
		}
		if isSelectorGroup(group) {
			group["proxies"] = appendUniqueStrings(stringList(group["proxies"]), LocalGroupName)
		}
	}
	if !hasLocal {
		groups = append(groups, map[string]any{
			"name":    LocalGroupName,
			"type":    "select",
			"proxies": localNames,
		})
	}
	root["proxy-groups"] = groups
	if len(anySlice(root["rules"])) == 0 {
		root["rules"] = []string{"MATCH," + LocalGroupName}
	}
}

func dropProxyByName(proxies []any, name string) []any {
	out := proxies[:0]
	for _, proxy := range proxies {
		if m, ok := proxy.(map[string]any); ok && str(m["name"]) == name {
			continue
		}
		out = append(out, proxy)
	}
	return out
}

func isSelectorGroup(group map[string]any) bool {
	switch strings.ToLower(str(group["type"])) {
	case "select", "selector":
		return true
	default:
		return false
	}
}

func appendUniqueStrings(values []string, extra ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values)+len(extra))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	for _, value := range extra {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
