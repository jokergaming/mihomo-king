// Package autostart manages the current user's XDG desktop login startup entry.
package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const filename = "mihomo-king.desktop"

func entryPath() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("desktop autostart is only supported on Linux")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", filename), nil
}

// Enabled reads the startup entry itself, the single persisted switch state.
func Enabled() (bool, error) {
	path, err := entryPath()
	if err != nil {
		return false, err
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	inEntry := false
	for line := range strings.Lines(string(body)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
		}
		if inEntry && (line == "Hidden=true" || line == "X-GNOME-Autostart-enabled=false") {
			return false, nil
		}
	}
	return true, nil
}

// SetEnabled changes future login startup only; it never starts or stops a core.
func SetEnabled(enabled bool) error {
	path, err := entryPath()
	if err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// A go run executable disappears when that process exits.
	if filepath.Base(filepath.Dir(executable)) == "exe" && strings.Contains(executable, "/go-build") {
		return fmt.Errorf("autostart needs a permanent binary; build and run mihomo-king instead of go run")
	}
	return writeEntry(path, executable)
}

func writeEntry(path, executable string) error {
	envBin, err := exec.LookPath("env")
	if err != nil {
		return fmt.Errorf("autostart requires env: %w", err)
	}
	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	configDir := filepath.Dir(filepath.Dir(path))
	// Preserve config discovery and named core lookup in the desktop environment.
	args := []string{envBin, "XDG_CONFIG_HOME=" + configDir, "PATH=" + os.Getenv("PATH"), executable, "start"}
	for i := range args {
		args[i] = execArg(args[i])
	}
	body := "[Desktop Entry]\nType=Application\nName=mihomo-king\n" +
		"Comment=Start the configured proxy core at desktop login\n" +
		"Exec=" + strings.Join(args, " ") + "\n" +
		"Path=" + desktopString(workDir) + "\nTerminal=false\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mihomo-king-*.desktop")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(body)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Desktop files decode string escapes before parsing Exec's quoted arguments.
// Percent signs are desktop field codes; Exec is never passed through a shell.
func execArg(value string) string {
	value = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`, "%", "%%").Replace(value)
	return desktopString(`"` + value + `"`)
}

func desktopString(value string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(value)
}
