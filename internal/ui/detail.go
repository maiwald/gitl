package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

// DetailModel renders a commit's metadata, message and diff as a single
// scrollable panel (so a long commit message doesn't permanently eat into
// the screen). A sticky header row above the panel always shows whichever
// section is currently in view — "Commit" for the metadata/message block,
// "Files changed" for the stats block, or the file header for whichever
// diff file is in view — updating as the user scrolls or jumps between
// sections with [ / ].
type DetailModel struct {
	detail *git.CommitDetail

	fileRanges []FileRange

	// sections covers the whole flattened content (commit metadata+message,
	// the optional stats block, then each diff file) contiguously and in
	// document order, so a scroll position maps to exactly one section —
	// used for the sticky header's title and for [ / ] navigation. Built
	// from fileRanges, so it's never present without them (the
	// len(m.fileRanges) == 0 guards below double as "is there anything to
	// be sticky about").
	sections []detailSection

	vp rowViewport

	width, height int

	// displayTerm and searchDismissed mirror the root Model's canonical search
	// state, pushed down via SetSearchDisplay. displayTerm is what
	// messageMatches/diffMatches were computed from: SetSearchDisplay
	// recomputes them whenever it changes, so a match set never outlives the
	// term it belongs to.
	displayTerm     string
	searchDismissed bool

	// messageMatches, statMatches and diffMatches together form one ordered
	// sequence that matchPos indexes into, in the blocks' own on-screen order
	// (see matchBlock): message matches first, then one per stats row naming a
	// matching file, then diff matches in file/hunk/line order with each
	// file's own name match ahead of its line matches — so n/N sweeps a commit
	// top to bottom the way you'd read it.
	messageMatches []MessageMatch
	statMatches    []StatMatch
	diffMatches    []DiffMatch
	matchPos       int // index into the combined sequence; -1 when there are none

	messageMatchLines []int // parallel to messageMatches: wrapped-line index within the header block
	statMatchLines    []int // parallel to statMatches: wrapped-line index within the flattened content (offset applied)
	diffMatchLines    []int // parallel to diffMatches: wrapped-line index within the flattened content (offset applied)

	// cursorLine is a tig-style line cursor: an index into the same
	// flattened content line space vp's scroll offset lives in (see
	// contentLen). Distinct from vp.YOffset — movement keys move the
	// cursor and the viewport follows it (setCursor's ensureCursorVisible),
	// not the reverse. Every reflow re-clamps it to [0, contentLen-1], so a
	// resize that rewraps the content can't leave it out of bounds; its
	// position survives a resize, but not necessarily its intended target.
	cursorLine int
	// contentLen is reflow's most recent flattened content length. It
	// excludes the trailing blank padding reflow appends (see reflow), so
	// the cursor can't wander into rows that hold no content.
	contentLen int

	// selecting reports whether a linewise selection (V) is in progress, and
	// selectAnchor is the row it was anchored at, meaningful only while it
	// is. The selection runs from the anchor's logical line to the cursor's,
	// in whichever direction the cursor has since moved, so every existing
	// motion extends it without knowing it exists — see highlight, which is
	// the only thing that reads either field.
	//
	// A bool rather than a sentinel anchor because row 0 is a perfectly good
	// place to start a selection, and because the zero DetailModel then
	// means "not selecting" without having to be constructed.
	selecting    bool
	selectAnchor int

	// lines are the logical lines the last reflow assembled, in document
	// order, each able to re-render itself with or without the cursor row's
	// background; content is what they rendered into (padding included), and
	// lineStart[i] is the row lines[i] begins at. Keeping all three is what
	// makes a cursor move cost two line renders (repaintHighlight) instead of a
	// whole reflow — with the obligation that anything changing
	// what a line *says* must go through reflow, which rebuilds all three
	// together.
	//
	// lineStart also defines the logical-line grid the cursor moves on: it
	// only ever rests on one of these rows (setCursor snaps to blockStart)
	// and moves a whole line at a time, since a wrapped line is highlighted
	// as one block.
	lines     []lineRenderer
	content   []string
	lineStart []int

	// jl is the shared, cross-view jumplist (ctrl-o/ctrl-i), owned by the
	// root Model. pushJump is this model's only interaction with it;
	// traversal lives on Model.
	jl *jumplist
}

// sectionKind distinguishes the three kinds of entry that can appear in
// DetailModel.sections. Only sectionKindFile carries a fileIndex into
// m.detail.Files; the other two render a fixed title.
type sectionKind int

const (
	sectionKindCommit sectionKind = iota
	sectionKindStats
	sectionKindFile
)

// detailSection marks one contiguous, offset-adjusted line range within the
// flattened viewport content (see DetailModel.sections), used to resolve a
// scroll position to a sticky-header title and to drive [ / ] navigation.
type detailSection struct {
	kind      sectionKind
	fileIndex int // meaningful only when kind == sectionKindFile
	startLine int
	endLine   int // exclusive
}

