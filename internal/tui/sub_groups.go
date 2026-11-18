package tui

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
)

// Group paths are presentation metadata. An empty assignment follows the
// source's default, while "/" explicitly places an item at the top level.
const (
	fileSubFolder      = "@files"
	nodeSubFolder      = "@nodes"
	explicitRoot       = "/"
	folderRowPrefix    = "/group/"
	localRowPrefix     = "/node/"
	subGroupPreference = "subscription_groups"
)

var defaultSubFolders = [...]struct{ id, label string }{
	{fileSubFolder, "Imported configuration files"},
	{nodeSubFolder, "Imported individual nodes"},
}

func (m Model) hasSubFolder(folder string) bool {
	return folder == "" || folder == explicitRoot || folder == fileSubFolder || folder == nodeSubFolder || slices.Contains(m.settings.SubscriptionGroups, folder)
}

func entryFolder(group, defaultFolder string) string {
	if group == explicitRoot {
		return ""
	}
	return cmp.Or(group, defaultFolder)
}

func parentSubFolder(group string) string {
	if i := strings.LastIndexByte(group, '/'); i >= 0 {
		return group[:i]
	}
	return ""
}

func (m Model) subFolderTitle() string {
	title := m.tr("Subscriptions")
	if m.subFolder != "" {
		top, rest, nested := strings.Cut(m.subFolder, "/")
		for _, folder := range defaultSubFolders {
			if top == folder.id {
				top = m.tr(folder.label)
				break
			}
		}
		title += " / " + top
		if nested {
			title += " / " + strings.ReplaceAll(rest, "/", " / ")
		}
	}
	if len(m.movingSubs) > 0 {
		return m.tr("Move %d item(s) to %s", len(m.movingSubs), title)
	}
	return title
}

func (m *Model) openSubFolder(folder string) {
	m.subFolder = folder
	m.subs.ResetFilter()
	m.reloadSubs()
	m.subs.Select(0)
}

func (m *Model) leaveSubFolder() {
	folder := m.subFolder
	m.openSubFolder(parentSubFolder(folder))
	for i, row := range m.subs.Items() {
		if row.(item).id == folderRowPrefix+folder {
			m.subs.Select(i)
			break
		}
	}
}

func (m Model) createSubFolder() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.nameInput.Value())
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.ContainsFunc(name, unicode.IsControl) {
		m.setErr("enter a group name without slashes or control characters")
		return m, nil
	}
	folder := path.Join(m.subFolder, name)
	if m.subFolder == "" {
		for _, builtin := range defaultSubFolders {
			if name == builtin.label || name == chinese[builtin.label] {
				m.setErr("a group with this name already exists")
				return m, nil
			}
		}
	}
	if m.hasSubFolder(folder) {
		m.setErr("a group with this name already exists")
		return m, nil
	}
	return m.savePreference(subGroupPreference, func(s *config.Settings) {
		s.SubscriptionGroups = append(slices.Clone(s.SubscriptionGroups), folder)
	})
}

func (m Model) deleteSubFolder(folder string) (tea.Model, tea.Cmd) {
	if folder == fileSubFolder || folder == nodeSubFolder {
		m.setErr("the default group is permanent")
		return m, nil
	}
	if len(m.subChildren()[folder]) > 0 {
		m.setErr("move the contents out before deleting this group")
		return m, nil
	}
	return m.savePreference(subGroupPreference, func(s *config.Settings) {
		s.SubscriptionGroupOrder = maps.Clone(s.SubscriptionGroupOrder)
		delete(s.SubscriptionGroupOrder, folder)
		s.SubscriptionGroups = slices.DeleteFunc(slices.Clone(s.SubscriptionGroups), func(group string) bool { return group == folder })
	})
}

func (m Model) updateMoveSub(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.movingSubs = nil
		m.openSubFolder(m.moveFromFolder)
		m.restoreSubFilter()
		m.focusSub(m.subFocus)
	case "backspace":
		if m.subFolder != "" {
			m.leaveSubFolder()
		}
	case "enter":
		if row, ok := m.subs.SelectedItem().(item); ok {
			if folder, ok := strings.CutPrefix(row.id, folderRowPrefix); ok {
				m.openSubFolder(folder)
			}
		}
	case "v":
		ids, group := m.movingSubs, cmp.Or(m.subFolder, explicitRoot)
		return m.savePreference(subGroupPreference, func(s *config.Settings) {
			s.Subscriptions = slices.Clone(s.Subscriptions)
			s.LocalNodes = slices.Clone(s.LocalNodes)
			for i, sub := range s.Subscriptions {
				if ids[sub.Name] && sub.ViewGroup != group {
					s.Subscriptions[i].ViewOrder = 0
					s.Subscriptions[i].ViewGroup = group
				}
			}
			for i, node := range s.LocalNodes {
				if ids[localRowPrefix+node.Name] && node.ViewGroup != group {
					s.LocalNodes[i].ViewOrder = 0
					s.LocalNodes[i].ViewGroup = group
				}
			}
		})
	default:
		var cmd tea.Cmd
		m.subs, cmd = m.subs.Update(k)
		return m, cmd
	}
	return m, nil
}
