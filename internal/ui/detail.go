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
	// used for the sticky header's title and for [ / ] navigation. It's
	// built from fileRanges (for the file entries) plus the header/stats
	// boundaries already computed in reflow, so it's never present without
	// fileRanges also being present (see the len(m.fileRanges) == 0 guards
	// below, which double as "is there anything to be sticky about").
	sections       []detailSection
	currentSection int

	leftVP viewport.Model

	width, height int

	// displayTerm and searchDismissed mirror the root Model's canonical search
	// state (searchTerm/searchDismissed in app.go), pushed down via
	// SetSearchDisplay — see the comment on ListModel's own copies for why a
	// local mirror exists at all. displayTerm is what messageMatches/
	// diffMatches below were computed from: SetSearchDisplay recomputes them
	// whenever it changes, which is the one place that keeps a match set from
	// outliving the term it belongs to.
	displayTerm     string
	searchDismissed bool

	// messageMatches and diffMatches together form one ordered sequence
	// (message matches first, in document order, then diff matches in
	// existing file/hunk/line order — where each file's own name match, if
	// any, sorts ahead of its line matches, matching its header row's
	// position above its hunks) that matchPos indexes into, so n/N sweeps a
	// commit top to bottom the way you'd read it.
	messageMatches []MessageMatch
	diffMatches    []DiffMatch
	matchPos       int // index into the combined sequence; -1 when there are none

	messageMatchLines []int // parallel to messageMatches: wrapped-line index within the header block
	diffMatchLines    []int // parallel to diffMatches: wrapped-line index within the flattened content (offset applied)

	// cursorLine is a tig-style line cursor: an index into the same
	// flattened content line space leftVP's scroll offset lives in (see
	// contentLen). Distinct from leftVP.YOffset the way ListModel.cursor is
	// distinct from vp.YOffset — movement keys move the cursor, and the
	// viewport follows it (see setCursor's ensureCursorVisible call), not
	// the reverse. Re-clamped to [0, contentLen-1] at the end of every
	// reflow, so a resize that changes word-wrapping (and therefore shifts
	// what each flattened line index means) can't leave it out of bounds —
	// a known, accepted approximation: the cursor's *position* survives a
	// resize, but not necessarily its intended *target*, the same tradeoff
	// jumpBack/jumpForward positions (below) make.
	cursorLine int
	// contentLen is reflow's most recent flattened content length, cached
	// so setCursor/jumpCursor can clamp without threading it through as a
	// parameter. Deliberately excludes the trailing blank padding rows
	// reflow appends past the real content so jumpToSection can always
	// scroll a short trailing file's header to the very top of the
	// viewport — the cursor has no business visiting that padding (see
	// reflow, where this is captured before the padding is appended).
	contentLen int
	// lineStarts is parallel to that content: true where a flattened line
	// begins a logical line, false on the continuation rows a word-wrapped
	// one spilled into. The cursor only ever rests on a start (setCursor
	// snaps), and moves a whole logical line at a time — a wrapped line is
	// highlighted as one block, so stepping row by row through it would
	// leave presses looking like they did nothing.
	lineStarts []bool

	// jl is the shared, cross-view jumplist (vim's ctrl-o/ctrl-i) — see its
	// doc comment in app.go for why it lives on the root Model rather than
	// here. pushJump below is this model's only interaction with it: actual
	// JumpBack/JumpForward key handling lives on the root Model instead,
	// since popping an entry can require switching the active view.
	jl *jumplist
}

// sectionKind distinguishes the three kinds of entry that can appear in
// DetailModel.sections, since only sectionKindFile carries a fileIndex into
// m.detail.Files — the other two always render a fixed title.
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
// sections are contiguous and gapless in document order (unlike FileRange,
// which only covers diff files), so the answer is simply the last section
// whose startLine doesn't exceed lineIndex — clamped to the first section
// for any line above it (there is none, since section 0 always starts at
// 0) and to the last section for any line past the final one (e.g. the
// bottom-padding rows reflow adds past the last file's content).
func sectionAt(sections []detailSection, lineIndex int) int {
	if len(sections) == 0 {
		return -1
	}
	idx := sort.Search(len(sections), func(i int) bool { return sections[i].startLine > lineIndex }) - 1
	return clamp(idx, 0, len(sections)-1)
}

