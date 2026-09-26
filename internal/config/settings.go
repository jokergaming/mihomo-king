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
// at <appdir>/subscriptions/<Name>.yaml. Updating re-downloads URL, or re-reads
// Path for a subscription imported from a local file.
type Subscription struct {
	Name      string `yaml:"name"`
	URL       string `yaml:"url,omitempty"`
	Path      string `yaml:"path,omitempty"`
	UpdatedAt string `yaml:"updated_at,omitempty"`
	UserInfo  string `yaml:"user_info,omitempty"` // raw subscription-userinfo header
	NoNodes   bool   `yaml:"no_nodes,omitempty"`
}

// LocalNode is a single node link pasted by the user. It is merged into the
// runtime config under the Local proxy group.
type LocalNode struct {
	Name string `yaml:"name"`
	Link string `yaml:"link"`
}

// Settings is the persisted application configuration.
type Settings struct {
	ManagedTool   string         `yaml:"managed_tool"`
	MihomoBin     string         `yaml:"mihomo_bin"`
	MihomoDir     string         `yaml:"mihomo_dir"`
	SingBoxBin    string         `yaml:"sing_box_bin"`
	SingBoxDir    string         `yaml:"sing_box_dir"`
	Controller    string         `yaml:"controller"`
	Secret        string         `yaml:"secret"`
	MixedPort     int            `yaml:"mixed_port"`
	Mode          string         `yaml:"mode"`
	LogLevel      string         `yaml:"log_level"`
	TestURL       string         `yaml:"test_url"`
	TunEnable     bool           `yaml:"tun_enable"`
	TunDevice     string         `yaml:"tun_device"`
	TunAddress    string         `yaml:"tun_address"`
	Active        string         `yaml:"active"` // active subscription name
	Subscriptions []Subscription `yaml:"subscriptions"`
	LocalNodes    []LocalNode    `yaml:"local_nodes,omitempty"`

	appDir string // resolved at Load; not persisted
}

const (
	ToolMihomo  = "mihomo"
	ToolSingBox = "sing-box"
)

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
func (s *Settings) PidFile() string         { return filepath.Join(s.appDir, s.Tool()+".pid") }
func (s *Settings) LogFile() string         { return filepath.Join(s.appDir, s.Tool()+".log") }
func (s *Settings) ConfigPath() string {
	if s.Tool() == ToolSingBox {
		return filepath.Join(s.SingBoxDir, "config.json")
	}
	return filepath.Join(s.MihomoDir, "config.yaml")
}

func (s *Settings) Tool() string {
	if s.ManagedTool == ToolSingBox {
		return ToolSingBox
	}
	return ToolMihomo
}

func (s *Settings) ToolLabel() string {
	if s.Tool() == ToolSingBox {
		return "sing-box"
	}
	return "mihomo"
}

func (s *Settings) Binary() string {
	if s.Tool() == ToolSingBox {
		return s.SingBoxBin
	}
	return s.MihomoBin
}

func (s *Settings) SetBinary(path string) {
	if s.Tool() == ToolSingBox {
		s.SingBoxBin = path
		return
	}
	s.MihomoBin = path
}

func (s *Settings) RuntimeDir() string {
	if s.Tool() == ToolSingBox {
		return s.SingBoxDir
	}
	return s.MihomoDir
}

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

func (s *Settings) FindLocalNode(name string) (*LocalNode, int) {
	for i := range s.LocalNodes {
		if s.LocalNodes[i].Name == name {
			return &s.LocalNodes[i], i
		}
	}
	return nil, -1
}

func (s *Settings) AddLocalNode(rawLink string) (LocalNode, error) {
	proxy, err := ParseProxyLink(rawLink)
	if err != nil {
		return LocalNode{}, err
	}
	node := LocalNode{Name: str(proxy["name"]), Link: strings.TrimSpace(rawLink)}
	if node.Name == "" {
		return LocalNode{}, fmt.Errorf("node name required")
	}
	if _, i := s.FindLocalNode(node.Name); i >= 0 {
		s.LocalNodes[i] = node
	} else {
		s.LocalNodes = append(s.LocalNodes, node)
	}
	return node, nil
}

func (s *Settings) RemoveLocalNode(name string) bool {
	if _, i := s.FindLocalNode(name); i >= 0 {
		s.LocalNodes = append(s.LocalNodes[:i], s.LocalNodes[i+1:]...)
		return true
	}
	return false
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
		ManagedTool: detectManagedTool(),
		MihomoBin:   findMihomoBin(),
		MihomoDir:   defaultMihomoDir(),
		SingBoxBin:  findSingBoxBin(),
		SingBoxDir:  defaultSingBoxDir(),
		Controller:  "127.0.0.1:9091", // not 9090: avoid colliding with a typical clash/meta controller
		TunDevice:   "mihomo-king",    // not the default "Meta": avoid colliding with another instance's tun
		TunAddress:  "172.19.0.1/30",
		MixedPort:   7890,
		Mode:        "rule",
		LogLevel:    "info",
		TestURL:     "http://www.gstatic.com/generate_204",
	}
}

func (s *Settings) applyDefaults() {
	if s.ManagedTool == "" {
		s.ManagedTool = detectManagedTool()
	}
	if s.ManagedTool != ToolSingBox {
		s.ManagedTool = ToolMihomo
	}
	if s.MihomoBin == "" {
		s.MihomoBin = findMihomoBin()
	}
	if s.MihomoDir == "" {
		s.MihomoDir = defaultMihomoDir()
	}
	if s.SingBoxBin == "" {
		s.SingBoxBin = findSingBoxBin()
	}
	if s.SingBoxDir == "" {
		s.SingBoxDir = defaultSingBoxDir()
	}
	if s.Controller == "" {
		s.Controller = "127.0.0.1:9091"
	}
	if s.TunDevice == "" {
		s.TunDevice = "mihomo-king"
	}
	if s.TunAddress == "" {
		s.TunAddress = "172.19.0.1/30"
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
	if s.TestURL == "" {
		s.TestURL = "http://www.gstatic.com/generate_204"
	}
	s.NormalizeForTool()
}

func (s *Settings) NormalizeForTool() {
	if !validOption(s.Mode, Modes(s.Tool())) {
		s.Mode = "rule"
	}
	if !validOption(s.LogLevel, LogLevels(s.Tool())) {
		s.LogLevel = "info"
	}
}

func Modes(tool string) []string {
	return []string{"rule", "global", "direct"}
}

func LogLevels(tool string) []string {
	if tool == ToolSingBox {
		return []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}
	}
	return []string{"debug", "info", "warning", "error", "silent"}
}

func validOption(value string, options []string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
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
	if p := findBin([]string{"mihomo", "clash-meta", "clash"}, "mihomo"); p != "" {
		return p
	}
	return "mihomo"
}

func findSingBoxBin() string {
	if p := findBin([]string{"sing-box"}, "sing-box"); p != "" {
		return p
	}
	return "sing-box"
}

func findBin(names []string, fallback string) string {
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, ".bin", "cmd", fallback); fileExists(p) {
			return p
		}
	}
	return ""
}

func detectManagedTool() string {
	if p := findMihomoBin(); p != "mihomo" {
		return ToolMihomo
	}
	if p := findSingBoxBin(); p != "sing-box" {
		return ToolSingBox
	}
	return ToolMihomo
}

func defaultMihomoDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mihomo")
}

func defaultSingBoxDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sing-box")
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
