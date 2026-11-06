package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/mihomo"
)

func (m Model) updateDashboard(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.tunStage != tunIdle {
		return m.updateTunPrompt(msg)
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.coreBusy {
		switch k.String() {
		case "s", "x", "R", "t":
			m.setStatus("core operation in progress…")
			return m, nil
		}
	}
	switch k.String() {
	case "s":
		if m.running {
			m.setStatus("already running")
			return m, nil
		}
		m.setStatus("starting…")
		m.coreBusy = true
		return m, startCmd(m.settings)
	case "x":
		if !m.running {
			m.setStatus("not running")
			return m, nil
		}
		m.setStatus("stopping…")
		m.coreBusy = true
		return m, stopCmd(m.settings)
	case "R":
		m.setStatus("restarting…")
		m.coreBusy = true
		return m, restartCmd(m.settings)
	case "t":
		if m.settings.TunEnable {
			// disabling needs no privileges and no checks
			m.settings.TunEnable = false
			if err := m.settings.Save(); err != nil {
				m.setError(err)
				return m, nil
			}
			return m, toggleTunCmd(m.settings)
		}
		return m.beginTunEnable()
	case "r":
		m.setStatus("refreshing…")
		return m, refreshStatusCmd(m.settings)
	}
	if nm, cmd, handled := m.globalKey(k.String()); handled {
		return nm, cmd
	}
	return m, nil
}

// beginTunEnable starts the enable flow: ask about a foreign TUN if one is up,
// then collect a sudo password if privileged work (delete / setcap) is needed.
func (m Model) beginTunEnable() (tea.Model, tea.Cmd) {
	m.tunForeign = mihomo.ForeignTunDevices(m.settings.TunDevice)
	if len(m.tunForeign) > 0 {
		m.tunStage = tunAskForeign
		return m, nil
	}
	return m.afterForeignChoice(nil)
}

func (m Model) afterForeignChoice(delDevs []string) (tea.Model, tea.Cmd) {
	m.tunDelDevs = delDevs
	if len(delDevs) > 0 || mihomo.MissingTunCaps(m.settings) {
		m.tunStage = tunAskPassword
		m.pwInput.Reset()
		return m, m.pwInput.Focus()
	}
	m.tunStage = tunIdle
	m.setStatus("enabling TUN…")
	return m, enableTunCmd(m.settings, "", nil)
}

func (m Model) updateTunPrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, isKey := msg.(tea.KeyMsg)
	switch m.tunStage {
	case tunAskForeign:
		if !isKey {
			return m, nil
		}
		switch k.String() {
		case "d":
			return m.afterForeignChoice(m.tunForeign)
		case "c":
			return m.afterForeignChoice(nil)
		case "esc", "q":
			m.tunStage = tunIdle
			m.setStatus("cancelled")
			return m, nil
		}
		return m, nil

	case tunAskPassword:
		if isKey {
			switch k.String() {
			case "esc":
				m.tunStage = tunIdle
				m.pwInput.Reset()
				m.setStatus("cancelled")
				return m, nil
			case "enter":
				pw := m.pwInput.Value()
				m.pwInput.Reset()
				if pw == "" {
					m.setErr("password required (esc to cancel)")
					return m, nil
				}
				m.tunStage = tunIdle
				m.setStatus("enabling TUN (sudo)…")
				return m, enableTunCmd(m.settings, pw, m.tunDelDevs)
			}
		}
		var cmd tea.Cmd
		m.pwInput, cmd = m.pwInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) viewDashboard() string {
	var b strings.Builder
	row := func(label, val string) {
		fmt.Fprintf(&b, "%s %s\n", labelStyle.Width(14).Render(label), val)
	}

	state := errStyle.Render(m.tr("stopped"))
	if m.running {
		state = okStyle.Render(m.tr("running (pid %d)", m.pid))
	}
	row(m.settings.ToolLabel(), state)
	if m.version != "" {
		row(m.tr("version"), m.version)
	}

	mode := m.liveMode
	if mode == "" {
		mode = m.settings.Mode
	}
	row(m.tr("mode"), m.tr(mode))

	tun, tunStyle := m.tr("off"), dimStyle
	if m.liveTun {
		tun, tunStyle = m.tr("on"), okStyle
	}
	row(m.tr("tun"), tunStyle.Render(tun))

	active := m.settings.Active
	if active == "" {
		active = dimStyle.Render(m.tr("(none — press 2, pick one, press enter to activate)"))
	}
	row(m.tr("subscription"), active)
	switch {
	case !m.running:
		row(m.tr("nodes"), dimStyle.Render(m.tr("(not running)")))
	case m.nodeErr != nil:
		row(m.tr("nodes"), errStyle.Render(m.tr("unavailable: %s", m.nodeErr.Error())))
	case len(m.liveNodes) == 0:
		row(m.tr("nodes"), dimStyle.Render(m.tr("(no active proxy groups)")))
	default:
		for i, node := range m.liveNodes {
			label := ""
			if i == 0 {
				label = m.tr("nodes")
			}
			row(label, m.render(node))
		}
	}
	row(m.tr("controller"), m.settings.Controller)
	row(m.tr("binary"), m.settings.Binary())
	row(m.tr("config"), m.settings.ConfigPath())

	switch m.tunStage {
	case tunAskForeign:
		b.WriteString("\n" + warnStyle.Render(m.tr("⚠ Another TUN device is up: %s — two TUNs fight over routes/DNS.",
			strings.Join(m.tunForeign, ", "))) + "\n" +
			m.tr("  (d) shut it down with sudo and enable ours\n") +
			m.tr("  (c) enable ours anyway\n") +
			m.tr("  (esc) cancel"))
	case tunAskPassword:
		what := m.tr("grant net caps to %s (setcap)", m.settings.ToolLabel())
		if len(m.tunDelDevs) > 0 {
			what = m.tr("shut down %s", strings.Join(m.tunDelDevs, ", "))
			if mihomo.MissingTunCaps(m.settings) {
				what += m.tr(" + setcap ") + m.settings.ToolLabel()
			}
		}
		b.WriteString(m.tr("\n  sudo password — will %s, then enable TUN:\n  ", what) + m.pwInput.View())
	}

	if m.tunStage == tunIdle {
		if m.capWarn {
			b.WriteString("\n" + warnStyle.Render(
				m.tr("⚠ TUN is on but %s lacks net caps. Run:\n  sudo setcap cap_net_admin,cap_net_raw=ep %s", m.settings.ToolLabel(), m.settings.Binary())))
		}
		if len(m.foreignTun) > 0 {
			b.WriteString("\n" + warnStyle.Render(m.tr("⚠ TUN is on but another TUN device is already up: %s\n  Two TUNs fight over routes/DNS — disable the other one (e.g. in its GUI) first.",
				strings.Join(m.foreignTun, ", "))))
		}
	}
	return b.String()
}
