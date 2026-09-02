package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// lineRenderer is one logical line of a view's content: how to render it, and
// the plain text it renders from.
//
// render produces the line plain or with the cursor row's background merged
// in. It returns a slice because a word-wrapped line occupies several visual
// rows — and both renderings always occupy the same number of them (see
// wrapRanges), which is what lets a cursor move re-render one line in place
// instead of everything around it.
//
// Describing lines as renderers rather than finished rows is what makes the
// cursor a property applied at render time: a block describes its lines once,
// and renderRows decides afterwards where each lands and which one the cursor
// is on.
//
// text is the line as it would read with no terminal involved — tabs intact,
// unwrapped, unpadded — which is what a selection copies. It has to be kept
// here because the rendered rows can't be turned back into it: by then the
// tabs are expanded, the line may have been split across rows, and the fill
// to the right of the text is indistinguishable from trailing spaces of its
// own.
type lineRenderer struct {
	text   string
	render func(cursor bool) []string
}

// clippedLine is the lineRenderer for a line rendered in one uniform style
// and truncated (never wrapped) to width — a hunk header, a binary-file
// notice, the stats block's heading.
func clippedLine(style lipgloss.Style, text string, width int) lineRenderer {
	return lineRenderer{
		text: text,
		render: func(cursor bool) []string {
			return []string{renderClipped(cursorMerged(style, cursor), text, width)}
		},
	}
}

// blankRow is the lineRenderer for a blank separator row between two content
// blocks (see blankLine).
func blankRow(width int) lineRenderer {
	return lineRenderer{
		render: func(cursor bool) []string {
			return []string{blankLine(width, cursor)}
		},
	}
}

// renderRows renders every logical line in document order and then, once it
// knows where each one landed, re-renders every line the row range [from, to]
// touches with the cursor row's background. Returns the flattened rows and,
// parallel to lines, the row each logical line starts at.
//
// The range is a range because the highlight is not always one line: it's the
// cursor's own line normally (from == to), and the whole of a linewise
// selection while one is active. Either way it's stated in rows and resolved
// to whole logical lines here, so a wrapped line highlights as one block.
//
// A range outside the content selects nothing, so callers don't have to bound
// it themselves.
func renderRows(lines []lineRenderer, from, to int) (rows []string, lineStart []int) {
	lineStart = make([]int, len(lines))
	for i, l := range lines {
		lineStart[i] = len(rows)
		rows = append(rows, l.render(false)...)
	}
	first, last := logicalRangeAt(lineStart, len(rows), from, to)
	for i := first; i <= last; i++ {
		// Same row count either way, so the cursor rendering overwrites the
		// plain one in place and nothing below it shifts.
		copy(rows[lineStart[i]:], lines[i].render(true))
	}
	return rows, lineStart
}

// logicalRangeAt resolves an inclusive range of rows to the inclusive range of
// logical lines covering it, clipped to the rowCount rows that exist. Reports
// an empty range (first > last) when nothing of it is in the content, so
// callers can loop over the result unconditionally.
func logicalRangeAt(lineStart []int, rowCount, from, to int) (first, last int) {
	if len(lineStart) == 0 || from > to || to < 0 || from >= rowCount {
		return 0, -1
	}
	return logicalAt(lineStart, max(from, 0)), logicalAt(lineStart, min(to, rowCount-1))
}

// logicalAt reports which logical line the flattened row pos belongs to, given
// each logical line's starting row, or -1 when pos is above the content
// entirely. A row past the end reports the final line, which is what callers
// asking about trailing padding want.
func logicalAt(lineStart []int, pos int) int {
	if len(lineStart) == 0 || pos < lineStart[0] {
		return -1
	}
	return sort.Search(len(lineStart), func(i int) bool { return lineStart[i] > pos }) - 1
}

// halfPage is how far ctrl-u/ctrl-d move in a viewport of the given height:
// half a screen, floored at 1 so they still move something on a one- or
// two-row viewport.
func halfPage(height int) int {
	return max(height/2, 1)
}

// matchPosition reports pos as a 1-based position within total, or (0, 0) when
// there are no matches.
func matchPosition(pos, total int) (int, int) {
	if total == 0 {
		return 0, 0
	}
	return pos + 1, total
}

// stepMatchPos advances pos by delta within total matches, wrapping at both
// ends. A negative pos means no match is active yet: n selects the first match
// from there and N the last, rather than letting -1 through the modulo, which
// would land N two short of the end.
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
//
// Inline is what makes "single line" true: Width on its own word-wraps, so
// text longer than width would come back as several display lines inside what
// every caller counts as one entry in the flattened line space, shifting every
// row below it out of step with the line indices the cursor, FileRanges and
// scroll positions use. MaxWidth still truncates, and Width still pads the row
// out (Inline only suppresses padding set via Padding, not the alignment
// fill), so a cursor-merged style still colors the full row.
func renderClipped(style lipgloss.Style, text string, width int) string {
	if width <= 0 {
		return style.Render(text)
	}
	return style.Inline(true).Width(width).MaxWidth(width).Render(text)
}

