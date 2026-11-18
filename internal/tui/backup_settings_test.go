package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
)

func TestBackupDirectorySettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := New(s)
	m.screen = screenSettings
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	var cmd tea.Cmd
	for _, raw := range []string{"~/backup files", ""} {
		for i, row := range m.settingsList.Items() {
			if row.(item).id == settingBackupRoot {
				m.settingsList.Select(i)
				break
			}
		}
		m, _ = press(m, enter)
		m.settingInput.SetValue("relative")
		m, cmd = press(m, enter)
		if cmd != nil || !m.statusErr || !m.editingSetting {
			t.Fatal("accepted relative root")
		}
		m.settingInput.SetValue(raw)
		m, cmd = press(m, enter)
		m, _ = press(m, cmd())
		wantRoot := ""
		if raw != "" {
			wantRoot = filepath.Join(home, "backup files")
		}
		loaded, err := config.Load()
		if err != nil || loaded.CoreBackupRoot != wantRoot {
			t.Fatalf("saved root: %v %v", loaded, err)
		}
		dir, err := core.BackupDir(loaded, loaded.Tool(), loaded.Channel(loaded.Tool()))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for i, row := range m.settingsList.Items() {
			it := row.(item)
			if it.id == settingCoreRestore && it.desc == dir {
				found = true
				m.settingsList.Select(i)
			}
		}
		if !found {
			t.Fatalf("missing displayed path %s", dir)
		}
		m.width = 32
		if !strings.Contains(m.viewSettings(), ansi.Hardwrap(dir, m.width-4, false)) {
			t.Fatal("long backup path not fully visible")
		}
	}
}
