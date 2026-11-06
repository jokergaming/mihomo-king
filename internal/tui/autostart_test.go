package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/autostart"
)

func TestSettingsAutostartToggleAndPersistence(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG desktop autostart requires Linux")
	}
	m := editableSubsTestModel(t)
	m, cmd := m.switchScreen(screenSettings)
	if cmd == nil || !m.autostartBusy {
		t.Fatal("opening settings did not load autostart")
	}
	m, _ = press(m, cmd())
	if !m.autostartLoaded || m.autostartEnabled {
		t.Fatal("autostart must default to off")
	}
	selectAutostart := func() {
		t.Helper()
		for i, row := range m.settingsList.Items() {
			if row.(item).id == settingAutostart {
				m.settingsList.Select(i)
				return
			}
		}
		t.Fatal("autostart row missing")
	}
	selectAutostart()
	m, cmd = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.autostartBusy || m.editingSetting {
		t.Fatal("toggle should run asynchronously without an input prompt")
	}
	if _, duplicate := press(m, tea.KeyMsg{Type: tea.KeyEnter}); duplicate != nil {
		t.Fatal("allowed competing toggles")
	}
	m, _ = press(m, cmd())
	if !m.autostartEnabled || m.statusErr || m.autostartBusy {
		t.Fatalf("enable failed: %s", m.status.String())
	}

	// A fresh model must discover the persisted desktop entry, not a YAML flag.
	m = New(m.settings)
	m, cmd = m.switchScreen(screenSettings)
	m, _ = press(m, cmd())
	if !m.autostartEnabled {
		t.Fatal("fresh model lost autostart state")
	}
	selectAutostart()
	m, cmd = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(m, cmd())
	if m.autostartEnabled || m.statusErr {
		t.Fatalf("disable failed: %s", m.status.String())
	}
	if enabled, err := autostart.Enabled(); err != nil || enabled {
		t.Fatalf("disable not persisted: %v, %v", enabled, err)
	}
}

func TestSettingsAutostartFailureAndRetry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG desktop autostart requires Linux")
	}
	m := editableSubsTestModel(t)
	blocker := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.switchScreen(screenSettings)
	m, _ = press(m, cmd())
	if !m.statusErr || m.autostartLoaded || m.autostartBusy || !strings.Contains(m.autostartDescription(), "unavailable") {
		t.Fatalf("read error hidden: %s", m.status.String())
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	m, cmd = press(m, keyRunes("r"))
	m, _ = press(m, cmd())
	if !m.autostartLoaded || m.autostartEnabled || m.statusErr {
		t.Fatal("refresh did not recover autostart state")
	}
	for i, row := range m.settingsList.Items() {
		if row.(item).id == settingAutostart {
			m.settingsList.Select(i)
			break
		}
	}
	// Status remains readable, but creating the launcher must fail without env.
	t.Setenv("PATH", t.TempDir())
	m, cmd = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(m, cmd())
	if !m.statusErr || m.autostartEnabled || m.autostartBusy || !strings.Contains(m.status.String(), "requires env") {
		t.Fatalf("failed toggle reported success: %s", m.status.String())
	}
	if enabled, err := autostart.Enabled(); err != nil || enabled {
		t.Fatalf("failed toggle created a startup entry: %v, %v", enabled, err)
	}
}
