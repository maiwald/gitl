package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
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
	sections       []detailSection
	currentSection int

	leftVP viewport.Model

	width, height int

	// displayTerm and searchDismissed mirror the root Model's canonical search
	// state, pushed down via SetSearchDisplay. displayTerm is what
	// messageMatches/diffMatches were computed from: SetSearchDisplay
	// recomputes them whenever it changes, so a match set never outlives the
	// term it belongs to.
	displayTerm     string
	searchDismissed bool

	// messageMatches and diffMatches together form one ordered sequence that
	// matchPos indexes into — message matches first, then diff matches in
	// file/hunk/line order, each file's own name match ahead of its line
	// matches — so n/N sweeps a commit top to bottom the way you'd read it.
	messageMatches []MessageMatch
	diffMatches    []DiffMatch
	matchPos       int // index into the combined sequence; -1 when there are none

	messageMatchLines []int // parallel to messageMatches: wrapped-line index within the header block
	diffMatchLines    []int // parallel to diffMatches: wrapped-line index within the flattened content (offset applied)

	// cursorLine is a tig-style line cursor: an index into the same
	// flattened content line space leftVP's scroll offset lives in (see
	// contentLen). Distinct from leftVP.YOffset — movement keys move the
	// cursor and the viewport follows it (setCursor's ensureCursorVisible),
	// not the reverse. Every reflow re-clamps it to [0, contentLen-1], so a
	// resize that rewraps the content can't leave it out of bounds; its
	// position survives a resize, but not necessarily its intended target.
	cursorLine int
	// contentLen is reflow's most recent flattened content length. It
	// excludes the trailing blank padding reflow appends (see reflow), so
	// the cursor can't wander into rows that hold no content.
	contentLen int

	// lines are the logical lines the last reflow assembled, in document
	// order, each able to re-render itself with or without the cursor row's
	// background; content is what they rendered into (padding included), and
	// lineStart[i] is the row lines[i] begins at. Keeping all three is what
	// makes a cursor move cost two line renders and a join (repaintCursor)
	// instead of a whole reflow — with the obligation that anything changing
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
		leftVP:   viewport.New(0, 0),
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

	diffWidth := m.width

	// Every block contributes logical lines only, knowing neither where its
	// lines land nor which of them the cursor is on. So the indices a block
	// returns (a file's range, a match's line) count logical lines from the
	// block's own start, which is what the offsets below record; renderRows
	// then lays the lines out and rowOf rebases those indices onto rows.
	var lines []lineRenderer
	headerLines, messageMatchLines := m.renderHeaderBlock(diffWidth)
	lines = append(lines, headerLines...)
	lines = append(lines, blankRow(diffWidth))

	statsStart := len(lines)
	statLines := statLinesFor(m.detail.Files, diffWidth)
	if len(statLines) > 0 {
		lines = append(lines, statLines...)
		lines = append(lines, blankRow(diffWidth))
	}

	diffStart := len(lines)
	diffLines, fileRanges, diffMatchLines := m.renderDiffBlock(diffWidth)
	lines = append(lines, diffLines...)

	m.lines = lines
	m.content, m.lineStart = renderRows(lines, m.cursorLine)
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
	m.diffMatchLines = make([]int, len(diffMatchLines))
	for i, ln := range diffMatchLines {
		m.diffMatchLines[i] = m.rowOf(diffStart + ln)
	}

	m.leftVP.Width = diffWidth
	m.leftVP.Height = m.height
	if len(m.fileRanges) > 0 {
		m.leftVP.Height--
	}
	m.leftVP.Height = max(m.leftVP.Height, 0)
	// viewport.SetYOffset clamps to the content's own max scroll position,
	// which would leave a short trailing file's landing line (see
	// sectionContentStart) unreachable as the top-of-viewport line. Pad with
	// blank lines so jumpToSection can always bring the last file's content
	// to the top. contentLen, captured above, excludes this padding, so G
	// and the clamp below bound themselves to the real content.
	if len(m.fileRanges) > 0 {
		lastStart := m.fileRanges[len(m.fileRanges)-1].StartLine
		if need := lastStart + 1 + m.leftVP.Height - len(m.content); need > 0 {
			m.content = append(m.content, make([]string, need)...)
		}
	}

	// A resize can shrink contentLen out from under an already-set
	// cursorLine — clamp it back in range and paint the cursor onto wherever
	// it ended up, since renderRows above was given a position that no
	// longer exists.
	if clamped := clamp(m.cursorLine, 0, m.contentLen-1); clamped != m.cursorLine {
		m.cursorLine = clamped
		m.patchLine(m.cursorLine, logicalAt(m.lineStart, m.cursorLine))
	}

	m.setContent()
	m.syncHighlightFromScroll()
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

