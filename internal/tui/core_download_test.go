package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"mihomo-king/internal/config"
)

func TestSelectXrayDownloadWhileMihomoRuns(t *testing.T) {
	s := &config.Settings{ManagedTool: config.ToolMihomo, Language: config.LanguageChinese, CoreChannels: map[string]string{config.ToolXray: config.Alpha}}
	m := New(s)
	m.screen, m.running = screenSettings, true
	for _, row := range m.settingsList.Items() {
		if strings.HasPrefix(row.(item).id, settingCoreDownloadPrefix) {
			t.Fatal("individual core download in top-level settings")
		}
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	m = enterSettingsMenu(t, m, menuCore, menuDownloads)
	rows := m.settingsList.Items()
	if m.settingsList.Title != "内核下载" || len(rows) != 3 {
		t.Fatalf("unexpected submenu %q with %d rows", m.settingsList.Title, len(rows))
	}
	for i, tool := range []string{config.ToolMihomo, config.ToolSingBox, config.ToolXray} {
		if rows[i].(item).title != "下载 "+tool {
			t.Fatalf("unexpected download row: %v", rows[i])
		}
	}
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != screenSettings || m.settingsList.SelectedItem().(item).id != menuDownloads {
		t.Fatal("Esc did not return to the downloads entry in Settings")
	}
	m, _ = press(m, enter)
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnd})
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