// sectionAt returns the index into sections whose range contains lineIndex.
// sections are contiguous and gapless in document order, so the answer is
// the last section whose startLine doesn't exceed lineIndex, clamped at both
// ends for a line outside the content (e.g. reflow's bottom padding).
func sectionAt(sections []detailSection, lineIndex int) int {
	if len(sections) == 0 {
		return -1
	}
	idx := sort.Search(len(sections), func(i int) bool { return sections[i].startLine > lineIndex }) - 1
	return clamp(idx, 0, len(sections)-1)
}

// NewDetailModel constructs a detail view for the given commit, sharing the
// root Model's cross-view jumplist for pushJump to record into.
func NewDetailModel(detail *git.CommitDetail, jl *jumplist) DetailModel {
	return DetailModel{
		detail:   detail,
		matchPos: -1,
		jl:       jl,
	}
}

// SetSize records the available rendering area and reflows both panes.
func (m *DetailModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.reflow()
}

func (m *DetailModel) reflow() {
	if m.detail == nil {
		return
	}

	// Every block contributes logical lines only, knowing neither where its
	// lines land nor which of them the cursor is on. So the indices a block
	// returns (a file's range, a match's line) count logical lines from the
	// block's own start, which is what the offsets below record; renderRows
	// then lays the lines out and rowOf rebases those indices onto rows.
	var lines []lineRenderer
	// The header block starts at line 0, so its match lines need no block
	// offset below — renderHeaderLines emits one logical line per line
	// headerLinesFor produced, however many rows each wraps to.
	headerText, _, _, headerSpans := headerLinesFor(m.detail)
	headerLines, messageMatchLines := renderHeaderLines(headerText, headerSpans, m.width, m.messageMatches, m.activeMatchIn(matchBlockMessage), m.searchDismissed)
	lines = append(lines, headerLines...)
	lines = append(lines, blankRow(m.width))

	statsStart := len(lines)
	statLines, statMatchLines := statLinesFor(m.detail.Files, m.width, m.statMatches, m.activeMatchIn(matchBlockStats), m.searchDismissed)
	if len(statLines) > 0 {
		lines = append(lines, statLines...)
		lines = append(lines, blankRow(m.width))
	}

	diffStart := len(lines)
	diffLines, fileRanges, diffMatchLines := m.renderDiffBlock(m.width)
	lines = append(lines, diffLines...)

	m.lines = lines
	hlFrom, hlTo := m.highlight()
	m.content, m.lineStart = renderRows(lines, hlFrom, hlTo)
	m.contentLen = len(m.content)

	m.fileRanges = make([]FileRange, len(fileRanges))
	for i, r := range fileRanges {
		m.fileRanges[i] = FileRange{
			FileIndex: r.FileIndex,
			StartLine: m.rowOf(diffStart + r.StartLine),
			EndLine:   m.rowOf(diffStart + r.EndLine),
		}
	}
	m.sections = buildSections(m.fileRanges, m.rowOf(statsStart), m.rowOf(diffStart))

	m.messageMatchLines = make([]int, len(messageMatchLines))
	for i, ln := range messageMatchLines {
		m.messageMatchLines[i] = m.rowOf(ln)
	}
	m.statMatchLines = make([]int, len(statMatchLines))
	for i, ln := range statMatchLines {
		m.statMatchLines[i] = m.rowOf(statsStart + ln)
	}
	m.diffMatchLines = make([]int, len(diffMatchLines))
	for i, ln := range diffMatchLines {
		m.diffMatchLines[i] = m.rowOf(diffStart + ln)
	}

	m.vp.Height = m.height
	if len(m.fileRanges) > 0 {
		m.vp.Height--
	}
	m.vp.Height = max(m.vp.Height, 0)
	// rowViewport.SetYOffset clamps to the content's own max scroll position,
	// which would leave a short trailing file's landing line (see
	// sectionContentStart) unreachable as the top-of-viewport line. Pad with
	// blank lines so jumpToSection can always bring the last file's content
	// to the top. contentLen, captured above, excludes this padding, so G
	// and the clamp below bound themselves to the real content.
	if len(m.fileRanges) > 0 {
		lastStart := m.fileRanges[len(m.fileRanges)-1].StartLine
		if need := lastStart + 1 + m.vp.Height - len(m.content); need > 0 {
			m.content = append(m.content, make([]string, need)...)
		}
	}

	// A resize can shrink contentLen out from under an already-set cursorLine
	// or selection anchor — clamp both back in range and repaint the
	// highlight onto wherever they ended up, since renderRows above was given
	// a range that no longer exists.
	//
	// The anchor has to be clamped and not merely clipped when it's read: an
	// anchor left pointing past the content comes back to life the moment a
	// resize makes the content that long again, silently resizing a selection
	// nobody touched.
	cursor := clamp(m.cursorLine, 0, m.contentLen-1)
	anchor := clamp(m.selectAnchor, 0, m.contentLen-1)
	if m.contentLen > 0 && (cursor != m.cursorLine || (m.selecting && anchor != m.selectAnchor)) {
		drawn0, drawn1 := m.highlight()
		m.cursorLine, m.selectAnchor = cursor, anchor
		m.repaintHighlight(drawn0, drawn1)
	} else {
		m.cursorLine = cursor
	}

	m.setContent()
}

