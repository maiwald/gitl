package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Model is the root Bubble Tea model for the application.
type Model struct {
	width, height int
	quitting      bool
}

// NewModel constructs the initial root model.
func NewModel() Model {
	return Model{}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	return "gitl\n\npress q to quit\n"
}
