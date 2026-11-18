package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mihomo-king/internal/config"
	"mihomo-king/internal/mihomo"
)

var ErrBinaryRunning = errors.New("stop the running core before renaming its binary")
var ErrBinaryName = errors.New("enter a file name without a directory")

// RenameBinary moves the selected channel and its backups together, and persists
// the new path. The caller passes a snapshot, then applies the path on success.
func RenameBinary(s *config.Settings, tool, channel, name string) (string, error) {
	if strings.TrimSpace(name) == "" || !filepath.IsLocal(name) || filepath.Base(name) != name || name == "." {
		return "", ErrBinaryName
	}
	from, err := exec.LookPath(s.CoreBinary(tool, channel))
	if err != nil {
		return "", err
	}
	from, err = filepath.Abs(from)
	if err != nil {
		return "", err
	}
	to := filepath.Join(filepath.Dir(from), name)
	if from == to {
		return from, nil
	}
	resolved, err := filepath.EvalSymlinks(from)
	if err != nil {
		return "", err
	}
	lock, err := lockInstallDir(filepath.Dir(resolved))
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if tool == s.Tool() && channel == s.Channel(tool) {
		if running, _ := mihomo.Running(s); running {
			return "", ErrBinaryRunning
		}
	}
	for _, other := range []string{config.ToolMihomo, config.ToolSingBox, config.ToolXray} {
		for _, ch := range Channels() {
			if other == tool && ch == channel {
				continue
			}
			path, e := installPath(s, other, ch)
			if e == nil && (path == to || path == resolved) {
				return "", fmt.Errorf("%s %s shares the binary path %s", other, ch, path)
			}
		}
	}
	if err := renameExclusive(from, to); err != nil {
		return "", err
	}
	rollback := func(err error) (string, error) { return "", errors.Join(err, renameExclusive(to, from)) }
	// Renaming a symlink leaves its target and target-based backup directory intact.
	newResolved, err := filepath.EvalSymlinks(to)
	if err != nil {
		return rollback(err)
	}
	oldDir := backupDir(s.CoreBackupRoot, resolved, tool, channel)
	newDir := backupDir(s.CoreBackupRoot, newResolved, tool, channel)
	movedBackups := false
	if oldDir != newDir {
		err := renameExclusive(oldDir, newDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return rollback(err)
		}
		movedBackups = err == nil
	}
	updated := *s
	updated.SetChannelPath(tool, channel, to)
	if err := updated.Save(); err != nil {
		if movedBackups {
			err = errors.Join(err, renameExclusive(newDir, oldDir))
		}
		return rollback(err)
	}
	return to, nil
}
