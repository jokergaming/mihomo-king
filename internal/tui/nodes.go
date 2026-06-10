package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/api"
)

func (m Model) updateNodes(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			if m.curGroup != "" {
				m.curGroup = ""
				m.showGroups()
				return m, nil
			}
			m.screen = screenDashboard
			return m, refreshStatusCmd(m.settings)
		case "enter":
			return m.nodesEnter()
		case "r":
			m.setStatus("reloading…")
			return m, loadGroupsCmd(m.settings)
		}
		if nm, cmd, handled := m.globalKey(k.String()); handled {
			return nm, cmd
		}
	}
	var cmd tea.Cmd
	m.nodes, cmd = m.nodes.Update(msg)
	return m, cmd
}

func (m Model) nodesEnter() (tea.Model, tea.Cmd) {
	sel, ok := m.nodes.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	if m.curGroup == "" {
		m.curGroup = sel.id
		m.showMembers(sel.id)
		return m, nil
	}
	if g := m.findGroup(m.curGroup); g != nil && g.Type != "Selector" {
		m.setErr(fmt.Sprintf("%s picks nodes automatically (%s); only select-type groups are switchable", m.curGroup, g.Type))
		return m, nil
	}
	m.setStatus("switching…")
	// select, then reload groups to reflect the new selection
	return m, tea.Sequence(
		selectNodeCmd(m.settings, m.curGroup, sel.id),
		loadGroupsCmd(m.settings),
	)
}

func (m *Model) findGroup(name string) *api.Group {
	for i := range m.groups {
		if m.groups[i].Name == name {
			return &m.groups[i]
		}
	}
	return nil
}

func (m *Model) showGroups() {
	items := make([]list.Item, 0, len(m.groups))
	for _, g := range m.groups {
		auto := ""
		if g.Type != "Selector" {
			auto = "  ·  auto"
		}
		items = append(items, item{
			title: g.Name,
			desc:  fmt.Sprintf("%s%s  ·  now: %s  ·  %d nodes", g.Type, auto, g.Now, len(g.All)),
			id:    g.Name,
		})
	}
	m.nodes.Title = fmt.Sprintf("Proxy groups (%d)", len(m.groups))
	m.nodes.SetItems(items)
	m.nodes.ResetSelected()
}

func (m *Model) showMembers(group string) {
	g := m.findGroup(group)
	if g == nil {
		return
	}
	items := make([]list.Item, 0, len(g.All))
	for _, name := range g.All {
		title := name
		if name == g.Now {
			title = "● " + name
		}
		items = append(items, item{title: title, id: name})
	}
	m.nodes.Title = group
	if g.Type != "Selector" {
		m.nodes.Title = fmt.Sprintf("%s (%s — picks automatically)", group, g.Type)
	}
	m.nodes.SetItems(items)
	m.nodes.ResetSelected()
}
