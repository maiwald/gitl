package ui

import "github.com/charmbracelet/lipgloss"

// cursorRowBg is the background applied to the row under the cursor. It is
// merged into each field's own style rather than replacing it, so a selected
// row stays tinted without losing the foreground colors that distinguish its
// fields.
const cursorRowBg = lipgloss.Color("238")

// errFg is the foreground for error text, shared by errStyle (a fatal error)
// and statusBarErrStyle (a non-fatal one). Basic ANSI red rather than a
// 256-color value, for the reason below, so it renders as the terminal's own
// themed red.
const errFg = lipgloss.Color("1")

var (
	normalRowStyle = lipgloss.NewStyle()

	selectedRowStyle = lipgloss.NewStyle().
				Background(cursorRowBg)

	// dateStyle, shaStyle, authorStyle, refStyle, diffAddStyle, and
	// hunkHeaderStyle use basic ANSI color codes (0-15) rather than
	// 256-color/hex values: termenv passes ANSI codes through unconverted
	// regardless of color profile, so these render via the terminal's own
	// palette (e.g. Solarized) instead of a fixed hue.
	dateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2"))

	shaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("3"))

	authorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("4"))

	// refStyle reuses errFg's red, distinguished by weight (bold) rather
	// than a new hue, to stay within the 16-color ANSI palette.
	refStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true)

	errStyle = lipgloss.NewStyle().
			Foreground(errFg)

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

	// diffAddEmphStyle and diffDeleteEmphStyle mark the intraline span that
	// changed within a paired add/delete line (see intralineSpans): the same
	// hue as diffAddStyle/diffDeleteStyle plus a tinted background and bold
	// weight, so the changed substring stands out against the rest of the
	// line rather than recoloring it.
	diffAddEmphStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("2")).
				Background(lipgloss.Color("22"))

	diffDeleteEmphStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("167")).
				Background(lipgloss.Color("52"))

	diffContextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("250"))

	binaryNoticeStyle = lipgloss.NewStyle().
				Italic(true).
				Foreground(lipgloss.Color("245"))

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	// statusBarErrStyle swaps the whole bar's style for a non-fatal error
	// rather than nesting errStyle inside the bar's content — the same reset
	// hazard rowFieldStyle describes, resolved the same way.
	statusBarErrStyle = statusBarStyle.Foreground(errFg)

	// entryTitleStyle is the "Staged changes"/"Working tree" title in a
	// synthetic leading row, bold so it draws the eye the way a real row's
	// color-coded fields do.
	entryTitleStyle = lipgloss.NewStyle().Bold(true)

	// dimStyle uses the terminal-native Faint attribute rather than a fixed
	// color, so a dimmed row respects the user's own palette instead of
	// fighting it with a hardcoded grey.
	dimStyle = lipgloss.NewStyle().Faint(true)

	searchMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("226")).
				Foreground(lipgloss.Color("0"))

	searchActiveMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("208")).
				Foreground(lipgloss.Color("0")).
				Bold(true)
)

// matchStyle picks searchActiveMatchStyle for the n/N-selected match, else
// searchMatchStyle.
func matchStyle(active bool) lipgloss.Style {
	if active {
		return searchActiveMatchStyle
	}
	return searchMatchStyle
}
