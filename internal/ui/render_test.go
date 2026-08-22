package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// fastPathLines are the shapes a line reaches padRow/wrapRanges in: plain
// text, text carrying its own trailing spaces, several independently
// Render()'d spans concatenated (the usual case — see renderLineWithBaseSpans),
// wide/combining runes, and the empty line.
var fastPathLines = []string{
	"",
	"plain context line",
	"trailing spaces   ",
	"   leading spaces",
	diffAddStyle.Render("+added") + diffContextStyle.Render(" tail"),
	searchMatchStyle.Render("hit") + " rest " + shaStyle.Render("0f39ede"),
	"unicode ünïcödé ✓ 日本語 text",
}

// TestPadRowPlainFillMatchesLipgloss pins the assumption padRow's fast path
// rests on: for a row already within width, a fill style with nothing set
// contributes nothing, so both the truncating Render and the Render of the
// padding spaces can be skipped. A lipgloss upgrade that made either of
// those do something would silently change every non-cursor row in the app,
// so compare the fast path against the rendered one directly.
func TestPadRowPlainFillMatchesLipgloss(t *testing.T) {
	for _, row := range fastPathLines {
		for _, width := range []int{0, 1, 12, 40, 200} {
			want := normalRowStyle.MaxWidth(width).Render(row)
			if pad := width - lipgloss.Width(want); pad > 0 {
				want += normalRowStyle.Render(strings.Repeat(" ", pad))
			}
			if got := padRow(row, width, false); got != want {
				t.Errorf("padRow(%q, %d, false) = %q, want %q (what rendering the fill produces)", row, width, got, want)
			}
		}
	}
}

// TestWrapRangesFittingLineMatchesLipgloss is the same guard for wrapRanges'
// own fast path: a line that fits must produce exactly the single range
// lipgloss's wrapper would have yielded for it.
func TestWrapRangesFittingLineMatchesLipgloss(t *testing.T) {
	for _, line := range fastPathLines {
		for _, width := range []int{12, 40, 200} {
			if lipgloss.Width(line) > width {
				continue
			}
			rows := strings.Split(lipgloss.NewStyle().Width(width).Render(line), "\n")
			if len(rows) != 1 {
				t.Fatalf("lipgloss wrapped %q at width %d into %d rows, expected a fitting line to stay on one", line, width, len(rows))
			}
			want := [][2]int{{0, len(strings.TrimRight(rows[0], " "))}}
			got := wrapRanges(line, width)
			if len(got) != 1 || got[0] != want[0] {
				t.Errorf("wrapRanges(%q, %d) = %v, want %v (what the wrapper produces)", line, width, got, want)
			}
		}
	}
}
