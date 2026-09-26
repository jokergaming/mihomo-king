package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mihomo-king/internal/config"
	"mihomo-king/internal/mihomo"
	"mihomo-king/internal/subscription"
)

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the managed core in the background",
		Args:  cobra.NoArgs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			if running, pid := mihomo.Running(s); running {
				fmt.Fprintf(cmd.OutOrStdout(), "%s already running (pid %d)\n", s.ToolLabel(), pid)
				return nil
			}
			if err := mihomo.Start(s); err != nil {
				return err
			}
			_, pid := mihomo.Running(s)
			fmt.Fprintf(cmd.OutOrStdout(), "%s started (pid %d)\n", s.ToolLabel(), pid)
			return nil
		}),
	}
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the managed core",
		Args:  cobra.NoArgs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			if running, _ := mihomo.Running(s); !running {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is not running\n", s.ToolLabel())
				return nil
			}
			if err := mihomo.Stop(s); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s stopped\n", s.ToolLabel())
			return nil
		}),
	}
}

func restartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the managed core (starts it if stopped)",
		Args:  cobra.NoArgs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			if err := mihomo.Restart(s); err != nil {
				return err
			}
			_, pid := mihomo.Running(s)
			fmt.Fprintf(cmd.OutOrStdout(), "%s restarted (pid %d)\n", s.ToolLabel(), pid)
			return nil
		}),
	}
}

func useCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "use <subscription>",
		Aliases:           []string{"switch"},
		Short:             "Switch the active subscription (validated first, rolled back on failure)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSubs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			name := args[0]
			if err := mihomo.SwitchSub(s, name); err != nil {
				return err
			}
			s.Active = name
			if err := s.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "switched to %s\n", name)
			return nil
		}),
	}
}

func updateCmd() *cobra.Command {
	var all, apply bool
	cmd := &cobra.Command{
		Use:   "update [subscription...]",
		Short: "Re-download subscriptions (default: the active one)",
		Long: `Re-download subscriptions from their URL, or re-read them from their file.
Without arguments the active subscription is updated. The new body is stored;
pass --apply to also load it when the active subscription was updated.`,
		ValidArgsFunction: completeSubs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			names := args
			switch {
			case all:
				names = nil
				for _, sub := range s.Subscriptions {
					names = append(names, sub.Name)
				}
			case len(names) == 0 && s.Active == "":
				return fmt.Errorf("no active subscription; name one or pass --all")
			case len(names) == 0:
				names = []string{s.Active}
			}
			out := cmd.OutOrStdout()
			var failed []string
			for _, name := range names {
				note, err := updateSub(s, name)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %v\n", name, err)
					failed = append(failed, name)
					continue
				}
				fmt.Fprintf(out, "%s: %s\n", name, note)
			}
			if err := s.Save(); err != nil {
				return err
			}
			if slices.Contains(names, s.Active) && !slices.Contains(failed, s.Active) {
				if active, _ := s.FindSub(s.Active); active != nil && active.NoNodes {
					fmt.Fprintln(out, "active subscription has no usable nodes; current runtime unchanged")
				} else if apply {
					if err := mihomo.SwitchSub(s, s.Active); err != nil {
						return err
					}
					fmt.Fprintf(out, "applied %s\n", s.Active)
				} else {
					fmt.Fprintf(out, "active subscription updated; run 'mihomo-king use %s' to load it\n", s.Active)
				}
			}
			if len(failed) > 0 {
				return fmt.Errorf("update failed: %s", strings.Join(failed, ", "))
			}
			return nil
		}),
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "update every subscription")
	cmd.Flags().BoolVar(&apply, "apply", false, "load the active subscription after updating it")
	return cmd
}

// updateSub re-fetches one subscription and records it in s (unsaved).
func updateSub(s *config.Settings, name string) (string, error) {
	sub, _ := s.FindSub(name)
	if sub == nil {
		return "", fmt.Errorf("no such subscription")
	}
	if err := config.ValidateSubName(sub.Name); err != nil {
		return "", err
	}
	var res *subscription.Result
	var err error
	if sub.Path != "" {
		res, err = subscription.ReadFile(sub.Path)
	} else {
		res, err = subscription.Download(sub.URL)
	}
	if err != nil {
		return "", err
	}
	if err := subscription.Store(s.SubPath(sub.Name), res); err != nil {
		return "", err
	}
	updated := *sub
	updated.UpdatedAt = time.Now().Format("2006-01-02 15:04")
	updated.NoNodes = res.NoNodes
	if sub.Path == "" {
		updated.UserInfo = res.UserInfo
	}
	s.UpsertSub(updated)
	note := "updated"
	if summary := res.Summary(); summary != "" {
		note += " · " + summary
	}
	if info := subscription.FormatUserInfo(updated.UserInfo); info != "" {
		note += " · " + info
	}
	return note, nil
}

func tunCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "tun [on|off|toggle]",
		Short: "Show or switch TUN mode",
		Long: `Without an argument, print whether TUN is on. Enabling TUN needs the core
binary to carry cap_net_admin,cap_net_raw (sudo setcap once, or use the TUI's
t key which can do it for you). Enabling is refused while another TUN device
(e.g. another clash's "Meta") is up unless --force is given.`,
		Args: cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []cobra.Completion{
			cobra.CompletionWithDesc("on", "enable TUN"),
			cobra.CompletionWithDesc("off", "disable TUN"),
			cobra.CompletionWithDesc("toggle", "flip TUN"),
		},
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			out := cmd.OutOrStdout()
			current := readStatus(s).Tun
			if len(args) == 0 {
				fmt.Fprintln(out, onOff(current))
				return nil
			}
			want := args[0] == "on" || args[0] == "toggle" && !current
			if want {
				if mihomo.MissingTunCaps(s) {
					return fmt.Errorf("%s lacks net caps; run: sudo setcap cap_net_admin,cap_net_raw=ep %s (then restart)", s.ToolLabel(), s.Binary())
				}
				if foreign := mihomo.ForeignTunDevices(s.TunDevice); len(foreign) > 0 && !force {
					return fmt.Errorf("other TUN devices are up (%s); two TUNs fight over routes — pass --force to enable anyway", strings.Join(foreign, ", "))
				}
			}
			s.TunEnable = want
			if err := s.Save(); err != nil {
				return err
			}
			if err := mihomo.ApplyTun(s); err != nil {
				return err
			}
			fmt.Fprintln(out, "tun", onOff(want))
			return nil
		}),
	}
	cmd.Flags().BoolVar(&force, "force", false, "enable even if another TUN device is up")
	return cmd
}

func modeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mode [rule|global|direct]",
		Short: "Show or switch the routing mode",
		Args:  cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []cobra.Completion{
			cobra.CompletionWithDesc("rule", "route by the subscription's rules"),
			cobra.CompletionWithDesc("global", "send everything through the GLOBAL group"),
			cobra.CompletionWithDesc("direct", "connect directly"),
		},
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				fmt.Fprintln(out, readStatus(s).Mode)
				return nil
			}
			if !slices.Contains(config.Modes(s.Tool()), args[0]) {
				return fmt.Errorf("mode must be one of: %s", strings.Join(config.Modes(s.Tool()), ", "))
			}
			s.Mode = args[0]
			if err := s.Save(); err != nil {
				return err
			}
			if err := mihomo.ApplyMode(s); err != nil {
				return err
			}
			fmt.Fprintln(out, "mode", s.Mode)
			return nil
		}),
	}
}