// rowOf is the row a logical line begins at, or contentLen for the
// one-past-the-end index an exclusive range carries. Only meaningful after
// renderRows has run for the current reflow.
func (m DetailModel) rowOf(logical int) int {
	if logical < len(m.lineStart) {
		return m.lineStart[logical]
	}
	return m.contentLen
}

// setContent hands the cached rows to the viewport, which borrows the slice
// rather than copying it. So a line patched in place (patchLogical) is already
// visible without this call; what needs it is a reflow, which builds a fresh
// slice, and the padding append below it, which may move the backing array.
// It's also where a scroll offset left past the end of shrunken content gets
// pinned back to the bottom.
func (m *DetailModel) setContent() {
	m.vp.SetRows(m.content)
}

// highlight is the inclusive range of rows drawn with the cursor row's
// background: the line the cursor is on, or everything between the selection
// anchor and the cursor while a selection is active.
func (m DetailModel) highlight() (from, to int) {
	if !m.selecting {
		return m.cursorLine, m.cursorLine
	}
	return min(m.selectAnchor, m.cursorLine), max(m.selectAnchor, m.cursorLine)
}

// startSelection anchors a linewise selection at the cursor. Every motion
// from here extends it, since they all move the cursor and highlight reads
// both ends.
func (m *DetailModel) startSelection() {
	if m.selecting {
		return
	}
	m.selecting = true
	m.selectAnchor = m.cursorLine
	// Nothing moved, so nothing to repaint: the anchor line is the cursor
	// line, and it already carries the highlight.
}

// clearSelection drops the selection, repainting the lines that lose their
// highlight. A no-op when there is none, so callers can clear unconditionally
// — which every transition out of the state a selection was made in does: a
// selection never outlives the view it was taken in.
func (m *DetailModel) clearSelection() {
	if !m.selecting {
		return
	}
	from, to := m.highlight()
	m.selecting = false
	m.repaintHighlight(from, to)
}

// selectedLines is the inclusive range of logical lines the selection covers,
// empty (first > last) when there is no selection.
func (m DetailModel) selectedLines() (first, last int) {
	if !m.selecting {
		return 0, -1
	}
	from, to := m.highlight()
	return logicalRangeAt(m.lineStart, m.contentLen, from, to)
}

// selectedLineCount is how many logical lines the selection covers.
func (m DetailModel) selectedLineCount() int {
	first, last := m.selectedLines()
	return last - first + 1
}

// SelectedText is the selection as plain text: one logical line per line,
// verbatim — tabs, diff markers and any trailing whitespace of the line's own
// intact — ending in a newline the way a linewise yank does. Empty when
// nothing is selected.
func (m DetailModel) SelectedText() string {
	first, last := m.selectedLines()
	if first > last {
		return ""
	}
	var b strings.Builder
	for i := first; i <= last; i++ {
		b.WriteString(m.lines[i].text)
		b.WriteByte('\n')
	}
	return b.String()
}

// repaintHighlight re-renders only the logical lines whose highlight actually
// changed between the range that was drawn (old) and the one now current,
// splicing them into the cached content, so moving the cursor through a large
// diff doesn't re-render every line of it. Falls back to a full reflow when
// the cache isn't what the current lines would have produced.
func (m *DetailModel) repaintHighlight(oldFrom, oldTo int) {
	if len(m.lineStart) == 0 || len(m.content) < m.contentLen {
		m.reflow()
		return
	}
	newFrom, newTo := m.highlight()
	was0, was1 := logicalRangeAt(m.lineStart, m.contentLen, oldFrom, oldTo)
	now0, now1 := logicalRangeAt(m.lineStart, m.contentLen, newFrom, newTo)

	// Only the difference between the two ranges is repainted, never both in
	// full: extending a selection by a line has to cost one line render, not
	// one per line already selected.
	for i := max(min(was0, now0), 0); i <= max(was1, now1); i++ {
		if wasOn, nowOn := i >= was0 && i <= was1, i >= now0 && i <= now1; wasOn != nowOn {
			m.patchLogical(i, nowOn)
		}
	}
	m.setContent()
}

// patchLogical re-renders logical line i in place, with or without the cursor
// row's background. A no-op for a line that doesn't exist, so callers can pass
// a stale position without special-casing it.
func (m *DetailModel) patchLogical(i int, highlighted bool) {
	if i < 0 || i >= len(m.lines) {
		return
	}
	copy(m.content[m.lineStart[i]:], m.lines[i].render(highlighted))
}

// mergeNotice stands in for the diff of a merge commit, which gitl doesn't
// build: with more than one parent there's no single canonical diff, and
// picking one is deferred. It's the one thing an empty diff still says — an
// empty diff that isn't a merge simply shows nothing, since gitl no longer
// knows what was asked for (git parsed that) and couldn't explain it anyway.
const mergeNotice = "Merge commit: diff not shown (viewing merge diffs isn't supported yet)."

