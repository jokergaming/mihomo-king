package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSubName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "provider-a"},
		{name: "provider A"},
		{name: "", wantErr: true},
		{name: "   ", wantErr: true},
		{name: ".", wantErr: true},
		{name: "..", wantErr: true},
		{name: "../escape", wantErr: true},
		{name: `a\b`, wantErr: true},
		{name: "bad\nname", wantErr: true},
	}

	for _, tt := range tests {
		err := ValidateSubName(tt.name)
		if tt.wantErr && err == nil {
			t.Errorf("ValidateSubName(%q) returned nil, want error", tt.name)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("ValidateSubName(%q) returned %v, want nil", tt.name, err)
		}
	}
}

func TestSettingsApplyDefaultsAddsToolFields(t *testing.T) {
	s := &Settings{}
	s.applyDefaults()

	if s.Tool() != ToolMihomo {
		t.Fatalf("Tool() = %q, want %q", s.Tool(), ToolMihomo)
	}
	if s.MihomoBin == "" {
		t.Fatalf("MihomoBin was not defaulted")
	}
	if s.SingBoxBin == "" {
		t.Fatalf("SingBoxBin was not defaulted")
	}
	if s.SingBoxDir == "" {
		t.Fatalf("SingBoxDir was not defaulted")
	}
	if s.TunAddress != "172.19.0.1/30" {
		t.Fatalf("TunAddress = %q, want default address", s.TunAddress)
	}
}

func TestSettingsBinaryFollowsManagedTool(t *testing.T) {
	s := &Settings{ManagedTool: ToolMihomo, MihomoBin: "/bin/mihomo", SingBoxBin: "/bin/sing-box"}
	if got := s.Binary(); got != "/bin/mihomo" {
		t.Fatalf("Binary() = %q, want mihomo path", got)
	}
	s.ManagedTool = ToolSingBox
	if got := s.Binary(); got != "/bin/sing-box" {
		t.Fatalf("Binary() = %q, want sing-box path", got)
	}
}

func TestNormalizeForToolFixesInvalidLogLevel(t *testing.T) {
	s := &Settings{ManagedTool: ToolMihomo, Mode: "rule", LogLevel: "fatal"}
	s.NormalizeForTool()
	if s.LogLevel != "info" {
		t.Fatalf("mihomo log level = %q, want info", s.LogLevel)
	}

	s.ManagedTool = ToolSingBox
	s.LogLevel = "fatal"
	s.NormalizeForTool()
	if s.LogLevel != "fatal" {
		t.Fatalf("sing-box log level = %q, want fatal", s.LogLevel)
	}
}

func TestEditSubPreservesCacheAndActive(t *testing.T) {
	dir := t.TempDir()
	s := &Settings{
		appDir:        dir,
		Active:        "old",
		Subscriptions: []Subscription{{Name: "old", URL: "https://old.example/sub", UpdatedAt: "yesterday"}},
	}
	if err := os.MkdirAll(s.SubsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("proxies: [cached]\n")
	if err := os.WriteFile(s.SubPath("old"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := s.EditSub("old", "new", "https://new.example/sub", false)
	if err != nil || !changed {
		t.Fatalf("EditSub() changed=%v err=%v", changed, err)
	}
	if s.Active != "new" || !s.Subscriptions[0].NeedsUpdate || s.Subscriptions[0].UpdatedAt != "yesterday" {
		t.Fatalf("edited settings = %#v", s)
	}
	got, err := os.ReadFile(s.SubPath("new"))
	if err != nil || string(got) != string(body) {
		t.Fatalf("renamed cache = %q, %v", got, err)
	}
	if _, err := os.Stat(s.SubPath("old")); !os.IsNotExist(err) {
		t.Fatalf("old cache still exists: %v", err)
	}
	settings, err := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	if err != nil || !strings.Contains(string(settings), "active: new") || !strings.Contains(string(settings), "needs_update: true") {
		t.Fatalf("saved settings = %q, %v", settings, err)
	}
}

func TestEditSubRejectsNameCollisionWithoutReplacingCache(t *testing.T) {
	dir := t.TempDir()
	s := &Settings{appDir: dir, Subscriptions: []Subscription{{Name: "old"}, {Name: "taken"}}}
	if err := os.MkdirAll(s.SubsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.SubPath("taken"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditSub("old", "taken", "https://example.com", false); err == nil {
		t.Fatal("expected duplicate-name error")
	}
	got, err := os.ReadFile(s.SubPath("taken"))
	if err != nil || string(got) != "keep" {
		t.Fatalf("existing cache = %q, %v", got, err)
	}
}

func TestEditLocalNodePersistsNameAndLink(t *testing.T) {
	dir := t.TempDir()
	s := &Settings{appDir: dir, LocalNodes: []LocalNode{{Name: "old", Link: "socks5://a.example:1080"}}}
	link := "socks5://b.example:1080"
	if err := s.EditLocalNode("old", "region/new", link); err != nil {
		t.Fatal(err)
	}
	if s.LocalNodes[0].Name != "region/new" || s.LocalNodes[0].Link != link {
		t.Fatalf("edited node = %#v", s.LocalNodes[0])
	}
	if err := s.EditLocalNode("region/new", "bad", "not-a-link"); err == nil {
		t.Fatal("expected invalid-link error")
	}
	if s.LocalNodes[0].Name != "region/new" {
		t.Fatal("invalid edit changed node")
	}
}
