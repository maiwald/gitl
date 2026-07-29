package ui

import "github.com/charmbracelet/lipgloss"

// cursorRowBg is the background applied to the row under the cursor. It is
// merged into each field's own style (see renderCommitRow) rather than used
// as a single flat replacement style, so a selected row stays tinted without
// discarding the sha/date/author/ref foreground colors that distinguish
// those fields on every other row.
const cursorRowBg = lipgloss.Color("238")

// errFg is the foreground used for error text, shared by errStyle (the
// full-screen fatal error) and statusBarErrStyle (a non-fatal error shown
// in the status bar), so both read as the same kind of message. Uses the
// basic ANSI red (see the dateStyle/shaStyle/authorStyle comment below)
// rather than a 256-color value, so it renders as the terminal's own
// themed red instead of a fixed hue.
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

	// refStyle reuses the same ANSI red as errFg, distinguished by weight
	// (bold) rather than a different hue — the same way a bold vs. plain
	// red both being "red" reads fine in a 16-color palette (mirroring how
	// git tools like git log --decorate get by with a handful of basic
	// colors, none unique to just one kind of decoration).
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

	diffContextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("250"))

	binaryNoticeStyle = lipgloss.NewStyle().
				Italic(true).
				Foreground(lipgloss.Color("245"))

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	// statusBarErrStyle is a whole-bar style swap (background plus errFg),
	// used for a non-fatal error, rather than nesting errStyle inside the
	// bar's normal content: a nested Render ends with its own ANSI reset,
	// which would cancel the bar's background for everything rendered
	// after it — the same reset hazard rowFieldStyle's doc comment
	// describes for row fields, resolved the same way (swap the whole
	// base style rather than layering).
	statusBarErrStyle = statusBarStyle.Foreground(errFg)

	// entryTitleStyle is the "Staged changes"/"Working tree" title in a
	// synthetic leading row (worktreerow.go), bold so it stands out the way
	// a commit's subject doesn't need to (a real row already has color-coded
	// sha/date/author fields drawing the eye; a synthetic row has none of
	// those, so the title carries that job instead).
	entryTitleStyle = lipgloss.NewStyle().Bold(true)

	// dimStyle uses the terminal-native Faint attribute rather than a fixed
	// color (see the dateStyle/shaStyle/authorStyle comment above) so a
	// dimmed row still respects the user's own palette/contrast instead of
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
