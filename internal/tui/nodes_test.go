package tui

import (
	"testing"

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

func testGroups() []api.Group {
	return []api.Group{
		{Name: "PROXY", Type: "Selector", Now: config.LocalGroupName, All: []string{config.LocalGroupName, "remote-1"}},
		{Name: "AUTO", Type: "URLTest", Now: config.LocalGroupName, All: []string{config.LocalGroupName, "remote-1"}},
		{Name: config.LocalGroupName, Type: "Selector", Now: "local-ss", All: []string{"local-vless", "local-ss"}},
	}
}
