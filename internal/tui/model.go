// Package tui implements the Bubble Tea terminal UI for mihomo-king.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
	"mihomo-king/internal/subscription"
)

type screen int

const (
	screenDashboard screen = iota
	screenSubs
	screenNodes
)

// Model is the root Bubble Tea model.
type Model struct {
	settings  *config.Settings
	screen    screen
	width     int
	height    int
	status    string // transient status / error line
	statusErr bool

	// dashboard (live state)
	running    bool
	pid        int
	version    string
	liveMode   string
	liveTun    bool
	capWarn    bool
	foreignTun []string

	// subscriptions
	subs      list.Model
	adding    bool
	addStage  int // 0 = name, 1 = url
	nameInput textinput.Model
	urlInput  textinput.Model

	// nodes
	groups   []api.Group
	nodes    list.Model
	curGroup string // "" = group list shown; else members of this group
}

// item is a generic list row. id holds the underlying name (title may be decorated).
type item struct {
	title string
	desc  string
	id    string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

// New builds the root model from loaded settings.
func New(s *config.Settings) Model {
	m := Model{settings: s}
	m.subs = newList("Subscriptions")
	m.nodes = newList("Nodes")

	m.nameInput = textinput.New()
	m.nameInput.Placeholder = "name (e.g. provider-a)"
	m.nameInput.CharLimit = 64
	m.urlInput = textinput.New()
	m.urlInput.Placeholder = "https://example.com/sub.yaml"
	m.urlInput.CharLimit = 2048

	m.reloadSubs()
	return m
}

func newList(title string) list.Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	return l
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(refreshStatusCmd(m.settings), tickCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		bodyH := msg.Height - 8
		if bodyH < 3 {
			bodyH = 3
		}
		m.subs.SetSize(msg.Width-4, bodyH)
		m.nodes.SetSize(msg.Width-4, bodyH)
		return m, nil

	case statusMsg:
		m.running, m.pid, m.version = msg.running, msg.pid, msg.version
		m.liveMode, m.liveTun, m.capWarn = msg.mode, msg.tun, msg.capWarn
		m.foreignTun = msg.foreignTun
		return m, nil

	case tickMsg:
		return m, tea.Batch(refreshStatusCmd(m.settings), tickCmd())

	case actionMsg:
		if msg.err != nil {
			m.setErr(msg.err.Error())
		} else {
			m.setStatus(msg.note)
		}
		return m, refreshStatusCmd(m.settings)

	case subDownloadedMsg:
		if msg.err != nil {
			m.setErr(msg.err.Error())
			return m, nil
		}
		m.settings.UpsertSub(msg.sub)
		if err := m.settings.Save(); err != nil {
			m.setErr(err.Error())
		} else {
			m.setStatus("downloaded " + msg.sub.Name)
		}
		m.reloadSubs()
		return m, nil

	case groupsMsg:
		if msg.err != nil {
			m.setErr("API: " + msg.err.Error())
			m.groups = nil
		} else {
			m.groups = msg.groups
			m.setStatus("loaded proxy groups")
		}
		m.curGroup = ""
		m.showGroups()
		return m, nil
	}

	switch m.screen {
	case screenSubs:
		return m.updateSubs(msg)
	case screenNodes:
		return m.updateNodes(msg)
	default:
		return m.updateDashboard(msg)
	}
}

// globalKey handles keys common to every screen (quit, screen switching).
func (m Model) globalKey(k string) (Model, tea.Cmd, bool) {
	switch k {
	case "ctrl+c", "q":
		return m, tea.Quit, true
	case "1":
		m.screen = screenDashboard
		return m, refreshStatusCmd(m.settings), true
	case "2":
		m.screen = screenSubs
		m.reloadSubs()
		return m, nil, true
	case "3":
		m.screen = screenNodes
		return m, loadGroupsCmd(m.settings), true
	}
	return m, nil, false
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	switch m.screen {
	case screenSubs:
		b.WriteString(m.viewSubs())
	case screenNodes:
		b.WriteString(m.nodes.View())
	default:
		b.WriteString(m.viewDashboard())
	}
	b.WriteString("\n\n")
	b.WriteString(m.footer())
	return appStyle.Render(b.String())
}

func (m Model) header() string {
	tabs := []string{"1 Dashboard", "2 Subscriptions", "3 Nodes"}
	parts := make([]string, len(tabs))
	for i, t := range tabs {
		if int(m.screen) == i {
			parts[i] = activeTab.Render("[" + t + "]")
		} else {
			parts[i] = dimStyle.Render(" " + t + " ")
		}
	}
	return titleStyle.Render("mihomo-king") + "   " + strings.Join(parts, " ")
}

func (m Model) footer() string {
	var status string
	switch {
	case m.status == "":
	case m.statusErr:
		status = errStyle.Render("✗ " + m.status)
	default:
		status = okStyle.Render("• " + m.status)
	}
	return status + "\n" + dimStyle.Render(m.help())
}

func (m Model) help() string {
	switch m.screen {
	case screenSubs:
		if m.adding {
			return "enter confirm · esc cancel"
		}
		return "a add · u update · enter activate · d delete · 1/2/3 switch · q quit"
	case screenNodes:
		return "enter open/select · esc back · r reload · 1/2/3 switch · q quit"
	default:
		return "s start · x stop · t toggle tun · r refresh · 2 subs · 3 nodes · q quit"
	}
}

func (m *Model) reloadSubs() {
	items := make([]list.Item, 0, len(m.settings.Subscriptions))
	for _, sub := range m.settings.Subscriptions {
		desc := sub.URL
		if info := subscription.FormatUserInfo(sub.UserInfo); info != "" {
			desc = info + "  ·  " + sub.URL
		}
		title := sub.Name
		if sub.Name == m.settings.Active {
			title = "● " + sub.Name + " (active)"
		}
		items = append(items, item{title: title, desc: desc, id: sub.Name})
	}
	m.subs.SetItems(items)
}

func (m *Model) setStatus(s string) { m.status, m.statusErr = s, false }
func (m *Model) setErr(s string)    { m.status, m.statusErr = s, true }

var (
	appStyle   = lipgloss.NewStyle().Padding(1, 2)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	activeTab  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)
