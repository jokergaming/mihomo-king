package tui

import (
	"os"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
)

const settingRenameBinary = "rename_binary"

type binaryCapsMsg struct{ actionMsg }

type binaryRenamedMsg struct {
	tool, channel, path string
	err                 error
}

func (m Model) selectedBinaryTool() string {
	row, ok := m.settingsList.SelectedItem().(item)
	if !ok {
		return ""
	}
	switch row.id {
	case settingMihomoBin:
		return config.ToolMihomo
	case settingSingBoxBin:
		return config.ToolSingBox
	case settingXrayBin:
		return config.ToolXray
	}
	return ""
}

func (m Model) renameSelectedBinary() (tea.Model, tea.Cmd) {
	tool := m.selectedBinaryTool()
	s := *m.settings
	channel, name := s.Channel(tool), m.settingInput.Value()
	m.coreBusy = true
	return m, func() tea.Msg {
		path, err := core.RenameBinary(&s, tool, channel, name)
		return binaryRenamedMsg{tool, channel, path, err}
	}
}

func (m Model) beginInstalledCaps(tool string) (tea.Model, tea.Cmd) {
	if runtime.GOOS != "linux" {
		m.setErr("network capabilities are available on Linux only")
		return m, nil
	}
	channel := m.settings.Channel(tool)
	if os.Geteuid() == 0 {
		return m.applyInstalledCaps(tool, channel, "")
	}
	m.pendingCore = coreDownload{tool: tool, channel: channel}
	return m.askCorePassword(tunAskInstalledCaps)
}

func (m Model) applyInstalledCaps(tool, channel, password string) (tea.Model, tea.Cmd) {
	path := m.settings.CoreBinary(tool, channel)
	m.coreBusy = true
	return m, func() tea.Msg {
		err := core.GrantNetCaps(path, password)
		return binaryCapsMsg{actionMsg{err: err, note: message("network capabilities granted to %s (effective on next start)", path)}}
	}
}
