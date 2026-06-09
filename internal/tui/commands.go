package tui

import (
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
	err    error
}

type subDownloadedMsg struct {
	sub config.Subscription
	err error
}

type actionMsg struct {
	note string
	err  error
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
				msg.tun = cfg.Tun.Enable
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
		return actionMsg{note: "mihomo started"}
	}
}

func stopCmd(s *config.Settings) tea.Cmd {
	return func() tea.Msg {
		if err := mihomo.Stop(s); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{note: "mihomo stopped"}
	}
}

// toggleTunCmd assumes s.TunEnable has already been flipped and saved.
func toggleTunCmd(s *config.Settings) tea.Cmd {
	controller, secret, want := s.Controller, s.Secret, s.TunEnable
	return func() tea.Msg {
		if err := s.WriteActiveFromStore(); err != nil {
			return actionMsg{err: err}
		}
		if running, _ := mihomo.Running(s); running {
			if err := api.New(controller, secret).SetTun(want); err != nil {
				return actionMsg{err: fmt.Errorf("tun live toggle: %w", err)}
			}
		}
		if want {
			return actionMsg{note: "TUN on"}
		}
		return actionMsg{note: "TUN off"}
	}
}

// switchSubCmd assumes s.Active has already been set and saved.
func switchSubCmd(s *config.Settings) tea.Cmd {
	controller, secret, active := s.Controller, s.Secret, s.Active
	return func() tea.Msg {
		if err := s.WriteActiveFromStore(); err != nil {
			return actionMsg{err: err}
		}
		if running, _ := mihomo.Running(s); running {
			if err := api.New(controller, secret).ReloadConfig(s.ConfigPath()); err != nil {
				return actionMsg{err: fmt.Errorf("reload: %w", err)}
			}
		}
		return actionMsg{note: "switched to " + active}
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
		return subDownloadedMsg{sub: config.Subscription{
			Name:      name,
			URL:       rawURL,
			UpdatedAt: time.Now().Format("2006-01-02 15:04"),
			UserInfo:  res.UserInfo,
		}}
	}
}

func loadGroupsCmd(s *config.Settings) tea.Cmd {
	controller, secret := s.Controller, s.Secret
	return func() tea.Msg {
		if running, _ := mihomo.Running(s); !running {
			return groupsMsg{err: fmt.Errorf("mihomo is not running; start it from dashboard first")}
		}
		groups, err := api.New(controller, secret).Groups()
		if errors.Is(err, api.ErrUnauthorized) {
			err = fmt.Errorf("controller rejected the saved secret; restart mihomo from dashboard or check %s", s.ConfigPath())
		}
		return groupsMsg{groups: groups, err: err}
	}
}

func selectNodeCmd(s *config.Settings, group, node string) tea.Cmd {
	controller, secret := s.Controller, s.Secret
	return func() tea.Msg {
		if running, _ := mihomo.Running(s); !running {
			return actionMsg{err: fmt.Errorf("mihomo is not running; start it from dashboard first")}
		}
		err := api.New(controller, secret).SelectNode(group, node)
		if errors.Is(err, api.ErrUnauthorized) {
			err = fmt.Errorf("controller rejected the saved secret; restart mihomo from dashboard or check %s", s.ConfigPath())
		}
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{note: group + " → " + node}
	}
}
