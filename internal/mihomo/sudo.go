package mihomo

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"mihomo-king/internal/config"
)

// DeleteTunDevice removes a TUN interface (e.g. another clash's "Meta") via
// sudo. The owning process is not told — it keeps running without its TUN.
func DeleteTunDevice(password, device string) error {
	return runSudo(password, "ip", "link", "delete", device)
}

// GrantNetCaps gives the mihomo binary the capabilities TUN needs. Takes
// effect on the next exec of the binary, not on an already-running process.
func GrantNetCaps(password, bin string) error {
	return runSudo(password, "setcap", "cap_net_admin,cap_net_raw=ep", bin)
}

// Restart stops the tracked mihomo (if any), waits for the controller port to
// be released, and starts it again. Needed after setcap: capabilities are
// evaluated at exec time.
func Restart(s *config.Settings) error {
	if ok, _ := Running(s); ok {
		if err := Stop(s); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for controllerPortBusy(s.Controller) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	return Start(s)
}

// runSudo runs a command via sudo, feeding the password on stdin (-S). The
// password is never placed in argv and never appears in errors.
func runSudo(password string, args ...string) error {
	cmd := exec.Command("sudo", append([]string{"-S", "-p", "", "--"}, args...)...)
	cmd.Stdin = strings.NewReader(password + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if strings.Contains(detail, "incorrect password") || strings.Contains(detail, "no password was provided") {
			return errors.New("sudo: incorrect password")
		}
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("sudo %s: %s", args[0], detail)
	}
	return nil
}
