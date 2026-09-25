package cli

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
	"mihomo-king/internal/subscription"
)

func isSelector(g api.Group) bool {
	return strings.EqualFold(g.Type, "selector") || strings.EqualFold(g.Type, "select")
}

func findGroup(groups []api.Group, name string) *api.Group {
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i]
		}
	}
	return nil
}

// liveGroups returns the running core's groups.
func liveGroups(s *config.Settings) (*api.Client, []api.Group, error) {
	c, err := liveClient(s)
	if err != nil {
		return nil, nil, err
	}
	groups, err := c.Groups()
	if err != nil {
		return nil, nil, apiErr(s, err)
	}
	return c, groups, nil
}

func groupsCmd() *cobra.Command {
	var selectors bool
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "List proxy groups",
		Args:  cobra.NoArgs,
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			_, groups, err := liveGroups(s)
			if err != nil {
				return err
			}
			w := newTable(cmd.OutOrStdout())
			fmt.Fprintln(w, "GROUP\tTYPE\tNODES\tNOW")
			for _, g := range groups {
				if selectors && !isSelector(g) {
					continue
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", g.Name, g.Type, len(g.All), g.Now)
			}
			return w.Flush()
		}),
	}
	cmd.Flags().BoolVarP(&selectors, "selectors", "s", false, "only groups that accept a manual pick")
	return cmd
}

func nodesCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "nodes <group>",
		Short:             "List a group's members (* marks the current pick)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeGroupThenMember(false),
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			_, groups, err := liveGroups(s)
			if err != nil {
				return err
			}
			g := findGroup(groups, args[0])
			if g == nil {
				return fmt.Errorf("no group named %q", args[0])
			}
			out := cmd.OutOrStdout()
			for _, name := range g.All {
				mark := " "
				if name == g.Now {
					mark = "*"
				}
				fmt.Fprintf(out, "%s %s\n", mark, name)
			}
			return nil
		}),
	}
}

func selectCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "select <group> <node>",
		Short:             "Pick a node in a select group",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeGroupThenMember(true),
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			c, groups, err := liveGroups(s)
			if err != nil {
				return err
			}
			group, node := args[0], args[1]
			g := findGroup(groups, group)
			switch {
			case g == nil:
				return fmt.Errorf("no group named %q", group)
			case !isSelector(*g):
				return fmt.Errorf("%s picks nodes automatically (%s); only select groups are switchable", group, g.Type)
			case !slices.Contains(g.All, node):
				return fmt.Errorf("%s has no member %q", group, node)
			}
			if err := c.SelectNode(group, node); err != nil {
				return apiErr(s, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s → %s\n", group, node)
			return nil
		}),
	}
}

func delayCmd() *cobra.Command {
	var testURL string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "delay <group|node> [node]",
		Short: "Latency-test a node, or every member of a group",
		Long: `Latency-test through the running core. With a group, every member is tested
and results are sorted fastest first; with a group and a node, or a single
node name, only that node is tested.`,
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completeGroupThenMember(false),
		RunE: withSettings(func(cmd *cobra.Command, s *config.Settings, args []string) error {
			c, groups, err := liveGroups(s)
			if err != nil {
				return err
			}
			url := cmp.Or(testURL, s.TestURL)
			ms := int(timeout.Milliseconds())
			out := cmd.OutOrStdout()
			g := findGroup(groups, args[0])
			if len(args) == 2 || g == nil {
				node := args[len(args)-1]
				d, err := c.Delay(node, url, ms)
				if err != nil {
					return fmt.Errorf("%s: %w", node, apiErr(s, err))
				}
				w := newTable(out)
				fmt.Fprintf(w, "%s\t%s\n", node, delayText(d))
				return w.Flush()
			}
			delays, err := c.GroupDelay(g.Name, url, ms)
			if err != nil && s.Tool() == config.ToolSingBox {
				delays, err = map[string]int{}, nil
				for _, node := range g.All {
					delays[node], _ = c.Delay(node, url, ms)
				}
			}
			if err != nil {
				return apiErr(s, err)
			}
			names := slices.Clone(g.All)
			slices.SortStableFunc(names, func(a, b string) int {
				da, db := delays[a], delays[b]
				if (da > 0) != (db > 0) {
					if da > 0 {
						return -1
					}
					return 1
				}
				return cmp.Compare(da, db)
			})
			w := newTable(out)
			for _, name := range names {
				fmt.Fprintf(w, "%s\t%s\n", name, delayText(delays[name]))
			}
			return w.Flush()
		}),
	}
	cmd.Flags().StringVar(&testURL, "url", "", "test URL (default: the saved test_url)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "per-node timeout")
	return cmd
}

func delayText(ms int) string {
	if ms <= 0 {
		return "timeout"
	}
	return fmt.Sprintf("%d ms", ms)
}

// --- dynamic completion ---

const noFiles = cobra.ShellCompDirectiveNoFileComp

// completionGroups fetches groups for completion with a short timeout, so a
// stopped or hung core never stalls the shell.
func completionGroups() []api.Group {
	s, err := loadSettings()
	if err != nil {
		return nil
	}
	c, err := liveClient(s)
	if err != nil {
		return nil
	}
	c.SetTimeouts(2*time.Second, 2*time.Second, 0)
	groups, _ := c.Groups()
	return groups
}

// completeSubs completes subscription names not already given.
func completeSubs(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if cmd.Args != nil && cmd.Args(cmd, append(slices.Clone(args), "x")) != nil {
		return nil, noFiles // no room for another argument
	}
	s, err := loadSettings()
	if err != nil {
		return nil, noFiles
	}
	var out []cobra.Completion
	for _, sub := range s.Subscriptions {
		if slices.Contains(args, sub.Name) {
			continue
		}
		desc := cmp.Or(subscription.FormatUserInfo(sub.UserInfo), sub.UpdatedAt)
		if sub.Name == s.Active {
			desc = strings.TrimSuffix("active · "+desc, " · ")
		}
		out = append(out, cobra.CompletionWithDesc(sub.Name, desc))
	}
	return out, noFiles
}

// completeGroupThenMember completes a group name, then one of its members.
func completeGroupThenMember(selectorsOnly bool) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 1 || len(args) == 1 && cmd.Name() == "nodes" {
			return nil, noFiles
		}
		groups := completionGroups()
		var out []cobra.Completion
		if len(args) == 0 {
			for _, g := range groups {
				if selectorsOnly && !isSelector(g) {
					continue
				}
				out = append(out, cobra.CompletionWithDesc(g.Name, fmt.Sprintf("%s · now %s", g.Type, g.Now)))
			}
			return out, noFiles
		}
		if g := findGroup(groups, args[0]); g != nil {
			for _, name := range g.All {
				desc := ""
				if name == g.Now {
					desc = "current"
				}
				out = append(out, cobra.CompletionWithDesc(name, desc))
			}
		}
		return out, noFiles
	}
}
