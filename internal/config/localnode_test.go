package config

import (
	"encoding/base64"
	"testing"
)

func TestParseVLESSLink(t *testing.T) {
	proxy, err := ParseProxyLink("vless://7e550ef0-9601-4968-8d8e-103fab975c5d@example.com:443?security=tls&sni=edge.example.com&fp=chrome&type=ws&host=cdn.example.com&path=%2Fws#home")
	if err != nil {
		t.Fatalf("ParseProxyLink: %v", err)
	}
	if proxy["name"] != "home" || proxy["type"] != "vless" || proxy["server"] != "example.com" || proxy["port"] != 443 {
		t.Fatalf("basic vless fields not parsed: %#v", proxy)
	}
	if proxy["uuid"] != "7e550ef0-9601-4968-8d8e-103fab975c5d" || proxy["tls"] != true || proxy["servername"] != "edge.example.com" {
		t.Fatalf("vless tls fields not parsed: %#v", proxy)
	}
	wsOpts := proxy["ws-opts"].(map[string]any)
	if wsOpts["path"] != "/ws" {
		t.Fatalf("ws path = %v, want /ws", wsOpts["path"])
	}
	headers := wsOpts["headers"].(map[string]any)
	if headers["Host"] != "cdn.example.com" {
		t.Fatalf("ws host = %v, want cdn.example.com", headers["Host"])
	}
}

func TestParseSSLink(t *testing.T) {
	proxy, err := ParseProxyLink("ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388#ss-home")
	if err != nil {
		t.Fatalf("ParseProxyLink: %v", err)
	}
	if proxy["name"] != "ss-home" || proxy["type"] != "ss" || proxy["server"] != "example.com" || proxy["port"] != 8388 {
		t.Fatalf("basic ss fields not parsed: %#v", proxy)
	}
	if proxy["cipher"] != "aes-256-gcm" || proxy["password"] != "pass" {
		t.Fatalf("ss credentials not parsed: %#v", proxy)
	}
}

