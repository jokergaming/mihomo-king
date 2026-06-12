package tui

import (
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
	"mihomo-king/internal/mihomo"
)

const (
	settingTool       = "managed_tool"
	settingMihomoBin  = "mihomo_bin"
	settingSingBoxBin = "sing_box_bin"
	settingController = "controller"
	settingMixedPort  = "mixed_port"
	settingTunDevice  = "tun_device"
	settingTunAddress = "tun_address"
	settingTestURL    = "test_url"
	settingMode       = "mode"
	settingLogLevel   = "log_level"
)

func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.editingSetting {
		return m.updateSettingInput(msg)
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		if filterOwnsKey(m.settingsList, k.String()) {
			var cmd tea.Cmd
			m.settingsList, cmd = m.settingsList.Update(msg)
			return m, cmd
		}
		if wrapListKey(&m.settingsList, k.String()) {
			return m, nil
		}
		switch k.String() {
		case "esc":
			m.screen = screenDashboard
			return m, refreshStatusCmd(m.settings)
		case "enter":
			return m.settingsEnter()
		case "r":
			m.reloadSettings()
			m.setStatus("settings refreshed")
			return m, nil
		}
		if nm, cmd, handled := m.globalKey(k.String()); handled {
			return nm, cmd
		}
	}
	var cmd tea.Cmd
	m.settingsList, cmd = m.settingsList.Update(msg)
	return m, cmd
}

func (m Model) settingsEnter() (tea.Model, tea.Cmd) {
	sel, ok := m.settingsList.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	if sel.id == settingTool {
		if m.running {
			m.setErr("stop current tool before switching managed tool")
			return m, nil
		}
		if m.settings.Tool() == config.ToolMihomo {
			m.settings.ManagedTool = config.ToolSingBox
		} else {
			m.settings.ManagedTool = config.ToolMihomo
		}
		m.settings.NormalizeForTool()
		if err := m.settings.Save(); err != nil {
			m.setErr(err.Error())
			return m, nil
		}
		m.reloadSettings()
		m.setStatus("managing " + m.settings.ToolLabel())
		return m, refreshStatusCmd(m.settings)
	}
	m.editingSetting = true
	m.settingKey = sel.id
	m.settingInput.Placeholder = sel.title
	m.settingInput.SetValue(m.settingValue(sel.id))
	m.settingInput.CursorEnd()
	return m, m.settingInput.Focus()
}

func (m Model) updateSettingInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			m.editingSetting = false
			m.settingInput.Blur()
			m.setStatus("cancelled")
			return m, nil
		case "enter":
			if err := m.saveSettingValue(m.settingKey, strings.TrimSpace(m.settingInput.Value())); err != nil {
				m.setErr(err.Error())
				return m, nil
			}
			m.editingSetting = false
			m.settingInput.Blur()
			m.reloadSettings()
			m.setStatus("setting saved")
			return m, refreshStatusCmd(m.settings)
		case "tab":
			if options := m.settingOptions(m.settingKey); len(options) > 0 {
				m.settingInput.SetValue(nextSettingOption(strings.TrimSpace(m.settingInput.Value()), options))
				m.settingInput.CursorEnd()
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	m.settingInput, cmd = m.settingInput.Update(msg)
	return m, cmd
}

func (m Model) settingValue(key string) string {
	switch key {
	case settingMihomoBin:
		return m.settings.MihomoBin
	case settingSingBoxBin:
		return m.settings.SingBoxBin
	case settingController:
		return m.settings.Controller
	case settingMixedPort:
		return strconv.Itoa(m.settings.MixedPort)
	case settingTunDevice:
		return m.settings.TunDevice
	case settingTunAddress:
		return m.settings.TunAddress
	case settingTestURL:
		return m.settings.TestURL
	case settingMode:
		return m.settings.Mode
	case settingLogLevel:
		return m.settings.LogLevel
	default:
		return ""
	}
}