// renderDiffBlock describes this commit's diff at width, honoring the current
// search state. Its returned coordinates count logical lines from the block's
// own start — see reflow, which rebases them.
func (m *DetailModel) renderDiffBlock(width int) (lines []lineRenderer, fileRanges []FileRange, matchLines []int) {
	if len(m.detail.Files) == 0 {
		if m.detail.IsMerge() {
			return []lineRenderer{clippedLine(binaryNoticeStyle, mergeNotice, width)}, nil, nil
		}
		return nil, nil, nil
	}
	return RenderDiff(m.detail.Files, width, m.diffMatches, m.activeMatchIn(matchBlockDiff), m.searchDismissed)
}

// buildSections assembles one reflow pass's sticky-header/[ ]-navigation
// sections: the commit metadata block, optionally the stats block, then each
// already rebased file range, in document order. statsStart and diffStart
// are the rows those blocks begin at, and are equal when the commit has no
// diff to summarize — exactly when there's no stats section to emit.
func buildSections(fileRanges []FileRange, statsStart, diffStart int) []detailSection {
	sections := make([]detailSection, 0, len(fileRanges)+2)
	sections = append(sections, detailSection{kind: sectionKindCommit, startLine: 0, endLine: statsStart})
	if statsStart < diffStart {
		sections = append(sections, detailSection{kind: sectionKindStats, startLine: statsStart, endLine: diffStart})
	}
	for _, r := range fileRanges {
		sections = append(sections, detailSection{kind: sectionKindFile, fileIndex: r.FileIndex, startLine: r.StartLine, endLine: r.EndLine})
	}
	return sections
}

// headerLinesFor builds the commit header's pre-wrap logical lines, one entry
// per line; wrapping happens later, in renderHeaderLines, so matchLines
// bookkeeping stays exact.
//
// authorLine and bodyStart mark the two regions eligible for search: the
// author line and the message body, excluding the hash/parent/date metadata
// lines. List search matches the hash too (git.HashesMatching), but that's
// for finding a commit in the list, not for searching within a commit you're
// already viewing. spans (keyed by line index) marks the hash/name/date
// substrings that get base coloring regardless of search state.
//
// The body is indented 4 spaces, matching `git show`'s own convention.
func headerLinesFor(d *git.CommitDetail) (lines []string, authorLine, bodyStart int, spans map[int][]baseSpan) {
	spans = make(map[int][]baseSpan)

	// A piped diff carrying a header gitl couldn't resolve to a commit shows
	// that header verbatim rather than guessing at its shape: git's own
	// format is reconfigurable (--pretty, log.date) and this is whatever the
	// user's command produced. bodyStart = 0 makes all of it searchable,
	// since it's the only description of the diff there is.
	if d.Kind == git.EntryPiped && d.Preamble != "" {
		return strings.Split(strings.TrimRight(d.Preamble, "\n"), "\n"), -1, 0, spans
	}

	// A pseudo entry (working tree/staged) has no commit metadata to show,
	// just a title and a dim description, and nothing eligible for message
	// search — hence authorLine = -1. The title is left out whenever there's a
	// diff, because the sticky header then already shows it directly above;
	// only an empty diff, which gets no sticky header, needs it in the block.
	if d.IsPseudo() {
		if len(d.Files) == 0 {
			lines = append(lines, entryTitle(d.Kind))
		}
		desc := entryDescription(d.Kind)
		spans[len(lines)] = []baseSpan{{start: 0, end: len(desc), style: &dimStyle}}
		lines = append(lines, desc)
		return lines, -1, len(lines), spans
	}

	const commitPrefix = "Commit: "
	lines = append(lines, commitPrefix+d.Hash)
	spans[0] = []baseSpan{{start: len(commitPrefix), end: len(commitPrefix) + len(d.Hash), style: &shaStyle}}

	if d.IsMerge() {
		const mergePrefix = "Merge:  "
		lines = append(lines, mergePrefix+strings.Join(d.Parents, " "))
		mergeSpans := make([]baseSpan, 0, len(d.Parents))
		col := len(mergePrefix)
		for i, p := range d.Parents {
			mergeSpans = append(mergeSpans, baseSpan{start: col, end: col + len(p), style: &shaStyle})
			col += len(p)
			if i < len(d.Parents)-1 {
				col++ // the separating space strings.Join inserts
			}
		}
		spans[len(lines)-1] = mergeSpans
	}

	authorLine = len(lines)
	const authorPrefix = "Author: "
	lines = append(lines, fmt.Sprintf("%s%s <%s>", authorPrefix, d.AuthorName, d.AuthorEmail))
	spans[authorLine] = []baseSpan{{start: len(authorPrefix), end: len(authorPrefix) + len(d.AuthorName), style: &authorStyle}}

	const datePrefix = "Date:   "
	dateText := d.AuthorDate.Local().Format("2006-01-02 15:04:05 -0700")
	lines = append(lines, datePrefix+dateText)
	spans[len(lines)-1] = []baseSpan{{start: len(datePrefix), end: len(datePrefix) + len(dateText), style: &dateStyle}}

	lines = append(lines, "")
	bodyStart = len(lines)
	for _, l := range strings.Split(strings.TrimRight(d.Body, "\n"), "\n") {
		lines = append(lines, "    "+l)
	}
	return lines, authorLine, bodyStart, spans
}

