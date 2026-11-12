package mihomo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mihomo-king/internal/config"
)

var ErrDarwinTunPrivileges = errors.New("macOS TUN requires a root core; use sudo to restart mihomo-king with the same HOME and XDG_CONFIG_HOME (see README)")

// CheckTunPrivileges checks the core's identity, not just the TUI's: a root
// TUI cannot grant privileges to an already running unprivileged core.
func CheckTunPrivileges(s *config.Settings) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if err := s.ValidateTunDevice(); err != nil {
		return err
	}
	if running, pid := Running(s); running {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "uid=").Output()
		if err != nil {
			return fmt.Errorf("inspect core TUN privileges: %w", err)
		}
		if strings.TrimSpace(string(out)) == "0" {
			return nil
		}
		return ErrDarwinTunPrivileges
	}
	if os.Geteuid() != 0 {
		return ErrDarwinTunPrivileges
	}
	return nil
}

// TunEnabled checks the device on macOS because mihomo can report enable=true
// even after failing to create its utun interface.
func TunEnabled(s *config.Settings, enabled bool) bool {
	return enabled && (runtime.GOOS != "darwin" || TunDeviceUp(s.TunDevice))
}

// SetTun applies and persists one state change for both CLI and TUI. A failed
// listener change must not become the saved state on the next launch.
func SetTun(s *config.Settings, enable bool) error {
	if enable {
		if err := CheckTunPrivileges(s); err != nil {
			return err
		}
	}
	next := *s
	next.TunEnable = enable
	if err := ApplyTun(&next); err != nil {
		return restoreTun(s, err)
	}
	if err := next.Save(); err != nil {
		return restoreTun(s, fmt.Errorf("save TUN state: %w", err))
	}
	s.TunEnable = enable
	return nil
}

func restoreTun(s *config.Settings, err error) error {
	if restoreErr := ApplyTun(s); restoreErr != nil {
		return fmt.Errorf("%w; restoring previous TUN state also failed: %v", err, restoreErr)
	}
	return err
}

func verifyTun(s *config.Settings) error {
	if runtime.GOOS == "darwin" && s.TunEnable && !TunDeviceUp(s.TunDevice) {
		return fmt.Errorf("TUN device %s did not come up; see %s", s.TunDevice, s.LogFile())
	}
	return nil
}
