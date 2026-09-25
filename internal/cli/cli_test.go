package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"mihomo-king/internal/config"
)

// run executes the CLI against a throwaway config dir holding two stored
// subscriptions, "a" (active) and "b".
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	settings = nil
	t.Cleanup(func() { settings = nil })
	s, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	s.MihomoBin = filepath.Join(dir, "missing-mihomo")
	s.Subscriptions = []config.Subscription{
		{Name: "a", URL: "https://sub.example.com/a?token=secret", UserInfo: "upload=1; download=1; total=1024"},
		{Name: "b", Path: "/tmp/b.yaml"},
	}
	s.Active = "a"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), err
}

func TestSubsHidesTokens(t *testing.T) {
	out, err := run(t, "subs")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "token") || !strings.Contains(out, "sub.example.com") || !strings.Contains(out, "file /tmp/b.yaml") {
		t.Fatalf("subs output:\n%s", out)
	}
	if out, _ := run(t, "subs", "--urls"); !strings.Contains(out, "token=secret") {
		t.Fatalf("subs --urls output:\n%s", out)
	}
}

func TestArgValidation(t *testing.T) {
	for _, args := range [][]string{
		{"mode", "bogus"},
		{"tun", "maybe"},
		{"paths", "nope"},
		{"select", "only-group"},
		{"use"},
		{"use", "missing"},
		{"check", "missing"},
		{"nosuch"},
	} {
		if _, err := run(t, args...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

func TestLiveCommandsNeedRunningCore(t *testing.T) {
	for _, args := range [][]string{{"groups"}, {"nodes", "g"}, {"select", "g", "n"}, {"delay", "n"}} {
		_, err := run(t, args...)
		if err == nil || !strings.Contains(err.Error(), "not running") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}

func TestPathsAndStatus(t *testing.T) {
	out, err := run(t, "paths", "settings")
	if err != nil || !strings.HasSuffix(strings.TrimSpace(out), filepath.Join("mihomo-king", "settings.yaml")) {
		t.Fatalf("paths settings = %q, %v", out, err)
	}
	out, err = run(t, "status", "--json")
	if err != nil || !strings.Contains(out, `"running": false`) || !strings.Contains(out, `"subscription": "a"`) {
		t.Fatalf("status --json = %s, %v", out, err)
	}
}

func TestCompleteSubs(t *testing.T) {
	out, err := run(t, "__complete", "use", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a\tactive") || !strings.Contains(out, "\nb") {
		t.Fatalf("completion:\n%s", out)
	}
	// use takes one argument: nothing more to offer once it is given.
	if out, _ := run(t, "__complete", "use", "a", ""); strings.Contains(out, "b") {
		t.Fatalf("completion after the argument:\n%s", out)
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		out, err := run(t, "completion", shell)
		if err != nil || !strings.Contains(out, "__complete") {
			t.Fatalf("completion %s: %v\n%.200s", shell, err, out)
		}
	}
}

func TestTableAlignsWideRunes(t *testing.T) {
	var b bytes.Buffer
	w := newTable(&b)
	w.Write([]byte("手动选择\tSelector\n"))
	w.Write([]byte("HBO\tSelector\n"))
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "手动选择  Selector\nHBO       Selector\n"
	if b.String() != want {
		t.Fatalf("table:\n%q\nwant\n%q", b.String(), want)
	}
}

func TestLastLines(t *testing.T) {
	text := "1\n2\n3\n"
	for n, want := range map[int]string{0: text, 1: "3\n", 2: "2\n3\n", 5: text} {
		if got := lastLines(text, n); got != want {
			t.Errorf("lastLines(%d) = %q, want %q", n, got, want)
		}
	}
}