// renderHeaderLines turns each logical header/message line into a
// lineRenderer that word-wraps it to width via wrapStyledRows, which applies
// base coloring (spans, from headerLinesFor) and search-match highlighting
// per wrapped row — the wrap itself decided on the plain text. matchLines is
// parallel to matches: the logical line each match sits on, tracked even when
// dismissed suppresses the styling. spans are never suppressed, being no part
// of search.
func renderHeaderLines(lines []string, spans map[int][]baseSpan, width int, matches []MessageMatch, activeIdx int, dismissed bool) (rendered []lineRenderer, matchLines []int) {
	mc := newMatchCursor[int](matches, activeIdx, dismissed)
	for li, line := range lines {
		lineMatches := mc.take(li, li)
		lineSpans := spans[li]
		rendered = append(rendered, lineRenderer{
			text:  line,
			blank: strings.TrimSpace(line) == "",
			render: func(cursor bool) []string {
				return wrapStyledRows(line, lineSpans, lineMatches, plainLine, width, cursor)
			},
		})
	}
	return rendered, mc.lines
}

// MessageMatch identifies one case-insensitive occurrence of a search term
// within a single pre-wrap header/message line (see headerLinesFor):
// LineIndex indexes into that line slice, not diff content.
type MessageMatch struct {
	LineIndex        int
	StartCol, EndCol int
}

func (m MessageMatch) at() int                { return m.LineIndex }
func (m MessageMatch) cols() (start, end int) { return m.StartCol, m.EndCol }

// FindMessageMatches returns every occurrence of term in the header lines
// eligible for search: the author line and every line from bodyStart onward,
// not the hash/parent/date lines (see headerLinesFor).
func FindMessageMatches(lines []string, authorLine, bodyStart int, term string) []MessageMatch {
	if term == "" {
		return nil
	}
	var matches []MessageMatch
	if authorLine >= 0 && authorLine < len(lines) {
		matches = append(matches, findMessageLineMatches(lines[authorLine], term, authorLine)...)
	}
	for li := bodyStart; li < len(lines); li++ {
		matches = append(matches, findMessageLineMatches(lines[li], term, li)...)
	}
	return matches
}

func findMessageLineMatches(line, term string, lineIndex int) []MessageMatch {
	occs := findOccurrences(line, term)
	out := make([]MessageMatch, len(occs))
	for i, occ := range occs {
		out[i] = MessageMatch{LineIndex: lineIndex, StartCol: occ[0], EndCol: occ[1]}
	}
	return out
}

// Update handles a navigation key press against the diff viewport. j/k,
// ctrl-u/ctrl-d and g/G move the line cursor (see cursorLine) rather than
// scrolling directly; the viewport follows it. [ / ] and g/G record a
// jumplist entry when they actually move the cursor, plain j/k/ctrl-u/ctrl-d
// don't, matching vim's own jumplist. ctrl-o/ctrl-i aren't handled here — see
// the root Model. Enter jumps to a file's diff section when the cursor is on
// that file's row in the stats block, and is a no-op anywhere else.
func (m *DetailModel) Update(msg tea.KeyMsg) {
	switch {
	case key.Matches(msg, DefaultKeyMap.Up):
		m.moveCursor(-1)
	case key.Matches(msg, DefaultKeyMap.Down):
		m.moveCursor(1)
	case key.Matches(msg, DefaultKeyMap.HalfPageUp):
		m.moveCursor(-halfPage(m.vp.Height))
	case key.Matches(msg, DefaultKeyMap.HalfPageDown):
		m.moveCursor(halfPage(m.vp.Height))
	case key.Matches(msg, DefaultKeyMap.Top):
		m.jumpCursor(0)
	case key.Matches(msg, DefaultKeyMap.Bottom):
		m.jumpCursor(m.contentLen - 1)
	case key.Matches(msg, DefaultKeyMap.PrevPara):
		m.jumpParagraph(-1)
	case key.Matches(msg, DefaultKeyMap.NextPara):
		m.jumpParagraph(1)
	case key.Matches(msg, DefaultKeyMap.PrevFile):
		m.jumpToSection(-1)
	case key.Matches(msg, DefaultKeyMap.NextFile):
		m.jumpToSection(1)
	case key.Matches(msg, DefaultKeyMap.Enter):
		if fi, ok := m.statsFileIndexAt(m.cursorLine); ok {
			m.jumpToFile(fi)
		}
	}
}

