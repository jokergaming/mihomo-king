// Package mihomo launches and tracks the mihomo proxy as a detached background
// subprocess, recording its PID so it survives this TUI exiting.
package mihomo

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"mihomo-king/internal/config"
)

// Running reports whether the tracked mihomo process is alive, with its PID.
func Running(s *config.Settings) (bool, int) {
	pid, err := readPID(s.PidFile())
	if err != nil || !alive(pid) {
		return false, 0
	}
	return true, pid
}

// Start writes the active config from the stored subscription and launches
// mihomo detached. It is a no-op if mihomo is already running.
func Start(s *config.Settings) error {
	if ok, _ := Running(s); ok {
		return nil
	}
	if err := checkBinary(s.MihomoBin); err != nil {
		return err
	}
	if err := s.WriteActiveFromStore(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	logf, err := os.OpenFile(s.LogFile(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()

	cmd := exec.Command(s.MihomoBin, "-d", s.MihomoDir, "-f", s.ConfigPath())
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own process group: outlive the TUI
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return os.WriteFile(s.PidFile(), []byte(strconv.Itoa(pid)), 0o644)
}

// Stop terminates the tracked mihomo process group.
func Stop(s *config.Settings) error {
	pid, err := readPID(s.PidFile())
	if err != nil {
		return fmt.Errorf("not running (no pid file)")
	}
	// Negative pid signals the whole process group (we started it with Setpgid).
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if proc, e := os.FindProcess(pid); e == nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
	}
	return os.Remove(s.PidFile())
}

// MissingTunCaps reports whether TUN is likely to fail because the binary lacks
// the needed capabilities and we are not root. Best-effort: false when unsure.
func MissingTunCaps(s *config.Settings) bool {
	if os.Geteuid() == 0 {
		return false
	}
	out, err := exec.Command("getcap", s.MihomoBin).Output()
	if err != nil {
		return false // getcap unavailable or path odd; don't cry wolf
	}
	return !strings.Contains(string(out), "cap_net_admin")
}

func checkBinary(bin string) error {
	if bin == "" {
		return fmt.Errorf("mihomo binary not configured")
	}
	if _, err := exec.LookPath(bin); err == nil {
		return nil
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("mihomo binary %q not found", bin)
	}
	return nil
}

func readPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

func alive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
