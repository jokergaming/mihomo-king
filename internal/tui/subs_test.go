package tui

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
)

func TestImportFileFormPrefillsNameFromPath(t *testing.T) {
	m, _ := press(newSubsTestModel(), keyRunes("i"))
	if m.addForm != addFile || m.addStage != 0 {
		t.Fatalf("form = %v stage %d, want the file form's path field", m.addForm, m.addStage)
	}
	if got := m.help(); !strings.HasPrefix(got, "tab complete path") {
		t.Fatalf("help = %q, want path completion hint", got)
	}

	m, _ = press(m, paste("~/Downloads/my-nodes.txt"))
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.addStage != 1 || m.nameInput.Value() != "my-nodes" {
		t.Fatalf("stage %d, name %q; want name stage prefilled with my-nodes", m.addStage, m.nameInput.Value())
	}

	m, cmd := press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.addForm != addNone {
		t.Fatalf("submitting the name should close the form and start the import")
	}
	if m.status != "importing my-nodes…" {
		t.Fatalf("status = %q", m.status)
	}
}

func TestImportFileFormTabCompletesPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "provider.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := press(newSubsTestModel(), keyRunes("i"))
	m, _ = press(m, paste(dir+"/pro"))

	m, cmd := press(m, tea.KeyMsg{Type: tea.KeyTab})
	if cmd == nil {
		t.Fatalf("tab returned no completion command")
	}
	m, _ = press(m, cmd())
	if got := m.urlInput.Value(); got != dir+"/provider.yaml" {
		t.Fatalf("path = %q, want completed provider.yaml", got)
	}
}

func TestStalePathCompletionIsIgnored(t *testing.T) {
	m, _ := press(newSubsTestModel(), keyRunes("i"))
	m, _ = press(m, paste("/tmp/ab"))
	m, _ = press(m, pathCompletionMsg{input: "/tmp/a", value: "/tmp/abc.yaml", matches: []string{"abc.yaml"}})
	if got := m.urlInput.Value(); got != "/tmp/ab" {
		t.Fatalf("path = %q, stale completion must not overwrite newer input", got)
	}
}

func TestAddSubscriptionFormAcceptsLocalPath(t *testing.T) {
	m, _ := press(newSubsTestModel(), keyRunes("a"))
	m, _ = press(m, paste("home"))
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(m, paste("~/sub.yaml"))
	m, cmd := press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.status != "importing home…" {
		t.Fatalf("status = %q, want a local path to be imported, not downloaded", m.status)
	}
}

func TestImportFileCmdStoresConvertedSubscription(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "nodes.txt")
	links := "trojan://pass@a.example.com:443#a\nss://YWVzLTI1Ni1nY206cGFzcw@b.example.com:8388#b\nssr://abc\n"
	if err := os.WriteFile(src, []byte(base64.StdEncoding.EncodeToString([]byte(links))), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "subscriptions", "nodes.yaml")

	msg, ok := importFileCmd("nodes", "file://"+src, dest)().(subDownloadedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("importFileCmd = %#v", msg)
	}
	if msg.sub.Name != "nodes" || msg.sub.Path != src || msg.sub.URL != "" {
		t.Fatalf("sub = %#v, want the resolved file path recorded", msg.sub)
	}
	if want := "imported nodes · 2 proxies · skipped 1 link (ssr)"; msg.note != want {
		t.Fatalf("note = %q, want %q", msg.note, want)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("stored subscription: %v", err)
	}
	if !strings.Contains(string(body), "MATCH,PROXY") {
		t.Fatalf("stored subscription is not the converted config:\n%s", body)
	}
}

func TestDownloadCmdRecordsEmptySubscription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "sub.yaml")
	msg, ok := downloadCmd("empty", srv.URL, dest)().(subDownloadedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("downloadCmd = %#v", msg)
	}
	if !msg.sub.NoNodes || msg.sub.URL != srv.URL || !strings.Contains(msg.note, "no usable nodes") {
		t.Fatalf("empty subscription message = %#v", msg)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("empty subscription created a config file: %v", err)
	}
}

func TestCompletePath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"sub-a.yaml", "sub-b.yaml", "other.txt", ".hidden.yaml", "cjk/文a.txt", "cjk/斗b.txt"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nodes"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)

	tests := []struct {
		input, want string
		matches     []string
	}{
		{dir + "/o", dir + "/other.txt", []string{"other.txt"}},
		{dir + "/no", dir + "/nodes/", []string{"nodes/"}},
		{dir + "/s", dir + "/sub-", []string{"sub-a.yaml", "sub-b.yaml"}},
		{dir + "/.h", dir + "/.hidden.yaml", []string{".hidden.yaml"}},
		{dir + "/cjk/", dir + "/cjk/", []string{"文a.txt", "斗b.txt"}}, // 文 and 斗 share leading bytes only
		{dir + "/x", dir + "/x", nil},
		{"~/o", "~/other.txt", []string{"other.txt"}},
		{"~", "~/", nil},
	}
	for _, tt := range tests {
		got, matches := completePath(tt.input)
		if got != tt.want || !slices.Equal(matches, tt.matches) {
			t.Errorf("completePath(%q) = %q, %q; want %q, %q", tt.input, got, matches, tt.want, tt.matches)
		}
	}
}

func newSubsTestModel() Model {
	return Model{
		settings:  &config.Settings{},
		screen:    screenSubs,
		subs:      newList("Subscriptions"),
		nameInput: textinput.New(),
		urlInput:  textinput.New(),
	}
}

func press(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// paste enters text the way a terminal delivers a bracketed paste.
func paste(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}
