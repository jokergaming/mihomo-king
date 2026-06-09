// Package config manages mihomo-king's own settings and the generation of the
// mihomo runtime config.yaml from a subscription.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Subscription is a stored subscription's metadata. Its YAML body lives on disk
// at <appdir>/subscriptions/<Name>.yaml.
type Subscription struct {
	Name      string `yaml:"name"`
	URL       string `yaml:"url"`
	UpdatedAt string `yaml:"updated_at,omitempty"`
	UserInfo  string `yaml:"user_info,omitempty"` // raw subscription-userinfo header
}

// Settings is the persisted application configuration.
type Settings struct {
	MihomoBin     string         `yaml:"mihomo_bin"`
	MihomoDir     string         `yaml:"mihomo_dir"`
	Controller    string         `yaml:"controller"`
	Secret        string         `yaml:"secret"`
	MixedPort     int            `yaml:"mixed_port"`
	Mode          string         `yaml:"mode"`
	LogLevel      string         `yaml:"log_level"`
	TunEnable     bool           `yaml:"tun_enable"`
	TunDevice     string         `yaml:"tun_device"`
	Active        string         `yaml:"active"` // active subscription name
	Subscriptions []Subscription `yaml:"subscriptions"`

	appDir string // resolved at Load; not persisted
}

// Load reads settings.yaml (creating it with defaults if absent), ensures the
// app directories and a secret exist, and persists any fill-ins.
func Load() (*Settings, error) {
	dir, err := appDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "subscriptions"), 0o755); err != nil {
		return nil, err
	}

	s := defaults()
	s.appDir = dir

	path := filepath.Join(dir, "settings.yaml")
	switch data, err := os.ReadFile(path); {
	case err == nil:
		if err := yaml.Unmarshal(data, s); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		s.appDir = dir
	case !os.IsNotExist(err):
		return nil, err
	}

	s.applyDefaults()
	s.ensureSecret()
	if err := s.Save(); err != nil {
		return nil, err
	}
	return s, nil
}

// Save writes settings.yaml.
func (s *Settings) Save() error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.appDir, "settings.yaml"), data, 0o600)
}

// Path helpers.
func (s *Settings) AppDir() string          { return s.appDir }
func (s *Settings) SubsDir() string         { return filepath.Join(s.appDir, "subscriptions") }
func (s *Settings) SubPath(n string) string { return filepath.Join(s.SubsDir(), n+".yaml") }
func (s *Settings) PidFile() string         { return filepath.Join(s.appDir, "mihomo.pid") }
func (s *Settings) LogFile() string         { return filepath.Join(s.appDir, "mihomo.log") }
func (s *Settings) ConfigPath() string      { return filepath.Join(s.MihomoDir, "config.yaml") }

// FindSub returns the subscription with the given name and its index, or (nil, -1).
func (s *Settings) FindSub(name string) (*Subscription, int) {
	for i := range s.Subscriptions {
		if s.Subscriptions[i].Name == name {
			return &s.Subscriptions[i], i
		}
	}
	return nil, -1
}

// UpsertSub inserts or replaces a subscription by name.
func (s *Settings) UpsertSub(sub Subscription) {
	if _, i := s.FindSub(sub.Name); i >= 0 {
		s.Subscriptions[i] = sub
		return
	}
	s.Subscriptions = append(s.Subscriptions, sub)
}

// RemoveSub deletes a subscription by name (and clears Active if it matched).
func (s *Settings) RemoveSub(name string) {
	if _, i := s.FindSub(name); i >= 0 {
		s.Subscriptions = append(s.Subscriptions[:i], s.Subscriptions[i+1:]...)
	}
	if s.Active == name {
		s.Active = ""
	}
}

// ActiveYAML reads the active subscription's stored YAML body, or (nil, nil) if
// none is set.
func (s *Settings) ActiveYAML() ([]byte, error) {
	if s.Active == "" {
		return nil, nil
	}
	return os.ReadFile(s.SubPath(s.Active))
}

// ValidateSubName rejects names that would escape the subscriptions directory
// or create awkward hidden/control-character filenames.
func ValidateSubName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("subscription name cannot be %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("subscription name cannot contain path separators")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("subscription name cannot contain control characters")
		}
	}
	return nil
}

func defaults() *Settings {
	return &Settings{
		MihomoBin:  findMihomoBin(),
		MihomoDir:  defaultMihomoDir(),
		Controller: "127.0.0.1:9091", // not 9090: avoid colliding with a typical clash/meta controller
		TunDevice:  "mihomo-king",    // not the default "Meta": avoid colliding with another instance's tun
		MixedPort:  7890,
		Mode:       "rule",
		LogLevel:   "info",
	}
}

func (s *Settings) applyDefaults() {
	if s.MihomoBin == "" {
		s.MihomoBin = findMihomoBin()
	}
	if s.MihomoDir == "" {
		s.MihomoDir = defaultMihomoDir()
	}
	if s.Controller == "" {
		s.Controller = "127.0.0.1:9091"
	}
	if s.TunDevice == "" {
		s.TunDevice = "mihomo-king"
	}
	if s.MixedPort == 0 {
		s.MixedPort = 7890
	}
	if s.Mode == "" {
		s.Mode = "rule"
	}
	if s.LogLevel == "" {
		s.LogLevel = "info"
	}
}

func (s *Settings) ensureSecret() {
	if s.Secret != "" {
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		s.Secret = "mihomo-king"
		return
	}
	s.Secret = hex.EncodeToString(b)
}

func findMihomoBin() string {
	for _, name := range []string{"mihomo", "clash-meta", "clash"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, ".bin", "cmd", "mihomo"); fileExists(p) {
			return p
		}
	}
	return "mihomo"
}

func defaultMihomoDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mihomo")
}

func appDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "mihomo-king"), nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