// NewDetailModel constructs a detail view for the given commit, sharing jl —
// the root Model's single cross-view jumplist — for pushJump to record into.
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

	// The header/message is wrapped the same way diff lines are (each
	// visual row its own entry) so the line offset applied to fileRanges
	// below stays exact regardless of terminal width — see the FileRange
	// convention in diffview.go. Each block is rendered immediately before
	// it's added, so where it lands (b.len()) is also where its own local
	// line space starts — which is what the cursor coordinate handed to it
	// has to be measured from.
	var b contentBuilder
	wrappedHeader, messageMatchLines, headerStarts := m.renderHeaderBlock(diffWidth)
	b.addBlock(wrappedHeader, headerStarts)
	b.addLines(blankLine(diffWidth, m.cursorLine == b.len()))

	statLines := statLinesFor(m.detail.Files, diffWidth, m.cursorLine-b.len())
	if len(statLines) > 0 {
		b.addLines(statLines...)
		b.addLines(blankLine(diffWidth, m.cursorLine == b.len()))
	}

	offset := b.len()
	diffLines, fileRanges, rawDiffMatchLines, diffStarts := m.renderDiffBlock(diffWidth, m.cursorLine-offset)
	b.addBlock(diffLines, diffStarts)

	content := b.lines
	m.lineStarts = b.starts

	m.fileRanges = make([]FileRange, len(fileRanges))
	for i, r := range fileRanges {
		m.fileRanges[i] = FileRange{FileIndex: r.FileIndex, StartLine: r.StartLine + offset, EndLine: r.EndLine + offset}
	}
	m.sections = buildSections(m.fileRanges, len(wrappedHeader), len(statLines) > 0, offset)

	m.messageMatchLines = messageMatchLines
	m.diffMatchLines = make([]int, len(rawDiffMatchLines))
	for i, ln := range rawDiffMatchLines {
		m.diffMatchLines[i] = ln + offset
	}

	m.leftVP.Width = diffWidth
	m.leftVP.Height = m.height
	if len(m.fileRanges) > 0 {
		m.leftVP.Height--
	}
	m.leftVP.Height = max(m.leftVP.Height, 0)
	// contentLen is captured here, before the padding appended below: that
	// padding exists only so the viewport can scroll a short trailing
	// file's header to the very top (see the comment on the append just
	// below), and the cursor has no business venturing into it — G, and
	// the end-of-reflow clamp further down, both bound themselves to this
	// value, not to len(content) after padding.
	m.contentLen = len(content)
	// viewport.SetYOffset clamps to the content's own max scroll position,
	// which is normally what you want (no blank space below the last
	// line) — but it also means a short trailing file's landing line (see
	// sectionContentStart: one line past its header) can be unreachable as
	// the top-of-viewport line if what follows it doesn't fill a whole
	// screen. Pad with blank lines so jumpToSection can always bring the
	// last file's content to the top, the same way editors let you scroll
	// the last line of a file to the top of the screen.
	if len(m.fileRanges) > 0 {
		lastStart := m.fileRanges[len(m.fileRanges)-1].StartLine
		if need := lastStart + 1 + m.leftVP.Height - len(content); need > 0 {
			content = append(content, make([]string, need)...)
		}
	}
	m.leftVP.SetContent(strings.Join(content, "\n"))

	// A resize can shrink contentLen out from under an already-set
	// cursorLine (rewrapping changes how many flattened lines the same
	// content occupies) — clamp it back in range. See cursorLine's doc
	// comment for why this doesn't try to preserve the cursor's original
	// target across a rewrap, only its validity.
	m.cursorLine = clamp(m.cursorLine, 0, m.contentLen-1)

	m.syncHighlightFromScroll()
}

