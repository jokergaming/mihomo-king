package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
	"mihomo-king/internal/geo"
	"mihomo-king/internal/mihomo"
	"mihomo-king/internal/subscription"
)

// --- messages ---

type statusMsg struct {
	running    bool
	pid        int
	version    string
	mode       string
	tun        bool
	capWarn    bool
	foreignTun []string // other live TUN devices (e.g. another clash's "Meta")
	nodes      []text
	nodeErr    error
}

type groupsMsg struct {
	groups []api.Group
	note   text
	err    error
}

type chainMsg struct {
	note    text
	running bool
	err     error
}

type subDownloadedMsg struct {
	sub  config.Subscription
	note text
	err  error
}

type subEditedMsg struct {
	name          string
	sourceChanged bool
	err           error
}

type pathCompletionMsg struct {
	input   string   // path input the completion was computed for
	value   string   // completed input
	matches []string // entries matching the last path element
}

type actionMsg struct {
	note   text
	err    error
	notice bool
}

type coreActionMsg struct{ actionMsg }

type subSwitchedMsg struct {
	name string
	err  error
}

type delayMsg struct {
	group  string
	node   string
	delays map[string]int
	err    error
}

type tickMsg time.Time

// --- commands ---

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refreshStatusCmd(s *config.Settings) tea.Cmd {
	controller, secret := s.Controller, s.Secret
	return func() tea.Msg {
		running, pid := mihomo.Running(s)
		msg := statusMsg{running: running, pid: pid, mode: s.Mode, tun: s.TunEnable}
		if running {
			if s.Tool() == config.ToolXray {
				msg.version = mihomo.CoreVersion(s)
				msg.tun = s.TunEnable && mihomo.TunDeviceUp(s.TunDevice)
				routes, err := xrayNodeRoutes(s.ConfigPath())
				msg.nodeErr = err
				for _, route := range routes {
					msg.nodes = append(msg.nodes, message("%s", route))
				}
			} else {
				c := api.New(controller, secret)
				if v, err := c.Version(); err == nil {
					msg.version = v
				}
				if cfg, err := c.Configs(); err == nil {
					msg.mode = cfg.Mode
					if s.Tool() == config.ToolMihomo {
						msg.tun = cfg.Tun.Enable
					}
					if cfg.Mode == "direct" {
						msg.nodes = []text{message("%s", "DIRECT")}
					} else {
						proxies, err := c.Proxies()
						msg.nodeErr = err
						if err == nil {
							msg.nodes = proxyNodeRoutes(proxies, cfg.Mode)
						}
					}
				} else {
					msg.nodeErr = err
				}
				if s.Tool() == config.ToolSingBox {
					msg.tun = s.TunEnable && mihomo.TunDeviceUp(s.TunDevice)
				}
			}
		}
		if s.TunEnable {
			msg.capWarn = mihomo.MissingTunCaps(s)
			msg.foreignTun = mihomo.ForeignTunDevices(s.TunDevice)
		}
		return msg
	}
}

func startCmd(s *config.Settings) tea.Cmd {
	return func() tea.Msg {
		if err := mihomo.Start(s); err != nil {
			return coreActionMsg{actionMsg{err: err}}
		}
		return coreActionMsg{actionMsg{note: message("%s started", s.ToolLabel())}}
	}
}

func stopCmd(s *config.Settings) tea.Cmd {
	return func() tea.Msg {
		if err := mihomo.Stop(s); err != nil {
			return coreActionMsg{actionMsg{err: err}}
		}
		return coreActionMsg{actionMsg{note: message("%s stopped", s.ToolLabel())}}
	}
}

func restartCmd(s *config.Settings) tea.Cmd {
	return func() tea.Msg {
		if err := mihomo.Restart(s); err != nil {
			return coreActionMsg{actionMsg{err: err}}
		}
		return coreActionMsg{actionMsg{note: message("%s restarted", s.ToolLabel())}}
	}
}

