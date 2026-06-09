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