// setContent hands the cached rows to the viewport. The join is O(total
// content), forced by viewport.SetContent's API (it re-splits internally);
// what the cache saves is the styling upstream of it.
func (m *DetailModel) setContent() {
	m.leftVP.SetContent(strings.Join(m.content, "\n"))
}

// repaintCursor re-renders the logical line the cursor left (from) and the
// one it now sits on, splicing both into the cached content, so moving the
// cursor through a large diff doesn't re-render every line of it. Falls back
// to a full reflow when the cache isn't what the current lines would have
// produced.
func (m *DetailModel) repaintCursor(from int) {
	if len(m.lineStart) == 0 || len(m.content) < m.contentLen {
		m.reflow()
		return
	}
	cursor := logicalAt(m.lineStart, m.cursorLine)
	m.patchLine(from, cursor)
	m.patchLine(m.cursorLine, cursor)
	m.setContent()
}

// patchLine re-renders the logical line containing row pos, with the cursor
// row's background when that line is cursor. A no-op for a row outside the
// real content, so callers can pass a stale cursor position without
// special-casing it.
func (m *DetailModel) patchLine(pos, cursor int) {
	i := logicalAt(m.lineStart, pos)
	if i < 0 || pos >= m.contentLen {
		return
	}
	copy(m.content[m.lineStart[i]:], m.lines[i](i == cursor))
}

// renderDiffBlock describes this commit's diff (or the noDiffNotice
// placeholder for a diff-less commit) at width, honoring the current search
// state. Its returned coordinates count logical lines from the block's own
// start — see reflow, which rebases them.
func (m *DetailModel) renderDiffBlock(width int) (lines []lineRenderer, fileRanges []FileRange, matchLines []int) {
	if len(m.detail.Files) == 0 {
		return []lineRenderer{clippedLine(binaryNoticeStyle, noDiffNotice(m.detail), width)}, nil, nil
	}
	return RenderDiff(m.detail.Files, width, m.diffMatches, m.activeMatchIn(false), m.searchDismissed)
}

