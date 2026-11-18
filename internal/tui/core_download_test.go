package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"mihomo-king/internal/config"
)

func TestSelectXrayDownloadWhileMihomoRuns(t *testing.T) {
	s := &config.Settings{ManagedTool: config.ToolMihomo, Language: config.LanguageChinese, CoreChannels: map[string]string{config.ToolXray: config.Alpha}}
	m := New(s)
	m.screen, m.running = screenSettings, true
	for i, row := range m.settingsList.Items() {
		if row.(item).title == "下载 xray" {
			m.settingsList.Select(i)
			break
		}
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	m, _ = press(m, enter)
	if !m.editingSetting || m.settingInput.Value() != config.Alpha {
		t.Fatal("Xray download did not open on its configured channel")
	}
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = press(m, enter)
	if m.pendingCore != (coreDownload{tool: config.ToolXray, channel: config.Stable}) || m.statusErr || !m.running || s.Tool() != config.ToolMihomo {
		t.Fatalf("download target: %+v, status: %v", m.pendingCore, m.status)
	}
}
