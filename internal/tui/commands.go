package tui

import (
	"cmp"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
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
}

type groupsMsg struct {
	groups []api.Group
	note   string
	err    error
}

type subDownloadedMsg struct {
	sub  config.Subscription
	note string
	err  error
}

type pathCompletionMsg struct {
	input   string   // path input the completion was computed for
	value   string   // completed input
	matches []string // entries matching the last path element
}

type actionMsg struct {
	note string
	err  error
}

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
			c := api.New(controller, secret)
			if v, err := c.Version(); err == nil {
				msg.version = v
			}
			if cfg, err := c.Configs(); err == nil {
				msg.mode = cfg.Mode
				if s.Tool() == config.ToolMihomo {
					msg.tun = cfg.Tun.Enable
				}
			}
			if s.Tool() == config.ToolSingBox {
				msg.tun = s.TunEnable && mihomo.TunDeviceUp(s.TunDevice)
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
			return actionMsg{err: err}
		}
		return actionMsg{note: s.ToolLabel() + " started"}
	}
}

func stopCmd(s *config.Settings) tea.Cmd {
	return func() tea.Msg {
		if err := mihomo.Stop(s); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{note: s.ToolLabel() + " stopped"}
	}
}

// toggleTunCmd assumes s.TunEnable has already been flipped and saved.
func toggleTunCmd(s *config.Settings) tea.Cmd {
	controller, secret, want := s.Controller, s.Secret, s.TunEnable
	return func() tea.Msg {
		if err := s.WriteActiveFromStore(); err != nil {
			return actionMsg{err: err}
		}
		if running, _ := mihomo.Running(s); running && s.Tool() == config.ToolMihomo {
			if err := api.New(controller, secret).SetTun(want); err != nil {
				return actionMsg{err: fmt.Errorf("tun live toggle: %w", err)}
			}
		} else if running {
			if err := mihomo.Restart(s); err != nil {
				return actionMsg{err: fmt.Errorf("restart for tun change: %w", err)}
			}
		}
		if want {
			return actionMsg{note: "TUN on"}
		}
		return actionMsg{note: "TUN off"}
	}
}

