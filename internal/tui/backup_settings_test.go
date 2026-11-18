package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
)

func TestBackupDeletionSettings(t *testing.T) {
	dir := t.TempDir()
	s := &config.Settings{MihomoBin: filepath.Join(dir, "mihomo"), CoreBackupRoot: dir, Language: config.LanguageChinese}
	backupDir, err := core.BackupDir(s, "mihomo", "stable")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	stamp := "20260102T000000.000000000Z"
	path := filepath.Join(backupDir, stamp)
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	m := New(s)
	m.screen = screenSettings
	m = enterSettingsMenu(t, m, menuCore, menuBackups)
	m.width = 240
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	selectAction := func(id string) {
		t.Helper()
		for i, row := range m.settingsList.Items() {
			if row.(item).id == id {
				m.settingsList.Select(i)
				return
			}
		}
		t.Fatalf("missing action %s", id)
	}
	run := func(key tea.KeyMsg) {
		t.Helper()
		var cmd tea.Cmd
		m, cmd = press(m, key)
		if cmd == nil {
			t.Fatal("expected backup command")
		}
		m, _ = press(m, cmd())
	}
	selectAction(settingDeleteBackups)
	run(enter)
	if !strings.Contains(m.viewSettings(), backupDir) {
		t.Fatal("missing folder preview")
	}
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if _, err := os.Stat(path); err != nil || m.pendingDeletion != nil {
		t.Fatal("cancelled deletion did not preserve backup")
	}
	selectAction(settingDeleteBackup)
	run(enter)
	run(enter)
	if !strings.Contains(m.viewSettings(), path) {
		t.Fatal("missing file preview")
	}
	run(keyRunes("y"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("single backup still exists")
	}
	selectAction(settingDeleteBackups)
	run(enter)
	run(keyRunes("y"))
	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Fatal("backup folder still exists")
	}
}

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
	m = enterSettingsMenu(t, m, menuCore, menuBackups)
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
