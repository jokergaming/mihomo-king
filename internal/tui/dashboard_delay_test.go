package tui

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
)

// Run the first (selected route) request from a summary's two-probe batch.
func summaryProbe(t *testing.T, cmd tea.Cmd) dashboardDelayMsg {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("expected selected route and direct probes, got %v", batch)
	}
	return batch[0]().(dashboardDelayMsg)
}

func TestDashboardFoldsOtherRuleProbes(t *testing.T) {
	const url = "https://www.gstatic.com/generate_204"
	var requested []string
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("url") != url {
			t.Errorf("unexpected test URL %q", r.URL.RawQuery)
		}
		requested = append(requested, r.URL.Path)
		fmt.Fprint(w, `{"delay":12}`)
	}))
	defer controller.Close()
	m := Model{settings: &config.Settings{TestURL: url, Controller: controller.URL, DashboardLatencyEnabled: true}}
	proxies := map[string]api.Proxy{
		"GLOBAL": {All: []string{"Main", "Audio", "Video"}},
		"Main":   {Now: "HK", All: []string{"HK"}},
		"Audio":  {Now: "JP", All: []string{"JP"}},
		"Video":  {Now: "JP", All: []string{"JP"}},
		"HK":     {Type: "SS"}, "JP": {Type: "SS"},
	}
	snapshot := statusMsg{running: true, nodes: proxyNodeRoutes(proxies, "rule", "")}
	m, cmd := press(m, snapshot)
	for _, probe := range cmd().(tea.BatchMsg) {
		m, _ = press(m, probe())
	}
	if got := m.viewDashboard(); strings.Contains(got, "Audio") || !strings.Contains(got, "Main → HK") || strings.Count(got, "12 ms") != 2 {
		t.Fatal(got)
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	m, cmd = press(m, enter)
	if cmd == nil {
		t.Fatal("expanding did not start the hidden proxy test")
	}
	m, _ = press(m, cmd())
	if got := m.viewDashboard(); !strings.Contains(got, "Audio → JP") || !strings.Contains(got, "Video → JP") || strings.Count(got, "12 ms") != 4 {
		t.Fatal(got)
	}
	m, cmd = press(m, enter)
	if cmd != nil || strings.Contains(m.viewDashboard(), "Audio") {
		t.Fatal("collapse did not hide the other routes")
	}
	if !slices.Equal(requested, []string{"/proxies/HK/delay", "/proxies/DIRECT/delay", "/proxies/JP/delay"}) {
		t.Fatalf("requests = %v", requested)
	}
	m, _ = m.refreshDashboard()
	m, _ = press(m, snapshot)
	if _, ok := m.dashboardDelays["JP"]; ok {
		t.Fatal("collapsed refresh scheduled a hidden node")
	}
}

func TestDashboardDelayPeriodicRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := Model{settings: &config.Settings{DashboardLatencyEnabled: true, DashboardLatencyInterval: 10 * time.Second}, subs: newList("Subscriptions")}
		snapshot := statusMsg{running: true, nodes: []dashboardNode{
			{text: message("%s", "HK"), target: "HK"},
			{text: message("%s", "JP"), target: "JP"},
		}}
		started := time.Now()
		m, cmd := press(m, snapshot)
		if cmd == nil || m.dashboardDelayPending != 2 {
			t.Fatal("initial test did not start")
		}
		epoch := m.dashboardDelayEpoch
		time.Sleep(2 * time.Second)
		m, cmd = press(m, dashboardDelayMsg{epoch: epoch, node: "HK", delay: 25})
		if cmd != nil {
			t.Fatal("timer scheduled before every node completed")
		}
		m, cmd = press(m, dashboardDelayMsg{epoch: epoch, node: directTarget, err: errors.New("timeout")})
		if cmd == nil {
			t.Fatal("last result did not schedule refresh")
		}
		tick := cmd()
		if elapsed := time.Since(started); elapsed != 10*time.Second {
			t.Fatalf("refresh after %v, want 10s from test start", elapsed)
		}
		m, cmd = press(m, tick)
		if cmd == nil || m.dashboardDelays != nil {
			t.Fatal("timer did not refresh runtime nodes")
		}
		m, cmd = press(m, snapshot)
		if cmd == nil || m.dashboardDelayPending != 2 {
			t.Fatal("timer did not start next test")
		}
		if _, cmd := press(m, tick); cmd != nil {
			t.Fatal("old timer started a duplicate test")
		}
		// A test slower than the interval finishes before the next round starts.
		time.Sleep(12 * time.Second)
		if _, cmd := press(m, snapshot); cmd != nil {
			t.Fatal("status poll overlapped a slow test")
		}
		epoch = m.dashboardDelayEpoch
		m, _ = press(m, dashboardDelayMsg{epoch: epoch, node: "HK", delay: 50})
		m, cmd = press(m, dashboardDelayMsg{epoch: epoch, node: directTarget, delay: 60})
		if cmd == nil {
			t.Fatal("slow test did not schedule next round")
		}
		tick = cmd()
		m, _ = m.switchScreen(screenSubs)
		if _, cmd := press(m, tick); cmd != nil {
			t.Fatal("timer kept testing outside dashboard")
		}
	})
}

