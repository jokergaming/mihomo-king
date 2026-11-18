package tui

import (
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
)

func TestCoreUpdateSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &config.Settings{CoreUpdateInterval: "1d", CoreUpdateReminders: true, MihomoBin: "/missing/mihomo"}
		m := Model{settings: s}
		m, next := m.finishCoreCheck(coreUpdateCheckedMsg{})
		done := make(chan tea.Msg, 1)
		go func() { done <- next() }()
		time.Sleep(24*time.Hour - time.Second)
		select {
		case <-done:
			t.Fatal("check ran before the configured interval")
		default:
		}
		time.Sleep(time.Second)
		result := (<-done).(coreUpdateCheckedMsg)
		if result.err == nil {
			t.Fatal("missing binary should report a check error")
		}
		m, retry := m.finishCoreCheck(result)
		go func() { done <- retry() }()
		synctest.Wait()
		s.CoreUpdateReminders = false
		m.restartCoreChecks()
		if msg := <-done; msg != nil {
			t.Fatalf("disabled timer returned %v", msg)
		}
	})
}

func TestCoreUpdatePreferencesAndNotice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := New(s)
	m.screen = screenSettings
	m = enterSettingsMenu(t, m, menuUpdates)
	rowIndex := func(key string) int {
		return slices.IndexFunc(m.settingsList.Items(), func(row list.Item) bool { return row.(item).id == key })
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	if s.CoreUpdateReminders || rowIndex(settingCoreInterval) >= 0 {
		t.Fatal("reminders should start disabled")
	}
	m.settingsList.Select(rowIndex(settingCoreReminders))
	m, cmd := press(m, enter)
	m, check := press(m, cmd())
	if check == nil || !s.CoreUpdateReminders || rowIndex(settingCoreInterval) < 0 {
		t.Fatal("toggle did not start checks or show interval")
	}
	m.settingsList.Select(rowIndex(settingCoreInterval))
	m, _ = press(m, enter)
	if m.settingInput.Value() != "1d" {
		t.Fatal("missing default interval")
	}
	for _, invalid := range []string{"", "0d", "-1h", "999999999d", "abc"} {
		m.settingInput.SetValue(invalid)
		m, cmd = press(m, enter)
		if cmd != nil || !m.editingSetting || !m.statusErr {
			t.Fatalf("accepted %q", invalid)
		}
	}
	m.settingInput.SetValue("2d")
	m, cmd = press(m, enter)
	m, _ = press(m, cmd())
	loaded, err := config.Load()
	if err != nil || !loaded.CoreUpdateReminders || loaded.CoreUpdateInterval != "2d" {
		t.Fatalf("saved preferences: %+v, %v", loaded, err)
	}
	m, _ = press(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	s.Language = config.LanguageChinese
	result := coreUpdateCheckedMsg{generation: m.coreUpdates.generation, update: core.Update{Tool: "mihomo", Installed: "v1.0.0", Latest: "v1.2.0"}}
	m, next := press(m, result)
	if next == nil {
		t.Fatal("next check was not scheduled")
	}
	small := m
	small.width, small.height = 20, 6
	page := small.withCoreUpdateNotice(strings.Repeat("content\n", 30))
	if lipgloss.Height(page) != small.height || lipgloss.Width(page) != small.width || !strings.HasSuffix(strings.Split(page, "\n")[0], "╮") {
		t.Fatalf("notice outside visible rows: %q", page)
	}
	for screen := screenDashboard; screen <= screenSettings; screen++ {
		m.screen = screen
		page := m.View()
		line, _, _ := strings.Cut(page, "\n")
		if lipgloss.Width(line) != 80 || !strings.HasSuffix(line, "╮") || !strings.Contains(page, "mihomo 有可用更新") || !strings.Contains(page, "v1.0.0 → v1.2.0") {
			t.Fatalf("misplaced notice: %q", page)
		}
	}
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyF2})
	m, _ = press(m, result)
	if m.coreUpdates.notice.Latest != "" {
		t.Fatal("dismissed version reappeared")
	}
	result.update.Latest = "v1.3.0"
	m, _ = press(m, result)
	if m.coreUpdates.notice.Latest != "v1.3.0" {
		t.Fatal("new version suppressed")
	}
	m.settingsList.Select(rowIndex(settingCoreReminders))
	m, cmd = press(m, enter)
	m, next = press(m, cmd())
	if next != nil || m.coreUpdates.notice.Latest != "" || rowIndex(settingCoreInterval) >= 0 {
		t.Fatal("disabling did not stop reminders")
	}
	m, next = press(m, result)
	if next != nil || m.coreUpdates.notice.Latest != "" {
		t.Fatal("disabled reminder accepted stale request")
	}
}
