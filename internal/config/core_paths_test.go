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
	base := filepath.Join(home, ".local", "bin")
	if s.Channel(ToolMihomo) != Alpha || s.Binary() != path || s.MihomoBin != path+"-stable" {
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

func TestCorePathsMigrateSplitInstallation(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", bin)
	retired := filepath.Join(home, ".local", "share", "bin")
	if err := os.MkdirAll(retired, 0700); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(bin, ToolXray)
	for _, path := range []string{stable, filepath.Join(retired, "xray"), filepath.Join(bin, "mihomo"), filepath.Join(retired, "xray-alpha"), filepath.Join(retired, "sing-box")} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho 1.0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s.XrayBin = stable
	s.MihomoBin = filepath.Join(retired, "mihomo")
	s.AlphaBinaries[ToolMihomo] = filepath.Join(bin, "mihomo")
	s.CoreChannels[ToolMihomo] = Alpha
	s.SingBoxBin = filepath.Join(retired, "sing-box")
	s.AlphaBinaries[ToolXray] = filepath.Join(retired, "xray-alpha")
	s.AlphaBinaries[ToolSingBox] = filepath.Join(retired, "sing-box-alpha")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", retired+string(os.PathListSeparator)+bin)
	for range 2 {
		s, err = Load()
		if err != nil {
			t.Fatal(err)
		}
		if s.CoreBinary(ToolMihomo, Alpha) != filepath.Join(bin, "mihomo") || s.MihomoBin != filepath.Join(bin, "mihomo-stable") {
			t.Fatal("lost legacy unsuffixed alpha")
		}
		for tool, want := range map[string]string{ToolXray: stable, ToolSingBox: filepath.Join(home, ".local", "bin", "sing-box")} {
			if s.CoreBinary(tool, Stable) != want || s.CoreBinary(tool, Alpha) != want+"-alpha" {
				t.Fatalf("%s: stable %s, alpha %s", tool, s.CoreBinary(tool, Stable), s.CoreBinary(tool, Alpha))
			}
		}
	}
	custom := filepath.Join(home, "elsewhere", "xray")
	s.SetCoreBinary(ToolXray, Stable, custom)
	if s.CoreBinary(ToolXray, Alpha) != custom+"-alpha" {
		t.Fatal("stable edit left a stale alpha path")
	}
	s.SetCoreBinary(ToolXray, Alpha, stable+"-alpha")
	if s.CoreBinary(ToolXray, Stable) != stable {
		t.Fatal("alpha edit left a stale stable path")
	}
}

func TestMissingAlphaPathRecognizesInstalledAlpha(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PATH", dir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	bin := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'Mihomo Meta alpha-g123'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s.MihomoBin = bin
	s.AlphaBinaries[ToolMihomo] = bin + "-alpha"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Binary() != bin || s.MihomoBin != bin+"-stable" {
		t.Fatalf("alpha %q, stable %q", s.Binary(), s.MihomoBin)
	}
}
