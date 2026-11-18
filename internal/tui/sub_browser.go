package tui

import (
	"cmp"
	"maps"
	"math"
	"path"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
	"mihomo-king/internal/subscription"
)

const subDisplayPreference = "subscription_display"

func (m Model) subChildren() map[string][]item {
	rows := make(map[string][]item)
	for _, sub := range m.settings.Subscriptions {
		defaultFolder := ""
		if sub.Path != "" {
			defaultFolder = fileSubFolder
		}
		parent := entryFolder(sub.ViewGroup, defaultFolder)
		source := sub.URL
		if sub.Path != "" {
			source = m.tr("file · ") + sub.Path
		}
		desc := source
		if info := subscription.FormatUserInfoWith(sub.UserInfo, m.tr); info != "" {
			desc = info + "  ·  " + source
		}
		if sub.NoNodes {
			desc = m.tr("no usable nodes  ·  ") + desc
		}
		if sub.NeedsUpdate {
			desc = m.tr("! update needed  ·  ") + desc
		}
		title := sub.Name
		if sub.Name == m.settings.Active {
			title = "● " + sub.Name + m.tr(" (active)")
		}
		rows[parent] = append(rows[parent], item{title: title, desc: desc, id: sub.Name, order: sub.ViewOrder})
	}
	for _, node := range m.settings.LocalNodes {
		parent := entryFolder(node.ViewGroup, nodeSubFolder)
		rows[parent] = append(rows[parent], item{
			title: m.tr("Local / ") + node.Name,
			desc:  m.tr("local node · ") + node.Link,
			id:    localRowPrefix + node.Name,
			order: node.ViewOrder,
		})
	}

	for _, group := range m.settings.SubscriptionGroups {
		parent := parentSubFolder(group)
		rows[parent] = append(rows[parent], item{id: folderRowPrefix + group, order: m.settings.SubscriptionGroupOrder[group], title: path.Base(group), desc: m.tr("Group")})
	}
	for _, children := range rows {
		switch m.settings.SubscriptionSort {
		case "manual":
			slices.SortStableFunc(children, func(a, b item) int { return cmp.Compare(cmp.Or(a.order, math.MaxInt), cmp.Or(b.order, math.MaxInt)) })
		case "asc", "desc":
			slices.SortStableFunc(children, func(a, b item) int {
				n := cmp.Compare(subSortName(a.id), subSortName(b.id))
				if m.settings.SubscriptionSort == "desc" {
					return -n
				}
				return n
			})
		}
	}
	for _, group := range defaultSubFolders {
		rows[""] = append(rows[""], item{id: folderRowPrefix + group.id, title: m.tr(group.label), desc: m.tr("Group")})
	}
	return rows
}

func subSortName(id string) string {
	if name, ok := localNodeID(id); ok {
		return strings.ToLower(name)
	}
	return strings.ToLower(path.Base(id))
}

func (m Model) subTree() bool { return m.settings.SubscriptionTree && len(m.movingSubs) == 0 }

func (m *Model) revealSubFolder(folder string) {
	if m.expandedSubs == nil {
		m.expandedSubs = make(map[string]bool)
	}
	for folder != "" {
		m.expandedSubs[folder] = true
		folder = parentSubFolder(folder)
	}
}

func (m Model) subParent(id string) string {
	if group, ok := strings.CutPrefix(id, folderRowPrefix); ok {
		return parentSubFolder(group)
	}
	if name, ok := localNodeID(id); ok {
		if node, _ := m.settings.FindLocalNode(name); node != nil {
			return entryFolder(node.ViewGroup, nodeSubFolder)
		}
	} else if sub, _ := m.settings.FindSub(id); sub != nil {
		defaultFolder := ""
		if sub.Path != "" {
			defaultFolder = fileSubFolder
		}
		return entryFolder(sub.ViewGroup, defaultFolder)
	}
	return ""
}

func (m *Model) focusSub(id string) {
	for i, row := range m.subs.VisibleItems() {
		if row.(item).id == id {
			m.subs.Select(i)
			return
		}
	}
}

func (m Model) browserRows(children map[string][]item) []list.Item {
	var rows []list.Item
	var visit func(string, int)
	visit = func(parent string, depth int) {
		for _, row := range children[parent] {
			folder, group := strings.CutPrefix(row.id, folderRowPrefix)
			if len(m.movingSubs) > 0 && !group {
				continue
			}
			if group {
				arrow := "▸ "
				if m.subTree() && m.expandedSubs[folder] {
					arrow = "▾ "
				}
				row.title = arrow + row.title
			} else if m.checkedSubs[row.id] {
				row.title = "[✓] " + row.title
			}
			row.title = strings.Repeat("  ", depth) + row.title
			rows = append(rows, row)
			if group && m.subTree() && m.expandedSubs[folder] {
				visit(folder, depth+1)
			}
		}
	}
	root := m.subFolder
	if m.subTree() {
		root = ""
	}
	visit(root, 0)
	return rows
}

var subscriptionSorts = [...]struct{ id, label string }{
	{"", "Default order"}, {"asc", "Name (A–Z)"}, {"desc", "Name (Z–A)"}, {"manual", "Manual order"},
}

