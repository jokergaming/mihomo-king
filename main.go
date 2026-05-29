package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
	"mihomo-king/internal/tui"
)

func main() {
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mihomo-king: load settings:", err)
		os.Exit(1)
	}
	p := tea.NewProgram(tui.New(settings), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "mihomo-king:", err)
		os.Exit(1)
	}
}
