package ui

import "github.com/charmbracelet/lipgloss"

var (
	normalRowStyle = lipgloss.NewStyle()

	selectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230"))

	dateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("34"))

	shaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("220"))

	authorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

	refStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
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

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	searchMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("226")).
				Foreground(lipgloss.Color("0"))

	searchActiveMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("208")).
				Foreground(lipgloss.Color("0")).
				Bold(true)
)
