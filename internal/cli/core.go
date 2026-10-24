package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
)

func coreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "core",
		Short: "Download or upgrade a managed core",
		Long: `Download a mihomo, sing-box or xray release into the path already configured
for that core. An upgrade overwrites that same path and does not rewrite settings.
The GitHub release page is printed so its changes can be reviewed.
Replacing the file clears capabilities, so the command then sets
cap_net_admin,cap_net_raw=ep. A sudo password is read from the terminal
and is not stored.`,
	}
	cmd.AddCommand(coreDownloadCmd())
	return cmd
}

func coreDownloadCmd() *cobra.Command {
	var channel string
	var restart bool
	cmd := &cobra.Command{
		Use:   "download [mihomo|sing-box|xray]",
		Short: "Download a core release into its configured path",
		Long: `Download the selected core's release or alpha build and replace the binary at
its configured path. The path in settings is left unchanged. If that core is the
one currently running, it is stopped before the file is replaced and started
again afterwards unless --restart=false.

The GitHub release page is printed afterwards. Open it to see what changed.
Replacing the file clears file capabilities, so the command then sets
cap_net_admin,cap_net_raw=ep. A sudo password is read from the terminal and
is not stored.`,
		Example: `  mihomo-king core download
  mihomo-king core download xray --channel alpha
  mihomo-king core download sing-box --channel release`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeCores,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			tool := s.Tool()
			if len(args) == 1 {
				tool = args[0]
			}
			if _, err := core.SpecFor(tool); err != nil {
				return err
			}
			if !core.ValidChannel(channel) {
				return fmt.Errorf("channel must be %s", strings.Join(core.Channels(), " or "))
			}
			password, err := corePassword(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
			defer cancel()
			res, err := core.Install(ctx, s, tool, channel, password, restart)
			if res != nil {
				fmt.Fprintln(cmd.OutOrStdout(), core.Note(tool, res))
			}
			return err
		}),
	}
	cmd.Flags().StringVar(&channel, "channel", core.ChannelRelease, "release channel: "+strings.Join(core.Channels(), " or "))
	cmd.Flags().BoolVar(&restart, "restart", true, "restart the core when the replaced binary was running")
	_ = cmd.RegisterFlagCompletionFunc("channel", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return core.Channels(), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func corePassword(cmd *cobra.Command) (string, error) {
	if os.Geteuid() == 0 {
		return "", nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("open terminal for sudo password: %w", err)
	}
	defer tty.Close()
	fmt.Fprint(tty, "sudo password for setcap: ")
	pw, err := term.ReadPassword(tty.Fd())
	fmt.Fprintln(tty)
	if err != nil {
		return "", fmt.Errorf("read sudo password: %w", err)
	}
	if len(pw) == 0 {
		return "", fmt.Errorf("sudo password required to set cap_net_admin,cap_net_raw=ep")
	}
	return string(pw), nil
}

func completeCores(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{config.ToolMihomo, config.ToolSingBox, config.ToolXray}, cobra.ShellCompDirectiveNoFileComp
}