func TestDashboardDelayDisabled(t *testing.T) {
	m := Model{settings: &config.Settings{DashboardLatencyInterval: 10 * time.Second}}
	snapshot := statusMsg{running: true, nodes: []dashboardNode{{text: message("%s", "HK"), target: "HK"}}}
	m, cmd := press(m, snapshot)
	if cmd != nil || strings.Contains(m.viewDashboard(), "latency:") {
		t.Fatal("disabled dashboard displayed or tested latency")
	}
	if _, cmd := press(m, dashboardDelayTickMsg{epoch: m.dashboardDelayEpoch}); cmd != nil {
		t.Fatal("disabled dashboard accepted a timer")
	}
	m, _ = m.refreshDashboard()
	if _, cmd := press(m, snapshot); cmd != nil {
		t.Fatal("manual refresh ignored disabled preference")
	}
}

func TestDashboardDelayRefreshLifecycle(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/proxies/HK/one/delay" || r.URL.Query().Get("url") != "https://example.com/check?a=1&b=2" || r.URL.Query().Get("timeout") != "5000" {
			t.Errorf("unexpected delay request: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing delay authentication")
		}
		fmt.Fprint(w, `{"delay":123}`)
	}))
	defer srv.Close()
	proxies := map[string]api.Proxy{
		"Proxy":  {Type: "Selector", Now: "Auto", All: []string{"Auto"}},
		"Auto":   {Type: "URLTest", Now: "HK/one", All: []string{"HK/one"}},
		"HK/one": {Type: "SS"},
	}
	snapshot := statusMsg{running: true, pid: 42, nodes: proxyNodeRoutes(proxies, "rule", "")}
	m := Model{settings: &config.Settings{DashboardLatencyEnabled: true, DashboardLatencyInterval: 10 * time.Second, Controller: srv.URL, Secret: "secret", TestURL: "https://example.com/check?a=1&b=2"}}
	m, cmd := press(m, snapshot)
	if cmd == nil || !strings.Contains(m.viewDashboard(), "latency: testing…") {
		t.Fatal("first dashboard status did not start a delay test")
	}
	result := summaryProbe(t, cmd)
	if result.err != nil || result.delay != 123 || calls.Load() != 1 {
		t.Fatalf("delay test = %+v, requests = %d", result, calls.Load())
	}
	m, _ = press(m, result)
	if got := m.viewDashboard(); strings.Count(got, "latency: 123 ms") != 1 {
		t.Fatalf("delay missing beside shared routes: %s", got)
	}
	if _, cmd := press(m, snapshot); cmd != nil {
		t.Fatal("unchanged background status started another delay test")
	}

	for _, reopen := range []string{"r", "return", "resume"} {
		t.Run(reopen, func(t *testing.T) {
			switch reopen {
			case "return":
				m.screen = screenNodes
				m, cmd = m.switchScreen(screenDashboard)
			case "resume":
				m, cmd = press(m, tea.ResumeMsg{})
			default:
				m, cmd = press(m, keyRunes("r"))
			}
			if cmd == nil || strings.Contains(m.viewDashboard(), "123 ms") {
				t.Fatal("reopening retained the old delay or skipped status refresh")
			}
			m, cmd = press(m, snapshot)
			if cmd == nil {
				t.Fatal("reopening did not retest")
			}
			m, _ = press(m, result)
			if strings.Contains(m.viewDashboard(), "123 ms") {
				t.Fatal("late result from previous visit was accepted")
			}
			m, _ = press(m, summaryProbe(t, cmd))
			if !strings.Contains(m.viewDashboard(), "123 ms") {
				t.Fatal("fresh result was not displayed")
			}
		})
	}

	proxies["Auto"] = api.Proxy{Type: "URLTest", Now: "JP", All: []string{"JP"}}
	proxies["JP"] = api.Proxy{Type: "Trojan"}
	snapshot.nodes = proxyNodeRoutes(proxies, "rule", "")
	m, cmd = press(m, snapshot)
	if cmd == nil || strings.Contains(m.viewDashboard(), "123 ms") {
		t.Fatal("changed selection retained old latency")
	}
	m, _ = press(m, result)
	if strings.Contains(m.viewDashboard(), "123 ms") {
		t.Fatal("old node result was accepted")
	}
	late := dashboardDelayMsg{epoch: m.dashboardDelayEpoch, node: "JP", delay: 99}
	m, cmd = press(m, statusMsg{})
	m, _ = press(m, late)
	if cmd != nil || strings.Contains(m.viewDashboard(), "99 ms") || m.dashboardDelays != nil {
		t.Fatal("stopped core retained latency or started testing")
	}
	m.screen = screenNodes
	if _, cmd := press(m, snapshot); cmd != nil {
		t.Fatal("background status outside dashboard started testing")
	}
}

