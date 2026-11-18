package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
)

func TestDashboardSettingsEditAndPersistence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := New(s)
	m.screen = screenSettings
	selectSetting := func(id string) {
		t.Helper()
		for i, row := range m.settingsList.Items() {
			if row.(item).id == id {
				m.settingsList.Select(i)
				return
			}
		}
		t.Fatalf("missing setting %s", id)
	}
	selectSetting(settingDashboardLatency)
	m, cmd := press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.settingsSaveBusy || !s.DashboardLatencyEnabled {
		t.Fatal("toggle must save asynchronously before changing live settings")
	}
	if _, duplicate := press(m, tea.KeyMsg{Type: tea.KeyEnter}); duplicate != nil {
		t.Fatal("concurrent preference save was allowed")
	}
	m, _ = press(m, cmd())
	if s.DashboardLatencyEnabled || m.settingsSaveBusy || m.statusErr {
		t.Fatalf("toggle failed: %s", m.status.String())
	}
	selectSetting(settingDashboardInterval)
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editingSetting || m.settingInput.Value() != "10s" {
		t.Fatal("interval editor did not show the default")
	}
	for _, raw := range []string{"", "0s", "-1s", "500ms", "10", "wrong"} {
		m.settingInput.SetValue(raw)
		m, cmd = press(m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || !m.statusErr || !m.editingSetting || s.DashboardLatencyInterval != 10*time.Second {
			t.Fatalf("invalid interval %q was accepted", raw)
		}
	}
	m.settingInput.SetValue("25s")
	m, cmd = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || s.DashboardLatencyInterval != 10*time.Second {
		t.Fatal("interval changed before persistence succeeded")
	}
	m, _ = press(m, cmd())
	if m.editingSetting || m.statusErr || s.DashboardLatencyInterval != 25*time.Second {
		t.Fatalf("interval save failed: %s", m.status.String())
	}
	reloaded, err := config.Load()
	if err != nil || reloaded.DashboardLatencyEnabled || reloaded.DashboardLatencyInterval != 25*time.Second {
		t.Fatalf("preferences not persisted: %v, %v", reloaded, err)
	}
	// A failed save keeps the old preference; the same toggle can be retried.
	path := filepath.Join(s.AppDir(), "settings.yaml")
	if err := os.Rename(path, path+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	selectSetting(settingDashboardLatency)
	m, cmd = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(m, cmd())
	if !m.statusErr || s.DashboardLatencyEnabled || m.settingsSaveBusy {
		t.Fatal("failed save changed the preference or hid the error")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".backup", path); err != nil {
		t.Fatal(err)
	}
	m, cmd = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(m, cmd())
	if !s.DashboardLatencyEnabled || m.statusErr {
		t.Fatal("retry did not enable latency")
	}
	s.Language = config.LanguageChinese
	m.refreshLanguage()
	selectSetting(settingDashboardInterval)
	if row := m.settingsList.SelectedItem().(item); row.title != "延迟刷新间隔" || row.desc != "25s" {
		t.Fatalf("localized interval row: %+v", row)
	}
	selectSetting(settingDashboardLatency)
	if row := m.settingsList.SelectedItem().(item); row.title != "主界面延迟显示" || !strings.Contains(row.desc, "开启") {
		t.Fatalf("localized toggle row: %+v", row)
	}
}
