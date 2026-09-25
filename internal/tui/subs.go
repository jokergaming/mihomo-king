package tui

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
	"mihomo-king/internal/subscription"
)

func (m Model) updateSubs(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.addForm != addNone {
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
			return m.openAddForm(addSub)
		case "i":
			return m.openAddForm(addFile)
		case "n":
			return m.openAddForm(addNode)
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

func (m Model) openAddForm(form addForm) (tea.Model, tea.Cmd) {
	m.addForm = form
	m.addStage = 0
	m.nameInput.Reset()
	m.urlInput.Reset()
	m.nameInput.Blur()
	m.urlInput.Blur()
	switch form {
	case addSub:
		m.nameInput.Placeholder = "name (e.g. provider-a)"
		m.urlInput.Placeholder = "https://example.com/sub.yaml"
	case addFile:
		m.nameInput.Placeholder = "defaults to the file name"
		m.urlInput.Placeholder = "~/Downloads/sub.yaml"
	case addNode:
		m.urlInput.Placeholder = "vless://, vmess://, ss://, trojan://, hy2://, socks5://, http://"
	}
	cmd := m.addInput().Focus()
	return m, cmd
}

// addInput returns the input the open form is editing: the subscription form
// asks name then url, the file form path then name, the node form a link.
func (m *Model) addInput() *textinput.Model {
	if m.addForm == addSub && m.addStage == 0 || m.addForm == addFile && m.addStage == 1 {
		return &m.nameInput
	}
	return &m.urlInput
}

func (m Model) updateAddForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			m.addForm = addNone
			m.setStatus("cancelled")
			return m, nil
		case "tab":
			if m.addForm == addFile && m.addStage == 0 {
				return m, completePathCmd(m.urlInput.Value())
			}
		case "enter":
			switch m.addForm {
			case addNode:
				rawLink := strings.TrimSpace(m.urlInput.Value())
				if rawLink == "" {
					m.setErr("node link required")
					return m, nil
				}
				m.addForm = addNone
				m.setStatus("adding local node…")
				return m, addLocalNodeCmd(m.settings, rawLink)
			case addFile:
				return m.fileFormEnter()
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
			m.addForm = addNone
			if subscription.IsLocalPath(rawURL) {
				m.setStatus("importing " + name + "…")
				return m, importFileCmd(name, rawURL, m.settings.SubPath(name))
			}
			m.setStatus("downloading " + name + "…")
			return m, downloadCmd(name, rawURL, m.settings.SubPath(name))
		}
	}
	in := m.addInput()
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	return m, cmd
}

// fileFormEnter advances the import form: the file path first, then a name
// that defaults to the file's base name.
func (m Model) fileFormEnter() (tea.Model, tea.Cmd) {
	rawPath := strings.TrimSpace(m.urlInput.Value())
	if rawPath == "" {
		m.setErr("file path required")
		return m, nil
	}
	if m.addStage == 0 {
		m.addStage = 1
		m.nameInput.SetValue(nameFromPath(rawPath))
		m.nameInput.CursorEnd()
		m.urlInput.Blur()
		cmd := m.nameInput.Focus()
		return m, cmd
	}
	name := strings.TrimSpace(m.nameInput.Value())
	if err := config.ValidateSubName(name); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.addForm = addNone
	m.setStatus("importing " + name + "…")
	return m, importFileCmd(name, rawPath, m.settings.SubPath(name))
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
	if sub.Path != "" {
		return m, importFileCmd(sub.Name, sub.Path, m.settings.SubPath(sub.Name))
	}
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
		m.screen = screenNodes
		m.pendingGroup = config.LocalGroupName
		m.pendingNode = localName
		m.setStatus("opening local node " + localName + "…")
		return m, loadGroupsCmd(m.settings)
	}
	m.setStatus("activating " + sel.id + "…")
	return m, switchSubCmd(m.settings, sel.id)
}

func (m Model) viewSubs() string {
	switch m.addForm {
	case addNode:
		return titleStyle.Render("Add local node") + "\n\n" +
			"  link: " + m.urlInput.View()
	case addSub:
		return titleStyle.Render("Add subscription") + "\n\n" +
			"  name: " + m.nameInput.View() + "\n" +
			"  url:  " + m.urlInput.View()
	case addFile:
		return titleStyle.Render("Import subscription file") + "\n\n" +
			"  file: " + m.urlInput.View() + "\n" +
			"  name: " + m.nameInput.View() + "\n\n" +
			dimStyle.Render("  Clash/mihomo YAML, a base64 node list, or node links one per line")
	}
	if len(m.settings.Subscriptions) == 0 && len(m.settings.LocalNodes) == 0 {
		return dimStyle.Render("No subscriptions yet — press 'a' to add one, 'i' to import a file or 'n' to add a local node.")
	}
	return m.subs.View()
}

func localNodeID(id string) (string, bool) {
	name, ok := strings.CutPrefix(id, "local:")
	return name, ok
}

// nameFromPath suggests a subscription name for a file: its base name without
// the extension.
func nameFromPath(path string) string {
	base := filepath.Base(strings.Trim(path, `'"`))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// completePath completes the last element of a typed path like a shell does:
// to the only match, or to the longest common prefix of several. It returns
// the new value and the matching names; directories get a trailing slash and
// dotfiles only match an explicit "." prefix.
func completePath(input string) (string, []string) {
	if input == "~" {
		return "~/", nil
	}
	dir, prefix := filepath.Split(input)
	readDir := cmp.Or(dir, ".")
	if rest, ok := strings.CutPrefix(readDir, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			readDir = filepath.Join(home, rest)
		}
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		return input, nil
	}
	var matches []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if info, err := os.Stat(filepath.Join(readDir, name)); err == nil && info.IsDir() {
			name += "/"
		}
		matches = append(matches, name)
	}
	if len(matches) == 0 {
		return input, nil
	}
	return dir + commonPrefix(matches), matches
}

func commonPrefix(values []string) string {
	prefix := values[0]
	for _, v := range values[1:] {
		for !strings.HasPrefix(v, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	for !utf8.ValidString(prefix) { // don't split a multi-byte character
		prefix = prefix[:len(prefix)-1]
	}
	return prefix
}

// matchList renders completion candidates for the status line.
func matchList(matches []string) string {
	const shown = 8
	if len(matches) <= shown {
		return strings.Join(matches, "  ")
	}
	return strings.Join(matches[:shown], "  ") + fmt.Sprintf("  … %d more", len(matches)-shown)
}
