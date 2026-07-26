package ui

import "github.com/charmbracelet/lipgloss"

var (
	normalRowStyle = lipgloss.NewStyle()

	selectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230"))

	dateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	authorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("109"))

	refStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("212")).
			Bold(true)

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))
)
