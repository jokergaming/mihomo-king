package cli

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mihomo-king/internal/config"
)

// Version overrides the version stamped by the Go toolchain:
// go build -ldflags "-X mihomo-king/internal/cli.Version=v1.2.0"
var Version string

type buildInfo struct {
	version, commit, built string
	dirty                  bool
}

func readBuildInfo() buildInfo {
	b := buildInfo{version: Version}
	if info, ok := debug.ReadBuildInfo(); ok {
		// A pseudo-version (v0.0.0-<time>-<hash>) repeats the commit shown
		// separately; only a real tag is worth printing.
		if v := info.Main.Version; b.version == "" && v != "(devel)" && !strings.HasPrefix(v, "v0.0.0-") {
			b.version = v
		}
		for _, kv := range info.Settings {
			switch kv.Key {
			case "vcs.revision":
				b.commit = kv.Value
			case "vcs.time":
				b.built = kv.Value
			case "vcs.modified":
				b.dirty = kv.Value == "true"
			}
		}
	}
	if b.version == "" {
		b.version = "dev"
	}
	if len(b.commit) > 12 {
		b.commit = b.commit[:12]
	}
	return b
}

// versionLine is the one-line form printed by --version.
func versionLine() string {
	b := readBuildInfo()
	line := b.version
	if b.commit != "" && !strings.Contains(b.version, b.commit) {
		line += " (" + b.commit
		if b.dirty {
			line += ", dirty"
		}
		line += ")"
	}
	return line
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show build info and the managed core's version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			b := readBuildInfo()
			fmt.Fprintf(out, "mihomo-king %s\n", b.version)
			if b.commit != "" {
				dirty := ""
				if b.dirty {
					dirty = " (dirty)"
				}
				fmt.Fprintf(out, "  commit  %s%s\n", b.commit, dirty)
			}
			if b.built != "" {
				fmt.Fprintf(out, "  date    %s\n", b.built)
			}
			fmt.Fprintf(out, "  go      %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			if s, err := loadSettings(); err == nil {
				fmt.Fprintf(out, "  core    %s\n", coreVersion(s))
			}
			return nil
		},
	}
}

// coreVersion asks the managed binary for its version line.
func coreVersion(s *config.Settings) string {
	arg := "-v"
	if s.Tool() == config.ToolSingBox {
		arg = "version"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.Binary(), arg).Output()
	if err != nil {
		return fmt.Sprintf("%s (%s: %v)", s.ToolLabel(), s.Binary(), err)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}
