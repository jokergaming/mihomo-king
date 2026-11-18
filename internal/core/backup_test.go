package core

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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

func TestCustomBackupRootAcrossFilesystems(t *testing.T) {
	root, err := os.MkdirTemp("/dev/shm", "mihomo-king-backup-test-")
	if err != nil {
		t.Skip("no writable /dev/shm")
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	s := &config.Settings{MihomoBin: filepath.Join(t.TempDir(), "mihomo"), CoreBackupCount: 2}
	install := func(version string) {
		t.Helper()
		_, err := changeBinary(t.Context(), s, "mihomo", "stable", true, func(stage string, _ *Result) error {
			return os.WriteFile(stage, []byte("#!/bin/sh\necho "+version+"\n"), 0700)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	install("first")
	install("second")
	s.CoreBackupRoot = root
	install("third")
	names, err := Backups(s, "mihomo", "stable")
	if err != nil || len(names) != 1 {
		t.Fatalf("custom backups: %v %v", names, err)
	}
	if _, err := os.Stat(filepath.Join(root, "mihomo", "stable", "mihomo", names[0])); err != nil {
		t.Fatal(err)
	}
	res, err := Restore(t.Context(), s, "mihomo", "stable", names[0], true)
	if err != nil || res.Version != "second" {
		t.Fatalf("custom restore: %+v %v", res, err)
	}
	s.CoreBackupRoot = ""
	res, err = Restore(t.Context(), s, "mihomo", "stable", "", true)
	if err != nil || res.Version != "first" {
		t.Fatalf("original backup: %+v %v", res, err)
	}
}

// The isolated child doubles as a core with a controller readiness endpoint.
func TestMain(m *testing.M) {
	if addr := os.Getenv("MIHOMO_KING_TEST_CONTROLLER"); addr != "" && len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println("Xray 1.2.3")
			os.Exit(0)
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

func startTestMihomo(t *testing.T) *config.Settings {
	t.Helper()
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
	return s
}

func TestDownloadXrayKeepsMihomoRunning(t *testing.T) {
	s := startTestMihomo(t)
	_, pid := mihomo.Running(s)
	dir := t.TempDir()
	s.XrayBin = filepath.Join(dir, "xray")
	body, err := os.ReadFile(s.Binary())
	if err != nil {
		t.Fatal(err)
	}
	archive := zipBytes(t, "xray", body)
	arch := "64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64-v8a"
	}
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/XTLS/Xray-core/releases":
			fmt.Fprintf(w, `[{"tag_name":"v1.2.3","assets":[{"name":"Xray-%s-%s.zip","browser_download_url":"http://%s/xray.zip"}]}]`, runtime.GOOS, arch, r.Host)
		case "/xray.zip":
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer releases.Close()
	original := githubAPI
	githubAPI = releases.URL
	defer func() { githubAPI = original }()
	for name, script := range map[string]string{"sudo": "exit 0", "setcap": "exit 0", "getcap": "echo cap_net_admin,cap_net_raw=ep"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	res, err := Install(t.Context(), s, "xray", "stable", "test", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Dest != s.XrayBin || res.Version != "Xray 1.2.3" || res.Stopped || res.Restarted {
		t.Fatalf("unexpected install: %+v", res)
	}
	if ok, after := mihomo.Running(s); !ok || after != pid {
		t.Fatalf("mihomo PID changed: %d -> %d", pid, after)
	}
	resp, err := http.Get("http://" + s.Controller + "/version")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("controller: %s", resp.Status)
	}
	s.XrayBin = s.Binary()
	if _, err := Restore(t.Context(), s, "xray", "stable", "", true); err == nil || !strings.Contains(err.Error(), "share") {
		t.Fatalf("overlapping target: %v", err)
	}
}

func TestRestartFailureRestoresRunningCore(t *testing.T) {
	s := startTestMihomo(t)
	_, oldPID := mihomo.Running(s)
	s.CoreBackupCount = 0
	_, err := changeBinary(t.Context(), s, "mihomo", s.Channel("mihomo"), true, func(stage string, _ *Result) error {
		return os.WriteFile(stage, []byte("#!/bin/sh\ncase \"$1\" in -v) echo v2;; -t) exit 0;; *) exit 2;; esac\n"), 0700)
	})
	if err == nil || !strings.Contains(err.Error(), "restored and restarted") {
		t.Fatalf("rollback: %v", err)
	}
	if ok, pid := mihomo.Running(s); !ok || pid == oldPID {
		t.Fatalf("old binary did not restart: %t %d", ok, pid)
	}
}
