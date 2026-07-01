package config

import "testing"

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
