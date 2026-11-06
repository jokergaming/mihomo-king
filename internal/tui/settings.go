package tui

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
	"mihomo-king/internal/geo"
	"mihomo-king/internal/mihomo"
)

const (
	settingTool       = "managed_tool"
	settingMihomoBin  = "mihomo_bin"
	settingSingBoxBin = "sing_box_bin"
	settingXrayBin    = "xray_bin"
	settingCoreGet    = "core_download"
	settingGeoGet     = "geo_download"
	settingController = "controller"
	settingMixedPort  = "mixed_port"
	settingTunDevice  = "tun_device"
	settingTunAddress = "tun_address"
	settingTestURL    = "test_url"
	settingMode       = "mode"
	settingLogLevel   = "log_level"
	settingAutostart  = "autostart"
)

func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.tunStage == tunAskCoreCaps {
		return m.updateCoreCapPrompt(msg)
	}
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
			return m.beginAutostart(false)
		case "g":
			return m.beginCoreDownload()
		case "G":
			return m.beginGeoDownload()
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
	if sel.id == settingCoreGet {
		return m.beginCoreDownload()
	}
	if sel.id == settingGeoGet {
		return m.beginGeoDownload()
	}
	if sel.id == settingAutostart {
		return m.beginAutostart(true)
	}
	if sel.id == settingTool {
		if m.running {
			m.setErr("stop current tool before switching managed tool")
			return m, nil
		}
		switch m.settings.Tool() {
		case config.ToolMihomo:
			m.settings.ManagedTool = config.ToolSingBox
		case config.ToolSingBox:
			m.settings.ManagedTool = config.ToolXray
		default:
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
			if m.settingKey == settingCoreGet {
				tool, channel, err := parseCoreDownload(m.settingInput.Value())
				if err != nil {
					m.setErr(err.Error())
					return m, nil
				}
				m.editingSetting = false
				m.settingInput.Blur()
				return m.beginCoreCaps(tool, channel)
			}
			if m.settingKey == settingGeoGet {
				tools, err := parseGeoDownload(m.settingInput.Value())
				if err != nil {
					m.setErr(err.Error())
					return m, nil
				}
				m.editingSetting = false
				m.settingInput.Blur()
				m.setStatus("downloading shared geoip…")
				return m, geoDownloadCmd(m.settings, tools)
			}
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
	case settingXrayBin:
		return m.settings.XrayBin
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
	case settingXrayBin:
		m.settings.XrayBin = raw
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
		item{title: "download core", desc: "release or alpha · replaces the configured binary · tab to choose", id: settingCoreGet},
		item{title: "download geoip", desc: geoStatus(m.settings), id: settingGeoGet},
		item{title: "managed tool", desc: m.settings.ToolLabel() + "  ·  options: mihomo, sing-box, xray  ·  enter to toggle", id: settingTool},
		item{title: "autostart / 开机启动", desc: m.autostartDescription(), id: settingAutostart},
		item{title: "mihomo binary", desc: binaryDesc(m.settings.MihomoBin), id: settingMihomoBin},
		item{title: "sing-box binary", desc: binaryDesc(m.settings.SingBoxBin), id: settingSingBoxBin},
		item{title: "xray binary", desc: binaryDesc(m.settings.XrayBin), id: settingXrayBin},
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
	body := m.settingsList.View()
	if m.coreNotice != "" {
		body += "\n\n" + okStyle.Render(m.coreNotice)
	}
	if m.tunStage == tunAskCoreCaps {
		return body + "\n\n  sudo password — set cap_net_admin,cap_net_raw=ep after download:\n  " + m.pwInput.View()
	}
	if !m.editingSetting {
		return body
	}
	return fmt.Sprintf("%s\n\n%s\n%s",
		body,
		labelStyle.Render(m.settingPrompt(m.settingKey)),
		m.settingInput.View())
}

func (m Model) settingOptions(key string) []string {
	switch key {
	case settingMode:
		return config.Modes(m.settings.Tool())
	case settingLogLevel:
		return config.LogLevels(m.settings.Tool())
	case settingCoreGet:
		return coreDownloadOptions()
	case settingGeoGet:
		return geoDownloadOptions()
	default:
		return nil
	}
}

func optionDesc(current string, options []string) string {
	return fmt.Sprintf("%s  ·  options: %s", current, strings.Join(options, ", "))
}

func (m Model) settingPrompt(key string) string {
	switch key {
	case settingCoreGet:
		return "core channel (tab cycles mihomo/sing-box/xray and release/alpha)"
	case settingGeoGet:
		return "cores (tab cycles one core or all; one Loyalsoldier download is shared)"
	}
	if options := m.settingOptions(key); len(options) > 0 {
		return fmt.Sprintf("%s (tab: %s)", key, strings.Join(options, " / "))
	}
	return key
}

func (m Model) beginCoreCaps(tool, channel string) (tea.Model, tea.Cmd) {
	m.pendingCore = coreDownload{tool: tool, channel: channel}
	if os.Geteuid() == 0 {
		m.setStatus("downloading " + tool + " " + channel + "…")
		return m, coreDownloadCmd(m.settings, tool, channel, "")
	}
	m.tunStage = tunAskCoreCaps
	m.pwInput.Reset()
	m.pwInput.Placeholder = "sudo password"
	return m, m.pwInput.Focus()
}

func (m Model) updateCoreCapPrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch k.String() {
		case "esc":
			m.tunStage = tunIdle
			m.pwInput.Reset()
			m.pendingCore = coreDownload{}
			m.setStatus("cancelled")
			return m, nil
		case "enter":
			pw := m.pwInput.Value()
			m.pwInput.Reset()
			m.tunStage = tunIdle
			if pw == "" {
				m.pendingCore = coreDownload{}
				m.setErr("password required (esc to cancel)")
				return m, nil
			}
			tool, channel := m.pendingCore.tool, m.pendingCore.channel
			m.pendingCore = coreDownload{}
			m.setStatus("downloading " + tool + " " + channel + "…")
			return m, coreDownloadCmd(m.settings, tool, channel, pw)
		}
	}
	var cmd tea.Cmd
	m.pwInput, cmd = m.pwInput.Update(msg)
	return m, cmd
}

