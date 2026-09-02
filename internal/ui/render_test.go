package ui

import (
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// TestRenderLineWithBaseSpansMatchesRenderPerSegment pins the other
// substitution renderLineWithBaseSpans makes: a line that does carry a span or
// a match still walks boundaries, but only the covered stretches go through
// lipgloss — the base-styled ones in between use the precomputed sequence. The
// reference is what the walk produced when every segment was a Render, so a
// lipgloss upgrade that made the two disagree (over tab expansion in a segment
// that doesn't start at a tab stop, say) fails here.
func TestRenderLineWithBaseSpansMatchesRenderPerSegment(t *testing.T) {
	const text = "func x() {\tbody = value // trailing"
	cases := []struct {
		name    string
		spans   []baseSpan
		matches []lineMatch
	}{
		{name: "span in the middle", spans: []baseSpan{{start: 11, end: 15, style: diffAddEmphStyle}}},
		{name: "span at the start", spans: []baseSpan{{start: 0, end: 4, style: diffDeleteEmphStyle}}},
		{name: "span to the end", spans: []baseSpan{{start: 20, end: len(text), style: diffAddEmphStyle}}},
		{name: "match", matches: []lineMatch{{start: 11, end: 15}}},
		{name: "active match", matches: []lineMatch{{start: 5, end: 9, active: true}}},
	}

	for _, tc := range cases {
		for _, base := range allLineStyles {
			for _, cursor := range []bool{false, true} {
				want := renderSegmentsWithRender(text, tc.spans, tc.matches, base, cursor)
				got := renderLineWithBaseSpans(text, tc.spans, tc.matches, base, cursor)
				if got != want {
					t.Errorf("%s (cursor=%v): renderLineWithBaseSpans = %q, want %q (Render per segment)", tc.name, cursor, got, want)
				}
			}
		}
	}
}

// renderSegmentsWithRender is renderLineWithBaseSpans' boundary walk with
// every segment rendered through lipgloss, the reference the real one is
// allowed to shortcut.
func renderSegmentsWithRender(text string, spans []baseSpan, matches []lineMatch, base lineStyle, cursor bool) string {
	bounds := map[int]bool{0: true, len(text): true}
	for _, s := range spans {
		bounds[s.start], bounds[s.end] = true, true
	}
	for _, m := range matches {
		bounds[m.start], bounds[m.end] = true, true
	}
	points := make([]int, 0, len(bounds))
	for p := range bounds {
		points = append(points, p)
	}
	sort.Ints(points)

	styleAt := func(pos int) lipgloss.Style {
		for _, m := range matches {
			if pos >= m.start && pos < m.end {
				return matchStyle(m.active)
			}
		}
		for _, s := range spans {
			if pos >= s.start && pos < s.end {
				if cursor {
					return s.style.Background(cursorRowBg)
				}
				return s.style
			}
		}
		return cursorMerged(base.style, cursor)
	}

	var b strings.Builder
	for i := 0; i < len(points)-1; i++ {
		start, end := points[i], points[i+1]
		if start >= end {
			continue
		}
		b.WriteString(styleAt(start).Render(text[start:end]))
	}
	return b.String()
}

// TestDisplayWidthMatchesAnsiStringWidth pins the fast path displayWidth takes
// for printable ASCII, where it counts bytes instead of running the escape
// parser. Every shape a row can take has to agree with ansi.StringWidth
// exactly: it decides how much padding a row gets, so a disagreement would
// misalign every row of the kind that hit it.
func TestDisplayWidthMatchesAnsiStringWidth(t *testing.T) {
	cases := []string{
		"",
		"plain ascii line",
		"-\tremoved",
		"   ",
		"punctuation !\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~",
		"unicode ünïcödé",
		"日本語のテキスト",
		"emoji 👍🏽 and ✓",
		"combining é mark",
		"\x1b[32mstyled\x1b[0m",
		"\x1b[48;5;238mfilled \x1b[0m\x1b[1;32mspan\x1b[0m",
		"\x7f",
		"\x00null",
		"tab\there",
	}
	for _, s := range cases {
		if got, want := displayWidth(s), ansi.StringWidth(s); got != want {
			t.Errorf("displayWidth(%q) = %d, want %d (ansi.StringWidth)", s, got, want)
		}
	}
}