// enableTunCmd performs the privileged part of enabling TUN (shutting down a
// foreign TUN device, granting net caps), then enables it. The password stays
// in this closure and is never persisted or logged.
func enableTunCmd(s *config.Settings, password string, delDevs []string) tea.Cmd {
	controller, secret := s.Controller, s.Secret
	return func() tea.Msg {
		for _, dev := range delDevs {
			if err := mihomo.DeleteTunDevice(password, dev); err != nil {
				return actionMsg{err: fmt.Errorf("shut down %s: %v", dev, err)}
			}
		}
		needRestart := false
		if mihomo.MissingTunCaps(s) {
			if password == "" {
				return actionMsg{err: fmt.Errorf("%s lacks net caps; press t again to enter the sudo password", s.ToolLabel())}
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
		if err := s.WriteActiveFromStore(); err != nil {
			return actionMsg{err: err}
		}
		running, _ := mihomo.Running(s)
		switch {
		case running && needRestart:
			if err := mihomo.Restart(s); err != nil {
				return actionMsg{err: fmt.Errorf("restart after setcap: %v", err)}
			}
		case running && s.Tool() == config.ToolMihomo:
			if err := api.New(controller, secret).SetTun(true); err != nil {
				return actionMsg{err: fmt.Errorf("tun live toggle: %w", err)}
			}
		case running:
			if err := mihomo.Restart(s); err != nil {
				return actionMsg{err: fmt.Errorf("restart for tun change: %v", err)}
			}
		}
		return actionMsg{note: "TUN on"}
	}
}

// switchSubCmd makes subscription name active. Its config is validated first,
// and if the runtime still rejects it the previous subscription's config is
// restored, so a broken subscription never replaces a working one. s.Active
// is only updated (by the subSwitchedMsg handler) once the switch succeeded.
func switchSubCmd(s *config.Settings, name string) tea.Cmd {
	next := *s
	next.Active = name
	return func() tea.Msg {
		if err := mihomo.CheckActive(&next); err != nil {
			return subSwitchedMsg{name: name, err: fmt.Errorf("%s not activated: %w", name, err)}
		}
		if err := reloadRuntimeConfig(&next); err != nil {
			err = fmt.Errorf("%s not activated: %w", name, err)
			if restoreErr := reloadRuntimeConfig(s); restoreErr != nil {
				err = fmt.Errorf("%w; restoring %s also failed: %v", err, cmp.Or(s.Active, "previous config"), restoreErr)
			}
			return subSwitchedMsg{name: name, err: err}
		}
		return subSwitchedMsg{name: name}
	}
}

func downloadCmd(name, rawURL, dest string) tea.Cmd {
	return func() tea.Msg {
		res, err := subscription.Download(rawURL)
		if err != nil {
			return subDownloadedMsg{err: err}
		}
		if err := subscription.Store(dest, res.Body); err != nil {
			return subDownloadedMsg{err: err}
		}
		return subDownloadedMsg{
			sub: config.Subscription{
				Name:      name,
				URL:       rawURL,
				UpdatedAt: time.Now().Format("2006-01-02 15:04"),
				UserInfo:  res.UserInfo,
			},
			note: fetchedNote("downloaded "+name, res),
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
		if err := subscription.Store(dest, res.Body); err != nil {
			return subDownloadedMsg{err: err}
		}
		return subDownloadedMsg{
			sub: config.Subscription{
				Name:      name,
				Path:      path,
				UpdatedAt: time.Now().Format("2006-01-02 15:04"),
			},
			note: fetchedNote("imported "+name, res),
		}
	}
}

func fetchedNote(done string, res *subscription.Result) string {
	if summary := res.Summary(); summary != "" {
		return done + " · " + summary
	}
	return done
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
		return actionMsg{note: "added local node " + node.Name}
	}
}

func reloadRuntimeConfigCmd(s *config.Settings, note string) tea.Cmd {
	return func() tea.Msg {
		if err := reloadRuntimeConfig(s); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{note: note}
	}
}

func reloadRuntimeConfig(s *config.Settings) error {
	if err := s.WriteActiveFromStore(); err != nil {
		return err
	}
	if running, _ := mihomo.Running(s); running && s.Tool() == config.ToolMihomo {
		if err := api.New(s.Controller, s.Secret).ReloadConfig(s.ConfigPath()); err != nil {
			return fmt.Errorf("reload: %w", err)
		}
	} else if running {
		if err := mihomo.Restart(s); err != nil {
			return fmt.Errorf("restart: %w", err)
		}
	}
	return nil
}

func loadGroupsCmd(s *config.Settings) tea.Cmd {
	controller, secret := s.Controller, s.Secret
	return func() tea.Msg {
		if running, _ := mihomo.Running(s); !running {
			return groupsMsg{err: fmt.Errorf("%s is not running; start it from dashboard first", s.ToolLabel())}
		}
		groups, err := api.New(controller, secret).Groups()
		if errors.Is(err, api.ErrUnauthorized) {
			err = fmt.Errorf("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
		}
		return groupsMsg{groups: groups, err: err}
	}
}

func selectNodeAndLoadGroupsCmd(s *config.Settings, group, node string) tea.Cmd {
	controller, secret := s.Controller, s.Secret
	return func() tea.Msg {
		if running, _ := mihomo.Running(s); !running {
			return groupsMsg{err: fmt.Errorf("%s is not running; start it from dashboard first", s.ToolLabel())}
		}
		c := api.New(controller, secret)
		if err := c.SelectNode(group, node); err != nil {
			if errors.Is(err, api.ErrUnauthorized) {
				err = fmt.Errorf("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
			}
			return groupsMsg{err: err}
		}
		groups, err := c.Groups()
		if errors.Is(err, api.ErrUnauthorized) {
			err = fmt.Errorf("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
		}
		return groupsMsg{groups: groups, note: group + " → " + node, err: err}
	}
}

func testNodeCmd(s *config.Settings, group, node string) tea.Cmd {
	controller, secret, testURL := s.Controller, s.Secret, s.TestURL
	return func() tea.Msg {
		if running, _ := mihomo.Running(s); !running {
			return delayMsg{err: fmt.Errorf("%s is not running; start it from dashboard first", s.ToolLabel())}
		}
		delay, err := api.New(controller, secret).Delay(node, testURL, 5000)
		if errors.Is(err, api.ErrUnauthorized) {
			err = fmt.Errorf("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
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
			return delayMsg{err: fmt.Errorf("%s is not running; start it from dashboard first", s.ToolLabel())}
		}
		c := api.New(controller, secret)
		delays, err := c.GroupDelay(group, testURL, 5000)
		if err != nil && s.Tool() == config.ToolSingBox {
			delays, err = testGroupMembers(c, group, testURL)
		}
		if errors.Is(err, api.ErrUnauthorized) {
			err = fmt.Errorf("controller rejected the saved secret; restart %s from dashboard or check %s", s.ToolLabel(), s.ConfigPath())
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
		return nil, fmt.Errorf("group %s has no testable members", group)
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
