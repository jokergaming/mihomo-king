package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mihomo-king/internal/config"
)

func TestDeleteBackups(t *testing.T) {
	dir := t.TempDir()
	s := &config.Settings{CoreBackupRoot: filepath.Join(dir, "shared"), MihomoBin: filepath.Join(dir, "mihomo"), XrayBin: filepath.Join(dir, "xray"), AlphaBinaries: map[string]string{"mihomo": filepath.Join(dir, "mihomo-alpha")}}
	stamp := "20260101T000000.000000000Z"
	second := "20260102T000000.000000000Z"
	for _, pair := range [][2]string{{"mihomo", "stable"}, {"mihomo", "alpha"}, {"xray", "stable"}} {
		folder, err := BackupDir(s, pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{stamp, second} {
			if err := os.WriteFile(filepath.Join(folder, name), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	keep := filepath.Join(s.CoreBackupRoot, "personal")
	if err := os.WriteFile(keep, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanDeleteBackup(s, "mihomo", "stable", "../personal"); err == nil {
		t.Fatal("accepted traversal")
	}
	one, err := PlanDeleteBackup(s, "mihomo", "stable", stamp)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockInstallDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := one.Delete(); err == nil {
		t.Fatal("deleted while update lock was held")
	}
	lock.Close()
	if err := one.Delete(); err != nil {
		t.Fatal(err)
	}
	names, err := Backups(s, "mihomo", "stable")
	if err != nil || len(names) != 1 || names[0] != second {
		t.Fatalf("%v: %v", names, err)
	}
	all, err := PlanDeleteAllBackups(s)
	if err != nil || len(all.Paths()) != 3 {
		t.Fatalf("targets: %v, %v", all.Paths(), err)
	}
	if err := all.Delete(); err != nil {
		t.Fatal(err)
	}
	for _, p := range all.Paths() {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("directory still present: %s", p)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal(err)
	}
	// A replacement symlink must not redirect the confirmed action.
	p := one.Paths()[0]
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, stamp), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Dir(p)); err != nil {
		t.Fatal(err)
	}
	if err := one.Delete(); err == nil {
		t.Fatal("followed a replacement symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, stamp)); err != nil {
		t.Fatal(err)
	}
}
