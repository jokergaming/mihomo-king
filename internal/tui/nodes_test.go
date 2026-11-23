package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
)

func TestGroupsMsgOpensPendingLocalNode(t *testing.T) {
	m := Model{
		nodes:        newList("Nodes"),
		pendingGroup: config.LocalGroupName,
		pendingNode:  "local-ss",
	}

	gotModel, _ := m.Update(groupsMsg{groups: testGroups()})
	got := gotModel.(Model)
	if got.curGroup != config.LocalGroupName {
		t.Fatalf("curGroup = %q, want Local", got.curGroup)
	}
	sel, ok := got.nodes.SelectedItem().(item)
	if !ok {
		t.Fatalf("selected item has type %T", got.nodes.SelectedItem())
	}
	if sel.id != "local-ss" {
		t.Fatalf("selected item = %q, want local-ss", sel.id)
	}
}

func TestNodesEnterGroupMemberSelectsAndOpensPendingGroup(t *testing.T) {
	m := Model{
		settings: &config.Settings{},
		groups:   testGroups(),
		nodes:    newList("Nodes"),
		curGroup: "PROXY",
	}
	m.showMembers("PROXY")

	gotModel, cmd := m.nodesEnter()
	got := gotModel.(Model)
	if cmd == nil {
		t.Fatalf("nodesEnter returned nil command")
	}
	if got.pendingGroup != config.LocalGroupName {
		t.Fatalf("pendingGroup = %q, want Local", got.pendingGroup)
	}
}

func TestNodesEnterGroupMemberFromAutoGroupOnlyOpens(t *testing.T) {
	m := Model{
		groups:   testGroups(),
		nodes:    newList("Nodes"),
		curGroup: "AUTO",
	}
	m.showMembers("AUTO")

	gotModel, cmd := m.nodesEnter()
	got := gotModel.(Model)
	if cmd != nil {
		t.Fatalf("nodesEnter returned command for automatic group")
	}
	if got.curGroup != config.LocalGroupName {
		t.Fatalf("curGroup = %q, want Local", got.curGroup)
	}
}

func TestChooseChainNodeUsesTwoConcreteMembers(t *testing.T) {
	m := Model{
		settings: &config.Settings{},
		groups:   []api.Group{{Name: "Proxy", Type: "Selector", All: []string{"entry", "exit"}}},
		nodes:    newList("Nodes"),
		curGroup: "Proxy",
	}
	m.showMembers("Proxy")
	first, cmd := m.chooseChainNode()
	m = first.(Model)
	if cmd != nil || m.chainEntry != "entry" {
		t.Fatalf("first pick: chainEntry=%q, cmd=%v", m.chainEntry, cmd)
	}
	m.nodes.Select(1)
	second, cmd := m.chooseChainNode()
	m = second.(Model)
	if cmd == nil || m.chainEntry != "" {
		t.Fatalf("second pick: chainEntry=%q, cmd=%v", m.chainEntry, cmd)
	}
}

func TestNodesViewDrawsActiveChain(t *testing.T) {
	m := New(&config.Settings{
		Active: "sub",
		Chain:  &config.ProxyChain{Subscription: "sub", Entry: "香港入口节点01", Exit: "美国出口节点02"},
	})
	m.screen = screenNodes
	m, _ = press(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if page := ansi.Strip(m.View()); !strings.Contains(page, "Local ──▶ Entry 香港入口节点01 ──▶ Exit 美国出口节点02 ──▶ Target") {
		t.Fatalf("English route is missing from the Nodes screen:\n%s", page)
	}

	m.settings.Language = config.LanguageChinese
	m, _ = press(m, tea.WindowSizeMsg{Width: 38, Height: 24})
	var route string
	for line := range strings.SplitSeq(ansi.Strip(m.View()), "\n") {
		if strings.Contains(line, "本机─▶") {
			route = strings.TrimRight(line, " ")
			break
		}
	}
	if ansi.StringWidth(route) > m.width || !strings.Contains(route, "本机─▶入口 香港…─▶出口 美国…─▶目标") {
		t.Fatalf("narrow Chinese route = %q", route)
	}

	m.settings.Active = "another-sub"
	if page := ansi.Strip(m.View()); strings.Contains(page, "─▶") {
		t.Fatalf("another subscription's route appeared on the Nodes screen:\n%s", page)
	}
}

func testGroups() []api.Group {
	return []api.Group{
		{Name: "PROXY", Type: "Selector", Now: config.LocalGroupName, All: []string{config.LocalGroupName, "remote-1"}},
		{Name: "AUTO", Type: "URLTest", Now: config.LocalGroupName, All: []string{config.LocalGroupName, "remote-1"}},
		{Name: config.LocalGroupName, Type: "Selector", Now: "local-ss", All: []string{"local-vless", "local-ss"}},
	}
}
