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
	switch k.String() {
	case "s":
		if m.running {
			m.setStatus("already running")
			return m, nil
		}
		m.setStatus("starting…")
		return m, startCmd(m.settings)
	case "x":
		if !m.running {
			m.setStatus("not running")
			return m, nil
		}
		m.setStatus("stopping…")
		return m, stopCmd(m.settings)
	case "t":
		if m.settings.TunEnable {
			// disabling needs no privileges and no checks
			m.settings.TunEnable = false
			if err := m.settings.Save(); err != nil {
				m.setErr(err.Error())
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
		fmt.Fprintf(&b, "%s %s\n", labelStyle.Render(fmt.Sprintf("%-14s", label)), val)
	}

	state := errStyle.Render("stopped")
	if m.running {
		state = okStyle.Render(fmt.Sprintf("running (pid %d)", m.pid))
	}
	row("mihomo", state)
	if m.version != "" {
		row("version", m.version)
	}

	mode := m.liveMode
	if mode == "" {
		mode = m.settings.Mode
	}
	row("mode", mode)

	tun, tunStyle := "off", dimStyle
	if m.liveTun {
		tun, tunStyle = "on", okStyle
	}
	row("tun", tunStyle.Render(tun))

	active := m.settings.Active
	if active == "" {
		active = dimStyle.Render("(none — press 2, pick one, press enter to activate)")
	}
	row("subscription", active)
	row("controller", m.settings.Controller)

	switch m.tunStage {
	case tunAskForeign:
		b.WriteString("\n" + warnStyle.Render(fmt.Sprintf(
			"⚠ Another TUN device is up: %s — two TUNs fight over routes/DNS.",
			strings.Join(m.tunForeign, ", "))) + "\n" +
			"  (d) shut it down with sudo and enable ours\n" +
			"  (c) enable ours anyway\n" +
			"  (esc) cancel")
	case tunAskPassword:
		what := "grant net caps to mihomo (setcap)"
		if len(m.tunDelDevs) > 0 {
			what = "shut down " + strings.Join(m.tunDelDevs, ", ")
			if mihomo.MissingTunCaps(m.settings) {
				what += " + setcap mihomo"
			}
		}
		b.WriteString("\n  sudo password — will " + what + ", then enable TUN:\n  " + m.pwInput.View())
	}

	if m.tunStage == tunIdle {
		if m.capWarn {
			b.WriteString("\n" + warnStyle.Render(
				"⚠ TUN is on but mihomo lacks net caps. Run:\n  sudo setcap cap_net_admin,cap_net_raw=ep "+m.settings.MihomoBin))
		}
		if len(m.foreignTun) > 0 {
			b.WriteString("\n" + warnStyle.Render(fmt.Sprintf(
				"⚠ TUN is on but another TUN device is already up: %s\n  Two TUNs fight over routes/DNS — disable the other one (e.g. in its GUI) first.",
				strings.Join(m.foreignTun, ", "))))
		}
	}
	return b.String()
}