func TestParseVMessLink(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{
		"v":"2",
		"ps":"vmess-home",
		"add":"example.com",
		"port":"443",
		"id":"7e550ef0-9601-4968-8d8e-103fab975c5d",
		"aid":"0",
		"scy":"auto",
		"net":"ws",
		"host":"cdn.example.com",
		"path":"/vmess",
		"tls":"tls",
		"sni":"edge.example.com",
		"fp":"chrome"
	}`))
	proxy, err := ParseProxyLink("vmess://" + payload)
	if err != nil {
		t.Fatalf("ParseProxyLink: %v", err)
	}
	if proxy["name"] != "vmess-home" || proxy["type"] != "vmess" || proxy["server"] != "example.com" || proxy["port"] != 443 {
		t.Fatalf("basic vmess fields not parsed: %#v", proxy)
	}
	if proxy["uuid"] != "7e550ef0-9601-4968-8d8e-103fab975c5d" || proxy["alterId"] != 0 || proxy["cipher"] != "auto" {
		t.Fatalf("vmess auth fields not parsed: %#v", proxy)
	}
	if proxy["tls"] != true || proxy["servername"] != "edge.example.com" || proxy["client-fingerprint"] != "chrome" {
		t.Fatalf("vmess tls fields not parsed: %#v", proxy)
	}
	wsOpts := proxy["ws-opts"].(map[string]any)
	if wsOpts["path"] != "/vmess" {
		t.Fatalf("vmess ws path = %v, want /vmess", wsOpts["path"])
	}
	headers := wsOpts["headers"].(map[string]any)
	if headers["Host"] != "cdn.example.com" {
		t.Fatalf("vmess ws host = %v, want cdn.example.com", headers["Host"])
	}
}

func TestParseTrojanLink(t *testing.T) {
	proxy, err := ParseProxyLink("trojan://3beeff8a-34cb-47ed-a09f-dc3693225304@02ff24.qldeom.xyz:24123/?type=tcp&security=tls&sni=v-av1.douyinvod.com&allowInsecure=1#%F0%9F%87%AD%F0%9F%87%B0%20HK%20%7C%20%E9%A6%99%E6%B8%AF%2002")
	if err != nil {
		t.Fatalf("ParseProxyLink: %v", err)
	}
	if proxy["name"] != "🇭🇰 HK | 香港 02" || proxy["type"] != "trojan" {
		t.Fatalf("trojan identity not parsed: %#v", proxy)
	}
	if proxy["server"] != "02ff24.qldeom.xyz" || proxy["port"] != 24123 {
		t.Fatalf("trojan address not parsed: %#v", proxy)
	}
	if proxy["password"] != "3beeff8a-34cb-47ed-a09f-dc3693225304" {
		t.Fatalf("trojan password not parsed: %#v", proxy)
	}
	if proxy["tls"] != true || proxy["servername"] != "v-av1.douyinvod.com" || proxy["skip-cert-verify"] != true {
		t.Fatalf("trojan tls params not parsed: %#v", proxy)
	}
	if _, ok := proxy["network"]; ok {
		t.Fatalf("tcp trojan link should not write network: %#v", proxy)
	}
}

func TestParseHysteria2Link(t *testing.T) {
	proxy, err := ParseProxyLink("hy2://secret@example.com:8443?sni=edge.example.com&insecure=1&alpn=h3,h2&obfs=salamander&obfs-password=obfs-pass#hy2-home")
	if err != nil {
		t.Fatalf("ParseProxyLink: %v", err)
	}
	if proxy["name"] != "hy2-home" || proxy["type"] != "hysteria2" || proxy["server"] != "example.com" || proxy["port"] != 8443 {
		t.Fatalf("basic hysteria2 fields not parsed: %#v", proxy)
	}
	if proxy["password"] != "secret" || proxy["tls"] != true || proxy["sni"] != "edge.example.com" || proxy["skip-cert-verify"] != true {
		t.Fatalf("hysteria2 tls fields not parsed: %#v", proxy)
	}
	alpn := proxy["alpn"].([]string)
	if len(alpn) != 2 || alpn[0] != "h3" || alpn[1] != "h2" {
		t.Fatalf("hysteria2 alpn = %#v, want h3,h2", alpn)
	}
	if proxy["obfs"] != "salamander" || proxy["obfs-password"] != "obfs-pass" {
		t.Fatalf("hysteria2 obfs fields not parsed: %#v", proxy)
	}
}

func TestParseSocksLink(t *testing.T) {
	proxy, err := ParseProxyLink("socks5://user:pass@example.com:1080#socks-home")
	if err != nil {
		t.Fatalf("ParseProxyLink: %v", err)
	}
	if proxy["name"] != "socks-home" || proxy["type"] != "socks5" || proxy["server"] != "example.com" || proxy["port"] != 1080 {
		t.Fatalf("basic socks fields not parsed: %#v", proxy)
	}
	if proxy["username"] != "user" || proxy["password"] != "pass" || proxy["udp"] != true {
		t.Fatalf("socks auth fields not parsed: %#v", proxy)
	}
}

func TestParseHTTPProxyLink(t *testing.T) {
	proxy, err := ParseProxyLink("https://user:pass@example.com:3128?sni=edge.example.com#http-home")
	if err != nil {
		t.Fatalf("ParseProxyLink: %v", err)
	}
	if proxy["name"] != "http-home" || proxy["type"] != "http" || proxy["server"] != "example.com" || proxy["port"] != 3128 {
		t.Fatalf("basic http fields not parsed: %#v", proxy)
	}
	if proxy["username"] != "user" || proxy["password"] != "pass" || proxy["tls"] != true || proxy["servername"] != "edge.example.com" {
		t.Fatalf("http auth/tls fields not parsed: %#v", proxy)
	}
}
