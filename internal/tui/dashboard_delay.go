package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
)

type dashboardDelayMsg struct {
	epoch uint64
	node  string
	delay int
	err   error
}

type dashboardDelayTickMsg struct{ epoch uint64 }

func dashboardDelayTickCmd(epoch uint64, after time.Duration) tea.Cmd {
	return tea.Tick(max(0, after), func(time.Time) tea.Msg {
		return dashboardDelayTickMsg{epoch: epoch}
	})
}

func (m *Model) clearDashboardDelays() {
	m.dashboardDelayEpoch++
	m.dashboardDelays = nil
	m.dashboardDelayPending = 0
}

func (m Model) refreshDashboard() (Model, tea.Cmd) {
	m.clearDashboardDelays()
	return m, refreshStatusCmd(m.settings)
}

func (m *Model) beginDashboardDelays() tea.Cmd {
	if !m.settings.DashboardLatencyEnabled || m.screen != screenDashboard || !m.running || m.nodeErr != nil || m.dashboardDelays != nil {
		return nil
	}
	m.dashboardDelays = make(map[string]dashboardDelayMsg)
	m.dashboardDelayNext = time.Now().Add(m.settings.DashboardLatencyInterval)
	return m.scheduleVisibleDelays()
}

func (m *Model) scheduleVisibleDelays() tea.Cmd {
	if m.dashboardDelays == nil {
		return nil
	}
	// Capture settings before commands run; UI edits must not change an in-flight test.
	s := *m.settings
	epoch := m.dashboardDelayEpoch
	client := api.New(s.Controller, s.Secret)
	sem := make(chan struct{}, 4)
	var cmds []tea.Cmd
	for _, route := range m.visibleDashboardNodes() {
		node := route.target
		if _, scheduled := m.dashboardDelays[node]; node == "" || scheduled {
			continue
		}
		m.dashboardDelays[node] = dashboardDelayMsg{}
		m.dashboardDelayPending++
		cmds = append(cmds, func() tea.Msg {
			sem <- struct{}{}
			defer func() { <-sem }()
			var delay int
			var err error
			if s.Tool() == config.ToolXray && node == directTarget {
				delay, err = config.DirectHTTPDelay(s.TestURL, 5*time.Second)
			} else if s.Tool() == config.ToolXray {
				runtimeSettings := s
				runtimeSettings.MixedPort = route.proxyPort
				delay, err = runtimeSettings.XrayActiveDelay(s.TestURL, 5*time.Second)
			} else {
				delay, err = client.Delay(node, s.TestURL, 5000)
			}
			return dashboardDelayMsg{epoch: epoch, node: node, delay: delay, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) dashboardDelayLabel(node string) string {
	if node == "" || !m.settings.DashboardLatencyEnabled {
		return ""
	}
	result, ok := m.dashboardDelays[node]
	if !ok || result.node == "" {
		return "  ·  " + dimStyle.Render(m.tr("latency: testing…"))
	}
	if result.err != nil {
		return "  ·  " + errStyle.Render(m.tr("latency: failed (%s)", errorText(result.err)))
	}
	if result.delay <= 0 {
		return "  ·  " + errStyle.Render(m.tr("latency: unavailable"))
	}
	return "  ·  " + okStyle.Render(m.tr("latency: %d ms", result.delay))
}