// toggleTunCmd assumes s.TunEnable has already been flipped and saved.
func toggleTunCmd(s *config.Settings) tea.Cmd {
	want := s.TunEnable
	return func() tea.Msg {
		if err := mihomo.ApplyTun(s); err != nil {
			return actionMsg{err: err}
		}
		if want {
			return actionMsg{note: message("TUN on")}
		}
		return actionMsg{note: message("TUN off")}
	}
}

// enableTunCmd performs the privileged part of enabling TUN (shutting down a
// foreign TUN device, granting net caps), then enables it. The password stays
// in this closure and is never persisted or logged.
func enableTunCmd(s *config.Settings, password string, delDevs []string) tea.Cmd {
	return func() tea.Msg {
		for _, dev := range delDevs {
			if err := mihomo.DeleteTunDevice(password, dev); err != nil {
				return actionMsg{err: message("shut down %s: %v", dev, err)}
			}
		}
		needRestart := false
		if mihomo.MissingTunCaps(s) {
			if password == "" {
				return actionMsg{err: message("%s lacks net caps; press t again to enter the sudo password", s.ToolLabel())}
			}
			if err := mihomo.GrantNetCaps(password, s.Binary()); err != nil {
				return actionMsg{err: err}
			}
			needRestart = true // caps are read at exec time; the running process doesn't gain them
		}
		s.TunEnable = true
		if err := s.Save(); err != nil {
			return actionMsg{err: err}
		}
		if running, _ := mihomo.Running(s); running && needRestart {
			if err := mihomo.Restart(s); err != nil {
				return actionMsg{err: message("restart after setcap: %v", err)}
			}
		} else if err := mihomo.ApplyTun(s); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{note: message("TUN on")}
	}
}

// switchSubCmd makes subscription name active; see mihomo.SwitchSub. s.Active
// is only updated (by the subSwitchedMsg handler) once the switch succeeded.
func switchSubCmd(s *config.Settings, name string) tea.Cmd {
	return func() tea.Msg {
		return subSwitchedMsg{name: name, err: mihomo.SwitchSub(s, name)}
	}
}

// coreDownloadCmd replaces the configured binary for tool. A running core is
// stopped first and started again after the same path has been overwritten.
func coreDownloadCmd(s *config.Settings, tool, channel, password string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		res, err := core.Install(ctx, s, tool, channel, password, true)
		if err != nil {
			if res == nil {
				return actionMsg{err: err}
			}
			return actionMsg{note: coreNote(tool, res), err: err}
		}
		return actionMsg{note: coreNote(tool, res), notice: true}
	}
}

func geoDownloadCmd(s *config.Settings, tools []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		res, err := geo.Install(ctx, s, tools, true, func(tool string) bool {
			if tool != s.Tool() {
				return false
			}
			running, _ := mihomo.Running(s)
			return running
		}, func(string) error {
			return mihomo.Restart(s)
		})
		if err != nil {
			if res == nil {
				return actionMsg{err: err}
			}
			return actionMsg{note: geoNote(res), err: err}
		}
		return actionMsg{note: geoNote(res)}
	}
}

func downloadCmd(name, rawURL, dest string) tea.Cmd {
	return func() tea.Msg {
		res, err := subscription.Download(rawURL)
		if err != nil {
			return subDownloadedMsg{err: err}
		}
		if err := subscription.Store(dest, res); err != nil {
			return subDownloadedMsg{err: err}
		}
		return subDownloadedMsg{
			sub: config.Subscription{
				Name:      name,
				URL:       rawURL,
				UpdatedAt: time.Now().Format("2006-01-02 15:04"),
				UserInfo:  res.UserInfo,
				NoNodes:   res.NoNodes,
			},
			note: fetchedNote(message("downloaded %s", name), res),
		}
	}
}