func (m *Model) saveSettingValue(key, raw string) error {
	if raw == "" {
		return fmt.Errorf("%s required", key)
	}
	switch key {
	case settingMihomoBin:
		m.settings.MihomoBin = raw
	case settingSingBoxBin:
		m.settings.SingBoxBin = raw
	case settingController:
		hostPort := strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://")
		if _, _, err := net.SplitHostPort(hostPort); err != nil {
			return fmt.Errorf("invalid controller")
		}
		m.settings.Controller = raw
	case settingMixedPort:
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("mixed port must be 1-65535")
		}
		m.settings.MixedPort = port
	case settingTunDevice:
		m.settings.TunDevice = raw
	case settingTunAddress:
		m.settings.TunAddress = raw
	case settingTestURL:
		if err := validateTestURL(raw); err != nil {
			return err
		}
		m.settings.TestURL = raw
	case settingMode:
		if !validSettingOption(raw, m.settingOptions(key)) {
			return fmt.Errorf("mode must be one of: %s", strings.Join(m.settingOptions(key), ", "))
		}
		m.settings.Mode = raw
	case settingLogLevel:
		if !validSettingOption(raw, m.settingOptions(key)) {
			return fmt.Errorf("log level must be one of: %s", strings.Join(m.settingOptions(key), ", "))
		}
		m.settings.LogLevel = raw
	default:
		return fmt.Errorf("unknown setting %s", key)
	}
	return m.settings.Save()
}

func (m *Model) reloadSettings() {
	rows := []list.Item{
		item{title: "managed tool", desc: m.settings.ToolLabel() + "  ·  options: mihomo, sing-box  ·  enter to toggle", id: settingTool},
		item{title: "mihomo binary", desc: binaryDesc(m.settings.MihomoBin), id: settingMihomoBin},
		item{title: "sing-box binary", desc: binaryDesc(m.settings.SingBoxBin), id: settingSingBoxBin},
		item{title: "controller", desc: m.settings.Controller, id: settingController},
		item{title: "mixed port", desc: strconv.Itoa(m.settings.MixedPort), id: settingMixedPort},
		item{title: "tun device", desc: m.settings.TunDevice, id: settingTunDevice},
		item{title: "tun address", desc: m.settings.TunAddress, id: settingTunAddress},
		item{title: "test url", desc: m.settings.TestURL, id: settingTestURL},
		item{title: "mode", desc: optionDesc(m.settings.Mode, m.settingOptions(settingMode)), id: settingMode},
		item{title: "log level", desc: optionDesc(m.settings.LogLevel, m.settingOptions(settingLogLevel)), id: settingLogLevel},
	}
	m.settingsList.Title = "Settings"
	m.settingsList.SetItems(rows)
}

func (m Model) viewSettings() string {
	if !m.editingSetting {
		return m.settingsList.View()
	}
	return fmt.Sprintf("%s\n\n%s\n%s",
		m.settingsList.View(),
		labelStyle.Render(m.settingPrompt(m.settingKey)),
		m.settingInput.View())
}

func (m Model) settingOptions(key string) []string {
	switch key {
	case settingMode:
		return config.Modes(m.settings.Tool())
	case settingLogLevel:
		return config.LogLevels(m.settings.Tool())
	default:
		return nil
	}
}

func optionDesc(current string, options []string) string {
	return fmt.Sprintf("%s  ·  options: %s", current, strings.Join(options, ", "))
}

func (m Model) settingPrompt(key string) string {
	if options := m.settingOptions(key); len(options) > 0 {
		return fmt.Sprintf("%s (tab: %s)", key, strings.Join(options, " / "))
	}
	return key
}

func nextSettingOption(current string, options []string) string {
	if len(options) == 0 {
		return current
	}
	for i, option := range options {
		if option == current {
			return options[(i+1)%len(options)]
		}
	}
	for _, option := range options {
		if strings.HasPrefix(option, current) {
			return option
		}
	}
	return options[0]
}

func validSettingOption(value string, options []string) bool {
	if len(options) == 0 {
		return true
	}
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func binaryDesc(path string) string {
	resolved := resolveBinary(path)
	caps, ok := mihomo.NetCaps(resolved)
	capText := "caps: unknown"
	if ok && caps != "" {
		capText = "caps: " + caps
	} else if ok {
		capText = "caps: none"
	}
	return fmt.Sprintf("%s  ·  %s", resolved, capText)
}

func resolveBinary(path string) string {
	if path == "" {
		return "(not set)"
	}
	if filepath.IsAbs(path) {
		return path
	}
	if resolved, err := exec.LookPath(path); err == nil {
		return resolved
	}
	return path
}