// contentBuilder accumulates one reflow pass's flattened content and its
// parallel line-start map (see DetailModel.lineStarts) together, so the two
// can't drift: every row that goes into one goes into the other in the same
// call. len() doubles as "where does the next block start", which is the
// coordinate a block's own cursor line and its FileRanges are measured from.
type contentBuilder struct {
	lines  []string
	starts []bool
}

// addBlock appends a block that word-wraps internally, carrying its own
// line-start map (renderHeaderLines' and RenderDiff's `starts` returns).
func (b *contentBuilder) addBlock(rows []string, starts []bool) {
	b.lines = append(b.lines, rows...)
	b.starts = append(b.starts, starts...)
}

// addLines appends rows that are each a logical line of their own: the blank
// separators between blocks, and the stats rows, which are clipped to width
// rather than wrapped (see renderClipped).
func (b *contentBuilder) addLines(rows ...string) {
	b.lines = append(b.lines, rows...)
	for range rows {
		b.starts = append(b.starts, true)
	}
}

func (b *contentBuilder) len() int { return len(b.lines) }

// renderDiffBlock renders this commit's diff (or the noDiffNotice
// placeholder for a diff-less commit) at width, honoring the current
// search state so the active diff match, if any, is styled distinctly.
// cursorLine is already translated into this block's own local line space
// (see reflow) — negative or past the end simply selects no line, so
// callers don't need to guard the call themselves.
func (m *DetailModel) renderDiffBlock(width int, cursorLine int) (lines []string, fileRanges []FileRange, matchLines []int, starts []bool) {
	if len(m.detail.Files) == 0 {
		return []string{noDiffNotice(m.detail)}, nil, nil, []bool{true}
	}
	return RenderDiff(m.detail.Files, width, m.diffMatches, m.activeMatchIn(false), m.searchDismissed, cursorLine)
}

// renderHeaderBlock renders the commit metadata/message block at width,
// honoring the current search state so the active message match, if any,
// is styled distinctly. m.cursorLine is passed through unadjusted: the
// header block always starts at flattened line 0, so its local line space
// already matches m.cursorLine's — a cursor position past the header
// simply exceeds every header line's range and selects nothing here (see
// renderHeaderLines).
func (m *DetailModel) renderHeaderBlock(width int) (wrapped []string, matchLines []int, starts []bool) {
	headerLines, _, _, headerSpans := headerLinesFor(m.detail)
	return renderHeaderLines(headerLines, headerSpans, width, m.messageMatches, m.activeMatchIn(true), m.searchDismissed, m.cursorLine)
}

// buildSections assembles one reflow pass's sticky-header/[ ]-navigation
// sections: the commit metadata block, optionally the stats block, then
// each already offset-adjusted file range, in document order. headerLen is
// len(wrappedHeader); offset is the flattened line index where diff
// content begins (see reflow).
func buildSections(fileRanges []FileRange, headerLen int, hasStats bool, offset int) []detailSection {
	metaEnd := offset
	if hasStats {
		metaEnd = headerLen + 1
	}
	sections := make([]detailSection, 0, len(fileRanges)+2)
	sections = append(sections, detailSection{kind: sectionKindCommit, startLine: 0, endLine: metaEnd})
	if hasStats {
		sections = append(sections, detailSection{kind: sectionKindStats, startLine: metaEnd, endLine: offset})
	}
	for _, r := range fileRanges {
		sections = append(sections, detailSection{kind: sectionKindFile, fileIndex: r.FileIndex, startLine: r.StartLine, endLine: r.EndLine})
	}
	return sections
}