func (m Model) beginCoreDownload() (tea.Model, tea.Cmd) {
	m.editingSetting = true
	m.settingKey = settingCoreGet
	m.settingInput.Placeholder = m.settings.Tool() + " " + core.ChannelRelease
	m.settingInput.SetValue(m.settings.Tool() + " " + core.ChannelRelease)
	m.settingInput.CursorEnd()
	return m, m.settingInput.Focus()
}

func (m Model) beginGeoDownload() (tea.Model, tea.Cmd) {
	m.editingSetting = true
	m.settingKey = settingGeoGet
	m.settingInput.Placeholder = "all"
	m.settingInput.SetValue("all")
	m.settingInput.CursorEnd()
	return m, m.settingInput.Focus()
}

func geoDownloadOptions() []string {
	return []string{"all", config.ToolMihomo, config.ToolSingBox, config.ToolXray}
}

func parseGeoDownload(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "all" {
		return nil, nil
	}
	if _, err := geo.FilesFor(raw); err != nil {
		return nil, fmt.Errorf("choose one of: %s", strings.Join(geoDownloadOptions(), ", "))
	}
	return []string{raw}, nil
}

func geoStatus(s *config.Settings) string {
	var present []string
	if _, err := os.Stat(filepath.Join(s.MihomoDir, "GeoIP.dat")); err == nil {
		present = append(present, "mihomo")
	}
	if _, err := os.Stat(filepath.Join(s.SingBoxDir, "geoip-cn.srs")); err == nil {
		present = append(present, "sing-box")
	}
	if _, err := os.Stat(filepath.Join(s.XrayDir, "geoip.dat")); err == nil {
		present = append(present, "xray")
	}
	if len(present) == 0 {
		return "Loyalsoldier geoip · one download, shared by every core · not installed"
	}
	return "Loyalsoldier geoip · installed for " + strings.Join(present, ", ")
}

func coreDownloadOptions() []string {
	var options []string
	for _, tool := range []string{config.ToolMihomo, config.ToolSingBox, config.ToolXray} {
		for _, channel := range core.Channels() {
			options = append(options, tool+" "+channel)
		}
	}
	return options
}

func parseCoreDownload(raw string) (string, string, error) {
	tool, channel, ok := strings.Cut(strings.TrimSpace(raw), " ")
	if !ok || !core.ValidChannel(channel) {
		return "", "", fmt.Errorf("choose one of: %s", strings.Join(coreDownloadOptions(), ", "))
	}
	if _, err := core.SpecFor(tool); err != nil {
		return "", "", err
	}
	return tool, channel, nil
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
