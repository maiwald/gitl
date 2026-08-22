package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// This file holds the primitives every view renders through — clipping and
// padding a row to width, compositing styled spans into one line, the cursor
// and search-match styling both views layer on top — plus the small
// arithmetic their cursors and match navigation share. Nothing here knows
// about commits, diffs, or either view model; anything that does belongs
// with the view it serves.

// clamp constrains v to the range [lo, hi].
func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

// halfPage is how far ctrl-u/ctrl-d move in a viewport of the given height:
// half a screen, floored at 1 so they still move something on a one- or
// two-row viewport. Shared with DetailModel, whose cursor pages the same way.
func halfPage(height int) int {
	return max(height/2, 1)
}

// matchPosition reports pos as a 1-based position within total, or (0, 0)
// when there are no matches — the shared shape behind both ListModel's and
// DetailModel's MatchPosition methods.
func matchPosition(pos, total int) (int, int) {
	if total == 0 {
		return 0, 0
	}
	return pos + 1, total
}

// stepMatchPos advances pos by delta within total matches, wrapping at both
// ends — the shared arithmetic behind both views' n/N. A negative pos means
// no match is active yet (a view that has matches but was never jumped into
// one, see SetMatches/computeMatches): n selects the first match from there
// and N the last, rather than letting -1 through the modulo, which would
// land N two short of the end.
func stepMatchPos(pos, delta, total int) int {
	if pos < 0 {
		if delta > 0 {
			return 0
		}
		return total - 1
	}
	return (pos + delta + total) % total
}

// renderClipped renders text in a single line, truncated (not wrapped) to
// width. It skips the width constraint entirely when width isn't positive,
// since lipgloss's Width/MaxWidth degenerate at zero.
func renderClipped(style lipgloss.Style, text string, width int) string {
	if width <= 0 {
		return style.Render(text)
	}
	return style.Width(width).MaxWidth(width).Render(text)
}

// renderClippedStyled truncates content that may already contain
// independently-Render()'d spans (ANSI resets embedded) to width via padRow
// — see padRow's doc comment for why Width+MaxWidth reflow can't be used
// here. cursor fills the padding with the cursor row's background instead
// of plain spaces, for a stats-table row the detail view's cursor is
// currently on (see statsview.go's renderStatLine); otherwise the fill is
// unstyled plain spaces.
func renderClippedStyled(content string, width int, cursor bool) string {
	if width <= 0 {
		return content
	}
	fill := lipgloss.NewStyle()
	if cursor {
		fill = fill.Background(cursorRowBg)
	}
	return padRow(content, width, fill)
}

// padRow truncates row to width and pads any remainder with trailing
// spaces in style. MaxWidth alone (no Width) truncates in place; combining
// Width with MaxWidth would run row — which already contains several
// independently-Rendered, reset-terminated spans — through lipgloss's
// paragraph reflow, miscounting cell positions across those resets and
// risking a hard-wrap mid-field, so padding to width is done by hand
// instead.
func padRow(row string, width int, style lipgloss.Style) string {
	row = style.MaxWidth(width).Render(row)
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += style.Render(strings.Repeat(" ", pad))
	}
	return row
}