// headerLinesFor builds the commit header's pre-wrap logical lines, one
// entry per line (wrapping happens later, in renderHeaderLines, so its
// matchLines bookkeeping stays exact — the same convention RenderDiff/
// renderDiffLine use for diff content).
//
// authorLine and bodyStart mark the two regions eligible for search — the
// author line and the message body — matching what
// internal/git.SearchCommitHashes matches for list search's author/message
// fields, deliberately excluding the hash/parent/date metadata lines: list
// search also matches the commit hash (git.HashesMatching), but that's
// useful for finding a commit in the list, not for searching within a
// commit you're already viewing. spans (keyed by line index) marks the
// hash/name/date substrings that get base coloring regardless of search
// state.
//
// The body is indented 4 spaces, matching `git show`'s own convention.
func headerLinesFor(d *git.CommitDetail) (lines []string, authorLine, bodyStart int, spans map[int][]baseSpan) {
	spans = make(map[int][]baseSpan)

	// A pseudo entry (working tree/staged) has no commit metadata to show —
	// just a title and a dim description — and nothing eligible for message
	// search, hence authorLine = -1 (see FindMessageMatches's guard).
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

// renderHeaderLines word-wraps each logical header/message line to width via
// wrapStyledRows, which applies base coloring (spans, from headerLinesFor)
// and search-match highlighting per wrapped row — the wrap itself decided on
// the plain text, exactly like RenderDiff/renderDiffLine. matchLines is
// parallel to matches: the wrapped-line index each match's logical line
// starts at, computed from the full matches list regardless of dismissed
// (see RenderDiff's doc comment for why suppressing style doesn't need
// suppressing position tracking) — spans, unlike matches, are never
// suppressed by dismissed, since they're not part of search at all. starts
// marks which of the wrapped rows begin a logical line, exactly like
// RenderDiff's own.
//
// cursorLine gets the cursor row background merged in wherever it lands,
// using the same "render once plain to learn the wrapped-row range, then
// once more with the cursor flag if it's actually in range" two-pass
// convention RenderDiff/renderDiffLine use: the two passes now always
// produce the same row count (see wrapRanges), so the range learned from
// the first still describes the second.
func renderHeaderLines(lines []string, spans map[int][]baseSpan, width int, matches []MessageMatch, activeIdx int, dismissed bool, cursorLine int) (wrapped []string, matchLines []int, starts []bool) {
	matchLines = make([]int, len(matches))
	mi := 0
	for li, line := range lines {
		lineStart := len(wrapped)
		var lineMatches []lineMatch
		for mi < len(matches) && matches[mi].LineIndex == li {
			if !dismissed {
				lineMatches = append(lineMatches, lineMatch{
					start: matches[mi].StartCol, end: matches[mi].EndCol, active: mi == activeIdx,
				})
			}
			matchLines[mi] = lineStart
			mi++
		}
		render := func(cursor bool) []string {
			return wrapStyledRows(line, spans[li], lineMatches, lipgloss.NewStyle(), width, cursor)
		}
		rows := render(false)
		if cursorLine >= lineStart && cursorLine < lineStart+len(rows) {
			rows = render(true)
		}
		wrapped = append(wrapped, rows...)
		for i := range rows {
			starts = append(starts, i == 0)
		}
	}
	return wrapped, matchLines, starts
}

// MessageMatch identifies one case-insensitive occurrence of a search term
// within a single pre-wrap header/message line (see headerLinesFor):
// LineIndex indexes into that line slice, not diff content.
type MessageMatch struct {
	LineIndex        int
	StartCol, EndCol int
}

// FindMessageMatches returns every occurrence of term in the header lines
// eligible for search: the author line and every line from bodyStart
// onward (see headerLinesFor) — deliberately not the hash/parent/date
// lines (see headerLinesFor's doc comment for why hash is excluded here
// even though list search matches it).
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
// ctrl-u/ctrl-d, and g/G move the line cursor (see cursorLine) rather than
// scrolling directly — the viewport follows it (setCursor's
// ensureCursorVisible call), the tig-style relationship ListModel already
// has between its own cursor and vp.YOffset. [ / ] and g/G record a jumplist
// entry first when they actually move the cursor (see pushJump); plain
// j/k/ctrl-u/ctrl-d don't, matching vim's own jumplist (only "big" motions
// are jump-worthy). ctrl-o/ctrl-i themselves aren't handled here — see the
// root Model's doJumpBack/doJumpForward. Enter jumps straight to a file's
// diff section when the cursor is on that file's row in the "Files
// changed" stats block (see statsFileIndexAt/jumpToFile) and is a no-op
// anywhere else, including on a file's own header row inside the diff.
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
// entry — plain motion, not a jump (see Update's doc comment).
//
// Downward motion never lands short of the next logical line: setCursor
// snaps back to the start of whatever block it's given, so a plain j inside
// a line that wrapped to several rows would otherwise snap right back where
// it started and look like a dead key.
func (m *DetailModel) moveCursor(delta int) {
	pos := m.cursorLine + delta
	if delta > 0 {
		pos = max(pos, m.blockEnd(m.cursorLine))
	}
	m.setCursor(pos)
}

// blockStart returns the first flattened line of the logical line pos falls
// in; blockEnd returns one past its last. Both fall back to pos itself when
// there's no line-start map yet (a model that hasn't reflowed), so movement
// still degrades to plain per-row stepping rather than getting stuck.
func (m *DetailModel) blockStart(pos int) int {
	if pos < 0 || pos >= len(m.lineStarts) {
		return pos
	}
	for pos > 0 && !m.lineStarts[pos] {
		pos--
	}
	return pos
}

func (m *DetailModel) blockEnd(pos int) int {
	if pos < 0 || pos >= len(m.lineStarts) {
		return pos + 1
	}
	end := pos + 1
	for end < len(m.lineStarts) && !m.lineStarts[end] {
		end++
	}
	return end
}

// jumpCursor moves the cursor straight to line, recording the jump first
// (see pushJump) unless line is where the cursor already is — a no-op jump
// has nothing worth returning to. Used for g/G; [ / ] and search jumps
// instead go through jumpToSection/jumpToMatch, which land the target at
// the very top of the viewport rather than merely scrolling it into view
// (see setCursor vs. those two's direct leftVP.SetYOffset).
func (m *DetailModel) jumpCursor(line int) {
	if clamped := clamp(line, 0, m.contentLen-1); clamped != m.cursorLine {
		m.pushJump(m.cursorLine)
	}
	m.setCursor(line)
}

// setCursor moves the cursor to pos (clamped in range, and snapped back to
// the start of the logical line it lands in — see lineStarts), re-renders so
// the cursor highlight lands on the right line, then scrolls the viewport by
// the minimum needed to keep the cursor visible — unlike jumpToSection/
// jumpToMatch, which always land their target at the very top.
func (m *DetailModel) setCursor(pos int) {
	m.cursorLine = m.blockStart(clamp(pos, 0, m.contentLen-1))
	m.reflow()
	m.ensureCursorVisible()
	m.syncHighlightFromScroll()
}

// ensureCursorVisible scrolls the diff panel so the cursor line stays
// within the visible viewport, the DetailModel analogue of ListModel's
// ensureVisible. A wrapped line is highlighted as a whole block, so it's the
// block's last row that has to come into view when moving down — except when
// the block is taller than the viewport itself, where showing its start is
// the best that can be done.
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
// jumplist (see jl's doc comment), tagged with this model's own *git.
// CommitDetail so a later cross-view JumpBack/JumpForward can tell which
// commit/entry to reconstruct.
func (m *DetailModel) pushJump(pos int) {
	m.jl.push(jumpPos{view: viewDetail, commit: m.detail, cursorLine: pos})
}

// jumpToSection scrolls the diff panel directly to the section
// currentSection+delta away (commit metadata, the stats block, or a diff
// file — see DetailModel.sections), clamping at the first/last section (no
// wraparound), and moves the cursor there too. A no-op on a diff-less
// commit, where the commit section is the only one there is.
func (m *DetailModel) jumpToSection(delta int) {
	if len(m.sections) <= 1 {
		return
	}
	target := clamp(m.currentSection+delta, 0, len(m.sections)-1)
	m.landOnLine(sectionContentStart(m.sections[target]))
}

// jumpToFile scrolls the diff panel directly to the diff section for
// fileIndex, the same landing point jumpToSection uses when reached via
// [ / ] — used by the Enter-on-a-stats-row action (see statsFileIndexAt).
// A no-op if fileIndex has no corresponding section (shouldn't happen: a
// caller-side bounds check against m.detail.Files already guards this).
func (m *DetailModel) jumpToFile(fileIndex int) {
	for _, s := range m.sections {
		if s.kind == sectionKindFile && s.fileIndex == fileIndex {
			m.landOnLine(sectionContentStart(s))
			return
		}
	}
}

// statsFileIndexAt reports which file, if any, the stats block's row at
// line corresponds to — matching statLinesFor's own local line convention
// (0 = the "Files changed" heading, 1..len(files) = one row per file) —
// so ok is false for the heading row, the blank separator, and the totals
// row, as well as for any line outside the stats block entirely.
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
// moves the cursor (see pushJump) — the "land the target at the very top
// of the viewport" behavior shared by jumpToSection and jumpToFile, as
// opposed to setCursor's minimal-scroll-to-keep-visible behavior.
func (m *DetailModel) landOnLine(line int) {
	if line != m.cursorLine {
		m.pushJump(m.cursorLine)
	}
	m.cursorLine = m.blockStart(line)
	m.reflow()
	m.leftVP.SetYOffset(line)
	m.syncHighlightFromScroll()
}

// sectionContentStart returns the scroll position jumpToSection lands on:
// one line past the section's own header/heading row for the stats block
// and file sections, since that row would otherwise be indistinguishable
// from the sticky bar showing the exact same title once landed there (see
// renderStickyHeader) — for the commit section, its first content line
// ("commit <hash>") isn't a literal duplicate of the sticky title
// ("Commit"), so it still lands exactly at startLine (0).
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
// and message (the author line and body — see headerLinesFor's doc comment
// for why this deliberately excludes the hash, unlike list search) and its
// diff content, message matches first in document order and diff matches
// after (see messageMatches's doc comment) — without moving the cursor or
// marking any match "current" (matchPos stays -1). Called only from
// SetSearchDisplay, which owns the term this is keyed to; jumping to a match
// is jumpToFirstMatch's separate job, since a view that opens (or is jumped
// back to) with a search already active should highlight without landing the
// cursor in the middle of a diff.
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
// being SetSearchDisplay's. Unlike list search this is fully synchronous: the
// whole commit is already in memory once it's open, so there's no async
// loading to wait for.
func (m *DetailModel) jumpToFirstMatch() {
	if m.totalMatches() > 0 {
		m.matchPos = 0
	}
	m.jumpToMatch()
}

func (m DetailModel) totalMatches() int { return len(m.messageMatches) + len(m.diffMatches) }

// matchSegment reports which half of the combined message+diff match
// sequence matchPos falls in (see messageMatches's doc comment), and idx's
// position within that segment — the one place that boundary arithmetic
// lives, instead of being reimplemented at each of its call sites. Only
// meaningful for matchPos >= 0; callers with a "no active match" case
// (matchPos < 0) must still guard that separately before indexing with idx.
func (m DetailModel) matchSegment() (inMessage bool, idx int) {
	if m.matchPos < len(m.messageMatches) {
		return true, m.matchPos
	}
	return false, m.matchPos - len(m.messageMatches)
}

// activeMatchIn resolves which match one of the two rendered blocks should
// style as the active (n/N-selected) one: an index into that block's own
// match slice, or -1 for "none of mine" — because the active match is in the
// other block, because there is no active match yet, or because the search
// is dismissed, in which case no match is styled at all anywhere.
func (m DetailModel) activeMatchIn(message bool) int {
	if m.searchDismissed || m.matchPos < 0 {
		return -1
	}
	if inMessage, idx := m.matchSegment(); inMessage == message {
		return idx
	}
	return -1
}

// NextMatch and PrevMatch cycle to the following/preceding match (across
// the combined message+diff sequence), wrapping at both ends, and report
// the new 1-based position and total count.
func (m *DetailModel) NextMatch() (pos, total int) { return m.stepMatch(1) }
func (m *DetailModel) PrevMatch() (pos, total int) { return m.stepMatch(-1) }

func (m *DetailModel) stepMatch(delta int) (pos, total int) {
	total = m.totalMatches()
	if total == 0 {
		return 0, 0
	}
	m.matchPos = stepMatchPos(m.matchPos, delta, total)
	m.jumpToMatch()
	return m.matchPos + 1, total
}

// jumpToMatch scrolls the diff panel to the current match, whether it's a
// message match (within the header block) or a diff match, and re-renders
// so the newly active match gets its distinct style — that re-render is
// landOnLine's own reflow, which has to run anyway to bake the cursor
// highlight into the line being landed on. Re-running reflow at all on a
// match change (rather than patching just the affected lines) is the same
// cost already paid on every resize, and it's pure in-memory string work
// with no I/O — but paying it twice per n/N press isn't, so this reads the
// match's line out of the *previous* reflow rather than forcing a fresh one
// first.
//
// That's sound because messageMatchLines/diffMatchLines depend only on the
// match set and the wrap width, and every writer of either (SetSearchDisplay,
// SetSize) ends in a reflow — so they already describe the current matches by
// the time any of this model's match navigation can reach them.
//
// Every step pushes a jump, not just the initial search — see
// applyCurrentMatch's doc comment in list.go for why that's a deliberate
// departure from vim.
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
// currently dismissed, then re-renders. A changed term recomputes this
// commit's matches (synchronously — everything it needs is already in
// memory), which is what keeps them from outliving the term they were found
// for: the root Model pushes every search-state change down here, including
// ones made while some *other* view was active. Clearing the search is just
// the term going empty.
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

// noDiffNotice explains an empty diff. A view opened with a CLI pathspec
// (see StartTarget.Paths) names it, so "nothing here" can't be misread as
// "nothing anywhere" when the rest of the diff was simply filtered out.
func noDiffNotice(d *git.CommitDetail) string {
	paths, scope := strings.Join(d.Paths, ", "), ""
	if paths != "" {
		scope = " in " + paths
	}
	switch d.Kind {
	case git.EntryStaged:
		return binaryNoticeStyle.Render("No staged changes" + scope + ".")
	case git.EntryWorktree:
		return binaryNoticeStyle.Render("No uncommitted changes" + scope + ".")
	}
	if d.IsMerge() {
		return binaryNoticeStyle.Render("Merge commit: diff not shown (viewing merge diffs isn't supported yet).")
	}
	if paths != "" {
		return binaryNoticeStyle.Render("No changes to " + paths + " in this commit.")
	}
	return binaryNoticeStyle.Render("No changes in this commit.")
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

// renderStickyHeader renders the sticky row's title for "Commit" (the
// metadata/message block), statsSectionTitle (matching the stats block's
// own in-content heading, see statsview.go), or a diff file's header.
//
// Looked up one line behind YOffset, not currentSection: a section's own
// header row renders byte-for-byte identical to its sticky title, so
// showing it a line early would duplicate a title that's still on screen.
// jumpToSection's landing position (sectionContentStart) already accounts
// for this, so a [ / ] jump shows the new title immediately.
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
