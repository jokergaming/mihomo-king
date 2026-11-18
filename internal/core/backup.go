package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"time"

	"mihomo-king/internal/config"
	"mihomo-king/internal/corebin"
	"mihomo-king/internal/mihomo"
)

const backupFormat = "20060102T150405.000000000Z"

func installPath(s *config.Settings, tool, channel string) (string, error) {
	if _, err := SpecFor(tool); err != nil {
		return "", err
	}
	if !ValidChannel(channel) {
		return "", fmt.Errorf("invalid channel %q", channel)
	}
	path := s.CoreBinary(tool, channel)
	if path == "" {
		return "", fmt.Errorf("%s %s binary path is empty", tool, channel)
	}
	if !filepath.IsAbs(path) {
		p, err := exec.LookPath(path)
		if err != nil {
			return "", err
		}
		path, err = filepath.Abs(p)
		if err != nil {
			return "", err
		}
	}
	p, err := filepath.EvalSymlinks(path)
	if err == nil {
		return p, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return filepath.Clean(path), nil
		}
	}
	return "", err
}

func backupLocation(root, dest, tool, channel string) (string, string) {
	if root == "" {
		root = filepath.Join(filepath.Dir(dest), ".mihomo-king-backups")
	}
	return root, filepath.Join(tool, CanonicalChannel(channel), filepath.Base(dest))
}

func backupDir(root, dest, tool, channel string) string {
	base, relative := backupLocation(root, dest, tool, channel)
	return filepath.Join(base, relative)
}

func BackupDir(s *config.Settings, tool, channel string) (string, error) {
	dest, err := installPath(s, tool, channel)
	if err != nil {
		return "", err
	}
	return backupDir(s.CoreBackupRoot, dest, tool, channel), nil
}

func lockInstallDir(dir string) (*os.File, error) {
	lock, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("lock %s: %w", dir, err)
	}
	return lock, nil
}

func backupNames(dir string) ([]string, error) {
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range files {
		if _, err := time.Parse(backupFormat, f.Name()); err == nil && f.Type().IsRegular() {
			names = append(names, f.Name())
		}
	}
	slices.Reverse(names)
	return names, nil
}

func Backups(s *config.Settings, tool, channel string) ([]string, error) {
	dir, err := BackupDir(s, tool, channel)
	if err != nil {
		return nil, err
	}
	return backupNames(dir)
}

// Restore stages the selected backup locally before replacing the executable.
func Restore(ctx context.Context, s *config.Settings, tool, channel, name string, restart bool) (*Result, error) {
	return changeBinary(ctx, s, tool, channel, restart, func(stage string, res *Result) error {
		dir := backupDir(s.CoreBackupRoot, res.Dest, tool, channel)
		names, err := backupNames(dir)
		if err != nil {
			return err
		}
		if name == "" && len(names) > 0 {
			name = names[0]
		}
		if !slices.Contains(names, name) {
			return fmt.Errorf("no matching %s %s backup: %q", tool, channel, name)
		}
		res.Backup = name
		return linkOrCopy(ctx, filepath.Join(dir, name), stage)
	})
}

func changeBinary(ctx context.Context, s *config.Settings, tool, channel string, restart bool, prepare func(string, *Result) error) (*Result, error) {
	channel = CanonicalChannel(channel)
	dest, err := installPath(s, tool, channel)
	if err != nil {
		return nil, err
	}
	other := ChannelAlpha
	if channel == other {
		other = ChannelRelease
	}
	if path, e := installPath(s, tool, other); e == nil && path == dest {
		return nil, fmt.Errorf("stable and alpha share %s; configure separate paths", dest)
	}
	if active := s.Tool(); active != tool {
		if path, e := installPath(s, active, s.Channel(active)); e == nil && path == dest {
			return nil, fmt.Errorf("%s and %s share %s; configure separate binary paths", tool, active, dest)
		}
	}
	if s.CoreBackupCount < 0 {
		return nil, fmt.Errorf("core backup count must be non-negative")
	}
	dir := backupDir(s.CoreBackupRoot, dest, tool, channel)
	// Lock at the install location even when concurrent callers use different backup roots.
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return nil, err
	}
	lock, err := lockInstallDir(filepath.Dir(dest))
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".mihomo-king-update-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	stage := filepath.Join(tmp, tool)
	res := &Result{Dest: dest}
	if err := prepare(stage, res); err != nil {
		return nil, err
	}
	res.Version, err = corebin.Version(ctx, stage, tool)
	if err != nil {
		return nil, fmt.Errorf("candidate %s failed version check: %w", tool, err)
	}
	if res.Version == "" {
		return nil, fmt.Errorf("candidate %s returned an empty version", tool)
	}
	running := false
	if tool == s.Tool() && channel == s.Channel(tool) {
		running, _ = mihomo.Running(s)
	}
	if running {
		candidate := *s
		candidate.SetBinary(stage)
		if err := mihomo.CheckActive(&candidate); err != nil {
			return nil, err
		}
	}
	// Even with zero retained backups keep the old inode until restart succeeds.
	previous := filepath.Join(tmp, "previous")
	backup := filepath.Join(dir, time.Now().UTC().Format(backupFormat))
	if err := os.Link(dest, previous); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("back up %s: %w", dest, err)
		}
		backup = ""
	} else if err := linkOrCopy(ctx, previous, backup); err != nil {
		return nil, fmt.Errorf("back up %s to %s: %w", dest, backup, err)
	}
	if running {
		if err := mihomo.Stop(s); err != nil {
			return nil, err
		}
	}
	start := func() error {
		if running {
			return mihomo.Restart(s)
		}
		return nil
	}
	if err := os.Rename(stage, dest); err != nil {
		return nil, errors.Join(err, start())
	}
	if running && restart {
		if err := start(); err != nil {
			if backup == "" {
				return nil, err
			}
			if restoreErr := os.Rename(previous, dest); restoreErr != nil {
				return nil, errors.Join(err, fmt.Errorf("rollback from %s: %w", backup, restoreErr))
			}
			if startErr := start(); startErr != nil {
				return nil, errors.Join(err, fmt.Errorf("previous binary restored but restart failed: %w", startErr))
			}
			return nil, errors.Join(fmt.Errorf("update failed; previous binary restored and restarted: %w", err), os.Remove(backup))
		}
		res.Restarted = true
	}
	res.Stopped = running && !restart
	names, err := backupNames(dir)
	if err != nil {
		return res, err
	}
	for _, name := range names[min(len(names), s.CoreBackupCount):] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return res, err
		}
	}
	return res, nil
}

// Publish a complete copy when the backup volume cannot share the source inode.
func linkOrCopy(ctx context.Context, src, dst string) error {
	if err := os.Link(src, dst); !errors.Is(err, syscall.EXDEV) {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), ".backup-copy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	copyPath := filepath.Join(tmp, "binary")
	preserve := "-p"
	if runtime.GOOS == "linux" {
		preserve = "--preserve=mode,xattr"
	}
	if output, err := exec.CommandContext(ctx, "cp", preserve, src, copyPath).CombinedOutput(); err != nil {
		return fmt.Errorf("copy executable and attributes: %w: %s", err, output)
	}
	return os.Rename(copyPath, dst)
}
