package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
)

func selectSettingsRow(t *testing.T, m *Model, id string) {
	t.Helper()
	for i, row := range m.settingsList.VisibleItems() {
		if row.(item).id == id {
			m.settingsList.Select(i)
			return
		}
	}
	t.Fatalf("missing %q in %q", id, m.settingsList.Title)
}

func enterSettingsMenu(t *testing.T, m Model, menu string) Model {
	t.Helper()
	selectSettingsRow(t, &m, menu)
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.settingsMenu != menu || m.editingSetting {
		t.Fatalf("could not open %q", menu)
	}
	return m
}

func TestSettingsCategoryNavigation(t *testing.T) {
	m := editableSubsTestModel(t)
	m.screen = screenSettings
	m.settings.CoreUpdateReminders = true
	for _, row := range m.settingsList.Items() {
		menu := row.(item).id
		m = enterSettingsMenu(t, m, menu)
		m = switchLanguage(t, m)
		if m.settingsList.Title != m.tr(menu) || !hasChinese(m.settingsList.Title) || len(m.settingsList.Items()) == 0 {
			t.Fatalf("unexpected submenu %q", m.settingsList.Title)
		}
		for _, child := range m.settingsList.Items() {
			if !hasChinese(child.(item).title) {
				t.Fatalf("untranslated row in %q: %v", menu, child)
			}
		}
		m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
		if m.settingsMenu != "" || m.settingsList.SelectedItem().(item).id != menu {
			t.Fatalf("Esc did not return to %q in Settings", menu)
		}
		m = switchLanguage(t, m)
	}
}

func TestCorePathSettingsPersist(t *testing.T) {
	m := editableSubsTestModel(t)
	m.screen = screenSettings
	m = enterSettingsMenu(t, m, menuPaths)
	if len(m.settingsList.Items()) != 3 {
		t.Fatalf("unexpected binary path rows: %v", m.settingsList.Items())
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	for tool, key := range map[string]string{config.ToolMihomo: settingMihomoBin, config.ToolSingBox: settingSingBoxBin, config.ToolXray: settingXrayBin} {
		selectSettingsRow(t, &m, key)
		m, _ = press(m, enter)
		path := filepath.Join(t.TempDir(), tool)
		m.settingInput.SetValue(path)
		m, _ = press(m, enter)
		if m.editingSetting || m.statusErr || m.settingsMenu != menuPaths {
			t.Fatalf("could not save %q: %s", tool, m.status)
		}
		s, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if got := s.CoreBinary(tool, s.Channel(tool)); got != path {
			t.Fatalf("%s path = %q, want %q", tool, got, path)
		}
	}
}
