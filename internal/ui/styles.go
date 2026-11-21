package ui

import (
	"sync"

	"github.com/charmbracelet/lipgloss"
)

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
			Foreground(errFg).
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
	diffAddEmphStyle = diffAddStyle.
				Bold(true).
				Background(lipgloss.Color("22"))

	diffDeleteEmphStyle = diffDeleteStyle.
				Bold(true).
				Background(lipgloss.Color("52"))

	// diffWhitespaceStyle marks trailing whitespace an added or removed
	// line carries (see withTrailingWhitespace) — git's own
	// color.diff.whitespace default, a red background, since whitespace has
	// no foreground to color.
	diffWhitespaceStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("1"))

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

// lineStyle is a style a whole line can be rendered in, bundled with the ANSI
// sequences lipgloss produces for it (see sgr) — plain, and with the cursor
// row's background merged in. renderLineWithBaseSpans renders a line carrying
// no span and no match through those instead of calling Render per line.
//
// The sequences are derived on first use rather than at package init, because
// lipgloss fixes its color profile the first time anything renders: deriving
// eagerly would beat an explicit SetColorProfile to it (the tests set one, see
// commititem_test.go's init) and bake in a profile nothing else is using.
// Deferring makes this exactly as late as the Render it replaces.
type lineStyle struct {
	style lipgloss.Style
	seqs  func() [2]sgr
}

func newLineStyle(s lipgloss.Style) lineStyle {
	return lineStyle{
		style: s,
		seqs: sync.OnceValue(func() [2]sgr {
			return [2]sgr{newSGR(s), newSGR(s.Background(cursorRowBg))}
		}),
	}
}

// sgr is the precomputed sequence for a plain row or the cursor's.
func (l lineStyle) sgr(cursor bool) sgr {
	seqs := l.seqs()
	if cursor {
		return seqs[1]
	}
	return seqs[0]
}

// The line styles a whole line is ever rendered in. plainLine is the colorless
// fallback for header/message and stats rows, which carry their color in spans
// rather than in the line's base style.
var (
	plainLine       = newLineStyle(lipgloss.NewStyle())
	diffAddLine     = newLineStyle(diffAddStyle)
	diffDeleteLine  = newLineStyle(diffDeleteStyle)
	diffContextLine = newLineStyle(diffContextStyle)
	fileHeaderLine  = newLineStyle(fileHeaderStyle)
)

// The styles a stretch within a line is rendered in: a baseSpan's, or a search
// match's. renderLineWithBaseSpans renders those through their sequences too,
// since a line carrying one is exactly the line n/N reflows on every press.
var (
	shaSpan            = newLineStyle(shaStyle)
	authorSpan         = newLineStyle(authorStyle)
	dateSpan           = newLineStyle(dateStyle)
	dimSpan            = newLineStyle(dimStyle)
	diffAddEmphSpan    = newLineStyle(diffAddEmphStyle)
	diffDeleteEmphSpan = newLineStyle(diffDeleteEmphStyle)
	diffWhitespaceSpan = newLineStyle(diffWhitespaceStyle)

	searchMatchSpan       = newLineStyle(searchMatchStyle)
	searchActiveMatchSpan = newLineStyle(searchActiveMatchStyle)
)

// matchLine picks the n/N-selected match's style, else the ordinary match's.
func matchLine(active bool) lineStyle {
	if active {
		return searchActiveMatchSpan
	}
	return searchMatchSpan
}

// matchStyle is matchLine's lipgloss style, for text rendered through Render.
func matchStyle(active bool) lipgloss.Style { return matchLine(active).style }

// matchSpan is matchLine's sequence. A match is always rendered in its own
// style's plain sequence, cursor row or not: its background is already the
// line's strongest signal.
func matchSpan(active bool) sgr { return matchLine(active).sgr(false) }