// importFileCmd stores a subscription read from a local file. The resolved
// absolute path is kept so 'u' can re-read the file later.
func importFileCmd(name, rawPath, dest string) tea.Cmd {
	return func() tea.Msg {
		path, err := subscription.ResolvePath(rawPath)
		if err != nil {
			return subDownloadedMsg{err: err}
		}
		res, err := subscription.ReadFile(path)
		if err != nil {
			return subDownloadedMsg{err: err}
		}
		if err := subscription.Store(dest, res); err != nil {
			return subDownloadedMsg{err: err}
		}
		return subDownloadedMsg{
			sub: config.Subscription{
				Name:      name,
				Path:      path,
				UpdatedAt: time.Now().Format("2006-01-02 15:04"),
			},
			note: fetchedNote(message("imported %s", name), res),
		}
	}
}

func editSubCmd(s *config.Settings, oldName, name, rawSource string) tea.Cmd {
	return func() tea.Msg {
		isFile := subscription.IsLocalPath(rawSource)
		source := rawSource
		if isFile {
			var err error
			source, err = subscription.ResolvePath(rawSource)
			if err != nil {
				return subEditedMsg{err: err}
			}
		} else {
			u, err := url.Parse(source)
			if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" {
				return subEditedMsg{err: message("source must be an HTTP(S) URL or file path")}
			}
		}
		changed, err := s.EditSub(oldName, name, source, isFile)
		return subEditedMsg{name: name, sourceChanged: changed, err: err}
	}
}

func editLocalNodeCmd(s *config.Settings, oldName, name, link string) tea.Cmd {
	return func() tea.Msg {
		if err := s.EditLocalNode(oldName, name, strings.TrimSpace(link)); err != nil {
			return subEditedMsg{err: err}
		}
		if err := reloadRuntimeConfig(s); err != nil {
			return subEditedMsg{name: name, err: fmt.Errorf("saved %s; reload: %w", name, err)}
		}
		return subEditedMsg{name: name}
	}
}

func completePathCmd(input string) tea.Cmd {
	return func() tea.Msg {
		value, matches := completePath(input)
		return pathCompletionMsg{input: input, value: value, matches: matches}
	}
}

func addLocalNodeCmd(s *config.Settings, rawLink string) tea.Cmd {
	return func() tea.Msg {
		node, err := s.AddLocalNode(rawLink)
		if err != nil {
			return actionMsg{err: err}
		}
		if err := s.Save(); err != nil {
			return actionMsg{err: err}
		}
		if err := reloadRuntimeConfig(s); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{note: message("added local node %s", node.Name)}
	}
}

func reloadRuntimeConfigCmd(s *config.Settings, note text) tea.Cmd {
	return func() tea.Msg {
		if err := reloadRuntimeConfig(s); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{note: note}
	}
}

func reloadRuntimeConfig(s *config.Settings) error {
	return mihomo.Reload(s)
}

