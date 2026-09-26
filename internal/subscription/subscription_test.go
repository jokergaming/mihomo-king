package subscription

import (
	"encoding/base64"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// nodeLinks mixes supported links, a duplicate name, a name that collides with
// the generated group, and an unsupported ssr link.
const nodeLinks = `trojan://pass@t1.example.com:443?sni=edge.example.com#HK%2001
ss://YWVzLTI1Ni1nY206cGFzcw@s.example.com:8388#SS%2001
trojan://pass@t2.example.com:443#HK%2001
ssr://c3NyLmV4YW1wbGUuY29tOjQ0MzpvcmlnaW46YWVzLTI1Ni1jZmI6cGxhaW46Y0dGemN3
trojan://pass@t3.example.com:443#PROXY
`

var nodeNames = []string{"HK 01", "SS 01", "HK 01 2", "PROXY 2"}

func TestFormatUserInfo(t *testing.T) {
	if got := FormatUserInfo(""); got != "" {
		t.Errorf("empty header = %q, want empty", got)
	}
	// 1 GiB total, an expiry timestamp.
	got := FormatUserInfo("upload=0; download=0; total=1073741824; expire=1751328000")
	if !strings.Contains(got, "GiB") {
		t.Errorf("got %q, want a GiB total", got)
	}
	if !strings.Contains(got, "expires") {
		t.Errorf("got %q, want an expiry", got)
	}
}

func TestParseKeepsClashConfig(t *testing.T) {
	body := `proxies:
  - {name: n1, type: ss, server: example.com, port: 443, cipher: aes-128-gcm, password: pass}
proxy-groups:
  - {name: Select, type: select, proxies: [n1]}
rules:
  - MATCH,Select
`
	res, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if string(res.Body) != body {
		t.Fatalf("clash config was rewritten:\n%s", res.Body)
	}
	if res.Proxies != 1 || res.Skipped != nil {
		t.Fatalf("Proxies = %d, Skipped = %v; want 1 proxy, nothing skipped", res.Proxies, res.Skipped)
	}
}

func TestParseBareProxyListAddsGroup(t *testing.T) {
	res, err := Parse([]byte(`proxies:
  - {name: PROXY, type: ss, server: a.example.com, port: 443, cipher: aes-128-gcm, password: pass}
  - {name: n2, type: ss, server: b.example.com, port: 443, cipher: aes-128-gcm, password: pass}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cfg := decodeConfig(t, res.Body)
	group := onlyGroup(t, cfg)
	if group["name"] != "PROXY 2" || !slices.Equal(stringsOf(group["proxies"]), []string{"PROXY", "n2"}) {
		t.Fatalf("group = %#v, want PROXY 2 over both proxies", group)
	}
	if rules := stringsOf(cfg["rules"]); !slices.Equal(rules, []string{"MATCH,PROXY 2"}) {
		t.Fatalf("rules = %#v, want MATCH,PROXY 2", rules)
	}
}

func TestParseBase64NodeList(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(nodeLinks))
	var wrapped strings.Builder // providers often wrap base64 across lines
	for len(encoded) > 76 {
		wrapped.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	wrapped.WriteString(encoded + "\r\n")

	res, err := Parse([]byte(wrapped.String()))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	checkNodeList(t, res)
}

func TestParseURLSafeBase64NodeList(t *testing.T) {
	res, err := Parse([]byte(base64.RawURLEncoding.EncodeToString([]byte(nodeLinks))))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	checkNodeList(t, res)
}

func TestParseBase64AnyTLSNodeList(t *testing.T) {
	links := "anytls://pass@example.com:443?sni=edge.example.com&allowInsecure=1&tfo=1#node-1\nanytls://other@example.org:8443#node-2\n"
	res, err := Parse([]byte(base64.StdEncoding.EncodeToString([]byte(links))))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Proxies != 2 || len(res.Skipped) != 0 {
		t.Fatalf("AnyTLS nodes = %#v", res)
	}
	cfg := decodeConfig(t, res.Body)
	group := onlyGroup(t, cfg)
	if !slices.Equal(stringsOf(group["proxies"]), []string{"node-1", "node-2"}) {
		t.Fatalf("group proxies = %#v", group["proxies"])
	}
}

func TestParsePlainNodeList(t *testing.T) {
	body := "\ufeff# exported nodes\r\n\r\n" + strings.ReplaceAll(nodeLinks, "\n", "\r\n")
	res, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	checkNodeList(t, res)
}

func TestParseBase64ClashConfig(t *testing.T) {
	body := "proxies:\n  - {name: n1, type: ss, server: example.com, port: 443, cipher: aes-128-gcm, password: pass}\nproxy-groups:\n  - {name: Select, type: select, proxies: [n1]}\n"
	res, err := Parse([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if string(res.Body) != body {
		t.Fatalf("decoded clash config = %q, want %q", res.Body, body)
	}
}

func TestParseRejectsUnusableBodies(t *testing.T) {
	tests := []struct {
		name, body, wantErr string
	}{
		{"empty", " \r\n", "empty"},
		{"html page", `<html><body><a href="https://example.com/login">login</a></body></html>`, "unrecognized"},
		{"no proxies", "proxies: []\nrules: []\n", "no proxies"},
		{"broken yaml", "proxies:\n  - {name: n1\n", "parse clash config"},
		{"only unsupported links", "ssr://abc\ntuic://uuid:pass@example.com:443\n", "no supported node links"},
	}
	for _, tt := range tests {
		if _, err := Parse([]byte(tt.body)); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want it to mention %q", tt.name, err, tt.wantErr)
		}
	}
}

func TestDownloadConvertsNodeList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("User-Agent = %q, want %q", got, userAgent)
		}
		w.Header().Set("subscription-userinfo", "upload=1; download=2; total=3")
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(nodeLinks))))
	}))
	defer srv.Close()

	res, err := Download(srv.URL)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.UserInfo != "upload=1; download=2; total=3" {
		t.Fatalf("UserInfo = %q", res.UserInfo)
	}
	checkNodeList(t, res)
}

func TestDownloadKeepsEmptyClashWhenAlternateHasPlaceholders(t *testing.T) {
	const clash = "proxies: []\n"
	const placeholder = "ss://YWVzLTI1Ni1nY206cGFzcw@192.0.2.1:1#No%20nodes\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "total=1024")
		switch r.Header.Get("User-Agent") {
		case userAgent:
			_, _ = w.Write([]byte(clash))
		case nodeListUserAgent:
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(placeholder))))
		default:
			t.Errorf("unexpected User-Agent %q", r.Header.Get("User-Agent"))
		}
	}))
	defer srv.Close()

	res, err := Download(srv.URL)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !res.NoNodes || res.Proxies != 0 || string(res.Body) != clash || res.UserInfo != "total=1024" {
		t.Fatalf("empty subscription = %#v", res)
	}
	path := filepath.Join(t.TempDir(), "sub.yaml")
	if err := Store(path, res); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty subscription created a config file: %v", err)
	}
	previous := &Result{Body: []byte("proxies:\n  - {name: old, type: direct}\n")}
	if err := Store(path, previous); err != nil {
		t.Fatal(err)
	}
	if err := Store(path, res); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != string(previous.Body) {
		t.Fatalf("existing config changed: %q, %v", body, err)
	}
}

func TestStoreUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Store(path, &Result{Body: []byte("new")}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("subscription mode = %#o, want 0600", got)
	}
}

func TestDownloadRetriesUsableNodeList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("User-Agent") {
		case userAgent:
			_, _ = w.Write([]byte("proxies: []\n"))
		case nodeListUserAgent:
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte("ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388#node\n"))))
		default:
			t.Errorf("unexpected User-Agent %q", r.Header.Get("User-Agent"))
		}
	}))
	defer srv.Close()

	res, err := Download(srv.URL)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.NoNodes || !res.Fallback || res.Proxies != 1 {
		t.Fatalf("node-list fallback = %#v", res)
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.txt")
	if err := os.WriteFile(path, []byte(nodeLinks), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	checkNodeList(t, res)

	if _, err := ReadFile(dir); err == nil {
		t.Fatalf("ReadFile(dir) returned nil error")
	}
	if _, err := ReadFile(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatalf("ReadFile(missing) returned nil error")
	}
}

func TestResolvePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ in, want string }{
		{"~/subs/a.yaml", filepath.Join(home, "subs", "a.yaml")},
		{"file:///tmp/my%20sub.yaml", "/tmp/my sub.yaml"},
		{"'/tmp/dropped file.yaml'", "/tmp/dropped file.yaml"},
		{"  sub.yaml  ", filepath.Join(wd, "sub.yaml")},
	}
	for _, tt := range tests {
		got, err := ResolvePath(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ResolvePath(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	if _, err := ResolvePath("  "); err == nil {
		t.Errorf("ResolvePath(blank) returned nil error")
	}
}

func TestIsLocalPath(t *testing.T) {
	for _, s := range []string{"/tmp/a.yaml", "~/a.yaml", "./a.yaml", "../a.yaml", "file:///a.yaml", "'/tmp/a b.yaml'"} {
		if !IsLocalPath(s) {
			t.Errorf("IsLocalPath(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"https://example.com/sub", "http://example.com/sub?token=x", "example.com/sub"} {
		if IsLocalPath(s) {
			t.Errorf("IsLocalPath(%q) = true, want false", s)
		}
	}
}

func TestResultSummary(t *testing.T) {
	tests := []struct {
		res  Result
		want string
	}{
		{Result{Proxies: 42, Skipped: map[string]int{"tuic": 1, "ssr": 2}}, "42 proxies · skipped 3 links (ssr, tuic)"},
		{Result{Proxies: 1}, "1 proxy"},
		{Result{}, ""},
	}
	for _, tt := range tests {
		if got := tt.res.Summary(); got != tt.want {
			t.Errorf("Summary() = %q, want %q", got, tt.want)
		}
	}
}

// checkNodeList verifies the Clash config generated from nodeLinks.
func checkNodeList(t *testing.T, res *Result) {
	t.Helper()
	if res.Proxies != len(nodeNames) || !maps.Equal(res.Skipped, map[string]int{"ssr": 1}) {
		t.Fatalf("Proxies = %d, Skipped = %v; want %d proxies and one skipped ssr link", res.Proxies, res.Skipped, len(nodeNames))
	}
	cfg := decodeConfig(t, res.Body)
	var names []string
	for _, proxy := range cfg["proxies"].([]any) {
		names = append(names, proxy.(map[string]any)["name"].(string))
	}
	if !slices.Equal(names, nodeNames) {
		t.Fatalf("proxy names = %q, want %q", names, nodeNames)
	}
	group := onlyGroup(t, cfg)
	if group["name"] != "PROXY" || group["type"] != "select" || !slices.Equal(stringsOf(group["proxies"]), nodeNames) {
		t.Fatalf("group = %#v, want PROXY selecting every node", group)
	}
	if rules := stringsOf(cfg["rules"]); !slices.Equal(rules, []string{"MATCH,PROXY"}) {
		t.Fatalf("rules = %#v, want MATCH,PROXY", rules)
	}
}

func decodeConfig(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("result is not valid yaml: %v\n%s", err, body)
	}
	return cfg
}

func onlyGroup(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	groups, _ := cfg["proxy-groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("proxy-groups = %#v, want exactly one", cfg["proxy-groups"])
	}
	return groups[0].(map[string]any)
}

func stringsOf(v any) []string {
	var out []string
	for _, item := range v.([]any) {
		out = append(out, item.(string))
	}
	return out
}

func TestDownloadErrorHidesToken(t *testing.T) {
	_, err := Download("http://127.0.0.1:1/sub?token=secret")
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("Download error = %v", err)
	}
}
