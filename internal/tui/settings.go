package tui

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
	"mihomo-king/internal/geo"
	"mihomo-king/internal/mihomo"
)

const (
	settingTool              = "managed_tool"
	settingMihomoBin         = "mihomo_bin"
	settingSingBoxBin        = "sing_box_bin"
	settingXrayBin           = "xray_bin"
	settingCoreGet           = "core_download"
	settingCoreRestore       = "core_restore"
	settingCoreBackups       = "core_backup_count"
	settingBackupRoot        = "core_backup_root"
	settingCoreChannel       = "core_channel"
	settingCoreSource        = "core_release_url"
	settingCoreReminders     = "core_update_reminders"
	settingCoreInterval      = "core_update_interval"
	settingGeoGet            = "geo_download"
	settingController        = "controller"
	settingMixedPort         = "mixed_port"
	settingTunDevice         = "tun_device"
	settingTunAddress        = "tun_address"
	settingTestURL           = "test_url"
	settingMode              = "mode"
	settingLogLevel          = "log_level"
	settingAutostart         = "autostart"
	settingDashboardLatency  = "dashboard_latency_enabled"
	settingDashboardInterval = "dashboard_latency_interval"
)

type backupsMsg struct {
	names []string
	err   error
}

func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.settingsSaveBusy || m.coreBusy {
		return m, nil
	}
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
			return m.switchScreen(screenDashboard)
		case "enter":
			return m.settingsEnter()
		case "r":
			return m.beginAutostart(false)
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
	if sel.id == settingCoreRestore {
		s := *m.settings
		m.coreBusy = true
		return m, func() tea.Msg {
			names, err := core.Backups(&s, s.Tool(), s.Channel(s.Tool()))
			return backupsMsg{names, err}
		}
	}
	if sel.id == settingCoreChannel {
		if m.running {
			m.setErr("stop current tool before switching managed tool")
			return m, nil
		}
		tool := m.settings.Tool()
		channel := config.Alpha
		if m.settings.Channel(tool) == config.Alpha {
			channel = config.Stable
		}
		return m.savePreference(sel.id, func(s *config.Settings) { s.CoreChannels = config.WithCoreValue(s.CoreChannels, tool, channel) })
	}
	if sel.id == settingGeoGet {
		return m.beginGeoDownload()
	}
	if sel.id == settingAutostart {
		return m.beginAutostart(true)
	}
	if sel.id == settingDashboardLatency {
		enabled := !m.settings.DashboardLatencyEnabled
		return m.savePreference(sel.id, func(s *config.Settings) { s.DashboardLatencyEnabled = enabled })
	}
	if sel.id == settingCoreReminders {
		enabled := !m.settings.CoreUpdateReminders
		return m.savePreference(sel.id, func(s *config.Settings) { s.CoreUpdateReminders = enabled })
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
			m.setError(err)
			return m, nil
		}
		m.reloadSettings()
		m.setStatus("managing %s", m.settings.ToolLabel())
		cmd := m.restartCoreChecks()
		return m, tea.Batch(refreshStatusCmd(m.settings), cmd)
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
			switch m.settingKey {
			case settingBackupRoot:
				root, err := config.ResolveCoreBackupRoot(m.settingInput.Value())
				if err != nil {
					m.setErr("enter an absolute backup path or a path starting with ~/")
					return m, nil
				}
				if root != "" {
					if info, err := os.Stat(root); err == nil && !info.IsDir() {
						m.setErr("backup path is not a directory")
						return m, nil
					} else if err != nil && !os.IsNotExist(err) {
						m.setError(err)
						return m, nil
					}
				}
				return m.savePreference(m.settingKey, func(s *config.Settings) { s.CoreBackupRoot = root })
			case settingCoreBackups:
				count, err := strconv.Atoi(strings.TrimSpace(m.settingInput.Value()))
				if err != nil || count < 0 {
					m.setErr("backup count must be a non-negative integer")
					return m, nil
				}
				return m.savePreference(m.settingKey, func(s *config.Settings) { s.CoreBackupCount = count })
			case settingCoreSource:
				tool, raw := m.settings.Tool(), strings.TrimSpace(m.settingInput.Value())
				if _, err := core.ReleaseSpec(tool, raw); err != nil {
					m.setErr("invalid release source: %v", err)
					return m, nil
				}
				return m.savePreference(m.settingKey, func(s *config.Settings) { s.CoreReleaseURLs = config.WithCoreValue(s.CoreReleaseURLs, tool, raw) })
			case settingCoreRestore:
				name := strings.TrimSpace(m.settingInput.Value())
				if !slices.Contains(m.backups, name) {
					m.setErr("select a listed backup")
					return m, nil
				}
				m.editingSetting = false
				m.settingInput.Blur()
				m.coreBusy = true
				s := *m.settings
				return m, func() tea.Msg {
					res, err := core.Restore(context.Background(), &s, s.Tool(), s.Channel(s.Tool()), name, true)
					return coreActionMsg{coreResultMsg(s.Tool(), res, err)}
				}
			}
			if m.settingKey == settingDashboardInterval {
				interval, err := time.ParseDuration(strings.TrimSpace(m.settingInput.Value()))
				if err != nil || config.ValidateDashboardLatencyInterval(interval) != nil {
					m.setError(config.ErrDashboardLatencyInterval)
					return m, nil
				}
				return m.savePreference(m.settingKey, func(s *config.Settings) { s.DashboardLatencyInterval = interval })
			}
			if m.settingKey == settingCoreInterval {
				raw := strings.TrimSpace(m.settingInput.Value())
				if _, err := config.ParseCoreUpdateInterval(raw); err != nil {
					m.setError(err)
					return m, nil
				}
				return m.savePreference(m.settingKey, func(s *config.Settings) { s.CoreUpdateInterval = raw })
			}
			if m.settingKey == settingCoreGet {
				tool, channel, err := parseCoreDownload(m.settingInput.Value())
				if err != nil {
					m.setError(err)
					return m, nil
				}
				m.editingSetting = false
				m.settingInput.Blur()
				return m.beginCoreCaps(tool, channel)
			}
			if m.settingKey == settingGeoGet {
				tools, err := parseGeoDownload(m.settingInput.Value())
				if err != nil {
					m.setError(err)
					return m, nil
				}
				m.editingSetting = false
				m.settingInput.Blur()
				m.setStatus("downloading shared geoip…")
				return m, geoDownloadCmd(m.settings, tools)
			}
			oldBin := m.settings.Binary()
			if err := m.saveSettingValue(m.settingKey, strings.TrimSpace(m.settingInput.Value())); err != nil {
				m.setError(err)
				return m, nil
			}
			m.editingSetting = false
			m.settingInput.Blur()
			m.reloadSettings()
			m.setStatus("setting saved")
			if oldBin != m.settings.Binary() {
				cmd := m.restartCoreChecks()
				return m, tea.Batch(refreshStatusCmd(m.settings), cmd)
			}
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
	case settingBackupRoot:
		return m.settings.CoreBackupRoot
	case settingCoreBackups:
		return strconv.Itoa(m.settings.CoreBackupCount)
	case settingCoreSource:
		return m.settings.CoreReleaseURLs[m.settings.Tool()]
	case settingCoreInterval:
		return m.settings.CoreUpdateInterval
	case settingMihomoBin:
		return m.settings.CoreBinary(config.ToolMihomo, m.settings.Channel(config.ToolMihomo))
	case settingSingBoxBin:
		return m.settings.CoreBinary(config.ToolSingBox, m.settings.Channel(config.ToolSingBox))
	case settingXrayBin:
		return m.settings.CoreBinary(config.ToolXray, m.settings.Channel(config.ToolXray))
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
	case settingDashboardInterval:
		return m.settings.DashboardLatencyInterval.String()
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
		return message("%s required", message(m.settingLabel(key)))
	}
	switch key {
	case settingMihomoBin:
		m.settings.SetCoreBinary(config.ToolMihomo, m.settings.Channel(config.ToolMihomo), raw)
	case settingSingBoxBin:
		m.settings.SetCoreBinary(config.ToolSingBox, m.settings.Channel(config.ToolSingBox), raw)
	case settingXrayBin:
		m.settings.SetCoreBinary(config.ToolXray, m.settings.Channel(config.ToolXray), raw)
	case settingController:
		hostPort := strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://")
		if _, _, err := net.SplitHostPort(hostPort); err != nil {
			return message("invalid controller")
		}
		m.settings.Controller = raw
	case settingMixedPort:
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return message("mixed port must be 1-65535")
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
			return message("mode must be one of: %s", strings.Join(m.settingOptions(key), ", "))
		}
		m.settings.Mode = raw
	case settingLogLevel:
		if !validSettingOption(raw, m.settingOptions(key)) {
			return message("log level must be one of: %s", strings.Join(m.settingOptions(key), ", "))
		}
		m.settings.LogLevel = raw
	default:
		return message("unknown setting %s", key)
	}
	return m.settings.Save()
}

