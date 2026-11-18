package core

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mihomo-king/internal/config"
	"mihomo-king/internal/mihomo"
)

func TestBackupRetentionAndRestore(t *testing.T) {
	dir := t.TempDir()
	s := &config.Settings{MihomoBin: filepath.Join(dir, "stable"), AlphaBinaries: map[string]string{"mihomo": filepath.Join(dir, "alpha")}, CoreBackupCount: 2}
	install := func(channel, version string) {
		t.Helper()
		_, err := changeBinary(t.Context(), s, "mihomo", channel, true, func(stage string, _ *Result) error {
			return os.WriteFile(stage, []byte("#!/bin/sh\necho "+version+"\n"), 0700)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	install("alpha", "alpha")
	for _, v := range []string{"one", "two", "three", "four"} {
		install("release", v)
	}
	names, err := Backups(s, "mihomo", "stable")
	if err != nil || len(names) != 2 {
		t.Fatalf("backups: %v %v", names, err)
	}
	res, err := Restore(t.Context(), s, "mihomo", "release", names[1], true)
	if err != nil || res.Version != "two" || res.Restarted {
		t.Fatalf("restore: %+v %v", res, err)
	}
	if names, err := Backups(s, "mihomo", "alpha"); err != nil || len(names) != 0 {
		t.Fatalf("alpha backups: %v %v", names, err)
	}
	if _, err := Restore(t.Context(), s, "mihomo", "stable", "../other", true); err == nil {
		t.Fatal("unknown backup accepted")
	}
	s.CoreBackupCount = 0
	install("stable", "five")
	if names, err := Backups(s, "mihomo", "stable"); err != nil || len(names) != 0 {
		t.Fatalf("zero retention: %v %v", names, err)
	}
}

// The isolated child doubles as a core with a controller readiness endpoint.
func TestMain(m *testing.M) {
	if addr := os.Getenv("MIHOMO_KING_TEST_CONTROLLER"); addr != "" && len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v":
			fmt.Println("Mihomo Meta v1.0.0")
			os.Exit(0)
		case "-t":
			os.Exit(0)
		case "-d":
			http.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"version":"1.0.0"}`) })
			if err := http.ListenAndServe(addr, nil); err != nil {
				os.Exit(1)
			}
		}
	}
	os.Exit(m.Run())
}

func TestRestartFailureRestoresRunningCore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.ManagedTool = "mihomo"
	s.MihomoDir = filepath.Join(dir, "data")
	if err := os.MkdirAll(s.MihomoDir, 0700); err != nil {
		t.Fatal(err)
	}
	s.SetBinary(filepath.Join(dir, "proxy"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Binary(), data, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Controller = listener.Addr().String()
	listener.Close()
	t.Setenv("MIHOMO_KING_TEST_CONTROLLER", s.Controller)
	s.LocalNodes = []config.LocalNode{{Name: "test", Link: "socks5://127.0.0.1:1#test"}}
	if err := mihomo.Start(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if ok, _ := mihomo.Running(s); ok {
			if err := mihomo.Stop(s); err != nil {
				t.Error(err)
			}
		}
	})
	_, oldPID := mihomo.Running(s)
	s.CoreBackupCount = 0
	_, err = changeBinary(t.Context(), s, "mihomo", s.Channel("mihomo"), true, func(stage string, _ *Result) error {
		return os.WriteFile(stage, []byte("#!/bin/sh\ncase \"$1\" in -v) echo v2;; -t) exit 0;; *) exit 2;; esac\n"), 0700)
	})
	if err == nil || !strings.Contains(err.Error(), "restored and restarted") {
		t.Fatalf("rollback: %v", err)
	}
	if ok, pid := mihomo.Running(s); !ok || pid == oldPID {
		t.Fatalf("old binary did not restart: %t %d", ok, pid)
	}
}
