package mihomo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"mihomo-king/internal/config"
)

func tunTestSettings(t *testing.T) *config.Settings {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.MihomoDir = t.TempDir()
	s.MihomoBin, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSetTunRestoresRuntimeAfterRejectedChange(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("this controller simulation has no privileged utun; real macOS checks are separate")
	}
	s := tunTestSettings(t)
	if err := os.WriteFile(s.PidFile(), fmt.Appendf(nil, "%d", os.Getpid()), 0o600); err != nil {
		t.Fatal(err)
	}
	var patches []bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"tun":{"enable":false}}`)
			return
		}
		var body struct {
			Tun struct{ Enable bool } `json:"tun"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		patches = append(patches, body.Tun.Enable)
		if body.Tun.Enable {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "TUN permission denied")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	s.Controller = srv.URL
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.AppDir(), "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := SetTun(s, true); err == nil {
		t.Fatal("rejected TUN change succeeded")
	}
	if !slices.Equal(patches, []bool{true, false}) || s.TunEnable {
		t.Fatalf("patches=%v saved TUN=%v", patches, s.TunEnable)
	}
	after, err := os.ReadFile(filepath.Join(s.AppDir(), "settings.yaml"))
	if err != nil || string(after) != string(before) {
		t.Fatal("failed change modified persisted settings")
	}
	body, err := os.ReadFile(s.ConfigPath())
	if err != nil || !strings.Contains(string(body), "enable: false") {
		t.Fatalf("runtime configuration was not restored: %s, %v", body, err)
	}
}

func TestDarwinTunRejectsUnprivilegedCore(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Geteuid() == 0 {
		t.Skip("requires a regular macOS user")
	}
	s := tunTestSettings(t)
	if err := os.WriteFile(s.PidFile(), fmt.Appendf(nil, "%d", os.Getpid()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.AppDir(), "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if running, _ := Running(s); !running {
		t.Fatal("macOS did not recognize the tracked process")
	}
	if err := SetTun(s, true); !errors.Is(err, ErrDarwinTunPrivileges) {
		t.Fatalf("expected macOS privilege error, got %v", err)
	}
	after, err := os.ReadFile(filepath.Join(s.AppDir(), "settings.yaml"))
	if err != nil || string(after) != string(before) || s.TunEnable {
		t.Fatal("rejected change modified settings")
	}
	if _, err := os.Stat(s.ConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("rejected change wrote a runtime config: %v", err)
	}
	s.TunEnable = true
	if err := Start(s); !errors.Is(err, ErrDarwinTunPrivileges) {
		t.Fatalf("start bypassed the privilege check: %v", err)
	}
	s.TunDevice = "utun65534"
	if TunEnabled(s, true) || verifyTun(s) == nil {
		t.Fatal("reported TUN on without an interface")
	}
}
