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
	stable, alpha := path, path+"-alpha"
	if channel == Alpha {
		alpha = path
		var suffixed bool
		stable, suffixed = strings.CutSuffix(path, "-alpha")
		if !suffixed {
			// A legacy alpha can already be running under the unsuffixed name.
			stable = path + "-stable"
		}
	} else if legacy, ok := strings.CutSuffix(path, "-stable"); ok && legacy == s.AlphaBinaries[tool] {
		alpha = legacy
	}
	s.setStableBinary(tool, stable)
	s.AlphaBinaries = WithCoreValue(s.AlphaBinaries, tool, alpha)
}

func findMihomoBin() string  { return detectBin(ToolMihomo) }
func findSingBoxBin() string { return detectBin(ToolSingBox) }
func findXrayBin() string    { return detectBin(ToolXray) }

func detectBin(name string) string {
	home, _ := os.UserHomeDir()
	retired := legacyCoreDir(home)
	names := []string{name}
	if name == ToolMihomo {
		names = append(names, "clash-meta", "clash")
	}
	for _, candidate := range names {
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			if !filepath.IsAbs(dir) || filepath.Clean(dir) == retired {
				continue
			}
			if p, err := exec.LookPath(filepath.Join(dir, candidate)); err == nil {
				return p
			}
		}
	}
	return name
}

// Older settings stored generated destinations here. Never select them for installation.
func legacyCoreDir(home string) string { return filepath.Join(home, ".local", "share", "bin") }

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
	s.CoreBackupRoot, err = ResolveCoreBackupRoot(s.CoreBackupRoot)
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
		base := filepath.Join(home, ".local", "bin", tool)
		// Recognize obsolete generated settings only during migration.
		retired := legacyCoreDir(home)
		stable := corePathCandidate(s.stableBinary(tool), tool, base, retired)
		alpha := corePathCandidate(s.AlphaBinaries[tool], tool+"-alpha", base+"-alpha", retired)
		fresh := s.CoreChannels[tool] == ""
		if stable != "" && stable == alpha && s.AlphaBinaries[tool] == alpha {
			// PATH can rediscover an unsuffixed binary already recorded as alpha.
			stable = ""
		}
		checkVersion := fresh || stable != s.stableBinary(tool) || channel == Alpha && !fileExists(alpha)
		if stable != "" && stable != alpha && checkVersion && fileExists(stable) {
			line, _ := corebin.Version(context.Background(), stable, tool)
			if strings.Contains(strings.ToLower(filepath.Base(stable)+" "+line), "alpha") {
				if !fileExists(alpha) {
					alpha = stable
				}
				stable = ""
			}
		}
		useAlpha := alpha != "" && (stable == "" || !fileExists(stable) && (channel == Alpha || fileExists(alpha)))
		if useAlpha {
			s.SetCoreBinary(tool, Alpha, alpha)
			if fresh {
				channel = Alpha
			}
		} else {
			s.SetCoreBinary(tool, Stable, cmp.Or(stable, base))
		}
		s.CoreChannels = WithCoreValue(s.CoreChannels, tool, channel)
	}
	return nil
}

func corePathCandidate(saved, name, fallback, retired string) string {
	if filepath.Dir(saved) == retired || saved == fallback && !fileExists(saved) {
		saved = ""
	}
	if filepath.IsAbs(saved) {
		return saved
	}
	path := detectBin(cmp.Or(saved, name))
	if filepath.IsAbs(path) && filepath.Dir(path) != retired {
		return path
	}
	if saved != "" && saved != name {
		return saved
	}
	if fileExists(fallback) {
		return fallback
	}
	return ""
}

func ResolveCoreBackupRoot(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		raw = filepath.Join(home, raw[1:])
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("enter an absolute backup path or a path starting with ~/")
	}
	return filepath.Clean(raw), nil
}