func (m *Model) reloadSettings() {
	tool := m.settings.Tool()
	spec, _ := core.SpecFor(tool)
	dir, err := core.BackupDir(m.settings, tool, m.settings.Channel(tool))
	if err != nil {
		dir = err.Error()
	}
	rows := []list.Item{
		item{title: m.tr("download core"), desc: m.tr("stable or alpha · replaces the configured binary · tab to choose"), id: settingCoreGet},
		item{title: m.tr("core update reminders"), desc: m.tr(autostartState(m.settings.CoreUpdateReminders)), id: settingCoreReminders},
	}
	if m.settings.CoreUpdateReminders {
		rows = append(rows, item{title: m.tr("core update check interval"), desc: m.settingValue(settingCoreInterval), id: settingCoreInterval})
	}
	rows = append(rows,
		item{title: m.tr("core channel"), desc: tool + " · " + m.settings.Channel(tool), id: settingCoreChannel},
		item{title: m.tr("restore core backup"), desc: dir, id: settingCoreRestore},
		item{title: m.tr("core backup root"), desc: cmp.Or(m.settings.CoreBackupRoot, m.tr("beside the core binary")), id: settingBackupRoot},
		item{title: m.tr("backups to keep per channel"), desc: m.settingValue(settingCoreBackups), id: settingCoreBackups},
		item{title: m.tr("core release source"), desc: cmp.Or(m.settingValue(settingCoreSource), "https://github.com/"+spec.Repo+"/releases"), id: settingCoreSource},
		item{title: m.tr("download geoip"), desc: m.geoStatus(m.settings), id: settingGeoGet},
		item{title: m.tr("managed tool"), desc: m.settings.ToolLabel() + m.tr("  ·  options: mihomo, sing-box, xray  ·  enter to toggle"), id: settingTool},
		item{title: m.tr("autostart"), desc: m.autostartDescription(), id: settingAutostart},
		item{title: m.tr("mihomo binary"), desc: m.binaryDesc(m.settingValue(settingMihomoBin)), id: settingMihomoBin},
		item{title: m.tr("sing-box binary"), desc: m.binaryDesc(m.settingValue(settingSingBoxBin)), id: settingSingBoxBin},
		item{title: m.tr("xray binary"), desc: m.binaryDesc(m.settingValue(settingXrayBin)), id: settingXrayBin},
		item{title: m.tr("controller"), desc: m.settings.Controller, id: settingController},
		item{title: m.tr("mixed port"), desc: strconv.Itoa(m.settings.MixedPort), id: settingMixedPort},
		item{title: m.tr("tun device"), desc: m.settings.TunDevice, id: settingTunDevice},
		item{title: m.tr("tun address"), desc: m.settings.TunAddress, id: settingTunAddress},
		item{title: m.tr("test url"), desc: m.settings.TestURL, id: settingTestURL},
		item{title: m.tr("dashboard latency"), desc: m.tr("%s · show and test latency on dashboard · enter to toggle", message(autostartState(m.settings.DashboardLatencyEnabled))), id: settingDashboardLatency},
		item{title: m.tr("latency refresh interval"), desc: m.settings.DashboardLatencyInterval.String(), id: settingDashboardInterval},
		item{title: m.tr("mode"), desc: m.optionDesc(m.settings.Mode, m.settingOptions(settingMode)), id: settingMode},
		item{title: m.tr("log level"), desc: m.optionDesc(m.settings.LogLevel, m.settingOptions(settingLogLevel)), id: settingLogLevel},
	)
	m.settingsList.Title = m.tr("Settings")
	previous := m.settingsList
	m.settingsList.SetItems(rows)
	if previous.FilterState() != list.Unfiltered {
		restoreListState(&m.settingsList, previous)
	}
}