// moveCursor moves the cursor by delta lines without recording a jumplist
// entry — plain motion, not a jump.
//
// Downward motion never lands short of the next logical line: setCursor
// snaps back to the start of whatever block it's given, so a plain j inside
// a line that wrapped to several rows would otherwise snap back where it
// started and look like a dead key.
//
// A block taller than the viewport (a minified file is one logical line of
// thousands of rows) is scrolled through before the cursor leaves it, delta
// rows at a time, since stepping straight to the next logical line would
// jump over everything of it that isn't on screen — and on the last line of
// a diff there is no next line to step to, so its rest could never be seen.
func (m *DetailModel) moveCursor(delta int) {
	if m.scrollWithinBlock(delta) {
		return
	}
	pos := m.cursorLine + delta
	if delta > 0 {
		pos = max(pos, m.blockEnd(m.cursorLine))
	}
	m.setCursor(pos)
}

// scrollWithinBlock scrolls the viewport delta rows through the cursor's
// block, stopping at its edge, if the block still extends past the viewport
// in that direction. It reports whether it did, leaving the cursor put.
func (m *DetailModel) scrollWithinBlock(delta int) bool {
	top := m.vp.YOffset
	switch end := m.blockEnd(m.cursorLine); {
	case delta > 0 && end > top+m.vp.Height:
		m.vp.SetYOffset(min(top+delta, end-m.vp.Height))
	case delta < 0 && m.cursorLine < top:
		m.vp.SetYOffset(max(top+delta, m.cursorLine))
	default:
		return false
	}
	return true
}

// blockStart returns the first row of the logical line pos falls in;
// blockEnd returns one past its last. Both fall back to pos itself for a row
// outside the content, so movement degrades to plain per-row stepping rather
// than getting stuck.
func (m *DetailModel) blockStart(pos int) int {
	i := logicalAt(m.lineStart, pos)
	if i < 0 || pos >= m.contentLen {
		return pos
	}
	return m.lineStart[i]
}

func (m *DetailModel) blockEnd(pos int) int {
	i := logicalAt(m.lineStart, pos)
	if i < 0 || pos >= m.contentLen {
		return pos + 1
	}
	if i+1 < len(m.lineStart) {
		return m.lineStart[i+1]
	}
	return m.contentLen
}

// jumpCursor moves the cursor straight to line, recording the jump first
// unless line is where the cursor already is. Used for g/G, which scroll the
// target into view; [ / ] and search jumps go through landOnLine instead,
// which puts the target at the very top of the viewport.
func (m *DetailModel) jumpCursor(line int) {
	if clamped := clamp(line, 0, m.contentLen-1); clamped != m.cursorLine {
		m.pushJump(m.cursorLine)
	}
	m.setCursor(line)
}

// setCursor moves the cursor to pos (clamped in range, and snapped back to
// the start of the logical line it lands in), repaints the two lines whose
// highlight changed, then scrolls the viewport by the minimum needed to keep
// the cursor visible.
func (m *DetailModel) setCursor(pos int) {
	from, to := m.highlight()
	m.cursorLine = m.blockStart(clamp(pos, 0, m.contentLen-1))
	m.repaintHighlight(from, to)
	m.ensureCursorVisible()
}

// ensureCursorVisible scrolls the diff panel so the cursor line stays within
// the visible viewport. A wrapped line is highlighted as a whole block, so
// it's the block's last row that has to come into view when moving down.
//
// A block taller than the viewport can't be shown whole, so any row of it on
// screen counts as visible, and one that isn't is brought in by its nearer
// edge — its first row at the bottom coming down, its last row at the top
// coming up — for moveCursor to scroll through from there
// (scrollWithinBlock). Snapping to its start instead would skip the whole of
// it on the way up, and undo a scroll into it on every repaint.
func (m *DetailModel) ensureCursorVisible() {
	if m.vp.Height <= 0 {
		return
	}
	last := m.blockEnd(m.cursorLine) - 1
	if last-m.cursorLine >= m.vp.Height {
		if last < m.vp.YOffset {
			m.vp.SetYOffset(last)
		} else if m.cursorLine >= m.vp.YOffset+m.vp.Height {
			m.vp.SetYOffset(m.cursorLine - m.vp.Height + 1)
		}
		return
	}
	if m.cursorLine < m.vp.YOffset {
		m.vp.SetYOffset(m.cursorLine)
	} else if last >= m.vp.YOffset+m.vp.Height {
		m.vp.SetYOffset(last - m.vp.Height + 1)
	}
}

// pushJump records pos — the cursor line about to be left — onto the shared
// jumplist, tagged with this model's own *git.CommitDetail so a later
// cross-view jump knows which commit/entry to reconstruct.
func (m *DetailModel) pushJump(pos int) {
	m.jl.push(jumpPos{view: viewDetail, commit: m.detail, cursorLine: pos})
}

