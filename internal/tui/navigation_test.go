package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestDashboardStartsAtTopAndScrollsWholePage(t *testing.T) {
	m := editableSubsTestModel(t)
	m.screen, m.running = screenDashboard, true
	for i := range 40 {
		m.liveNodes = append(m.liveNodes, dashboardNode{text: message("route-%02d", i)})
	}
	const height = 12
	m, _ = press(m, tea.WindowSizeMsg{Width: 120, Height: height})
	top := m.View()
	if lipgloss.Height(top) != height || !strings.Contains(top, "1 Dashboard") || !strings.Contains(top, "route-00") || strings.Contains(top, "route-39") {
		t.Fatalf("unexpected initial dashboard:\n%s", top)
	}
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.dashboardOffset != 1 {
		t.Fatalf("down offset = %d, want 1", m.dashboardOffset)
	}
	m, _ = press(m, keyRunes("G"))
	if got := m.View(); !strings.Contains(got, "route-39") || strings.Contains(got, "1 Dashboard") {
		t.Fatalf("unexpected dashboard at bottom:\n%s", got)
	}
	m, _ = press(m, keyRunes("g"))
	if m.dashboardOffset <= 1 {
		t.Fatal("single g moved the dashboard")
	}
	m, _ = press(m, keyRunes("g"))
	if got := m.View(); got != top {
		t.Fatalf("gg did not restore the dashboard top:\n%s", got)
	}
	m, _ = press(m, keyRunes("G"))
	m, _ = press(m, keyRunes("2"))
	m, _ = press(m, keyRunes("1"))
	if got := m.View(); got != top {
		t.Fatalf("returning to dashboard did not start at the top:\n%s", got)
	}
}

func TestListVimJumps(t *testing.T) {
	for _, tab := range []string{"2", "3", "4"} {
		t.Run(tab, func(t *testing.T) {
			m := editableSubsTestModel(t)
			m, _ = press(m, tea.WindowSizeMsg{Width: 120, Height: 20})
			m, _ = press(m, keyRunes(tab))
			items := make([]list.Item, 30)
			for i := range items {
				items[i] = item{title: fmt.Sprintf("row-%02d", i)}
			}
			m.activeList().SetItems(items)
			assertSelection := func(want int) {
				t.Helper()
				if got := m.activeList().Index(); got != want {
					t.Fatalf("selection = %d, want %d", got, want)
				}
			}
			m, _ = press(m, keyRunes("G"))
			assertSelection(len(items) - 1)
			m, _ = press(m, keyRunes("gg"))
			assertSelection(0)
			m, _ = press(m, keyRunes("G"))
			m, _ = press(m, keyRunes("g"))
			m, _ = press(m, keyRunes("k"))
			m, _ = press(m, keyRunes("g"))
			assertSelection(len(items) - 2)
			m, _ = press(m, keyRunes("g"))
			assertSelection(0)

			m, _ = press(m, keyRunes("/"))
			m, _ = press(m, keyRunes("gg"))
			m, _ = press(m, keyRunes("G"))
			if got := m.activeList().FilterValue(); got != "ggG" {
				t.Fatalf("filter input = %q, want ggG", got)
			}
			m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
			m, _ = press(m, keyRunes("G"))
			assertSelection(len(items) - 1)
			m, _ = press(m, keyRunes("1"))
			m, _ = press(m, keyRunes(tab))
			assertSelection(0)
		})
	}
}
