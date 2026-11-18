package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mihomo-king/internal/config"
)

// A deletion holds the exact targets displayed for confirmation.
type BackupDeletion struct {
	targets []backupTarget
}

type backupTarget struct {
	installDir, root, dir, name string
	info                        os.FileInfo
}

func (d BackupDeletion) Paths() []string {
	var paths []string
	for _, t := range d.targets {
		paths = append(paths, filepath.Join(t.root, t.dir, t.name))
	}
	return paths
}

func PlanDeleteBackup(s *config.Settings, tool, channel, name string) (BackupDeletion, error) {
	if _, err := time.Parse(backupFormat, name); err != nil {
		return BackupDeletion{}, fmt.Errorf("invalid backup timestamp %q", name)
	}
	var d BackupDeletion
	err := d.add(s, tool, channel, name)
	return d, err
}

func PlanDeleteAllBackups(s *config.Settings) (BackupDeletion, error) {
	var d BackupDeletion
	for _, tool := range []string{config.ToolMihomo, config.ToolSingBox, config.ToolXray} {
		for _, channel := range Channels() {
			if s.CoreBinary(tool, channel) == "" {
				continue
			}
			if err := d.add(s, tool, channel, ""); err != nil && !errors.Is(err, os.ErrNotExist) {
				return BackupDeletion{}, err
			}
		}
	}
	return d, nil
}

func (d *BackupDeletion) add(s *config.Settings, tool, channel, name string) error {
	dest, err := installPath(s, tool, channel)
	if err != nil {
		return err
	}
	base, relative := backupLocation(s.CoreBackupRoot, dest, tool, channel)
	if !filepath.IsLocal(filepath.Base(dest)) {
		return fmt.Errorf("invalid core binary path %q", dest)
	}
	t := backupTarget{installDir: filepath.Dir(dest), root: base, dir: relative, name: name}
	root, err := os.OpenRoot(t.root)
	if err != nil {
		return err
	}
	defer root.Close()
	t.info, err = t.inspect(root)
	if err != nil {
		return err
	}
	d.targets = append(d.targets, t)
	return nil
}

func (t backupTarget) inspect(root *os.Root) (os.FileInfo, error) {
	// The configured root may be shared with unrelated files. Only descend
	// through real, managed directories underneath it.
	for dir := t.dir; dir != "."; dir = filepath.Dir(dir) {
		info, err := root.Lstat(dir)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("expected backup directory: %s", filepath.Join(t.root, dir))
		}
	}
	info, err := root.Lstat(filepath.Join(t.dir, t.name))
	if err != nil {
		return nil, err
	}
	if t.name != "" && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected backup file: %s", t.name)
	}
	return info, nil
}

func (d BackupDeletion) Delete() error {
	locks := map[string]*os.File{}
	defer func() {
		for _, lock := range locks {
			lock.Close()
		}
	}()
	for _, t := range d.targets {
		if locks[t.installDir] != nil {
			continue
		}
		lock, err := lockInstallDir(t.installDir)
		if err != nil {
			return err
		}
		locks[t.installDir] = lock
	}
	for _, t := range d.targets {
		root, err := os.OpenRoot(t.root)
		if err != nil {
			return err
		}
		defer root.Close()
		info, err := t.inspect(root)
		if err != nil {
			return err
		}
		if !os.SameFile(t.info, info) {
			return fmt.Errorf("backup target changed: %s", filepath.Join(t.root, t.dir, t.name))
		}
		if t.name == "" {
			err = root.RemoveAll(t.dir)
		} else {
			err = root.Remove(filepath.Join(t.dir, t.name))
		}
		if err != nil {
			return err
		}
	}
	return nil
}
