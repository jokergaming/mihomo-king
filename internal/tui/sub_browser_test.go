package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
)

func saveSubKey(t *testing.T, m Model, key tea.KeyMsg) Model {
	t.Helper()
	m, cmd := press(m, key)
	if cmd == nil {
		t.Fatalf("%v: expected save", key)
	}
	m, cmd = press(m, cmd())
	if m.statusErr || cmd != nil {
		t.Fatalf("%v: unexpected result %s", key, m.status)
	}
	return m
}

func TestSubTreeBulkMoveCancelRetry(t *testing.T) {
	m := editableSubsTestModel(t)
	m.settings.Subscriptions = []config.Subscription{{Name: "remote"}, {Name: "file", Path: "file.yaml"}}
	m.settings.LocalNodes = []config.LocalNode{{Name: "node"}}
	m.settings.SubscriptionGroups = []string{"dest"}
	m.settings.Active = "remote"
	m.reloadSubs()
	m = saveSubKey(t, m, keyRunes("t"))
	space := tea.KeyMsg{Type: tea.KeySpace}
	m, _ = press(m, space)
	m.focusSub(folderRowPrefix + fileSubFolder)
	right := tea.KeyMsg{Type: tea.KeyRight}
	m, _ = press(m, right)
	m, _ = press(m, right)
	if got := m.subs.SelectedItem().(item).id; got != "file" {
		t.Fatalf("right selected %q", got)
	}
	m, _ = press(m, space)
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyLeft})
	m.focusSub(folderRowPrefix + nodeSubFolder)
	m, _ = press(m, right)
	m, _ = press(m, right)
	m, _ = press(m, space)
	if !strings.Contains(m.subs.Title, "3 selected") {
		t.Fatal(m.subs.Title)
	}
	m, _ = press(m, keyRunes("m"))
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.checkedSubs) != 3 || m.subs.SelectedItem().(item).id != localRowPrefix+"node" {
		t.Fatal("cancel lost selection or focus")
	}
	m, _ = press(m, keyRunes("m"))
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyBackspace})
	m.focusSub(folderRowPrefix + "dest")
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	// Make the settings path unwritable, even for root.
	dest := filepath.Join(m.settings.AppDir(), "settings.yaml")
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	m, cmd := press(m, keyRunes("v"))
	m, _ = press(m, cmd())
	if !m.statusErr || len(m.checkedSubs) != 3 || len(m.movingSubs) != 3 || m.settings.Subscriptions[0].ViewGroup != "" {
		t.Fatal("failed move partially applied")
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	m = saveSubKey(t, m, keyRunes("v"))
	if len(m.checkedSubs) != 0 || !m.expandedSubs["dest"] {
		t.Fatal("destination did not open")
	}
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !s.SubscriptionTree || s.Active != "remote" || s.Subscriptions[0].ViewGroup != "dest" || s.Subscriptions[1].ViewGroup != "dest" || s.LocalNodes[0].ViewGroup != "dest" {
		t.Fatal("bulk move or tree preference not saved")
	}
}

func TestSubSortAndManualOrder(t *testing.T) {
	m := editableSubsTestModel(t)
	m.settings.Subscriptions = []config.Subscription{{Name: "Zulu"}, {Name: "alpha"}, {Name: "Nested", ViewGroup: "Beta"}}
	m.settings.SubscriptionGroups = []string{"Beta"}
	m.reloadSubs()
	m, _ = press(m, keyRunes("s"))
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyDown})
	m = saveSubKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	file, node := folderRowPrefix+fileSubFolder, folderRowPrefix+nodeSubFolder
	beta := folderRowPrefix + "Beta"
	assertSubRows(t, m, "alpha", beta, "Zulu", file, node)
	if m.subs.SelectedItem().(item).id != "Zulu" {
		t.Fatal("sort lost focus")
	}
	m = saveSubKey(t, m, keyRunes("t"))
	m.focusSub(beta)
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyRight})
	m = saveSubKey(t, m, keyRunes("J"))
	assertSubRows(t, m, "alpha", "Zulu", beta, "Nested", file, node)
	m.focusSub(file)
	if _, cmd := press(m, keyRunes("K")); cmd != nil {
		t.Fatal("default group moved")
	}
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.UpsertSub(config.Subscription{Name: "Zulu", URL: "updated"})
	m = New(s)
	assertSubRows(t, m, "alpha", "Zulu", beta, file, node)
	m, _ = m.switchScreen(screenSubs)
	m, _ = press(m, keyRunes("s"))
	m.focusSub("desc")
	m = saveSubKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assertSubRows(t, m, "Zulu", beta, "alpha", file, node)
}

func TestSubFilteredSelectAllAndCancel(t *testing.T) {
	m := editableSubsTestModel(t)
	m.settings.Subscriptions = []config.Subscription{{Name: "alpha-1"}, {Name: "alpha-2"}, {Name: "beta"}}
	m.reloadSubs()
	m.focusSub("beta")
	m, _ = press(m, tea.KeyMsg{Type: tea.KeySpace})
	m.subs.SetFilterText("alpha")
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	if len(m.checkedSubs) != 3 {
		t.Fatalf("selected = %v", m.checkedSubs)
	}
	m, _ = press(m, keyRunes("m"))
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.subs.FilterValue(); got != "alpha" {
		t.Fatalf("lost filter: %s", got)
	}
	m, _ = press(m, keyRunes("s"))
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.subs.FilterValue(); got != "alpha" {
		t.Fatalf("sort lost filter: %s", got)
	}
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	if len(m.checkedSubs) != 1 || !m.checkedSubs["beta"] {
		t.Fatalf("hidden selection changed: %v", m.checkedSubs)
	}
}
