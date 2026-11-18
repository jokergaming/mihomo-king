package config

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mihomo-king/internal/corebin"
)

const Stable = "stable"
const Alpha = "alpha"

func (s *Settings) Channel(tool string) string { return cmp.Or(s.CoreChannels[tool], Stable) }

func (s *Settings) CoreBinary(tool, channel string) string {
	if channel == Alpha {
		return s.AlphaBinaries[tool]
	}
	return s.stableBinary(tool)
}

// Clone before changing maps: in-flight TUI commands hold settings snapshots.
func WithCoreValue(values map[string]string, tool, value string) map[string]string {
	copy := maps.Clone(values)
	if copy == nil {
		copy = make(map[string]string)
	}
	copy[tool] = value
	return copy
}

func (s *Settings) SetCoreBinary(tool, channel, path string) {
	if channel == Alpha {
		s.AlphaBinaries = WithCoreValue(s.AlphaBinaries, tool, path)
		return
	}
	s.setStableBinary(tool, path)
}

func findMihomoBin() string  { return detectBin(ToolMihomo) }
func findSingBoxBin() string { return detectBin(ToolSingBox) }
func findXrayBin() string    { return detectBin(ToolXray) }

func detectBin(name string) string {
	names := []string{name}
	if name == ToolMihomo {
		names = append(names, "clash-meta", "clash")
	}
	for _, candidate := range names {
		if p, err := exec.LookPath(candidate); err == nil {
			return p
		}
	}
	return name
}

func detectManagedTool() string {
	for _, tool := range []string{ToolMihomo, ToolSingBox, ToolXray} {
		if detectBin(tool) != tool || detectBin(tool+"-alpha") != tool+"-alpha" {
			return tool
		}
	}
	return ToolMihomo
}

func (s *Settings) resolveCorePaths() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if s.CoreBackupCount < 0 {
		return fmt.Errorf("core backup count must be non-negative")
	}
	for _, tool := range []string{ToolMihomo, ToolSingBox, ToolXray} {
		channel := s.Channel(tool)
		if channel != Stable && channel != Alpha {
			return fmt.Errorf("invalid %s channel %q", tool, channel)
		}
		base := filepath.Join(home, ".local", "share", "bin", tool)
		stable := s.stableBinary(tool)
		discovered := false
		if stable == base {
			if _, err := os.Stat(base); os.IsNotExist(err) {
				if path := detectBin(tool); filepath.IsAbs(path) && path != s.AlphaBinaries[tool] {
					stable, discovered = path, true
				}
			}
		}
		if !filepath.IsAbs(stable) {
			found := detectBin(stable)
			if filepath.IsAbs(found) {
				stable = found
			} else if stable == tool {
				stable = base
			}
		}
		alpha := s.AlphaBinaries[tool]
		if alpha == "" || (alpha == base+"-alpha" && !fileExists(alpha)) {
			alpha = detectBin(tool + "-alpha")
			if alpha == tool+"-alpha" {
				alpha = base + "-alpha"
			}
		}
		if s.CoreChannels[tool] == "" || discovered {
			// Older versions used a single path for both channels.
			if path, err := exec.LookPath(stable); err == nil {
				line, _ := corebin.Version(context.Background(), path, tool)
				if strings.Contains(strings.ToLower(filepath.Base(path)+" "+line), "alpha") {
					// A newly discovered alpha must not replace a saved custom path.
					if !discovered || (alpha == base+"-alpha" && !fileExists(alpha)) {
						alpha = stable
					}
					stable = base
					if !discovered {
						channel = Alpha
					}
					if stable == alpha {
						stable += "-stable"
					}
				}
			} else if _, err := exec.LookPath(alpha); err == nil {
				channel = Alpha
			}
			s.CoreChannels = WithCoreValue(s.CoreChannels, tool, channel)
		}
		s.setStableBinary(tool, stable)
		s.AlphaBinaries = WithCoreValue(s.AlphaBinaries, tool, alpha)
	}
	return nil
}
