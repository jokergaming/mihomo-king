package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/autostart"
)

type autostartMsg struct {
	enabled bool
	changed bool
	err     error
}

func (m Model) beginAutostart(toggle bool) (Model, tea.Cmd) {
	if m.autostartBusy {
		return m, nil
	}
	if toggle && !m.autostartLoaded {
		m.setErr("autostart unavailable; press r to retry")
		return m, nil
	}
	m.autostartBusy = true
	m.reloadSettings()
	return m, func() tea.Msg {
		enabled, err := autostart.Enabled()
		if err == nil && toggle {
			enabled = !enabled
			err = autostart.SetEnabled(enabled)
		}
		return autostartMsg{enabled: enabled, changed: toggle && err == nil, err: err}
	}
}

func autostartState(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func (m Model) autostartDescription() string {
	if m.autostartBusy {
		return "updating…"
	}
	if !m.autostartLoaded {
		return "unavailable · r to retry"
	}
	return autostartState(m.autostartEnabled) + " · start proxy in background at desktop login · enter to toggle"
}
