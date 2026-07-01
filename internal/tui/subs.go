package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
)

func (m Model) updateSubs(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.adding {
		return m.updateAddForm(msg)
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		if filterOwnsKey(m.subs, k.String()) {
			var cmd tea.Cmd
			m.subs, cmd = m.subs.Update(msg)
			return m, cmd
		}
		if wrapListKey(&m.subs, k.String()) {
			return m, nil
		}
		switch k.String() {
		case "a":
			m.adding = true
			m.addLocal = false
			m.addStage = 0
			m.nameInput.Reset()
			m.urlInput.Reset()
			m.nameInput.Placeholder = "name (e.g. provider-a)"
			m.urlInput.Placeholder = "https://example.com/sub.yaml"
			return m, m.nameInput.Focus()
		case "n":
			m.adding = true
			m.addLocal = true
			m.addStage = 1
			m.nameInput.Reset()
			m.urlInput.Reset()
			m.urlInput.Placeholder = "vless://... or ss://..."
			return m, m.urlInput.Focus()
		case "u":
			return m.updateSelectedSub()
		case "d":
			return m.deleteSelectedSub()
		case "enter":
			return m.activateSelectedSub()
		case "esc":
			m.screen = screenDashboard
			return m, refreshStatusCmd(m.settings)
		}
		if nm, cmd, handled := m.globalKey(k.String()); handled {
			return nm, cmd
		}
	}
	var cmd tea.Cmd
	m.subs, cmd = m.subs.Update(msg)
	return m, cmd
}

func (m Model) updateAddForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			m.adding = false
			m.addLocal = false
			m.setStatus("cancelled")
			return m, nil
		case "enter":
			if m.addLocal {
				rawLink := strings.TrimSpace(m.urlInput.Value())
				if rawLink == "" {
					m.setErr("node link required")
					return m, nil
				}
				m.adding = false
				m.addLocal = false
				m.setStatus("adding local node…")
				return m, addLocalNodeCmd(m.settings, rawLink)
			}
			if m.addStage == 0 {
				if strings.TrimSpace(m.nameInput.Value()) == "" {
					m.setErr("name required")
					return m, nil
				}
				m.addStage = 1
				m.nameInput.Blur()
				return m, m.urlInput.Focus()
			}
			name := strings.TrimSpace(m.nameInput.Value())
			if err := config.ValidateSubName(name); err != nil {
				m.setErr(err.Error())
				return m, nil
			}
			rawURL := strings.TrimSpace(m.urlInput.Value())
			if rawURL == "" {
				m.setErr("url required")
				return m, nil
			}
			m.adding = false
			m.setStatus("downloading " + name + "…")
			return m, downloadCmd(name, rawURL, m.settings.SubPath(name))
		}
	}
	var cmd tea.Cmd
	if m.addStage == 0 {
		m.nameInput, cmd = m.nameInput.Update(msg)
	} else {
		m.urlInput, cmd = m.urlInput.Update(msg)
	}
	return m, cmd
}

func (m Model) updateSelectedSub() (tea.Model, tea.Cmd) {
	sel, ok := m.subs.SelectedItem().(item)
	if !ok {
		m.setErr("no subscription selected")
		return m, nil
	}
	if localName, ok := localNodeID(sel.id); ok {
		m.setErr("local node " + localName + " has no remote update")
		return m, nil
	}
	sub, _ := m.settings.FindSub(sel.id)
	if sub == nil {
		return m, nil
	}
	if err := config.ValidateSubName(sub.Name); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.setStatus("updating " + sub.Name + "…")
	return m, downloadCmd(sub.Name, sub.URL, m.settings.SubPath(sub.Name))
}

func (m Model) deleteSelectedSub() (tea.Model, tea.Cmd) {
	sel, ok := m.subs.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	if localName, ok := localNodeID(sel.id); ok {
		if !m.settings.RemoveLocalNode(localName) {
			return m, nil
		}
		if err := m.settings.Save(); err != nil {
			m.setErr(err.Error())
			return m, nil
		}
		m.reloadSubs()
		m.setStatus("deleted local node " + localName)
		return m, reloadRuntimeConfigCmd(m.settings, "local node deleted")
	}
	if err := os.Remove(m.settings.SubPath(sel.id)); err != nil && !os.IsNotExist(err) {
		m.setErr(fmt.Sprintf("delete %s: %v", sel.id, err))
		return m, nil
	}
	m.settings.RemoveSub(sel.id)
	if err := m.settings.Save(); err != nil {
		m.setErr(err.Error())
	} else {
		m.setStatus("deleted " + sel.id)
	}
	m.reloadSubs()
	return m, nil
}

func (m Model) activateSelectedSub() (tea.Model, tea.Cmd) {
	sel, ok := m.subs.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	if localName, ok := localNodeID(sel.id); ok {
		m.setErr("local node " + localName + " is available in the Local group")
		return m, nil
	}
	m.settings.Active = sel.id
	if err := m.settings.Save(); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.reloadSubs()
	m.setStatus("activating " + sel.id + "…")
	return m, switchSubCmd(m.settings)
}

func (m Model) viewSubs() string {
	if m.adding {
		if m.addLocal {
			return titleStyle.Render("Add local node") + "\n\n" +
				"  link: " + m.urlInput.View()
		}
		return titleStyle.Render("Add subscription") + "\n\n" +
			"  name: " + m.nameInput.View() + "\n" +
			"  url:  " + m.urlInput.View()
	}
	if len(m.settings.Subscriptions) == 0 && len(m.settings.LocalNodes) == 0 {
		return dimStyle.Render("No subscriptions yet — press 'a' to add one or 'n' to add a local node.")
	}
	return m.subs.View()
}

func localNodeID(id string) (string, bool) {
	name, ok := strings.CutPrefix(id, "local:")
	return name, ok
}