func (m *Model) reloadSubs() {
	previous := m.subs
	m.subs.Title = m.subFolderTitle()
	var rows []list.Item
	if m.sortSubs {
		m.subs.Title = m.tr("Subscription sort order")
		for _, order := range subscriptionSorts {
			title := m.tr(order.label)
			if m.settings.SubscriptionSort == order.id {
				title = "● " + title
			}
			rows = append(rows, item{id: order.id, title: title})
		}
	} else {
		children := m.subChildren()
		if len(m.checkedSubs) > 0 {
			valid := make(map[string]bool, len(m.checkedSubs))
			for _, siblings := range children {
				for _, row := range siblings {
					if m.checkedSubs[row.id] {
						valid[row.id] = true
					}
				}
			}
			m.checkedSubs = valid
		}
		rows = m.browserRows(children)
		if m.subTree() {
			m.subs.Title = m.tr("Subscriptions · tree")
		}
		if len(m.checkedSubs) > 0 && len(m.movingSubs) == 0 {
			m.subs.Title += " · " + m.tr("%d selected", len(m.checkedSubs))
		}
	}
	m.subs.SetItems(rows)
	if previous.FilterState() != list.Unfiltered {
		restoreListState(&m.subs, previous)
	}
}

func (m Model) startSubMove(id string) (tea.Model, tea.Cmd) {
	m.movingSubs = maps.Clone(m.checkedSubs)
	if len(m.movingSubs) == 0 {
		if id == "" || strings.HasPrefix(id, folderRowPrefix) {
			return m, nil
		}
		m.movingSubs = map[string]bool{id: true}
	}
	m.subFocus = id
	m.subReturnFilter = m.subs.FilterValue()
	m.moveFromFolder = m.subFolder
	start := m.subFolder
	if m.settings.SubscriptionTree {
		start = m.subParent(id)
	}
	m.openSubFolder(start)
	return m, nil
}

func (m Model) updateSortMenu(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.sortSubs = false
		m.reloadSubs()
		m.restoreSubFilter()
		m.focusSub(m.subFocus)
	case "enter":
		if row, ok := m.subs.SelectedItem().(item); ok {
			return m.savePreference(subDisplayPreference, func(s *config.Settings) { s.SubscriptionSort = row.id })
		}
	case "up", "k", "down", "j", "home", "end":
		if !wrapListKey(&m.subs, k.String()) {
			m.subs, _ = m.subs.Update(k)
		}
	}
	return m, nil
}

func (m *Model) toggleBranch(folder string) {
	if m.expandedSubs[folder] {
		delete(m.expandedSubs, folder)
	} else {
		m.revealSubFolder(folder)
	}
	m.reloadSubs()
	m.focusSub(folderRowPrefix + folder)
}

func (m *Model) treeArrow(selected item, key string) {
	folder, isGroup := strings.CutPrefix(selected.id, folderRowPrefix)
	if key == "left" {
		if isGroup && m.expandedSubs[folder] {
			m.toggleBranch(folder)
			return
		}
		if parent := m.subParent(selected.id); parent != "" {
			m.focusSub(folderRowPrefix + parent)
		}
		return
	}
	if !isGroup {
		return
	}
	if !m.expandedSubs[folder] {
		m.toggleBranch(folder)
		return
	}
	rows := m.subs.VisibleItems()
	next := m.subs.Index() + 1
	if next < len(rows) && m.subParent(rows[next].(item).id) == folder {
		m.subs.Select(next)
	}
}

func (m Model) reorderSub(id string, step int) (tea.Model, tea.Cmd) {
	if m.subs.FilterState() != list.Unfiltered {
		m.setErr("clear the filter before reordering")
		return m, nil
	}
	parent := m.subParent(id)
	siblings := m.subChildren()[parent]
	if parent == "" {
		siblings = siblings[:len(siblings)-len(defaultSubFolders)]
	}
	i := slices.IndexFunc(siblings, func(row item) bool { return row.id == id })
	next := i + step
	if i < 0 || next < 0 || next >= len(siblings) {
		return m, nil
	}
	siblings[i], siblings[next] = siblings[next], siblings[i]
	ranks := make(map[string]int, len(siblings))
	for i, row := range siblings {
		ranks[row.id] = i + 1
	}
	m.subFocus = id
	return m.savePreference(subDisplayPreference, func(s *config.Settings) {
		s.SubscriptionSort = "manual"
		s.Subscriptions = slices.Clone(s.Subscriptions)
		for i, sub := range s.Subscriptions {
			if rank := ranks[sub.Name]; rank > 0 {
				s.Subscriptions[i].ViewOrder = rank
			}
		}
		s.LocalNodes = slices.Clone(s.LocalNodes)
		for i, node := range s.LocalNodes {
			if rank := ranks[localRowPrefix+node.Name]; rank > 0 {
				s.LocalNodes[i].ViewOrder = rank
			}
		}
		s.SubscriptionGroupOrder = maps.Clone(s.SubscriptionGroupOrder)
		for _, folder := range s.SubscriptionGroups {
			if rank := ranks[folderRowPrefix+folder]; rank > 0 {
				if s.SubscriptionGroupOrder == nil {
					s.SubscriptionGroupOrder = make(map[string]int)
				}
				s.SubscriptionGroupOrder[folder] = rank
			}
		}
	})
}

func (m *Model) restoreSubFilter() {
	if m.subReturnFilter != "" {
		m.subs.SetFilterText(m.subReturnFilter)
		m.subReturnFilter = ""
	}
}

func (m *Model) toggleVisibleSubs() {
	var ids []string
	allChecked := true
	for _, row := range m.subs.VisibleItems() {
		id := row.(item).id
		if !strings.HasPrefix(id, folderRowPrefix) {
			ids = append(ids, id)
			allChecked = allChecked && m.checkedSubs[id]
		}
	}
	if m.checkedSubs == nil {
		m.checkedSubs = make(map[string]bool)
	}
	for _, id := range ids {
		if allChecked {
			delete(m.checkedSubs, id)
		} else {
			m.checkedSubs[id] = true
		}
	}
	m.reloadSubs()
}