// renderClippedStyled truncates content that may already contain
// independently-Render()'d spans (ANSI resets embedded) to width via padRow —
// see padRow for why Width+MaxWidth can't be used here. cursor fills the
// padding with the cursor row's background instead of plain spaces.
func renderClippedStyled(content string, width int, cursor bool) string {
	if width <= 0 {
		return content
	}
	return padRow(content, width, cursor)
}

// rowFillStyle is the style a row's background is painted with: plain
// normally, or selectedRowStyle for the row under the cursor. It's padRow's
// fill, and also what a row's literal gap/separator text is rendered in (see
// rowGap in commititem.go), so a selected row's highlight has no holes in it.
func rowFillStyle(cursor bool) lipgloss.Style {
	if cursor {
		return selectedRowStyle
	}
	return normalRowStyle
}

// padRow truncates row to width and pads any remainder with trailing spaces,
// in the cursor row's background when cursor is set. MaxWidth alone truncates
// in place; adding Width would run row — which already contains several
// independently-Rendered, reset-terminated spans — through lipgloss's
// paragraph reflow, miscounting cell positions across those resets and risking
// a hard-wrap mid-field. Hence the padding by hand.
//
// Measured with ansi.StringWidth rather than lipgloss.Width, which is that
// same call taken over the string split on "\n": every row reaching here is
// one display line, which is the invariant renderClipped's Inline(true)
// exists to hold, so the split only ever produces the one piece.
func padRow(row string, width int, cursor bool) string {
	style := rowFillStyle(cursor)
	// A plain fill contributes nothing but the truncation itself, so a row
	// already within width comes back from that Render byte-for-byte
	// unchanged — skip it, and pad with plain spaces for the same reason.
	// The cursor's fill does carry a style, so its row is always rendered.
	// (Hot path: a reflow pads every line of the commit.)
	plain := !cursor
	w := ansi.StringWidth(row)
	if !plain || w > width {
		row = style.MaxWidth(width).Render(row)
		w = ansi.StringWidth(row)
	}
	if pad := width - w; pad > 0 {
		spaces := strings.Repeat(" ", pad)
		if plain {
			row += spaces
		} else {
			row += style.Render(spaces)
		}
	}
	return row
}

// lipglossTabWidth mirrors lipgloss's own default tab handling (Render
// replaces each tab with this many spaces before wrapping). wrapRanges applies
// the same expansion up front so its byte offsets describe the text lipgloss
// would actually lay out.
const lipglossTabWidth = 4

// tabSpaces is the run of spaces a single tab expands to.
const tabSpaces = "    "

// expandTabs replaces tabs with lipglossTabWidth spaces.
//
// It's the half of expandTabsMapped that most lines need: relocating offsets
// onto the expanded text costs an int per byte of the line, and a reflow would
// otherwise pay that on every tab-indented line of the commit — which, for
// source code, is nearly all of them, while carrying neither an intraline span
// nor a search match to relocate.
func expandTabs(text string) string {
	return strings.ReplaceAll(text, "\t", tabSpaces)
}

// expandTabsMapped expands tabs like expandTabs and additionally returns a
// mapping from each byte offset in text to its offset in the result (nil for
// the identity mapping, when there was nothing to expand), so span and match
// offsets computed against the original text can follow it.
func expandTabsMapped(text string) (string, offsetMap) {
	tabs := strings.Count(text, "\t")
	if tabs == 0 {
		return text, nil
	}
	var b strings.Builder
	b.Grow(len(text) + tabs*(lipglossTabWidth-1))
	offsets := make([]int, len(text)+1)
	prev := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '\t' {
			continue
		}
		b.WriteString(text[prev:i])
		for j := prev; j <= i; j++ {
			offsets[j] = b.Len() - (i - j)
		}
		b.WriteString(tabSpaces)
		prev = i + 1
	}
	b.WriteString(text[prev:])
	for j := prev; j <= len(text); j++ {
		offsets[j] = b.Len() - (len(text) - j)
	}
	return b.String(), func(i int) int { return offsets[i] }
}

