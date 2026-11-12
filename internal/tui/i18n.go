package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
	"mihomo-king/internal/mihomo"
)

// Keep templates separate from user data and render at display time, including
// results from commands that were started before the language changed.
type text struct {
	key    string
	args   []any
	format func(func(string, ...any) string) string
}

func message(key string, args ...any) text { return text{key: key, args: args} }
func (t text) Error() string               { return t.String() }
func (t text) String() string              { return t.render(config.LanguageEnglish) }

func (t text) render(language string) string {
	if t.format != nil {
		return t.format(func(key string, args ...any) string {
			return message(key, args...).render(language)
		})
	}
	format := t.key
	if language == config.LanguageChinese {
		if translated, ok := chinese[format]; ok {
			format = translated
		}
	}
	if len(t.args) == 0 {
		return format
	}
	args := make([]any, len(t.args))
	for i, arg := range t.args {
		if nested, ok := arg.(text); ok {
			args[i] = nested.render(language)
		} else {
			args[i] = arg
		}
	}
	return fmt.Sprintf(format, args...)
}

func (m Model) render(t text) string {
	language := config.LanguageEnglish
	if m.settings != nil {
		language = m.settings.Language
	}
	return t.render(language)
}
func (m Model) tr(key string, args ...any) string { return m.render(message(key, args...)) }

type languageSavedMsg struct {
	language string
	err      error
}

func (m Model) toggleLanguage() (tea.Model, tea.Cmd) {
	if m.languageBusy {
		return m, nil
	}
	language := config.LanguageChinese
	if m.settings.Language == config.LanguageChinese {
		language = config.LanguageEnglish
	}
	m.languageBusy = true
	return m, func() tea.Msg {
		return languageSavedMsg{language: language, err: m.settings.SaveLanguage(language)}
	}
}

func (m *Model) refreshLanguage() {
	subs, nodes, settings := m.subs, m.nodes, m.settingsList
	m.reloadSubs()
	m.reloadSettings()
	if m.curGroup == "" {
		m.showGroups()
	} else {
		m.showMembers(m.curGroup)
	}
	for _, pair := range []struct {
		current  *list.Model
		previous list.Model
	}{
		{&m.subs, subs}, {&m.nodes, nodes}, {&m.settingsList, settings},
	} {
		restoreListState(pair.current, pair.previous)
		pair.current.FilterInput.Prompt = m.tr("Filter: ")
	}
	m.subs.Title = m.tr("Subscriptions")
	m.nameInput.Placeholder = m.tr("name (e.g. provider-a)")
	if m.addForm == addFile {
		m.nameInput.Placeholder = m.tr("defaults to the file name")
	}
	switch {
	case m.editingSub && m.editLocal:
		m.urlInput.Placeholder = m.tr("node link")
	case m.editingSub:
		m.urlInput.Placeholder = m.tr("HTTP(S) URL or file path")
	case m.addForm == addFile:
		m.urlInput.Placeholder = "~/Downloads/sub.yaml"
	case m.addForm == addNode:
		m.urlInput.Placeholder = "vless://, vmess://, ss://, trojan://, hy2://, socks5://, http://"
	default:
		m.urlInput.Placeholder = "https://example.com/sub.yaml"
	}
	m.pwInput.Placeholder = m.tr("sudo password")
	if m.editingSetting && m.settingKey != settingCoreGet && m.settingKey != settingGeoGet {
		m.settingInput.Placeholder = m.settingLabel(m.settingKey)
	}
}

// Rebuilding translated rows must preserve the current input, filter and pick.
func restoreListState(current *list.Model, previous list.Model) {
	if state := previous.FilterState(); state != list.Unfiltered {
		current.SetFilterText(previous.FilterValue())
		current.SetFilterState(state)
	}
	current.FilterInput = previous.FilterInput
	selected, ok := previous.SelectedItem().(item)
	if !ok {
		return
	}
	for i, row := range current.VisibleItems() {
		if row.(item).id == selected.id {
			current.Select(i)
			return
		}
	}
}

func (m Model) listView(l list.Model) string {
	if len(l.VisibleItems()) > 0 {
		return l.View()
	}
	title := titleStyle.Render(l.Title)
	empty := m.tr("No items.")
	if l.FilterState() != list.Unfiltered {
		empty = m.tr("No matching items.")
	}
	if l.FilterState() == list.Filtering {
		title = l.FilterInput.View()
	}
	return title + "\n\n" + dimStyle.Render(empty)
}

func errorText(err error) text {
	// Match direct UI messages only; unwrapping could discard backend context.
	if t, ok := err.(text); ok {
		return t
	}
	switch err {
	case mihomo.ErrDarwinTunPrivileges, config.ErrInvalidTunDevice:
		return message(err.Error())
	default:
		return message("%s", err.Error())
	}
}
func (m *Model) setError(err error) { m.status, m.statusErr = errorText(err), true }
