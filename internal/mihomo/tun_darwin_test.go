package mihomo

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"mihomo-king/internal/api"
)

// Run explicitly with sudo and MIHOMO_KING_TEST_CORE=/absolute/path/to/mihomo.
// The temporary core creates a utun without installing routes or DNS settings.
func TestDarwinTunLifecycle(t *testing.T) {
	bin := os.Getenv("MIHOMO_KING_TEST_CORE")
	if bin == "" {
		t.Skip("set MIHOMO_KING_TEST_CORE to run the isolated real-core test")
	}
	if os.Geteuid() != 0 {
		t.Fatal("this test needs sudo to create a macOS utun")
	}
	s := tunTestSettings(t)
	s.MihomoBin = bin
	s.MixedPort = 0
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Controller = ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	for n := 1024; ; n++ {
		s.TunDevice = fmt.Sprintf("utun%d", n)
		if _, err := net.InterfaceByName(s.TunDevice); err != nil {
			break
		}
	}
	// SetTun uses PATCH /configs, preserving these runtime route/DNS flags.
	// Its generated config stays in the temporary directory and is not reloaded.
	body := fmt.Sprintf(`external-controller: %s
secret: %q
mixed-port: 0
log-level: info
dns:
  enable: false
tun:
  enable: false
  device: %s
  stack: system
  auto-route: false
  auto-detect-interface: false
rules: ["MATCH,DIRECT"]
`, s.Controller, s.Secret, s.TunDevice)
	if err := os.WriteFile(s.ConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	logf, err := os.Create(s.LogFile())
	if err != nil {
		t.Fatal(err)
	}
	defer logf.Close()
	cmd := exec.Command(bin, startArgs(s)...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			log, _ := os.ReadFile(s.LogFile())
			t.Logf("core log:\n%s", log)
		}
	})
	if err := os.WriteFile(s.PidFile(), fmt.Appendf(nil, "%d", cmd.Process.Pid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := waitController(s.Controller, s.Secret, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	for _, enable := range []bool{true, false, true, false} {
		if err := SetTun(s, enable); err != nil {
			t.Fatalf("SetTun(%v): %v", enable, err)
		}
		cfg, err := api.New(s.Controller, s.Secret).Configs()
		if err != nil || cfg.Tun.Enable != enable || TunDeviceUp(s.TunDevice) != enable {
			t.Fatalf("TUN=%v: config=%+v deviceUp=%v err=%v", enable, cfg, TunDeviceUp(s.TunDevice), err)
		}
		data, err := os.ReadFile(filepath.Join(s.AppDir(), "settings.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			TunEnable bool `yaml:"tun_enable"`
		}
		if err := yaml.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.TunEnable != enable || s.TunEnable != enable {
			t.Fatal("TUN state was not saved")
		}
		t.Logf("TUN=%v confirmed on %s, pid=%d", enable, s.TunDevice, cmd.Process.Pid)
	}
	if err := Stop(s); err != nil {
		t.Fatal(err)
	}
}