func TestDashboardDelayFailuresAndLanguage(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		status           int
	}{
		{"timeout", `{"message":"timeout"}`, "检测失败", http.StatusGatewayTimeout},
		{"unauthorized", `{"message":"Unauthorized"}`, "检测失败", http.StatusUnauthorized},
		{"zero", `{"delay":0}`, "不可用", http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()
			m := Model{settings: &config.Settings{DashboardLatencyEnabled: true, DashboardLatencyInterval: 10 * time.Second, Controller: srv.URL}}
			m, cmd := press(m, statusMsg{running: true, nodes: []dashboardNode{{text: message("%s", "HK"), target: "HK"}}})
			result := summaryProbe(t, cmd)
			if tt.status == http.StatusUnauthorized && !errors.Is(result.err, api.ErrUnauthorized) {
				t.Fatalf("lost authorization error: %v", result.err)
			}
			// A language switch while the request runs must affect the result too.
			m.settings.Language = config.LanguageChinese
			if !strings.Contains(m.viewDashboard(), "延迟：检测中") {
				t.Fatal("loading state was not translated")
			}
			m, _ = press(m, result)
			if got := m.viewDashboard(); !strings.Contains(got, "延迟："+tt.want) || strings.Contains(got, "0 ms") {
				t.Fatalf("invalid failure display: %s", got)
			}
		})
	}
}

func TestDashboardReturnKeysResetLatency(t *testing.T) {
	for _, from := range []screen{screenSubs, screenNodes, screenSettings} {
		for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, keyRunes("1")} {
			m := New(&config.Settings{DashboardLatencyEnabled: true, DashboardLatencyInterval: 10 * time.Second})
			m.screen = from
			m.dashboardDelays = map[string]dashboardDelayMsg{"HK": {delay: 123}}
			m, cmd := press(m, key)
			if m.screen != screenDashboard || cmd == nil || m.dashboardDelays != nil || m.dashboardDelayEpoch == 0 {
				t.Fatalf("return from screen %d via %s did not reset latency", from, key.String())
			}
		}
	}
}

func TestDashboardSkipsNonProxyRoutes(t *testing.T) {
	for _, kind := range []string{"Direct", "Reject", "RejectDrop", "Pass", "LoadBalance"} {
		t.Run(kind, func(t *testing.T) {
			proxies := map[string]api.Proxy{"GLOBAL": {Now: "special", All: []string{"special"}}, "special": {Type: kind}}
			m := Model{settings: &config.Settings{DashboardLatencyEnabled: true, DashboardLatencyInterval: 10 * time.Second}}
			m, cmd := press(m, statusMsg{running: true, nodes: proxyNodeRoutes(proxies, "global", "")})
			if _, direct := m.dashboardDelays[directTarget]; cmd == nil || !direct || len(m.dashboardDelays) != 1 {
				t.Fatal("expected only the direct probe")
			}
		})
	}
}

func TestXrayDashboardDelayUsesRuntimePort(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "http://latency.invalid/check" {
			t.Errorf("unexpected proxied URL: %s", r.URL)
		}
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	path := filepath.Join(t.TempDir(), "config.json")
	body := fmt.Sprintf(`{"inbounds":[{"protocol":"mixed","port":%d}],"outbounds":[{"tag":"runtime-node","protocol":"trojan"}],"routing":{"rules":[{"network":"tcp,udp","outboundTag":"runtime-node"}]}}`, port)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	routes, err := xrayNodeRoutes(path)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{settings: &config.Settings{DashboardLatencyEnabled: true, DashboardLatencyInterval: 10 * time.Second, ManagedTool: config.ToolXray, MixedPort: 1, XrayNode: "saved-but-not-active", TestURL: "http://latency.invalid/check"}}
	snapshot := statusMsg{running: true, nodes: routes}
	m, cmd := press(m, snapshot)
	result := summaryProbe(t, cmd)
	if result.node != "runtime-node" || result.err != nil || result.delay <= 0 {
		t.Fatalf("runtime proxy latency: %+v", result)
	}
	m, _ = press(m, result)
	fail.Store(true)
	m, _ = m.refreshDashboard()
	m, cmd = press(m, snapshot)
	result = summaryProbe(t, cmd)
	if result.err == nil || !strings.Contains(result.err.Error(), "502") {
		t.Fatalf("proxy failure was hidden by fallback: %+v", result)
	}
}
