package tui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
)

func TestBinaryActionsOnSelectedAlpha(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", dir)
	m := editableSubsTestModel(t)
	tool := config.ToolXray
	path := filepath.Join(dir, "xray")
	for name, body := range map[string]string{
		"xray":   "echo Xray",
		"sudo":   "read -r pw\n[ \"$pw\" = test-pw ] || exit 1\nshift 4\nexec \"$@\"",
		"setcap": "[ \"$1\" = cap_net_admin,cap_net_raw=ep ] && [ \"$2\" = \"$WANT_BINARY\" ] || exit 1\n: > \"$2.granted\"",
		"getcap": "[ ! -e \"$1.granted\" ] || echo \"$1 cap_net_admin,cap_net_raw=ep\"",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	m.settings.CoreChannels[tool] = config.Alpha
	m.settings.SetCoreBinary(tool, config.Alpha, path)
	stable := m.settings.CoreBinary(tool, config.Stable)
	m.screen = screenSettings
	m = enterSettingsMenu(t, m, menuCore, menuPaths)
	selectSettingsRow(t, &m, settingXrayBin)
	backupDir, err := core.BackupDir(m.settings, tool, config.Alpha)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	backup := "20261118T120000.000000000Z"
	if err := os.WriteFile(filepath.Join(backupDir, backup), nil, 0600); err != nil {
		t.Fatal(err)
	}
	m, _ = press(m, keyRunes("r"))
	if m.settingInput.Value() != "xray" {
		t.Fatal("wrong rename target")
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	m.settingInput.SetValue("sudo")
	m, cmd := press(m, enter)
	m, _ = press(m, cmd())
	if !m.editingSetting || !m.statusErr {
		t.Fatal("existing destination accepted")
	}
	m.settingInput.SetValue("xray-preview")
	m, cmd = press(m, enter)
	m, _ = press(m, cmd())
	to := filepath.Join(dir, "xray-preview")
	if m.statusErr || m.editingSetting || m.settings.CoreBinary(tool, config.Alpha) != to {
		t.Fatalf("rename: %s", m.status)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old path still exists")
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CoreBinary(tool, config.Alpha) != to || loaded.CoreBinary(tool, config.Stable) != stable {
		t.Fatal("channel paths changed on reload")
	}
	names, err := core.Backups(loaded, tool, config.Alpha)
	if err != nil || len(names) != 1 || names[0] != backup {
		t.Fatalf("lost backups: %v, %v", names, err)
	}
	if runtime.GOOS != "linux" {
		return
	}
	t.Setenv("WANT_BINARY", to)
	m, cmd = press(m, keyRunes("s"))
	if os.Geteuid() != 0 {
		m.pwInput.SetValue("test-pw")
		if m.tunStage != tunAskInstalledCaps || strings.Contains(m.viewSettings(), "test-pw") {
			t.Fatal("expected masked input")
		}
		m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
		if m.pwInput.Value() != "" || m.tunStage != tunIdle {
			t.Fatal("cancel kept password")
		}
		m, _ = press(m, keyRunes("s"))
		m.pwInput.SetValue("test-pw")
		m, cmd = press(m, enter)
	}
	m, _ = press(m, cmd())
	row := m.settingsList.SelectedItem().(item)
	if m.statusErr || !strings.Contains(row.desc, "cap_net_admin") || m.pwInput.Value() != "" {
		t.Fatalf("grant: %q, row %s", m.status, row.desc)
	}
}