func (m Model) viewSettings() string {
	body := m.listView(m.settingsList)
	if row, ok := m.settingsList.SelectedItem().(item); ok && (row.id == settingCoreRestore || row.id == settingBackupRoot) {
		width := max(1, m.width-4)
		if ansi.StringWidth(row.desc) > width-2 {
			body += "\n" + ansi.Hardwrap(row.desc, width, false)
		}
	}
	if m.coreNotice.key != "" {
		body += "\n\n" + okStyle.Render(m.render(m.coreNotice))
	}
	if m.tunStage == tunAskCoreCaps {
		return body + m.tr("\n\n  sudo password — set cap_net_admin,cap_net_raw=ep after download:\n  ") + m.pwInput.View()
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
	case settingCoreRestore:
		return m.backups
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

func (m Model) optionDesc(current string, options []string) string {
	return m.tr("%s  ·  options: %s", current, strings.Join(options, ", "))
}

func (m Model) settingPrompt(key string) string {
	switch key {
	case settingBackupRoot:
		return m.tr("backup root (empty for default)")
	case settingCoreSource:
		return m.tr("GitHub releases URL (empty for official source)")
	case settingCoreRestore:
		return m.tr("backup timestamp (Tab to select)")
	case settingCoreInterval:
		return m.tr("core update check interval (e.g. 1d, 12h or 30m)")
	case settingDashboardInterval:
		return m.tr("latency refresh interval (e.g. 10s or 1m, minimum 1s)")
	case settingCoreGet:
		return m.tr("core channel (tab cycles mihomo/sing-box/xray and stable/alpha)")
	case settingGeoGet:
		return m.tr("cores (tab cycles one core or all; one Loyalsoldier download is shared)")
	}
	if options := m.settingOptions(key); len(options) > 0 {
		return m.tr("%s (tab: %s)", m.settingLabel(key), strings.Join(options, " / "))
	}
	return m.settingLabel(key)
}

type preferenceSavedMsg struct {
	key   string
	apply func(*config.Settings)
	err   error
}

func (m Model) savePreference(key string, apply func(*config.Settings)) (Model, tea.Cmd) {
	s := *m.settings
	apply(&s)
	m.settingsSaveBusy = true
	m.setStatus("saving settings…")
	return m, func() tea.Msg {
		return preferenceSavedMsg{key: key, apply: apply, err: s.Save()}
	}
}

func (m Model) beginCoreCaps(tool, channel string) (tea.Model, tea.Cmd) {
	m.pendingCore = coreDownload{tool: tool, channel: channel}
	if os.Geteuid() == 0 || runtime.GOOS != "linux" {
		m.coreBusy = true
		m.setStatus("downloading %s %s…", tool, channel)
		return m, coreDownloadCmd(m.settings, tool, channel, "")
	}
	m.tunStage = tunAskCoreCaps
	m.pwInput.Reset()
	m.pwInput.Placeholder = m.tr("sudo password")
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
			m.coreBusy = true
			m.setStatus("downloading %s %s…", tool, channel)
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
	m.settingInput.Placeholder = m.settings.Tool() + " " + m.settings.Channel(m.settings.Tool())
	m.settingInput.SetValue(m.settingInput.Placeholder)
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
		return nil, message("choose one of: %s", strings.Join(geoDownloadOptions(), ", "))
	}
	return []string{raw}, nil
}

func (m Model) geoStatus(s *config.Settings) string {
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
		return m.tr("Loyalsoldier geoip · one download, shared by every core · not installed")
	}
	return m.tr("Loyalsoldier geoip · installed for ") + strings.Join(present, ", ")
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
		return "", "", message("choose one of: %s", strings.Join(coreDownloadOptions(), ", "))
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

func (m Model) binaryDesc(path string) string {
	resolved := resolveBinary(path)
	if resolved == "(not set)" {
		resolved = m.tr("(not set)")
	}
	caps, ok := mihomo.NetCaps(resolved)
	capText := m.tr("caps: unknown")
	if ok && caps != "" {
		capText = m.tr("caps: ") + caps
	} else if ok {
		capText = m.tr("caps: none")
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

func (m Model) settingLabel(key string) string {
	for _, row := range m.settingsList.Items() {
		if entry := row.(item); entry.id == key {
			return entry.title
		}
	}
	return key
}