func loadGroupsCmd(s *config.Settings, note ...text) tea.Cmd {
	controller, secret := s.Controller, s.Secret
	return func() tea.Msg {
		if running, _ := mihomo.Running(s); !running {
			return groupsMsg{err: message("%s is not running; start it from dashboard first", s.ToolLabel())}
		}
		if s.Tool() == config.ToolXray {
			groups, err := s.XrayGroups()
			msg := groupsMsg{groups: groups, err: err}
			if len(note) > 0 {
				msg.note = note[0]
			}
			return msg
		}
		groups, err := api.New(controller, secret).Groups()
		if errors.Is(err, api.ErrUnauthorized) {
			err = message("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
		}
		msg := groupsMsg{groups: groups, err: err}
		if len(note) > 0 {
			msg.note = note[0]
		}
		return msg
	}
}

func setChainCmd(s *config.Settings, chain *config.ProxyChain) tea.Cmd {
	return func() tea.Msg {
		if err := mihomo.SetChain(s, chain); err != nil {
			return chainMsg{err: err}
		}
		running, _ := mihomo.Running(s)
		if chain == nil {
			return chainMsg{note: message("chain cleared"), running: running}
		}
		return chainMsg{note: message("chain: %s → %s; select %s in a group", chain.Entry, chain.Exit, config.ChainNodeName), running: running}
	}
}

func selectNodeAndLoadGroupsCmd(s *config.Settings, group, node string) tea.Cmd {
	controller, secret := s.Controller, s.Secret
	return func() tea.Msg {
		if running, _ := mihomo.Running(s); !running {
			return groupsMsg{err: message("%s is not running; start it from dashboard first", s.ToolLabel())}
		}
		if s.Tool() == config.ToolXray {
			s.XrayNode = node
			if err := s.Save(); err != nil {
				return groupsMsg{err: err}
			}
			if err := mihomo.Reload(s); err != nil {
				return groupsMsg{err: err}
			}
			groups, err := s.XrayGroups()
			return groupsMsg{groups: groups, note: message("%s → %s", group, node), err: err}
		}
		c := api.New(controller, secret)
		if err := c.SelectNode(group, node); err != nil {
			if errors.Is(err, api.ErrUnauthorized) {
				err = message("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
			}
			return groupsMsg{err: err}
		}
		groups, err := c.Groups()
		if errors.Is(err, api.ErrUnauthorized) {
			err = message("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
		}
		return groupsMsg{groups: groups, note: message("%s → %s", group, node), err: err}
	}
}

func testNodeCmd(s *config.Settings, group, node string) tea.Cmd {
	controller, secret, testURL := s.Controller, s.Secret, s.TestURL
	return func() tea.Msg {
		if running, _ := mihomo.Running(s); !running {
			return delayMsg{err: message("%s is not running; start it from dashboard first", s.ToolLabel())}
		}
		if s.Tool() == config.ToolXray {
			delay, err := s.XrayDelay(node, testURL, 5*time.Second)
			if err != nil {
				return delayMsg{err: err}
			}
			return delayMsg{group: group, node: node, delays: map[string]int{node: delay}}
		}
		delay, err := api.New(controller, secret).Delay(node, testURL, 5000)
		if errors.Is(err, api.ErrUnauthorized) {
			err = message("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
		}
		if err != nil {
			return delayMsg{err: err}
		}
		return delayMsg{group: group, node: node, delays: map[string]int{node: delay}}
	}
}

func testGroupCmd(s *config.Settings, group string) tea.Cmd {
	controller, secret, testURL := s.Controller, s.Secret, s.TestURL
	return func() tea.Msg {
		if running, _ := mihomo.Running(s); !running {
			return delayMsg{err: message("%s is not running; start it from dashboard first", s.ToolLabel())}
		}
		if s.Tool() == config.ToolXray {
			delays, err := s.XrayGroupDelay(group, testURL, 5*time.Second)
			if err != nil {
				return delayMsg{err: err}
			}
			return delayMsg{group: group, delays: delays}
		}
		c := api.New(controller, secret)
		delays, err := c.GroupDelay(group, testURL, 5000)
		if err != nil && s.Tool() == config.ToolSingBox {
			delays, err = testGroupMembers(c, group, testURL)
		}
		if errors.Is(err, api.ErrUnauthorized) {
			err = message("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
		}
		if err != nil {
			return delayMsg{err: err}
		}
		return delayMsg{group: group, delays: delays}
	}
}

func testGroupMembers(c *api.Client, group, testURL string) (map[string]int, error) {
	groups, err := c.Groups()
	if err != nil {
		return nil, err
	}
	var members []string
	for _, g := range groups {
		if g.Name == group {
			members = g.All
			break
		}
	}
	if len(members) == 0 {
		return nil, message("group %s has no testable members", group)
	}
	delays := map[string]int{}
	for _, node := range members {
		delay, err := c.Delay(node, testURL, 5000)
		if err != nil {
			delays[node] = 0
			continue
		}
		delays[node] = delay
	}
	return delays, nil
}