// jumpParagraph moves the cursor to the next (delta > 0) or previous blank
// line, vim's } and {, clamping at the first/last line rather than wrapping.
//
// It follows vim's rule rather than "the nearest blank line": step off the
// current line, skip the rest of the blank run if the cursor was in one, then
// run to the far side of the text that follows. So a press from anywhere in a
// block of lines lands on the blank that ends it, and a press from a blank
// crosses the next block instead of sticking to its edge.
//
// Like g/G it's jump-worthy, and it goes through setCursor, so it extends a
// selection the way every other motion does.
func (m *DetailModel) jumpParagraph(delta int) {
	last := len(m.lines) - 1
	if last < 0 {
		return
	}
	i := logicalAt(m.lineStart, m.cursorLine)
	if i < 0 {
		return
	}

	startedBlank := m.lines[i].blank
	i += delta
	if startedBlank {
		for i >= 0 && i <= last && m.lines[i].blank {
			i += delta
		}
	}
	for i >= 0 && i <= last && !m.lines[i].blank {
		i += delta
	}

	target := clamp(i, 0, last)
	if row := m.lineStart[target]; row != m.cursorLine {
		m.pushJump(m.cursorLine)
		m.setCursor(row)
	}
}

// jumpToSection scrolls the diff panel directly to the section delta away
// from the current one, clamping at the first/last section (no
// wraparound), and moves the cursor there too. A no-op on a diff-less commit,
// where the commit section is the only one there is.
func (m *DetailModel) jumpToSection(delta int) {
	if len(m.sections) <= 1 {
		return
	}
	target := clamp(m.currentSection()+delta, 0, len(m.sections)-1)
	m.landOnLine(sectionContentStart(m.sections[target]))
}

// jumpToFile scrolls the diff panel to the diff section for fileIndex, the
// same landing point [ / ] uses, for the Enter-on-a-stats-row action. A no-op
// if fileIndex has no corresponding section.
func (m *DetailModel) jumpToFile(fileIndex int) {
	for _, s := range m.sections {
		if s.kind == sectionKindFile && s.fileIndex == fileIndex {
			m.landOnLine(sectionContentStart(s))
			return
		}
	}
}

// statsFileIndexAt reports which file, if any, the stats block's row at line
// corresponds to, following statLinesFor's layout (0 = the heading,
// 1..len(files) = one row per file). ok is false for the heading, the totals
// row, and any line outside the stats block.
func (m *DetailModel) statsFileIndexAt(line int) (fileIndex int, ok bool) {
	for _, s := range m.sections {
		if s.kind != sectionKindStats {
			continue
		}
		fi := line - s.startLine - 1
		if fi >= 0 && fi < len(m.detail.Files) {
			return fi, true
		}
		return -1, false
	}
	return -1, false
}

// landOnLine scrolls directly to line, pushing a jump first if it actually
// moves the cursor, and lands the target at the very top of the viewport —
// as opposed to setCursor, which scrolls the minimum to keep it visible.
func (m *DetailModel) landOnLine(line int) {
	if line != m.cursorLine {
		m.pushJump(m.cursorLine)
	}
	from, to := m.highlight()
	m.cursorLine = m.blockStart(line)
	m.repaintHighlight(from, to)
	m.vp.SetYOffset(line)
}

// sectionContentStart returns the scroll position jumpToSection lands on: one
// line past the section's own header row for the stats and file sections,
// which would otherwise duplicate the sticky bar's identical title. The
// commit section's first line isn't such a duplicate, so it lands at
// startLine.
func sectionContentStart(s detailSection) int {
	if s.kind == sectionKindCommit {
		return s.startLine
	}
	return s.startLine + 1
}

// currentSection is the index of the section the diff panel's topmost visible
// line belongs to, or -1 when there are no sections.
func (m DetailModel) currentSection() int {
	return sectionAt(m.sections, m.vp.YOffset)
}

// computeMatches finds every occurrence of term across this commit's header
// and message (see headerLinesFor), its stats block and its diff content, in
// that on-screen order, without moving the cursor or marking any match
// current. Jumping to a match is jumpToFirstMatch's separate job, so a view
// that opens with a search already active highlights without landing the
// cursor mid-diff.
func (m *DetailModel) computeMatches(term string) {
	if term == "" || m.detail == nil {
		m.messageMatches, m.statMatches, m.diffMatches = nil, nil, nil
	} else {
		lines, authorLine, bodyStart, _ := headerLinesFor(m.detail)
		m.messageMatches = FindMessageMatches(lines, authorLine, bodyStart, term)
		m.statMatches = FindStatMatches(m.detail.Files, term)
		m.diffMatches = FindDiffMatches(m.detail.Files, term)
	}
	m.matchPos = -1
}

// jumpToFirstMatch selects the first match and scrolls to it — the jumping
// half of submitting "/" while already viewing this commit, the compute half
// being SetSearchDisplay's. Unlike list search it's synchronous: the whole
// commit is already in memory.
func (m *DetailModel) jumpToFirstMatch() {
	if m.totalMatches() > 0 {
		m.setMatchPos(0)
	}
	m.jumpToMatch()
}

