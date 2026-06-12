package config

import "testing"

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
