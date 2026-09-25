package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mihomo-king/internal/config"
	"mihomo-king/internal/mihomo"
	"mihomo-king/internal/subscription"
)

type status struct {
	Tool         string   `json:"tool"`
	Binary       string   `json:"binary"`
	Running      bool     `json:"running"`
	PID          int      `json:"pid,omitempty"`
	CoreVersion  string   `json:"core_version,omitempty"`
	Controller   string   `json:"controller"`
	MixedPort    int      `json:"mixed_port"`
	Mode         string   `json:"mode"`
	Tun          bool     `json:"tun"`
	TunDevice    string   `json:"tun_device"`
	Subscription string   `json:"subscription,omitempty"`
	Usage        string   `json:"usage,omitempty"`
	LocalNodes   int      `json:"local_nodes,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

func readStatus(s *config.Settings) status {
	st := status{
		Tool:         s.ToolLabel(),
		Binary:       s.Binary(),
		Controller:   s.Controller,
		MixedPort:    s.MixedPort,
		Mode:         s.Mode,
		Tun:          s.TunEnable,
		TunDevice:    s.TunDevice,
		Subscription: s.Active,
		LocalNodes:   len(s.LocalNodes),
	}
	st.Running, st.PID = mihomo.Running(s)
	if sub, _ := s.FindSub(s.Active); sub != nil {
		st.Usage = subscription.FormatUserInfo(sub.UserInfo)
	}
	if st.Running {
		c, _ := liveClient(s)
		if v, err := c.Version(); err == nil {
			st.CoreVersion = v
		} else {
			st.Warnings = append(st.Warnings, "controller unreachable: "+apiErr(s, err).Error())
		}
		if cfg, err := c.Configs(); err == nil {
			st.Mode = cfg.Mode
			if s.Tool() == config.ToolMihomo {
				st.Tun = cfg.Tun.Enable
			}
		}
		if s.Tool() == config.ToolSingBox {
			st.Tun = s.TunEnable && mihomo.TunDeviceUp(s.TunDevice)
		}
	}
	if s.TunEnable {
		if mihomo.MissingTunCaps(s) {
			st.Warnings = append(st.Warnings, fmt.Sprintf("%s lacks cap_net_admin/cap_net_raw; TUN will fail (sudo setcap cap_net_admin,cap_net_raw=ep %s)", s.ToolLabel(), s.Binary()))
		}
		if foreign := mihomo.ForeignTunDevices(s.TunDevice); len(foreign) > 0 {
			st.Warnings = append(st.Warnings, "other TUN devices are up: "+strings.Join(foreign, ", "))
		}
	}
	return st
}

func statusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show core, controller, mode, TUN and subscription state",
		Args:  cobra.NoArgs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			st := readStatus(s)
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(st)
			}
			w := newTable(out)
			state := "stopped"
			if st.Running {
				state = fmt.Sprintf("running (pid %d)", st.PID)
				if st.CoreVersion != "" {
					state += " · " + st.CoreVersion
				}
			}
			fmt.Fprintf(w, "%s\t%s\n", st.Tool, state)
			fmt.Fprintf(w, "controller\t%s\n", st.Controller)
			fmt.Fprintf(w, "mixed-port\t%d\n", st.MixedPort)
			fmt.Fprintf(w, "mode\t%s\n", st.Mode)
			fmt.Fprintf(w, "tun\t%s (%s)\n", onOff(st.Tun), st.TunDevice)
			sub := st.Subscription
			switch {
			case sub == "":
				sub = "none"
			case st.Usage != "":
				sub += " · " + st.Usage
			}
			fmt.Fprintf(w, "subscription\t%s\n", sub)
			if st.LocalNodes > 0 {
				fmt.Fprintf(w, "local nodes\t%d\n", st.LocalNodes)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			for _, warn := range st.Warnings {
				fmt.Fprintln(out, "warning:", warn)
			}
			return nil
		}),
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func pathsCmd() *cobra.Command {
	keys := []string{"settings", "subs", "dir", "config", "log", "pid", "bin"}
	return &cobra.Command{
		Use:   "paths [" + strings.Join(keys, "|") + "]",
		Short: "Show file locations, or print one (e.g. tail -f $(mihomo-king paths log))",
		Args:  cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []cobra.Completion{
			cobra.CompletionWithDesc("settings", "mihomo-king settings.yaml"),
			cobra.CompletionWithDesc("subs", "stored subscriptions"),
			cobra.CompletionWithDesc("dir", "core data dir"),
			cobra.CompletionWithDesc("config", "runtime config written for the core"),
			cobra.CompletionWithDesc("log", "core log"),
			cobra.CompletionWithDesc("pid", "core pid file"),
			cobra.CompletionWithDesc("bin", "core binary"),
		},
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			paths := map[string]string{
				"settings": s.AppDir() + "/settings.yaml",
				"subs":     s.SubsDir(),
				"dir":      s.RuntimeDir(),
				"config":   s.ConfigPath(),
				"log":      s.LogFile(),
				"pid":      s.PidFile(),
				"bin":      s.Binary(),
			}
			out := cmd.OutOrStdout()
			if len(args) == 1 {
				fmt.Fprintln(out, paths[args[0]])
				return nil
			}
			w := newTable(out)
			for _, k := range keys {
				fmt.Fprintf(w, "%s\t%s\n", k, paths[k])
			}
			return w.Flush()
		}),
	}
}

func subsCmd() *cobra.Command {
	var urls bool
	cmd := &cobra.Command{
		Use:     "subs",
		Aliases: []string{"subscriptions"},
		Short:   "List subscriptions and local nodes (* marks the active one)",
		Args:    cobra.NoArgs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			out := cmd.OutOrStdout()
			if len(s.Subscriptions) == 0 && len(s.LocalNodes) == 0 {
				fmt.Fprintln(out, "no subscriptions; add one in the TUI")
				return nil
			}
			w := newTable(out)
			fmt.Fprintln(w, "\tNAME\tUSAGE\tUPDATED\tSOURCE")
			for _, sub := range s.Subscriptions {
				mark := ""
				if sub.Name == s.Active {
					mark = "*"
				}
				source := sub.URL
				switch {
				case sub.Path != "":
					source = "file " + sub.Path
				case !urls:
					source = sourceHost(sub.URL)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", mark, sub.Name, dash(subscription.FormatUserInfo(sub.UserInfo)), dash(sub.UpdatedAt), source)
			}
			for _, node := range s.LocalNodes {
				fmt.Fprintf(w, "\tLocal / %s\t-\t-\tlocal node\n", node.Name)
			}
			return w.Flush()
		}),
	}
	cmd.Flags().BoolVar(&urls, "urls", false, "show full subscription URLs (they usually contain tokens)")
	return cmd
}

// sourceHost shortens a subscription URL to its host; the rest usually
// carries an access token.
func sourceHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "check [subscription]",
		Short:             "Validate a subscription's merged config with mihomo -t (default: the active one)",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeSubs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			if s.Tool() != config.ToolMihomo {
				return fmt.Errorf("check is only available for mihomo")
			}
			target := *s
			if len(args) == 1 {
				if sub, _ := s.FindSub(args[0]); sub == nil {
					return fmt.Errorf("no subscription named %q", args[0])
				}
				target.Active = args[0]
			}
			if err := mihomo.CheckActive(&target); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: config ok\n", target.Active)
			return nil
		}),
	}
}

func logCmd() *cobra.Command {
	var lines int
	var follow bool
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Print the end of the managed core's log",
		Args:  cobra.NoArgs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			f, err := os.Open(s.LogFile())
			if err != nil {
				return err
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprint(out, lastLines(string(data), lines))
			if !follow {
				return nil
			}
			for {
				time.Sleep(500 * time.Millisecond)
				if _, err := io.Copy(out, f); err != nil {
					return err
				}
			}
		}),
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 20, "number of lines to print (0 = all)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new lines")
	return cmd
}

// lastLines returns the last n lines of text (all of it when n <= 0).
func lastLines(text string, n int) string {
	if n <= 0 {
		return text
	}
	trimmed := strings.TrimSuffix(text, "\n")
	count := 0
	for i := len(trimmed) - 1; i >= 0; i-- {
		if trimmed[i] == '\n' {
			count++
			if count == n {
				return text[i+1:]
			}
		}
	}
	return text
}
