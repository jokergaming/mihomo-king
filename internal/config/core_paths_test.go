package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverAndPersistCoreChannels(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", bin)
	path := filepath.Join(bin, "mihomo")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'Mihomo Meta alpha-g123'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(home, ".local", "share", "bin")
	if s.Channel(ToolMihomo) != Alpha || s.Binary() != path || s.MihomoBin != filepath.Join(base, "mihomo") {
		t.Fatalf("legacy alpha: %s %s %s", s.Channel(ToolMihomo), s.Binary(), s.MihomoBin)
	}
	if s.SingBoxBin != filepath.Join(base, "sing-box") || s.CoreBinary(ToolSingBox, Alpha) != filepath.Join(base, "sing-box-alpha") {
		t.Fatal("missing first-install channel defaults")
	}
	s.CoreBackupCount = 0
	s.SetBinary("/custom/alpha")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	late := filepath.Join(bin, "sing-box")
	if err := os.WriteFile(late, []byte("#!/bin/sh\necho 'sing-box version 1.2.3'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load()
	if err != nil || reloaded.Binary() != "/custom/alpha" || reloaded.CoreBackupCount != 0 {
		t.Fatalf("reload: %v %v", reloaded, err)
	}
	if reloaded.SingBoxBin != late {
		t.Fatalf("late PATH discovery: %s", reloaded.SingBoxBin)
	}
}
