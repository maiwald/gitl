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

// TestRenderLineWithBaseSpansFastPathMatchesGeneralPath pins the shortcut
// renderLineWithBaseSpans takes for a line with no base spans and no search
// matches. The reference is the general boundary walk driven by a single span
// covering the whole line in the base style: every position then resolves to
// the same style the fast path applies, so the two must agree character for
// character, cursor row or not.
func TestRenderLineWithBaseSpansFastPathMatchesGeneralPath(t *testing.T) {
	texts := []string{"", "plain context line", "\tindented", "unicode ünïcödé ✓ 日本語", "   "}
	for _, base := range allLineStyles {
		for _, text := range texts {
			for _, cursor := range []bool{false, true} {
				whole := []baseSpan{{start: 0, end: len(text), style: base.style}}
				want := renderLineWithBaseSpans(text, whole, nil, base, cursor)
				got := renderLineWithBaseSpans(text, nil, nil, base, cursor)
				if got != want {
					t.Errorf("renderLineWithBaseSpans(%q, nil, nil, base, %v) = %q, want %q (the general walk's output)", text, cursor, got, want)
				}
			}
		}
	}
}

// allLineStyles is every lineStyle the app renders a whole line in — the set
// TestLineStyleSGRMatchesLipgloss and the fast-path test above must cover.
var allLineStyles = []lineStyle{plainLine, diffAddLine, diffDeleteLine, diffContextLine, fileHeaderLine}

// TestLineStyleSGRMatchesLipgloss pins the substitution lineStyle.sgr makes
// for lipgloss.Style.Render: for a style setting none of width/padding/border/
// margin/transform, Render only wraps its argument in a fixed escape pair, so
// concatenating that pair around the text must be byte-for-byte what Render
// produces. A lipgloss upgrade that changed the sequence — or a style here
// growing a property that makes Render do more than wrap — fails here instead
// of silently changing every diff line in the app.
//
// The texts are tab-free deliberately: wrapping is all sgr claims to do, and
// Render additionally expands tabs. renderLineWithBaseSpans' fast path does
// that expansion itself, which is what
// TestRenderLineWithBaseSpansFastPathMatchesGeneralPath covers.
func TestLineStyleSGRMatchesLipgloss(t *testing.T) {
	texts := []string{"x", "plain context line", "unicode ünïcödé ✓ 日本語", "   ", "line with \x1b not-an-escape"}
	for i, ls := range allLineStyles {
		for _, text := range texts {
			for _, cursor := range []bool{false, true} {
				want := cursorMerged(ls.style, cursor).Render(text)
				if got := ls.sgr(cursor).render(text); got != want {
					t.Errorf("lineStyle %d: sgr(%v).render(%q) = %q, want %q (what Render produces)", i, cursor, text, got, want)
				}
			}
		}
	}
}