// setMatchPos makes match pos the active one and reflows. Which match is
// active is baked into the content, and both the match being left and the one
// being taken up can be anywhere in the commit, so unlike a cursor move this
// needs the whole reflow — which is also what makes the match lines
// jumpToMatch reads current.
func (m *DetailModel) setMatchPos(pos int) {
	m.matchPos = pos
	m.reflow()
}

func (m DetailModel) totalMatches() int {
	return len(m.messageMatches) + len(m.statMatches) + len(m.diffMatches)
}

// matchBlock names one of the three separately-searched blocks a detail view
// renders. The constants are in the blocks' on-screen order, which is also the
// order they are concatenated into the combined matchPos sequence.
type matchBlock int

const (
	matchBlockMessage matchBlock = iota
	matchBlockStats
	matchBlockDiff
)

// matchSegment reports which block of the combined match sequence matchPos
// falls in, and its position within that block's own matches. Only meaningful
// for matchPos >= 0; callers must guard the "no active match" case themselves
// before indexing with idx.
func (m DetailModel) matchSegment() (block matchBlock, idx int) {
	if m.matchPos < len(m.messageMatches) {
		return matchBlockMessage, m.matchPos
	}
	idx = m.matchPos - len(m.messageMatches)
	if idx < len(m.statMatches) {
		return matchBlockStats, idx
	}
	return matchBlockDiff, idx - len(m.statMatches)
}

// activeMatchIn resolves which match one of the rendered blocks should style
// as the active (n/N-selected) one: an index into that block's own match
// slice, or -1 when the active match is in another block, there is none yet,
// or the search is dismissed.
func (m DetailModel) activeMatchIn(block matchBlock) int {
	if m.searchDismissed || m.matchPos < 0 {
		return -1
	}
	if active, idx := m.matchSegment(); active == block {
		return idx
	}
	return -1
}

// NextMatch and PrevMatch cycle to the following/preceding match across the
// combined message+stats+diff sequence, wrapping at both ends. Where that
// left the position is read back through MatchPosition, which is what the
// status bar displays; unlike the list's, this stepping needs no fetch and so
// has nothing to report to its caller.
func (m *DetailModel) NextMatch() { m.stepMatch(1) }
func (m *DetailModel) PrevMatch() { m.stepMatch(-1) }

func (m *DetailModel) stepMatch(delta int) {
	total := m.totalMatches()
	if total == 0 {
		return
	}
	m.setMatchPos(stepMatchPos(m.matchPos, delta, total))
	m.jumpToMatch()
}

// jumpToMatch scrolls the diff panel to the current match, whichever block it
// lives in. It renders nothing itself: setMatchPos's reflow already styled the
// newly active match and produced the match lines read here, so the landing is
// an ordinary cursor move.
//
// Every step pushes a jump, not just the initial search — see
// applyCurrentMatch in list.go.
func (m *DetailModel) jumpToMatch() {
	if m.matchPos < 0 {
		return
	}
	block, idx := m.matchSegment()
	var lines []int
	switch block {
	case matchBlockMessage:
		lines = m.messageMatchLines
	case matchBlockStats:
		lines = m.statMatchLines
	default:
		lines = m.diffMatchLines
	}
	if idx >= len(lines) {
		return
	}
	m.landOnLine(lines[idx])
}

// SetSearchDisplay installs the term to highlight and whether highlighting is
// dismissed, then reflows. A changed term recomputes this commit's matches
// synchronously, so they never outlive the term they were found for. Clearing
// the search is just the term going empty.
func (m *DetailModel) SetSearchDisplay(term string, dismissed bool) {
	if term != m.displayTerm {
		m.displayTerm = term
		m.computeMatches(term)
	}
	m.searchDismissed = dismissed
	m.reflow()
}

// MatchPosition reports the current match position (1-based) and total
// count for status-bar display.
func (m DetailModel) MatchPosition() (pos, total int) {
	return matchPosition(m.matchPos, m.totalMatches())
}

func (m DetailModel) View() string {
	if m.detail == nil {
		return ""
	}
	if len(m.fileRanges) == 0 {
		return m.vp.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderStickyHeader(), m.vp.View())
}

// renderStickyHeader renders the sticky row's title: "Commit" for the
// metadata/message block, statsSectionTitle for the stats block, or a diff
// file's header.
//
// Looked up one line behind YOffset, not at currentSection: a section's own
// header row renders identically to its sticky title, so switching a line
// early would show that title twice. sectionContentStart accounts for this,
// so a [ / ] jump still shows the new title immediately.
func (m DetailModel) renderStickyHeader() string {
	s := m.sections[sectionAt(m.sections, m.vp.YOffset-1)]
	switch s.kind {
	case sectionKindStats:
		return renderClipped(fileHeaderStyle, statsSectionTitle, m.width)
	case sectionKindFile:
		return renderFileHeader(m.detail.Files[s.fileIndex], m.width, false, nil)
	default:
		title := "Commit"
		if m.detail.IsPseudo() {
			title = entryTitle(m.detail.Kind)
		}
		return renderClipped(fileHeaderStyle, title, m.width)
	}
}
