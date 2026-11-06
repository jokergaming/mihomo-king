package autostart

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStartupLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG desktop autostart requires Linux")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if enabled, err := Enabled(); err != nil || enabled {
		t.Fatalf("default: enabled=%v err=%v", enabled, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "autostart")); !os.IsNotExist(err) {
		t.Fatalf("reading status created startup files: %v", err)
	}
	if err := SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if enabled, err := Enabled(); err != nil || !enabled {
		t.Fatalf("enabled: enabled=%v err=%v", enabled, err)
	}
	path, err := entryPath()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, []byte("Hidden=true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if enabled, err := Enabled(); err != nil || enabled {
		t.Fatalf("desktop-disabled entry: enabled=%v err=%v", enabled, err)
	}
	if err := SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if enabled, err := Enabled(); err != nil || enabled {
		t.Fatalf("disabled: enabled=%v err=%v", enabled, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disable did not remove entry: %v", err)
	}
	if err := SetEnabled(false); err != nil {
		t.Fatalf("repeated disable: %v", err)
	}
}

func TestStartupIOErrorsAreReported(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG desktop autostart requires Linux")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "autostart"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Enabled(); err == nil {
		t.Fatal("status hid an inaccessible entry")
	}
	if err := SetEnabled(true); err == nil {
		t.Fatal("enable hid a write failure")
	}
	if err := SetEnabled(false); err == nil {
		t.Fatal("disable hid a removal failure")
	}
}

// Exercise the desktop parser with a harmless fixture, never a real proxy.
func TestDesktopEntryLaunch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG desktop autostart requires Linux")
	}
	gio, err := exec.LookPath("gio")
	if err != nil {
		t.Skip("gio is not available")
	}
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config $dollar %percent \\\"quote\" `tick`")
	path := filepath.Join(configDir, "autostart", filename)
	executable := filepath.Join(dir, "app $dollar %percent \\\"quote\" `tick`")
	output := filepath.Join(dir, "launch-output")
	t.Setenv("AUTOSTART_TEST_OUTPUT", output)
	fixture := "#!/bin/sh\nprintf '%s\\n' \"$#\" \"$1\" \"$XDG_CONFIG_HOME\" \"$PATH\" > \"$AUTOSTART_TEST_OUTPUT\"\n"
	if err := os.WriteFile(executable, []byte(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeEntry(path, executable); err != nil {
		t.Fatal(err)
	}
	if validator, err := exec.LookPath("desktop-file-validate"); err == nil {
		if out, err := exec.CommandContext(t.Context(), validator, path).CombinedOutput(); err != nil {
			t.Fatalf("invalid desktop entry: %v\n%s", err, out)
		}
	}
	if out, err := exec.CommandContext(t.Context(), gio, "launch", path).CombinedOutput(); err != nil {
		t.Fatalf("desktop launch: %v\n%s", err, out)
	}
	want := "1\nstart\n" + configDir + "\n" + os.Getenv("PATH") + "\n"
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(output)
		if err == nil && strings.HasSuffix(string(body), "\n") {
			if string(body) != want {
				t.Fatalf("launched arguments/environment = %q, want %q", body, want)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("desktop entry did not launch the fixture")
}
