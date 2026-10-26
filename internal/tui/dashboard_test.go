package tui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
)

func TestDashboardNodeRoutes(t *testing.T) {
	proxies := map[string]api.Proxy{
		"GLOBAL": {Type: "Selector", Now: "Proxy", All: []string{"Proxy", "HK"}},
		"Proxy":  {Type: "Selector", Now: "Auto", All: []string{"Auto", "HK"}},
		"Auto":   {Type: "URLTest", Now: "HK", All: []string{"HK", "JP"}},
		"HK":     {Type: "Shadowsocks"},
		"JP":     {Type: "Trojan"},
	}
	for _, tt := range []struct {
		mode string
		want []string
	}{
		{"rule", []string{"Auto → HK (Shadowsocks)", "Proxy → Auto → HK (Shadowsocks)"}},
		{"global", []string{"GLOBAL → Proxy → Auto → HK (Shadowsocks)"}},
		{"direct", []string{"DIRECT"}},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			if got := proxyNodeRoutes(proxies, tt.mode); !slices.Equal(got, tt.want) {
				t.Fatalf("routes = %v, want %v", got, tt.want)
			}
		})
	}

	proxies["Auto"] = api.Proxy{Now: "Proxy", All: []string{"Proxy"}}
	if got := proxyNodeRoute(proxies, "Proxy"); got != "Proxy → Auto → Proxy (cycle)" {
		t.Fatalf("cyclic route = %q", got)
	}
	delete(proxies, "Auto")
	if got := proxyNodeRoute(proxies, "Proxy"); got != "Proxy → Auto (unavailable)" {
		t.Fatalf("missing member = %q", got)
	}
	proxies["Auto"] = api.Proxy{Type: "LoadBalance", Now: "HK", All: []string{"HK", "JP"}}
	if got := proxyNodeRoute(proxies, "Proxy"); got != "Proxy → Auto (LoadBalance: per connection)" {
		t.Fatalf("load balancer must not imply a single active node: %q", got)
	}
}

func TestDashboardRefreshTracksRuntimeSelectionAndErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	// Track this test process only for the read-only Running check.
	s.MihomoBin, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.PidFile(), fmt.Appendf(nil, "%d", os.Getpid()), 0o600); err != nil {
		t.Fatal(err)
	}
	s.TunEnable = false
	var node = "HK"
	var unauthorized bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.Secret {
			t.Error("missing controller authentication")
		}
		switch r.URL.Path {
		case "/version":
			fmt.Fprint(w, `{"version":"test"}`)
		case "/configs":
			fmt.Fprint(w, `{"mode":"global"}`)
		case "/proxies":
			if unauthorized {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, `{"proxies":{"GLOBAL":{"type":"Selector","now":%q,"all":["HK","JP"]},"HK":{"type":"SS"},"JP":{"type":"Trojan"}}}`, node)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s.Controller = srv.URL
	m := Model{settings: s}
	refresh := func() {
		t.Helper()
		msg := refreshStatusCmd(s)()
		m, _ = press(m, msg)
	}
	refresh()
	if got := m.viewDashboard(); !strings.Contains(got, "GLOBAL → HK (SS)") {
		t.Fatalf("initial dashboard: %s", got)
	}
	node = "JP"
	refresh()
	if got := m.viewDashboard(); !strings.Contains(got, "GLOBAL → JP (Trojan)") || strings.Contains(got, "GLOBAL → HK") {
		t.Fatalf("stale dashboard after selection: %s", got)
	}
	unauthorized = true
	refresh()
	if !errors.Is(m.nodeErr, api.ErrUnauthorized) || len(m.liveNodes) != 0 {
		t.Fatalf("failed refresh retained old nodes: nodes=%v, err=%v", m.liveNodes, m.nodeErr)
	}
	if got := m.viewDashboard(); !strings.Contains(got, "unavailable:") {
		t.Fatalf("dashboard hides controller failure: %s", got)
	}
	if err := os.Remove(s.PidFile()); err != nil {
		t.Fatal(err)
	}
	refresh()
	if m.running || m.nodeErr != nil || len(m.liveNodes) != 0 {
		t.Fatalf("stopped dashboard retained live state: %+v", m)
	}
}

func TestDashboardRestartFailureAndBusyGuard(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.MihomoBin = filepath.Join(t.TempDir(), "missing-core")
	s.Controller = "127.0.0.1:0"
	m, cmd := press(Model{settings: s}, keyRunes("R"))
	if cmd == nil || !m.coreBusy || m.status != "restarting…" {
		t.Fatalf("restart did not begin: status=%q busy=%v", m.status, m.coreBusy)
	}
	for _, key := range []string{"R", "s", "x", "t"} {
		if _, cmd := press(m, keyRunes(key)); cmd != nil {
			t.Fatalf("%s allowed a competing operation during restart", key)
		}
	}
	m, refresh := press(m, cmd())
	if m.coreBusy || !m.statusErr || !strings.Contains(m.status, "not found") || refresh == nil {
		t.Fatalf("restart failure not exposed or refreshed: status=%q busy=%v", m.status, m.coreBusy)
	}
	if !strings.Contains(m.help(), "R restart") || !strings.Contains(m.help(), "r refresh") {
		t.Fatalf("missing shortcut help: %s", m.help())
	}
}

func TestXrayDashboardUsesRuntimeConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"outbounds":[{"tag":"HK","protocol":"vless","settings":{"password":"hidden"}},{"tag":"JP","protocol":"trojan"}],"routing":{"rules":[{"ip":["127.0.0.0/8"],"outboundTag":"DIRECT"},{"network":"tcp,udp","outboundTag":"JP"}]}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	routes, err := xrayNodeRoutes(path)
	if err != nil || !slices.Equal(routes, []string{"JP (trojan)"}) {
		t.Fatalf("runtime routes = %v, %v", routes, err)
	}
	if err := os.WriteFile(path, []byte(`{"outbounds":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := xrayNodeRoutes(path); err == nil {
		t.Fatal("missing runtime route was hidden")
	}
}
