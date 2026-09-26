package mihomo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mihomo-king/internal/config"
)

func TestCheckFailure(t *testing.T) {
	out := `time="2026-09-25T19:43:50+08:00" level=info msg="Load GeoSite rule: gfw"
time="2026-09-25T19:43:51+08:00" level=error msg="DNS FallbackGeosite[0] format error: list \"gfw\" not found in GeoSite.dat"
configuration file /tmp/c.yaml test failed
`
	want := `DNS FallbackGeosite[0] format error: list "gfw" not found in GeoSite.dat`
	if got := checkFailure(out); got != want {
		t.Fatalf("checkFailure = %q, want %q", got, want)
	}
	if got := checkFailure("boom\n"); got != "boom" {
		t.Fatalf("checkFailure without log line = %q", got)
	}
}

func fakeMihomo(t *testing.T, script string) *config.Settings {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &config.Settings{ManagedTool: config.ToolMihomo, MihomoBin: bin, MihomoDir: filepath.Join(dir, "data")}
}

func TestCheckActive(t *testing.T) {
	s := fakeMihomo(t, `echo 'level=error msg="list gfw not found in GeoSite.dat"'; exit 1`+"\n")
	err := CheckActive(s)
	if err == nil || !strings.Contains(err.Error(), "list gfw not found in GeoSite.dat") || !strings.Contains(err.Error(), "MetaCubeX") {
		t.Fatalf("CheckActive error = %v", err)
	}
	if entries, _ := os.ReadDir(s.RuntimeDir()); len(entries) != 0 {
		t.Fatalf("check left files behind: %v", entries)
	}

	s = fakeMihomo(t, "exit 0\n")
	if err := CheckActive(s); err != nil {
		t.Fatalf("CheckActive on a valid config: %v", err)
	}
}

func TestSwitchSubRejectsNoNodes(t *testing.T) {
	s := &config.Settings{
		Active:        "current",
		Subscriptions: []config.Subscription{{Name: "empty", NoNodes: true}},
	}
	if err := SwitchSub(s, "empty"); err == nil || !strings.Contains(err.Error(), "no usable nodes") {
		t.Fatalf("SwitchSub error = %v", err)
	}
	if s.Active != "current" {
		t.Fatalf("active subscription changed to %q", s.Active)
	}
}