// wrapRanges word-wraps plain (ANSI-free) text to width the way lipgloss
// would, returning each resulting visual row as a byte range into text.
//
// The wrap has to be computed on plain text, never on an already-styled
// string: lipgloss's wrapper only treats a space as a word boundary while the
// style in effect is "blank", and a background color isn't — so the same line
// carrying the cursor row's background wraps as one unbreakable word, onto a
// different number of rows than the identical line without it. A logical
// line's row count defines the flattened line space the cursor, FileRanges and
// match positions index into, so that difference desyncs the whole view.
//
// Ranges exclude the spaces the wrapper consumed at a break (and the
// padding it adds to short rows) — every caller re-pads to width itself.
func wrapRanges(text string, width int) [][2]int {
	if width <= 0 || text == "" {
		return [][2]int{{0, len(text)}}
	}
	// A line that already fits comes back from the wrapper as itself, padded
	// out to width — so measure first and skip the render, the expensive
	// half of laying a line out, which a reflow pays once per line of the
	// entire commit. Trimming the trailing spaces is what the general path
	// below would do to that single row anyway.
	if !strings.Contains(text, "\n") && ansi.StringWidth(text) <= width {
		return [][2]int{{0, len(strings.TrimRight(text, " "))}}
	}
	rows := strings.Split(lipgloss.NewStyle().Width(width).Render(text), "\n")
	ranges := make([][2]int, 0, len(rows))
	pos := 0
	for _, row := range rows {
		row = strings.TrimRight(row, " ")
		// The wrapper drops the run of spaces it broke at, so skip whatever
		// separated this row from the previous one.
		for pos < len(text) && text[pos] == ' ' && !strings.HasPrefix(text[pos:], row) {
			pos++
		}
		end := min(pos+len(row), len(text))
		ranges = append(ranges, [2]int{pos, end})
		pos = end
	}
	return ranges
}

// wrapStyledRows word-wraps text to width and styles each visual row
// separately: spans and matches clipped and rebased to that row, the cursor
// background merged in per span (renderLineWithBaseSpans), then padded to full
// width (padRow).
//
// Wrapping first and styling second is what keeps a cursor row's wrapped
// height identical to the same line's without it — see wrapRanges — and means
// no caller has to strip lipgloss's plain padding back off before re-padding.
func wrapStyledRows(text string, spans []baseSpan, matches []lineMatch, base lineStyle, width int, cursor bool) []string {
	// Expanding with an offset mapping costs an int per byte of the line, so
	// it's only done when there is something to relocate onto the expanded
	// text — see expandTabs.
	if len(spans) == 0 && len(matches) == 0 {
		text = expandTabs(text)
	} else {
		var expanded offsetMap
		text, expanded = expandTabsMapped(text)
		spans = shiftSpans(spans, expanded)
		matches = shiftMatches(matches, expanded)
	}

	ranges := wrapRanges(text, width)
	rows := make([]string, 0, len(ranges))
	for _, r := range ranges {
		rendered := renderLineWithBaseSpans(text[r[0]:r[1]], clipSpans(spans, r), clipMatches(matches, r), base, cursor)
		if width <= 0 {
			rows = append(rows, rendered)
			continue
		}
		rows = append(rows, padRow(rendered, width, cursor))
	}
	return rows
}

// offsetMap relocates a byte offset in one version of a line's plain text onto
// another: past a rendering prefix the offsets were computed without
// (shiftBy), or through tab expansion (expandTabsMapped). A nil offsetMap is the
// identity, which shiftSpans/shiftMatches take as "nothing to do".
type offsetMap func(int) int

// shiftBy is the offsetMap for text about to be rendered behind an n-byte
// prefix — a diff line's +/-/space marker, or a file header's status glyph —
// which spans and matches are always computed without.
func shiftBy(n int) offsetMap {
	return func(i int) int { return i + n }
}

// shiftSpans and shiftMatches move spans/matches through at, onto whatever
// version of the line's text is actually being rendered.
func shiftSpans(spans []baseSpan, at offsetMap) []baseSpan {
	if at == nil || len(spans) == 0 {
		return spans
	}
	shifted := make([]baseSpan, len(spans))
	for i, s := range spans {
		shifted[i] = baseSpan{start: at(s.start), end: at(s.end), style: s.style}
	}
	return shifted
}

func shiftMatches(matches []lineMatch, at offsetMap) []lineMatch {
	if at == nil || len(matches) == 0 {
		return matches
	}
	shifted := make([]lineMatch, len(matches))
	for i, m := range matches {
		shifted[i] = lineMatch{start: at(m.start), end: at(m.end), active: m.active}
	}
	return shifted
}

// clipSpans and clipMatches narrow spans/matches to the byte range r covers
// and rebase them onto it, so a span or search hit that straddles a wrap
// boundary keeps styling both of the rows it lands on. Spans/matches outside
// r are dropped.
func clipSpans(spans []baseSpan, r [2]int) []baseSpan {
	var clipped []baseSpan
	for _, s := range spans {
		if start, end, ok := clipRange(s.start, s.end, r); ok {
			clipped = append(clipped, baseSpan{start: start, end: end, style: s.style})
		}
	}
	return clipped
}