// padLeft right-aligns s within width, left-padding with spaces. It's a
// no-op (never truncates) when s is already at or beyond width.
func padLeft(s string, width int) string {
	if n := width - len(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// truncate shortens s to at most n runes, appending an ellipsis if cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// blankLine renders one of the blank separator rows reflow splices between
// the header/stats/diff blocks: plain empty when isCursor is false (as
// before), or a full-width row filled with the cursor background when it's
// the line the cursor is currently on — these separators live outside
// renderHeaderBlock/renderDiffBlock/statLinesFor, so they need their own
// cursor check rather than inheriting one from whichever block they sit
// between.
func blankLine(width int, isCursor bool) string {
	if !isCursor {
		return ""
	}
	return padRow("", width, lipgloss.NewStyle().Background(cursorRowBg))
}

// cursorMerged merges the cursor row's background into base when isCursor
// is true, else returns base unchanged. Only safe for a row rendered with a
// single, uniform style (a file/hunk header, the binary notice) — content
// that composites independently-rendered spans (diff/header text lines)
// instead threads the cursor flag into renderLineWithBaseSpans, which
// merges the background per-span before any Render() call fires (see
// CLAUDE.md's "style plain-text spans before any .Render() call" rule).
func cursorMerged(base lipgloss.Style, isCursor bool) lipgloss.Style {
	if isCursor {
		return base.Background(cursorRowBg)
	}
	return base
}

// baseSpan is a byte-offset [start,end) run of a header line rendered with
// a specific non-match base style (e.g. the author's name colored
// authorStyle), computed once per logical line in headerLinesFor alongside
// its plain text so it composes with search-match highlighting the same
// way a single uniform base already does for diff lines.
type baseSpan struct {
	start, end int
	style      lipgloss.Style
}

// lineMatch is a search match's byte-offset position within one rendered
// line's text — a diff line (prefix + DiffLine.Text), a file header, or a
// header/message line — plus whether it's the active (n/N-selected) match.
type lineMatch struct {
	start, end int
	active     bool
}

// renderLineWithBaseSpans composites per-column base coloring (spans) with
// search-match spans (matches) into one already-styled string: walks the
// boundary points from both span lists, and for each resulting piece
// renders with whichever match style covers it, else whichever baseSpan
// covers it, else base — a match always wins over a base span on overlap
// (e.g. a search hit inside the author's name still shows the search
// highlight, not the name's blue). base is the fallback for text outside
// every span and match: colorless for header/message lines (spans already
// cover every colored substring there), or a diff line's op color
// (diffAddStyle/diffDeleteStyle/diffContextStyle) when called from
// renderDiffLine with no spans at all.
//
// cursor merges the cursor row's background into whatever style would
// otherwise apply at each position, except inside a match: a match's own
// background is already the line's strongest signal, and layering the
// (much subtler) cursor background on top of it would only mute it for no
// benefit — so the cursor background applies everywhere on the line
// *except* a matched span.
func renderLineWithBaseSpans(text string, spans []baseSpan, matches []lineMatch, base lipgloss.Style, cursor bool) string {
	bounds := map[int]bool{0: true, len(text): true}
	for _, s := range spans {
		bounds[s.start] = true
		bounds[s.end] = true
	}
	for _, m := range matches {
		bounds[m.start] = true
		bounds[m.end] = true
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
		if cursor {
			return base.Background(cursorRowBg)
		}
		return base
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

// highlightSpans splits text at case-insensitive occurrences of term,
// rendering unmatched runs with base and matched runs with match, then
// concatenating the already-rendered pieces — the same nested-style pattern
// renderLineWithBaseSpans uses (style a plain-text span, then render it),
// reduced to the one-term case a list row needs. Returns
// base.Render(text) unchanged when term is empty.
func highlightSpans(text, term string, base, match lipgloss.Style) string {
	if term == "" {
		return base.Render(text)
	}
	var b strings.Builder
	pos := 0
	for _, occ := range findOccurrences(text, term) {
		b.WriteString(base.Render(text[pos:occ[0]]))
		b.WriteString(match.Render(text[occ[0]:occ[1]]))
		pos = occ[1]
	}
	b.WriteString(base.Render(text[pos:]))
	return b.String()
}

// findOccurrences returns the byte [start, end) offset of every
// case-insensitive occurrence of term within text.
func findOccurrences(text, term string) [][2]int {
	lowerText, lowerTerm := strings.ToLower(text), strings.ToLower(term)
	var occs [][2]int
	pos := 0
	for {
		idx := strings.Index(lowerText[pos:], lowerTerm)
		if idx < 0 {
			break
		}
		idx += pos
		occs = append(occs, [2]int{idx, idx + len(term)})
		pos = idx + len(term)
	}
	return occs
}
