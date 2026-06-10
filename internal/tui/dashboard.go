package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) updateDashboard(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.settings.TunEnable = !m.settings.TunEnable
		if err := m.settings.Save(); err != nil {
			m.setErr(err.Error())
			return m, nil
		}
		return m, toggleTunCmd(m.settings)
	case "r":
		m.setStatus("refreshing…")
		return m, refreshStatusCmd(m.settings)
	}
	if nm, cmd, handled := m.globalKey(k.String()); handled {
		return nm, cmd
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

	if m.capWarn {
		b.WriteString("\n" + warnStyle.Render(
			"⚠ TUN is on but mihomo lacks net caps. Run:\n  sudo setcap cap_net_admin,cap_net_raw=ep "+m.settings.MihomoBin))
	}
	if len(m.foreignTun) > 0 {
		b.WriteString("\n" + warnStyle.Render(fmt.Sprintf(
			"⚠ TUN is on but another TUN device is already up: %s\n  Two TUNs fight over routes/DNS — disable the other one (e.g. in its GUI) first.",
			strings.Join(m.foreignTun, ", "))))
	}
	return b.String()
}