func clipMatches(matches []lineMatch, r [2]int) []lineMatch {
	var clipped []lineMatch
	for _, m := range matches {
		if start, end, ok := clipRange(m.start, m.end, r); ok {
			clipped = append(clipped, lineMatch{start: start, end: end, active: m.active})
		}
	}
	return clipped
}

// clipRange intersects [start,end) with r and rebases the result onto r's
// own start, reporting false when the two don't overlap at all.
func clipRange(start, end int, r [2]int) (int, int, bool) {
	start, end = max(start, r[0]), min(end, r[1])
	if start >= end {
		return 0, 0, false
	}
	return start - r[0], end - r[0], true
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

// blankLine renders one of the blank separator rows reflow splices between the
// header/stats/diff blocks: plain empty, or a full-width row filled with the
// cursor background when it's the line the cursor is on. The separators live
// outside every block, so they carry their own cursor check.
func blankLine(width int, isCursor bool) string {
	if !isCursor {
		return ""
	}
	return padRow("", width, true)
}

// sgr is the ANSI prefix and suffix lipgloss puts around text rendered in one
// style. Rendering a whole line in a single uniform style is then a
// concatenation rather than a fresh trip through Render, which re-reads every
// style property and runs the padding/alignment/border/margin pipeline to
// rediscover a sequence that never changes — most of the cost of a reflow,
// paid once per line of the commit.
//
// It is only equivalent for a style setting none of width/padding/border/
// margin/transform, and for text with no tab or newline: exactly what a
// lineStyle carries and what renderLineWithBaseSpans is handed, since
// wrapStyledRows has already expanded tabs and wrapped. Each lineStyle is
// pinned against lipgloss's own output — see TestLineStyleSGRMatchesLipgloss.
type sgr struct{ prefix, suffix string }

// newSGR derives the pair from lipgloss itself, by rendering a sentinel and
// splitting around it, rather than assembling escape codes here — so a
// lipgloss upgrade that changes the sequence changes this with it instead of
// silently diverging from every other Render in the app.
func newSGR(s lipgloss.Style) sgr {
	const sentinel = "\x00"
	out := s.Render(sentinel)
	i := strings.Index(out, sentinel)
	return sgr{prefix: out[:i], suffix: out[i+len(sentinel):]}
}

func (g sgr) render(text string) string { return g.prefix + text + g.suffix }

// cursorMerged merges the cursor row's background into base when isCursor is
// true, else returns base unchanged. Only safe for a row rendered in a single
// uniform style; content that composites independently-rendered spans threads
// the cursor flag into renderLineWithBaseSpans instead, which merges the
// background per-span before any Render() call fires.
func cursorMerged(base lipgloss.Style, isCursor bool) lipgloss.Style {
	if isCursor {
		return base.Background(cursorRowBg)
	}
	return base
}

// baseSpan is a byte-offset [start,end) run of a header line rendered in a
// specific non-match base style (e.g. the author's name in authorStyle),
// computed alongside the line's plain text in headerLinesFor so it composes
// with search-match highlighting.
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
// search-match spans (matches) into one already-styled string: it walks the
// boundary points of both span lists and renders each resulting piece with
// whichever match style covers it, else whichever baseSpan covers it, else
// base. A match always wins over a base span on overlap. base is the fallback
// for text outside every span and match: colorless for header/message lines,
// or a diff line's op color when called from renderDiffLine with no spans.
//
// cursor merges the cursor row's background into whatever style would
// otherwise apply, except inside a match, whose own background is already the
// line's strongest signal and would only be muted by it.
func renderLineWithBaseSpans(text string, spans []baseSpan, matches []lineMatch, base lineStyle, cursor bool) string {
	// An empty line has no segment to render at all: the walk below produces
	// a single boundary point and no piece. Said here so the fast path can
	// stay a plain Render, which for a styled empty string would emit a bare
	// escape pair instead of nothing.
	if text == "" {
		return ""
	}
	// With nothing to composite, every boundary segment below resolves to the
	// same style and the whole walk collapses to this one Render — the common
	// case on the hot path: an ordinary diff line with no intraline emphasis
	// and no search hit. Pinned by
	// TestRenderLineWithBaseSpansFastPathMatchesGeneralPath.
	if len(spans) == 0 && len(matches) == 0 {
		// Render also expands tabs (see maybeConvertTabs, whose default
		// lipglossTabWidth mirrors); sgr only wraps, so do that part here. On
		// the hot path it's a scan that finds nothing, wrapStyledRows having
		// expanded already.
		return base.sgr(cursor).render(expandTabs(text))
	}

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

// highlightSpans splits text at case-insensitive occurrences of term,
// rendering unmatched runs with base and matched runs with match, then
// concatenating the already-rendered pieces — renderLineWithBaseSpans's
// pattern reduced to the one-term case a list row needs. Returns
// base.Render(text) when term is empty.
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
