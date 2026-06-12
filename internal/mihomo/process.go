// Package mihomo launches and tracks the managed proxy as a detached background
// subprocess, recording its PID so it survives this TUI exiting.
package mihomo

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mihomo-king/internal/config"
)

// Running reports whether the tracked managed process is alive, with its PID.
func Running(s *config.Settings) (bool, int) {
	pid, err := readPID(s.PidFile())
	if err != nil {
		return false, 0
	}
	if !alive(pid) || !matchesBinary(pid, s.Binary()) {
		_ = os.Remove(s.PidFile())
		return false, 0
	}
	return true, pid
}

// Start writes the active config from the stored subscription and launches the
// selected tool detached. It is a no-op if it is already running.
func Start(s *config.Settings) error {
	if ok, _ := Running(s); ok {
		return nil
	}
	if err := checkBinary(s.ToolLabel(), s.Binary()); err != nil {
		return err
	}
	// An empty active config (no proxies/groups) is never what the user wants;
	// it leaves the runtime with only built-in GLOBAL/DIRECT.
	if s.Active == "" {
		return fmt.Errorf("no active subscription — press 2, pick one, press enter to activate, then start")
	}
	// Our managed tool isn't running, so a taken controller port means another
	// process owns it — starting would silently collide with it.
	if controllerPortBusy(s.Controller) {
		return fmt.Errorf("controller %s is already in use by another process; change controller in settings", s.Controller)
	}
	if err := s.WriteActiveFromStore(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	logf, err := os.OpenFile(s.LogFile(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()

	cmd := exec.Command(s.Binary(), startArgs(s)...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own process group: outlive the TUI
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	if err := os.WriteFile(s.PidFile(), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return err
	}
	if err := waitController(s.Controller, s.Secret, 5*time.Second); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		_ = os.Remove(s.PidFile())
		return fmt.Errorf("%s controller was not ready: %w; see %s", s.ToolLabel(), err, s.LogFile())
	}
	return nil
}

// Stop terminates the tracked process group.
func Stop(s *config.Settings) error {
	pid, err := readPID(s.PidFile())
	if err != nil {
		return fmt.Errorf("not running (no pid file)")
	}
	// Negative pid signals the whole process group (we started it with Setpgid).
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.Remove(s.PidFile())
		}
		if proc, e := os.FindProcess(pid); e == nil {
			if sigErr := proc.Signal(syscall.SIGTERM); sigErr != nil && !errors.Is(sigErr, syscall.ESRCH) {
				return sigErr
			}
		} else {
			return err
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
	caps, ok := NetCaps(s.Binary())
	if !ok {
		return false // getcap unavailable or path odd; don't cry wolf
	}
	return !strings.Contains(caps, "cap_net_admin") || !strings.Contains(caps, "cap_net_raw")
}

func NetCaps(bin string) (string, bool) {
	out, err := exec.Command("getcap", bin).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// ForeignTunDevices lists TUN interfaces that are up but aren't ours (e.g.
// another clash instance's "Meta"). Two TUNs fight over routes/DNS, so the
// dashboard warns before our TUN is enabled alongside one. Best-effort via
// sysfs; empty when unsure.
func ForeignTunDevices(own string) []string {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil
	}
	var tuns []string
	for _, e := range entries {
		name := e.Name()
		if name == own {
			continue
		}
		if _, err := os.Stat(filepath.Join("/sys/class/net", name, "tun_flags")); err == nil {
			tuns = append(tuns, name)
		}
	}
	return tuns
}

func TunDeviceUp(name string) bool {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return false
	}
	return iface.Flags&net.FlagUp != 0
}

func controllerPortBusy(controller string) bool {
	addr := strings.TrimPrefix(strings.TrimPrefix(controller, "https://"), "http://")
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

func startArgs(s *config.Settings) []string {
	if s.Tool() == config.ToolSingBox {
		return []string{"run", "-D", s.RuntimeDir(), "-c", s.ConfigPath()}
	}
	return []string{"-d", s.RuntimeDir(), "-f", s.ConfigPath()}
}

func checkBinary(label, bin string) error {
	if bin == "" {
		return fmt.Errorf("%s binary not configured", label)
	}
	if _, err := exec.LookPath(bin); err == nil {
		return nil
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("%s binary %q not found", label, bin)
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
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func matchesBinary(pid int, bin string) bool {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return true // best effort outside Linux/procfs permission edge cases
	}
	want := bin
	if resolved, err := exec.LookPath(bin); err == nil {
		want = resolved
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe == want
}

func waitController(controller, secret string, timeout time.Duration) error {
	base, err := controllerURL(controller)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, base+"/version", nil)
		if err != nil {
			return err
		}
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("controller rejected configured secret")
			}
			lastErr = fmt.Errorf("controller returned %s", resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(150 * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("controller %s not reachable", base)
}

func controllerURL(controller string) (string, error) {
	if strings.HasPrefix(controller, "http://") || strings.HasPrefix(controller, "https://") {
		u, err := url.Parse(controller)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(u.String(), "/"), nil
	}
	return "http://" + strings.TrimRight(controller, "/"), nil
}
