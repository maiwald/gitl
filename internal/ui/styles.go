package ui

import "github.com/charmbracelet/lipgloss"

var (
	normalRowStyle = lipgloss.NewStyle()

	selectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230"))

	// dateStyle, shaStyle, authorStyle, diffAddStyle, and hunkHeaderStyle
	// use basic ANSI color codes (0-15) rather than 256-color/hex values:
	// termenv passes ANSI codes through unconverted regardless of color
	// profile, so these render via the terminal's own palette (e.g.
	// Solarized) instead of a fixed hue.
	dateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2"))

	shaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("3"))

	authorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("4"))

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
			Foreground(lipgloss.Color("4"))

	diffAddStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2"))

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
