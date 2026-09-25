// Package cli is mihomo-king's command line, built on cobra. Without a
// subcommand it starts the TUI; subcommands print build and runtime
// information and run the TUI's common actions non-interactively. Shell
// completion (including subscription, group and node names) comes from
// cobra's completion command.
package cli

import (
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
	"mihomo-king/internal/mihomo"
	"mihomo-king/internal/tui"
)

const (
	groupInfo    = "info"
	groupControl = "control"
	groupProxies = "proxies"
)

// Execute runs the command line and returns the process exit status.
func Execute() int {
	if err := NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "mihomo-king:", err)
		return 1
	}
	return 0
}

// NewRoot builds the command tree.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "mihomo-king",
		Short: "TUI and CLI for a mihomo / sing-box proxy",
		Long: `mihomo-king manages a mihomo (or sing-box) proxy: subscriptions, TUN mode,
routing mode and node selection. Run it without a command to open the TUI.`,
		Version:       versionLine(),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSettings()
			if err != nil {
				return err
			}
			_, err = tea.NewProgram(tui.New(s), tea.WithAltScreen()).Run()
			return err
		},
	}
	root.SetVersionTemplate("mihomo-king {{.Version}}\n")
	root.AddGroup(
		&cobra.Group{ID: groupInfo, Title: "Information:"},
		&cobra.Group{ID: groupControl, Title: "Control:"},
		&cobra.Group{ID: groupProxies, Title: "Proxies:"},
	)
	root.SetHelpCommandGroupID(groupInfo)
	root.SetCompletionCommandGroupID(groupInfo)

	for _, c := range []*cobra.Command{statusCmd(), versionCmd(), pathsCmd(), subsCmd(), checkCmd(), logCmd()} {
		c.GroupID = groupInfo
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{startCmd(), stopCmd(), restartCmd(), useCmd(), updateCmd(), tunCmd(), modeCmd()} {
		c.GroupID = groupControl
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{groupsCmd(), nodesCmd(), selectCmd(), delayCmd()} {
		c.GroupID = groupProxies
		root.AddCommand(c)
	}
	return root
}

var settings *config.Settings

// loadSettings loads settings once per process.
func loadSettings() (*config.Settings, error) {
	if settings == nil {
		s, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("load settings: %w", err)
		}
		settings = s
	}
	return settings, nil
}

// liveClient returns an API client for the running core.
func liveClient(s *config.Settings) (*api.Client, error) {
	if running, _ := mihomo.Running(s); !running {
		return nil, fmt.Errorf("%s is not running; run 'mihomo-king start' first", s.ToolLabel())
	}
	return api.New(s.Controller, s.Secret), nil
}

// withSettings adapts a command body that needs settings to cobra's RunE.
func withSettings(run func(cmd *cobra.Command, s *config.Settings, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		s, err := loadSettings()
		if err != nil {
			return err
		}
		return run(cmd, s, args)
	}
}

// apiErr rewords an unauthorized error into something actionable.
func apiErr(s *config.Settings, err error) error {
	if errors.Is(err, api.ErrUnauthorized) {
		return fmt.Errorf("controller rejected the saved secret; restart %s or check %s", s.ToolLabel(), s.ConfigPath())
	}
	return err
}
