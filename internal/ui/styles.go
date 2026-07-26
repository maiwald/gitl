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

	fileHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("237"))

	hunkHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("81"))

	diffAddStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("70"))

	diffDeleteStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("167"))

	diffContextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("250"))

	binaryNoticeStyle = lipgloss.NewStyle().
				Italic(true).
				Foreground(lipgloss.Color("245"))
)
