package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
)

type coreUpdateChecks struct {
	cancel            context.CancelFunc
	generation        uint64
	notice, dismissed core.Update
}

type startCoreChecksMsg struct{}
type coreUpdateCheckedMsg struct {
	generation uint64
	update     core.Update
	err        error
}

func (m *Model) restartCoreChecks() tea.Cmd {
	if m.coreUpdates.cancel != nil {
		m.coreUpdates.cancel()
	}
	m.coreUpdates = coreUpdateChecks{generation: m.coreUpdates.generation + 1}
	if !m.settings.CoreUpdateReminders {
		return nil
	}
	return m.scheduleCoreCheck(0)
}

func (m *Model) scheduleCoreCheck(delay time.Duration) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.coreUpdates.cancel = cancel
	generation, tool, bin := m.coreUpdates.generation, m.settings.Tool(), m.settings.Binary()
	channel, source := m.settings.Channel(tool), m.settings.CoreReleaseURLs[tool]
	return func() tea.Msg {
		defer cancel()
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil
			case <-timer.C:
			}
		}
		checkCtx, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()
		update, err := core.CheckUpdate(checkCtx, tool, bin, channel, source)
		return coreUpdateCheckedMsg{generation: generation, update: update, err: err}
	}
}

func (m Model) finishCoreCheck(msg coreUpdateCheckedMsg) (Model, tea.Cmd) {
	if !m.settings.CoreUpdateReminders || msg.generation != m.coreUpdates.generation {
		return m, nil
	}
	if msg.err != nil {
		m.setErr("core update check failed: %v", errorText(msg.err))
	} else if msg.update != m.coreUpdates.dismissed {
		m.coreUpdates.notice = msg.update
	} else {
		m.coreUpdates.notice = core.Update{}
	}
	delay, err := config.ParseCoreUpdateInterval(m.settings.CoreUpdateInterval)
	if err != nil {
		m.setError(err)
		return m, nil
	}
	cmd := m.scheduleCoreCheck(delay)
	return m, cmd
}

func (m Model) withCoreUpdateNotice(page string) string {
	update := m.coreUpdates.notice
	if update.Latest == "" || m.width < 4 {
		return page
	}
	body := strings.Join([]string{m.tr("%s update available", update.Tool), update.Installed + " → " + update.Latest, m.tr("F2 dismiss")}, "\n")
	box := warnStyle.Border(lipgloss.RoundedBorder()).Width(min(m.width-2, lipgloss.Width(body))).Render(body)
	lines := strings.Split(page, "\n")
	// Bubble Tea renders the last screenful when a form exceeds terminal height.
	if m.height > 0 && len(lines) > m.height {
		lines = lines[len(lines)-m.height:]
	}
	left := m.width - lipgloss.Width(box)
	for i, row := range strings.Split(box, "\n") {
		if m.height > 0 && i >= m.height {
			break
		}
		if i == len(lines) {
			lines = append(lines, "")
		}
		prefix := ansi.Truncate(lines[i], left, "")
		lines[i] = prefix + strings.Repeat(" ", max(0, left-lipgloss.Width(prefix))) + row
	}
	return strings.Join(lines, "\n")
}
