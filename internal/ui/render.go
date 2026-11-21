package ui

import (
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

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
//
// blank marks a paragraph break for { and }. It isn't derived from text
// because a diff line's text carries its marker column: an added empty line
// reads as "+", which is blank in the file being shown but not in the string.
// Each block sets it, knowing what its own lines are.
type lineRenderer struct {
	text   string
	blank  bool
	render func(cursor bool) []string
}

// clippedLine is the lineRenderer for a line rendered in one uniform style
// and truncated (never wrapped) to width — a hunk header, a binary-file
// notice, the stats block's heading.
func clippedLine(style lipgloss.Style, text string, width int) lineRenderer {
	return lineRenderer{
		text:  text,
		blank: strings.TrimSpace(text) == "",
		render: func(cursor bool) []string {
			return []string{renderClipped(cursorMerged(style, cursor), text, width)}
		},
	}
}

// blankRow is the lineRenderer for a blank separator row between two content
// blocks: plain empty, or a full-width row filled with the cursor background
// when the cursor is on it.
func blankRow(width int) lineRenderer {
	return lineRenderer{
		blank: true,
		render: func(cursor bool) []string {
			if !cursor {
				return []string{""}
			}
			return []string{padRow("", width, true)}
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
	// Most lines are one row, so this is the right order of magnitude and
	// saves regrowing a slice the length of the whole commit.
	rows = make([]string, 0, len(lines))
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
	return padMeasuredRow(row, displayWidth(row), width, cursor)
}

// padMeasuredRow is padRow for a caller that already knows the row's display
// width. wrapStyledRows does: it wrapped the plain text the row was styled
// from, and styling only adds zero-width escapes, so that text's width is the
// row's. Measuring there instead means the escape parser never runs over the
// styled string, once per line of the whole commit.
func padMeasuredRow(row string, w, width int, cursor bool) string {
	style := rowFillStyle(cursor)
	// A plain fill contributes nothing but the truncation itself, so a row
	// already within width comes back from that Render byte-for-byte
	// unchanged — skip it, and pad with plain spaces for the same reason.
	// The cursor's fill does carry a style, so its row is always rendered.
	// (Hot path: a reflow pads every line of the commit.)
	plain := !cursor
	if !plain || w > width {
		row = style.MaxWidth(width).Render(row)
		w = displayWidth(row)
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

// displayWidth is ansi.StringWidth with a fast path for what the great
// majority of a diff is: printable ASCII, where every byte is one cell and the
// escape parser has nothing to find. Anything else — an escape sequence, a
// wide or combining rune, a control character — falls back to the real thing.
func displayWidth(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] >= 0x7f {
			return ansi.StringWidth(s)
		}
	}
	return len(s)
}

// lipglossTabWidth mirrors lipgloss's own default tab handling (Render
// replaces each tab with this many spaces before wrapping). wrapRanges applies
// the same expansion up front so its byte offsets describe the text lipgloss
// would actually lay out.
const lipglossTabWidth = 4

// tabSpaces is the run of spaces a single tab expands to.
var tabSpaces = strings.Repeat(" ", lipglossTabWidth)

// expandTabs replaces tabs with lipglossTabWidth spaces.
//
// It's the half of expandTabsMapped that most lines need: the offset map is a
// closure allocated per tab-indented line, and a reflow would otherwise pay
// for one on nearly every line of source code, most of which carry neither an
// intraline span nor a search match to relocate.
func expandTabs(text string) string {
	return strings.ReplaceAll(text, "\t", tabSpaces)
}

// expandTabsMapped expands tabs like expandTabs and additionally returns a
// mapping from a byte offset in text to its offset in the result (nil for the
// identity mapping, when there was nothing to expand), so span and match
// offsets computed against the original text can follow it.
func expandTabsMapped(text string) (string, offsetMap) {
	if !strings.Contains(text, "\t") {
		return text, nil
	}
	// Counting the tabs in front of an offset, rather than tabulating where
	// every byte of the line moved to: a line has a handful of offsets to
	// relocate (a span's two ends, a match's two), which a table would spend
	// an int per byte of the line to answer.
	return expandTabs(text), func(i int) int {
		return i + (lipglossTabWidth-1)*strings.Count(text[:i], "\t")
	}
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
// padding it adds to short rows) — every caller re-pads to width itself. The
// line's own trailing spaces are the exception: the wrapper drops those too,
// but they are content — a diff removing or adding trailing whitespace would
// otherwise show nothing where the change is — so they're kept, spilling onto
// rows of their own when the last row can't hold them.
func wrapRanges(text string, width int) [][2]int {
	if width <= 0 || text == "" {
		return [][2]int{{0, len(text)}}
	}
	// A line that already fits comes back from the wrapper as itself, padded
	// out to width — so measure first and skip the render, the expensive
	// half of laying a line out, which a reflow pays once per line of the
	// entire commit.
	if !strings.Contains(text, "\n") && displayWidth(text) <= width {
		return [][2]int{{0, len(text)}}
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
	return appendTrailingSpaces(ranges, text, width)
}

// wrapRowOf is the index of the row wrapStyledRows puts byte offset of text on
// when wrapping it to width — the same tab expansion and the same wrapRanges,
// so it can't name a row other than the one drawn. An offset past the end
// lands on the last row.
func wrapRowOf(text string, offset, width int) int {
	expanded, at := expandTabsMapped(text)
	if at != nil {
		offset = at(min(offset, len(text)))
	}
	ranges := wrapRanges(expanded, width)
	for i, r := range ranges {
		if offset < r[1] {
			return i
		}
	}
	return len(ranges) - 1
}

// appendTrailingSpaces extends ranges over whatever of text's tail the wrapper
// dropped — only ever trailing spaces, tabs having been expanded before any
// line is wrapped — filling the last row up to width and continuing onto rows
// of their own.
func appendTrailingSpaces(ranges [][2]int, text string, width int) [][2]int {
	last := &ranges[len(ranges)-1]
	if last[1] >= len(text) {
		return ranges
	}
	room := width - displayWidth(text[last[0]:last[1]])
	last[1] += min(max(room, 0), len(text)-last[1])
	for pos := last[1]; pos < len(text); pos += width {
		ranges = append(ranges, [2]int{pos, min(pos+width, len(text))})
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
	// The offset map is only built when there is something to relocate onto
	// the expanded text — see expandTabs.
	if len(spans) == 0 && len(matches) == 0 {
		text = expandTabs(text)
	} else {
		var expanded offsetMap
		text, expanded = expandTabsMapped(text)
		spans = shiftSpans(spans, expanded)
		matches = shiftMatches(matches, expanded)
	}

	plain := !cursor && len(spans) == 0 && len(matches) == 0
	ranges := wrapRanges(text, width)
	rows := make([]string, 0, len(ranges))
	for _, r := range ranges {
		seg := text[r[0]:r[1]]
		w := displayWidth(seg)
		// Styling a row and then padding it are two concatenations of the
		// same string; on the path most of a diff takes — no cursor, nothing
		// composited, and a row that fits — they are the same concatenation,
		// and a reflow does this once per line of the commit. The pieces are
		// exactly what those two steps would have produced, which is what
		// TestWrapStyledRowsFusedRowMatchesUnfused checks.
		if plain && width > 0 && seg != "" && w <= width {
			rows = append(rows, base.sgr(false).renderPadded(seg, width-w))
			continue
		}
		rendered := renderLineWithBaseSpans(seg, clipSpans(spans, r), clipMatches(matches, r), base, cursor)
		if width <= 0 {
			rows = append(rows, rendered)
			continue
		}
		rows = append(rows, padMeasuredRow(rendered, w, width, cursor))
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

// renderPadded is render followed by padRow's plain fill, in one
// concatenation rather than two — see wrapStyledRows, its only caller.
func (g sgr) renderPadded(text string, pad int) string {
	return g.prefix + text + g.suffix + strings.Repeat(" ", pad)
}

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
// specific non-match base style (e.g. the author's name in authorSpan),
// computed alongside the line's plain text in headerLinesFor so it composes
// with search-match highlighting.
type baseSpan struct {
	start, end int
	// style points at one of the package's own span styles rather than
	// holding a copy: a lineStyle carries a 552-byte lipgloss.Style, and spans
	// are copied per line and again per wrapped row (shiftSpans, clipSpans).
	style *lineStyle
}

// lineMatch is a search match's byte-offset position within one rendered
// line's text — a diff line (prefix + DiffLine.Text), a file header, or a
// header/message line — plus whether it's the active (n/N-selected) match.
type lineMatch struct {
	start, end int
	active     bool
}

// blockMatch is a search match as a block's Find* function reports it: the
// line it sits on, as a key into the block's own traversal (a line index, a
// file index, a diff position), and its byte columns within that line.
type blockMatch[K comparable] interface {
	at() K
	cols() (start, end int)
}

// matchCursor walks a block's matches alongside the lines the block emits.
// Both follow the same traversal order, so the matches on each line are the
// run starting at the cursor whose key is that line's.
type matchCursor[K comparable, M blockMatch[K]] struct {
	matches   []M
	next      int
	active    int
	dismissed bool
	// locs is parallel to matches: where each one landed. It is filled in
	// even while dismissed, so a dismissed match still lands correctly once
	// n/N revives it.
	locs []matchLoc
}

// matchLoc is where a match landed in its block: the logical line, and the
// byte offset its start sits at in that line's lineRenderer.text. The offset
// is what lets a jump reach a match deep inside a line wrapped over many rows
// (see DetailModel.matchRow) instead of only the row the line begins at.
type matchLoc struct {
	line, col int
	// styled is the match's entry in the lineMatch slice its line renders
	// from, so flipping its active flag restyles it without rebuilding the
	// line (see DetailModel.restyleMatch). Nil while dismissed, when no line
	// carries the match at all.
	styled *lineMatch
}

func newMatchCursor[K comparable, M blockMatch[K]](matches []M, active int, dismissed bool) *matchCursor[K, M] {
	return &matchCursor[K, M]{matches: matches, active: active, dismissed: dismissed, locs: make([]matchLoc, len(matches))}
}

// take consumes the matches keyed at, which sit on logical line line, and
// returns them styled for that line. It returns none while dismissed. shift is
// how far the line's text puts the matched string past the start the match's
// own columns count from — a diff line's marker, a file header's glyph.
func (c *matchCursor[K, M]) take(line int, at K, shift int) []lineMatch {
	var out []lineMatch
	first := c.next
	for c.next < len(c.matches) && c.matches[c.next].at() == at {
		start, end := c.matches[c.next].cols()
		if !c.dismissed {
			out = append(out, lineMatch{start: start, end: end, active: c.next == c.active})
		}
		c.locs[c.next] = matchLoc{line: line, col: start + shift}
		c.next++
	}
	// Only now that out has stopped growing are pointers into it stable.
	for i := range out {
		c.locs[first+i].styled = &out[i]
	}
	return out
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

	points := make([]int, 0, 2+2*(len(spans)+len(matches)))
	points = append(points, 0, len(text))
	for _, s := range spans {
		points = append(points, s.start, s.end)
	}
	for _, m := range matches {
		points = append(points, m.start, m.end)
	}
	slices.Sort(points)
	points = slices.Compact(points)

	// Every stretch is rendered through a precomputed sequence rather than
	// Render, for the same reason the whole fast path above is. Tabs are
	// Render's other job (see the fast path), so they're expanded here.
	styleAt := func(pos int) sgr {
		for _, m := range matches {
			if pos >= m.start && pos < m.end {
				return matchSpan(m.active)
			}
		}
		for _, s := range spans {
			if pos >= s.start && pos < s.end {
				return s.style.sgr(cursor)
			}
		}
		return base.sgr(cursor)
	}

	var b strings.Builder
	for i := 0; i < len(points)-1; i++ {
		start, end := points[i], points[i+1]
		b.WriteString(styleAt(start).render(expandTabs(text[start:end])))
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
// case-insensitive occurrence of term within text, none for an empty term.
//
// Offsets index text itself, never a lowercased copy: lowercasing can change a
// rune's byte length (İ is two bytes, i one), and an offset into the copy then
// lands on the wrong bytes of the original. ASCII can't do that, so the common
// case still searches a lowercased copy with strings.Index.
func findOccurrences(text, term string) [][2]int {
	if term == "" {
		return nil
	}
	var occs [][2]int
	if isASCII(text) && isASCII(term) {
		lowerText, lowerTerm := strings.ToLower(text), strings.ToLower(term)
		for pos := 0; ; {
			idx := strings.Index(lowerText[pos:], lowerTerm)
			if idx < 0 {
				return occs
			}
			idx += pos
			occs = append(occs, [2]int{idx, idx + len(term)})
			pos = idx + len(term)
		}
	}
	for pos := 0; pos < len(text); {
		if end, ok := foldedPrefixEnd(text, pos, term); ok {
			occs = append(occs, [2]int{pos, end})
			pos = end
			continue
		}
		_, size := utf8.DecodeRuneInString(text[pos:])
		pos += size
	}
	return occs
}

// foldedPrefixEnd reports whether text, from pos, starts with term under the
// per-rune lowercasing strings.ToLower applies, and where in text that prefix
// ends.
func foldedPrefixEnd(text string, pos int, term string) (int, bool) {
	for _, want := range term {
		if pos >= len(text) {
			return 0, false
		}
		r, size := utf8.DecodeRuneInString(text[pos:])
		if r != want && unicode.ToLower(r) != unicode.ToLower(want) {
			return 0, false
		}
		pos += size
	}
	return pos, true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
