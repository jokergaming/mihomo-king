package tui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/api"
)

func (m Model) updateNodes(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.editingTestURL {
		return m.updateTestURL(msg)
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		if filterOwnsKey(m.nodes, k.String()) {
			var cmd tea.Cmd
			m.nodes, cmd = m.nodes.Update(msg)
			return m, cmd
		}
		if wrapListKey(&m.nodes, k.String()) {
			return m, nil
		}
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
		case "l":
			return m.testSelectedNode()
		case "a":
			return m.testCurrentGroup()
		case "u":
			m.editingTestURL = true
			m.testURLInput.SetValue(m.settings.TestURL)
			m.testURLInput.CursorEnd()
			return m, m.testURLInput.Focus()
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

func (m Model) updateTestURL(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			m.editingTestURL = false
			m.testURLInput.Blur()
			m.setStatus("cancelled")
			return m, nil
		case "enter":
			raw := strings.TrimSpace(m.testURLInput.Value())
			if err := validateTestURL(raw); err != nil {
				m.setErr(err.Error())
				return m, nil
			}
			m.settings.TestURL = raw
			if err := m.settings.Save(); err != nil {
				m.setErr(err.Error())
				return m, nil
			}
			m.editingTestURL = false
			m.testURLInput.Blur()
			m.setStatus("test url saved")
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.testURLInput, cmd = m.testURLInput.Update(msg)
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
	if g := m.findGroup(m.curGroup); g != nil && !isSelectorType(g.Type) {
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

func (m Model) testSelectedNode() (tea.Model, tea.Cmd) {
	if m.curGroup == "" {
		return m.testCurrentGroup()
	}
	sel, ok := m.nodes.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	m.setStatus("testing " + sel.id + "…")
	return m, testNodeCmd(m.settings, m.curGroup, sel.id)
}

func (m Model) testCurrentGroup() (tea.Model, tea.Cmd) {
	group := m.curGroup
	if group == "" {
		sel, ok := m.nodes.SelectedItem().(item)
		if !ok {
			return m, nil
		}
		group = sel.id
	}
	m.setStatus("testing " + group + "…")
	return m, testGroupCmd(m.settings, group)
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
	m.nodes.ResetFilter() // stale filter from the previous view would hide items
	items := make([]list.Item, 0, len(m.groups))
	for _, g := range m.groups {
		auto := ""
		if !isSelectorType(g.Type) {
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
	m.nodes.ResetFilter() // stale filter from the previous view would hide items
	items := make([]list.Item, 0, len(g.All))
	for _, name := range g.All {
		title := name
		if name == g.Now {
			title = "● " + name
		}
		items = append(items, item{title: title, desc: m.delayDescription(name), id: name})
	}
	m.nodes.Title = group
	if !isSelectorType(g.Type) {
		m.nodes.Title = fmt.Sprintf("%s (%s — picks automatically)", group, g.Type)
	}
	m.nodes.SetItems(items)
	m.nodes.ResetSelected()
}

func isSelectorType(typ string) bool {
	return strings.EqualFold(typ, "selector") || strings.EqualFold(typ, "select")
}

func (m Model) viewNodes() string {
	if !m.editingTestURL {
		return m.nodes.View()
	}
	return fmt.Sprintf("%s\n\n%s\n%s",
		m.nodes.View(),
		labelStyle.Render("test url"),
		m.testURLInput.View())
}

func (m *Model) applyDelayMsg(msg delayMsg) {
	if m.nodeDelays == nil {
		m.nodeDelays = map[string]int{}
	}
	for name, delay := range msg.delays {
		m.nodeDelays[name] = delay
	}
	if msg.group != "" && msg.group == m.curGroup {
		m.showMembers(msg.group)
	}
}

func (m *Model) delayDescription(name string) string {
	if m.nodeDelays == nil {
		return ""
	}
	delay, ok := m.nodeDelays[name]
	if !ok {
		return ""
	}
	if delay <= 0 {
		return "timeout"
	}
	return fmt.Sprintf("%d ms", delay)
}

func validateTestURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("test url required")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid test url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("test url must start with http:// or https://")
	}
	return nil
}
