package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
)

func assertSubRows(t *testing.T, m Model, want ...string) {
	t.Helper()
	var ids []string
	for _, row := range m.subs.Items() {
		ids = append(ids, row.(item).id)
	}
	if !slices.Equal(ids, want) {
		t.Fatalf("in %q got %v, want %v", m.subFolder, ids, want)
	}
}

func TestSubscriptionGroupOrderAndMembership(t *testing.T) {
	m := editableSubsTestModel(t)
	m.settings.Subscriptions = []config.Subscription{{Name: "file", Path: "/import.yaml"}, {Name: "online", URL: "https://example.com"}}
	m.settings.LocalNodes = []config.LocalNode{{Name: "node"}}
	m.settings.SubscriptionGroups = []string{"work"}
	m.settings.Language = config.LanguageChinese
	m.reloadSubs()
	assertSubRows(t, m, "online", folderRowPrefix+"work", folderRowPrefix+fileSubFolder, folderRowPrefix+nodeSubFolder)
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	for i, tt := range []struct{ id, label string }{{"file", "本地导入配置文件"}, {localRowPrefix + "node", "本地导入单个节点"}} {
		m.subs.Select(i + 2)
		m, _ = press(m, enter)
		assertSubRows(t, m, tt.id)
		if !strings.HasSuffix(m.subs.Title, tt.label) {
			t.Fatal(m.subs.Title)
		}
		m, _ = press(m, keyRunes("f"))
		m, _ = press(m, paste("亚洲"))
		next, cmd := press(m, enter)
		if cmd == nil {
			t.Fatal("missing group save")
		}
		m, _ = press(next, cmd())
		assertSubRows(t, m, tt.id, folderRowPrefix+m.subFolder+"/亚洲")
		m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	}
}

func TestSubscriptionMoveIntoNestedGroupPersists(t *testing.T) {
	m := editableSubsTestModel(t)
	m.settings.Subscriptions = []config.Subscription{{Name: "provider", URL: "https://example.com/sub"}}
	m.settings.Active = "provider"
	target := fileSubFolder + "/Asia/HK"
	m.settings.SubscriptionGroups = []string{fileSubFolder + "/Asia", target}
	m.reloadSubs()
	m, _ = press(m, keyRunes("m"))
	for range 3 {
		m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	}
	if m.subFolder != target {
		t.Fatalf("destination = %q", m.subFolder)
	}
	m, cmd := press(m, keyRunes("v"))
	if cmd == nil {
		t.Fatal("missing move command")
	}
	m, cmd = press(m, cmd())
	if cmd != nil || m.statusErr || m.movingSub != "" {
		t.Fatalf("move failed or triggered a runtime command: %s", m.status)
	}
	assertSubRows(t, m, "provider")
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.UpsertSub(config.Subscription{Name: "provider", URL: "https://updated.example/sub"})
	if s.Active != "provider" || s.Subscriptions[0].ViewGroup != target {
		t.Fatalf("lost active subscription or placement: %#v", s.Subscriptions)
	}
	m = New(s)
	m.openSubFolder(target)
	assertSubRows(t, m, "provider")
}

func TestSubscriptionGroupSaveFailureCanRetry(t *testing.T) {
	m := editableSubsTestModel(t)
	m, _ = press(m, keyRunes("f"))
	m, _ = press(m, paste("work"))
	dest := filepath.Join(m.settings.AppDir(), "settings.yaml")
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	m, cmd := press(m, enter)
	m, _ = press(m, cmd())
	if !m.statusErr || m.addForm != addSubGroup || m.hasSubFolder("work") {
		t.Fatal("failed save did not preserve editable state")
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	m, cmd = press(m, enter)
	m, _ = press(m, cmd())
	if m.statusErr || m.addForm != addNone || !m.hasSubFolder("work") {
		t.Fatal("retry failed")
	}
}