// renderHeaderBlock describes the commit metadata/message block at width,
// honoring the current search state. Its matchLines are logical line
// indices, which for this block are also indices into headerLinesFor's own
// lines (one logical line each, however many rows they wrap to).
func (m *DetailModel) renderHeaderBlock(width int) (lines []lineRenderer, matchLines []int) {
	headerLines, _, _, headerSpans := headerLinesFor(m.detail)
	return renderHeaderLines(headerLines, headerSpans, width, m.messageMatches, m.activeMatchIn(true), m.searchDismissed)
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

	// A pseudo entry (working tree/staged) has no commit metadata to show,
	// just a title and a dim description, and nothing eligible for message
	// search — hence authorLine = -1.
	if d.IsPseudo() {
		desc := entryDescription(d.Kind)
		lines = append(lines, entryTitle(d.Kind), desc)
		spans[1] = []baseSpan{{start: 0, end: len(desc), style: dimStyle}}
		return lines, -1, len(lines), spans
	}

	const commitPrefix = "Commit: "
	lines = append(lines, commitPrefix+d.Hash)
	spans[0] = []baseSpan{{start: len(commitPrefix), end: len(commitPrefix) + len(d.Hash), style: shaStyle}}

	if d.IsMerge() {
		const mergePrefix = "Merge:  "
		lines = append(lines, mergePrefix+strings.Join(d.Parents, " "))
		mergeSpans := make([]baseSpan, 0, len(d.Parents))
		col := len(mergePrefix)
		for i, p := range d.Parents {
			mergeSpans = append(mergeSpans, baseSpan{start: col, end: col + len(p), style: shaStyle})
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
	spans[authorLine] = []baseSpan{{start: len(authorPrefix), end: len(authorPrefix) + len(d.AuthorName), style: authorStyle}}

	const datePrefix = "Date:   "
	dateText := d.AuthorDate.Local().Format("2006-01-02 15:04:05 -0700")
	lines = append(lines, datePrefix+dateText)
	spans[len(lines)-1] = []baseSpan{{start: len(datePrefix), end: len(datePrefix) + len(dateText), style: dateStyle}}

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
	matchLines = make([]int, len(matches))
	mi := 0
	for li, line := range lines {
		var lineMatches []lineMatch
		for mi < len(matches) && matches[mi].LineIndex == li {
			if !dismissed {
				lineMatches = append(lineMatches, lineMatch{
					start: matches[mi].StartCol, end: matches[mi].EndCol, active: mi == activeIdx,
				})
			}
			matchLines[mi] = li
			mi++
		}
		lineSpans := spans[li]
		rendered = append(rendered, func(cursor bool) []string {
			return wrapStyledRows(line, lineSpans, lineMatches, lipgloss.NewStyle(), width, cursor)
		})
	}
	return rendered, matchLines
}

// MessageMatch identifies one case-insensitive occurrence of a search term
// within a single pre-wrap header/message line (see headerLinesFor):
// LineIndex indexes into that line slice, not diff content.
type MessageMatch struct {
	LineIndex        int
	StartCol, EndCol int
}

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
		m.moveCursor(-halfPage(m.leftVP.Height))
	case key.Matches(msg, DefaultKeyMap.HalfPageDown):
		m.moveCursor(halfPage(m.leftVP.Height))
	case key.Matches(msg, DefaultKeyMap.Top):
		m.jumpCursor(0)
	case key.Matches(msg, DefaultKeyMap.Bottom):
		m.jumpCursor(m.contentLen - 1)
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
func (m *DetailModel) moveCursor(delta int) {
	pos := m.cursorLine + delta
	if delta > 0 {
		pos = max(pos, m.blockEnd(m.cursorLine))
	}
	m.setCursor(pos)
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
	from := m.cursorLine
	m.cursorLine = m.blockStart(clamp(pos, 0, m.contentLen-1))
	m.repaintCursor(from)
	m.ensureCursorVisible()
	m.syncHighlightFromScroll()
}

// ensureCursorVisible scrolls the diff panel so the cursor line stays within
// the visible viewport. A wrapped line is highlighted as a whole block, so
// it's the block's last row that has to come into view when moving down —
// except when the block is taller than the viewport, where showing its start
// is the best that can be done.
func (m *DetailModel) ensureCursorVisible() {
	if m.leftVP.Height <= 0 {
		return
	}
	last := m.blockEnd(m.cursorLine) - 1
	if last-m.cursorLine >= m.leftVP.Height {
		last = m.cursorLine
	}
	if m.cursorLine < m.leftVP.YOffset {
		m.leftVP.SetYOffset(m.cursorLine)
	} else if last >= m.leftVP.YOffset+m.leftVP.Height {
		m.leftVP.SetYOffset(last - m.leftVP.Height + 1)
	}
}

// pushJump records pos — the cursor line about to be left — onto the shared
// jumplist, tagged with this model's own *git.CommitDetail so a later
// cross-view jump knows which commit/entry to reconstruct.
func (m *DetailModel) pushJump(pos int) {
	m.jl.push(jumpPos{view: viewDetail, commit: m.detail, cursorLine: pos})
}

// jumpToSection scrolls the diff panel directly to the section
// currentSection+delta away, clamping at the first/last section (no
// wraparound), and moves the cursor there too. A no-op on a diff-less commit,
// where the commit section is the only one there is.
func (m *DetailModel) jumpToSection(delta int) {
	if len(m.sections) <= 1 {
		return
	}
	target := clamp(m.currentSection+delta, 0, len(m.sections)-1)
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
	from := m.cursorLine
	m.cursorLine = m.blockStart(line)
	m.repaintCursor(from)
	m.leftVP.SetYOffset(line)
	m.syncHighlightFromScroll()
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

// syncHighlightFromScroll recomputes which section the diff panel's topmost
// visible line belongs to, for the sticky header.
func (m *DetailModel) syncHighlightFromScroll() {
	if len(m.sections) == 0 {
		return
	}
	m.currentSection = sectionAt(m.sections, m.leftVP.YOffset)
}

// computeMatches finds every occurrence of term across this commit's header
// and message (see headerLinesFor) and its diff content, message matches
// first, without moving the cursor or marking any match current. Jumping to a
// match is jumpToFirstMatch's separate job, so a view that opens with a
// search already active highlights without landing the cursor mid-diff.
func (m *DetailModel) computeMatches(term string) {
	if term == "" || m.detail == nil {
		m.messageMatches, m.diffMatches = nil, nil
	} else {
		lines, authorLine, bodyStart, _ := headerLinesFor(m.detail)
		m.messageMatches = FindMessageMatches(lines, authorLine, bodyStart, term)
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

func (m DetailModel) totalMatches() int { return len(m.messageMatches) + len(m.diffMatches) }

// matchSegment reports which half of the combined message+diff match sequence
// matchPos falls in, and its position within that segment. Only meaningful
// for matchPos >= 0; callers must guard the "no active match" case themselves
// before indexing with idx.
func (m DetailModel) matchSegment() (inMessage bool, idx int) {
	if m.matchPos < len(m.messageMatches) {
		return true, m.matchPos
	}
	return false, m.matchPos - len(m.messageMatches)
}

// activeMatchIn resolves which match one of the two rendered blocks should
// style as the active (n/N-selected) one: an index into that block's own
// match slice, or -1 when the active match is in the other block, there is
// none yet, or the search is dismissed.
func (m DetailModel) activeMatchIn(message bool) int {
	if m.searchDismissed || m.matchPos < 0 {
		return -1
	}
	if inMessage, idx := m.matchSegment(); inMessage == message {
		return idx
	}
	return -1
}

// NextMatch and PrevMatch cycle to the following/preceding match across the
// combined message+diff sequence, wrapping at both ends, and report the new
// 1-based position and total count.
func (m *DetailModel) NextMatch() (pos, total int) { return m.stepMatch(1) }
func (m *DetailModel) PrevMatch() (pos, total int) { return m.stepMatch(-1) }

func (m *DetailModel) stepMatch(delta int) (pos, total int) {
	total = m.totalMatches()
	if total == 0 {
		return 0, 0
	}
	m.setMatchPos(stepMatchPos(m.matchPos, delta, total))
	m.jumpToMatch()
	return m.matchPos + 1, total
}

// jumpToMatch scrolls the diff panel to the current match, whether it's a
// message match or a diff match. It renders nothing itself: setMatchPos's
// reflow already styled the newly active match and produced the match lines
// read here, so the landing is an ordinary cursor move.
//
// Every step pushes a jump, not just the initial search — see
// applyCurrentMatch in list.go.
func (m *DetailModel) jumpToMatch() {
	if m.matchPos < 0 {
		return
	}
	var line int
	if inMessage, idx := m.matchSegment(); inMessage {
		line = m.messageMatchLines[idx]
	} else if idx < len(m.diffMatchLines) {
		line = m.diffMatchLines[idx]
	} else {
		return
	}
	m.landOnLine(line)
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

// noDiffNotice explains an empty diff, naming the CLI pathspec (see
// StartTarget.Paths) when there is one, so "nothing here" can't be misread as
// "nothing anywhere". It returns plain text: renderDiffBlock hands it to
// clippedLine, which styles and truncates it to one row like every other
// content line.
func noDiffNotice(d *git.CommitDetail) string {
	paths, scope := strings.Join(d.Paths, ", "), ""
	if paths != "" {
		scope = " in " + paths
	}
	switch d.Kind {
	case git.EntryStaged:
		return "No staged changes" + scope + "."
	case git.EntryWorktree:
		return "No uncommitted changes" + scope + "."
	}
	if d.IsMerge() {
		return "Merge commit: diff not shown (viewing merge diffs isn't supported yet)."
	}
	if paths != "" {
		return "No changes to " + paths + " in this commit."
	}
	return "No changes in this commit."
}

func (m DetailModel) View() string {
	if m.detail == nil {
		return ""
	}
	if len(m.fileRanges) == 0 {
		return m.leftVP.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderStickyHeader(), m.leftVP.View())
}

// renderStickyHeader renders the sticky row's title: "Commit" for the
// metadata/message block, statsSectionTitle for the stats block, or a diff
// file's header.
//
// Looked up one line behind YOffset, not from currentSection: a section's own
// header row renders identically to its sticky title, so switching a line
// early would show that title twice. sectionContentStart accounts for this,
// so a [ / ] jump still shows the new title immediately.
func (m DetailModel) renderStickyHeader() string {
	s := m.sections[sectionAt(m.sections, m.leftVP.YOffset-1)]
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
