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
// file is currently in view, updating as the user scrolls or jumps between
// files with [ / ]. While the scroll position is still within the
// header/message (above the first file), the first file is treated as
// "current" since that's where scrolling down will land next.
type DetailModel struct {
	detail *git.CommitDetail

	fileRanges  []FileRange
	currentFile int

	leftVP viewport.Model

	width, height int

	// searchDismissed mirrors the root Model's canonical search-dismissed
	// state (searchDismissed in app.go), pushed down via SetSearchDisplay —
	// see the comment on ListModel's displayTerm/searchDismissed fields for
	// why a local mirror exists at all. Unlike ListModel, DetailModel has no
	// use for the term itself: RunSearch already computed messageMatches/
	// diffMatches synchronously from it, so only dismissed state is needed
	// here.
	searchDismissed bool

	// messageMatches and diffMatches together form one ordered sequence
	// (message matches first, in document order, then diff matches in
	// existing file/hunk/line order) that matchPos indexes into, so n/N
	// sweeps a commit top to bottom the way you'd read it.
	messageMatches []MessageMatch
	diffMatches    []DiffMatch
	matchPos       int // index into the combined sequence; -1 when there are none

	messageMatchLines []int // parallel to messageMatches: wrapped-line index within the header block
	diffMatchLines    []int // parallel to diffMatches: wrapped-line index within the flattened content (offset applied)
}

// NewDetailModel constructs a detail view for the given commit.
func NewDetailModel(detail *git.CommitDetail) DetailModel {
	return DetailModel{
		detail:   detail,
		leftVP:   viewport.New(0, 0),
		matchPos: -1,
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

	var diffLines []string
	var fileRanges []FileRange
	var rawDiffMatchLines []int
	if len(m.detail.Files) == 0 {
		diffLines = []string{noDiffNotice(m.detail)}
	} else {
		diffActive := -1
		if !m.searchDismissed && m.matchPos >= len(m.messageMatches) {
			diffActive = m.matchPos - len(m.messageMatches)
		}
		diffLines, fileRanges, rawDiffMatchLines = RenderDiff(m.detail.Files, diffWidth, m.diffMatches, diffActive, m.searchDismissed)
	}

	// The header/message is wrapped the same way diff lines are (each
	// visual row its own entry) so the line offset applied to fileRanges
	// below stays exact regardless of terminal width — see the
	// FileRange/FileAt convention in diffview.go.
	headerLines, _, _, headerSpans := headerLinesFor(m.detail)
	msgActive := -1
	if !m.searchDismissed && m.matchPos >= 0 && m.matchPos < len(m.messageMatches) {
		msgActive = m.matchPos
	}
	wrappedHeader, messageMatchLines := renderHeaderLines(headerLines, headerSpans, diffWidth, m.messageMatches, msgActive, m.searchDismissed)
	statLines := statLinesFor(m.detail.Files, diffWidth)
	offset := len(wrappedHeader) + 1 + len(statLines)
	if len(statLines) > 0 {
		offset++
	}

	content := make([]string, 0, len(wrappedHeader)+1+len(statLines)+1+len(diffLines))
	content = append(content, wrappedHeader...)
	content = append(content, "")
	content = append(content, statLines...)
	if len(statLines) > 0 {
		content = append(content, "")
	}
	content = append(content, diffLines...)

	m.fileRanges = make([]FileRange, len(fileRanges))
	for i, r := range fileRanges {
		m.fileRanges[i] = FileRange{FileIndex: r.FileIndex, StartLine: r.StartLine + offset, EndLine: r.EndLine + offset}
	}

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
	if m.leftVP.Height < 0 {
		m.leftVP.Height = 0
	}
	// viewport.SetYOffset clamps to the content's own max scroll position,
	// which is normally what you want (no blank space below the last
	// line) — but it also means a short trailing file's StartLine can be
	// unreachable as the top-of-viewport line if what follows it doesn't
	// fill a whole screen. Pad with blank lines so jumpToFile can always
	// bring the last file's header to the top, the same way editors let
	// you scroll the last line of a file to the top of the screen.
	if len(m.fileRanges) > 0 {
		lastStart := m.fileRanges[len(m.fileRanges)-1].StartLine
		if need := lastStart + m.leftVP.Height - len(content); need > 0 {
			content = append(content, make([]string, need)...)
		}
	}
	m.leftVP.SetContent(strings.Join(content, "\n"))

	m.syncHighlightFromScroll()
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

// headerLinesFor builds the commit header's pre-wrap logical lines (one
// entry per line; the message body is included verbatim, unwrapped —
// wrapping happens later in renderHeaderLines so its matchLines bookkeeping
// stays exact, the same convention RenderDiff/renderDiffLine use for diff
// content). authorLine and bodyStart mark the two regions eligible for
// search — the author line and the message body — matching exactly what
// internal/git.SearchCommitHashes matches for list search (author name OR
// message), deliberately excluding the hash/parent/date metadata lines.
// spans (keyed by line index) marks the hash/name/date substrings that get
// base coloring regardless of search state — computed here, where each
// substring's byte offset is already known, rather than re-found later.
// The body is indented 4 spaces, matching real `git show`'s own convention
// of an indented subject/body under flush-left metadata; since both
// FindMessageMatches and renderHeaderLines read these same stored strings,
// the indentation is simply part of the line text from here on, with no
// separate column-shifting logic needed elsewhere.
func headerLinesFor(d *git.CommitDetail) (lines []string, authorLine, bodyStart int, spans map[int][]baseSpan) {
	spans = make(map[int][]baseSpan)

	const commitPrefix = "commit  "
	lines = append(lines, commitPrefix+d.Hash)
	spans[0] = []baseSpan{{start: len(commitPrefix), end: len(commitPrefix) + len(d.Hash), style: shaStyle}}

	if len(d.Parents) > 1 {
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

// renderHeaderLines word-wraps each logical header/message line to width,
// applying base coloring (spans, from headerLinesFor) and search-match
// highlighting together first — styling the plain pre-wrap text via
// renderLineWithBaseSpans so both survive wrapping (see CLAUDE.md's "style
// plain-text spans before any .Render() call" rule) — then wrapping the
// already-styled line via lipgloss, splitting on the resulting newlines
// exactly like RenderDiff/renderDiffLine do. matchLines is parallel to
// matches: the wrapped-line index each match's logical line starts at,
// computed from the full matches list regardless of dismissed (see
// RenderDiff's doc comment for why suppressing style doesn't need
// suppressing position tracking) — spans, unlike matches, are never
// suppressed by dismissed, since they're not part of search at all.
func renderHeaderLines(lines []string, spans map[int][]baseSpan, width int, matches []MessageMatch, activeIdx int, dismissed bool) (wrapped []string, matchLines []int) {
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
		content := line
		if len(spans[li]) > 0 || len(lineMatches) > 0 {
			content = renderLineWithBaseSpans(line, spans[li], lineMatches)
		}
		if width <= 0 {
			wrapped = append(wrapped, content)
			continue
		}
		wrapped = append(wrapped, strings.Split(lipgloss.NewStyle().Width(width).Render(content), "\n")...)
	}
	return wrapped, matchLines
}

// renderLineWithBaseSpans composites per-column base coloring (spans) with
// search-match spans (matches) into one already-styled string: walks the
// boundary points from both span lists, and for each resulting piece
// renders with whichever match style covers it, else whichever baseSpan
// covers it, else colorless — a match always wins over a base span on
// overlap (e.g. a search hit inside the author's name still shows the
// search highlight, not the name's blue).
func renderLineWithBaseSpans(text string, spans []baseSpan, matches []lineMatch) string {
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
				if m.active {
					return searchActiveMatchStyle
				}
				return searchMatchStyle
			}
		}
		for _, s := range spans {
			if pos >= s.start && pos < s.end {
				return s.style
			}
		}
		return lipgloss.NewStyle()
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
// lines, keeping scope consistent with internal/git.SearchCommitHashes.
func FindMessageMatches(lines []string, authorLine, bodyStart int, term string) []MessageMatch {
	if term == "" {
		return nil
	}
	var matches []MessageMatch
	if authorLine < len(lines) {
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

// Update handles a navigation key press against the diff viewport.
func (m *DetailModel) Update(msg tea.KeyMsg) {
	switch {
	case key.Matches(msg, DefaultKeyMap.Up):
		m.leftVP.LineUp(1)
	case key.Matches(msg, DefaultKeyMap.Down):
		m.leftVP.LineDown(1)
	case key.Matches(msg, DefaultKeyMap.HalfPageUp):
		m.leftVP.HalfPageUp()
	case key.Matches(msg, DefaultKeyMap.HalfPageDown):
		m.leftVP.HalfPageDown()
	case key.Matches(msg, DefaultKeyMap.Top):
		m.leftVP.GotoTop()
	case key.Matches(msg, DefaultKeyMap.Bottom):
		m.leftVP.GotoBottom()
	case key.Matches(msg, DefaultKeyMap.PrevFile):
		m.jumpToFile(-1)
	case key.Matches(msg, DefaultKeyMap.NextFile):
		m.jumpToFile(1)
	}
	m.syncHighlightFromScroll()
}

// jumpToFile scrolls the diff panel directly to the start of the file
// currentFile+delta files away, clamping at the first/last file (no
// wraparound).
func (m *DetailModel) jumpToFile(delta int) {
	if len(m.fileRanges) == 0 {
		return
	}
	target := clampIndex(m.currentFile+delta, len(m.fileRanges)-1)
	m.leftVP.SetYOffset(m.fileRanges[target].StartLine)
}

// syncHighlightFromScroll recomputes which file the diff panel's topmost
// visible line belongs to, for the sticky header. Above the first file
// (still within the header/commit message) FileAt has nothing to report,
// so it clamps to the first file, since that's what scrolling down reaches
// next.
func (m *DetailModel) syncHighlightFromScroll() {
	if len(m.fileRanges) == 0 {
		return
	}
	i := FileAt(m.fileRanges, m.leftVP.YOffset)
	if i < 0 {
		i = 0
	}
	m.currentFile = i
}

// RunSearch finds every occurrence of term across this commit's header and
// message (the author line and body — the same two fields
// internal/git.SearchCommitHashes matches for list search) and its diff
// content, message matches first in document order and diff matches after,
// and jumps to the first one, returning the total match count. Unlike list
// search this is fully synchronous: the whole commit is already in memory
// once it's open, so there's no async loading to wait for. The term/
// dismissed display state is set separately via SetSearchDisplay, since
// it's shared with the list view and owned by the root Model.
func (m *DetailModel) RunSearch(term string) int {
	if term == "" {
		m.messageMatches, m.diffMatches = nil, nil
	} else {
		lines, authorLine, bodyStart, _ := headerLinesFor(m.detail)
		m.messageMatches = FindMessageMatches(lines, authorLine, bodyStart, term)
		m.diffMatches = FindDiffMatches(m.detail.Files, term)
	}
	m.matchPos = -1
	if m.totalMatches() > 0 {
		m.matchPos = 0
	}
	m.jumpToMatch()
	return m.totalMatches()
}

func (m DetailModel) totalMatches() int { return len(m.messageMatches) + len(m.diffMatches) }

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
	m.matchPos = (m.matchPos + delta + total) % total
	m.jumpToMatch()
	return m.matchPos + 1, total
}

// jumpToMatch re-renders (so the newly active match gets its distinct
// style) and scrolls the diff panel to bring it into view, whether the
// current match is a message match (within the header block) or a diff
// match. Re-running reflow on every match change — rather than patching
// just the affected lines — is the same cost already paid on every resize,
// and it's pure in-memory string work with no I/O.
func (m *DetailModel) jumpToMatch() {
	m.reflow()
	switch {
	case m.matchPos < 0:
		return
	case m.matchPos < len(m.messageMatches):
		m.leftVP.SetYOffset(m.messageMatchLines[m.matchPos])
	default:
		if di := m.matchPos - len(m.messageMatches); di < len(m.diffMatchLines) {
			m.leftVP.SetYOffset(m.diffMatchLines[di])
		}
	}
	m.syncHighlightFromScroll()
}

// ClearSearch discards the active search's matches. The term/dismissed
// display state is cleared separately via SetSearchDisplay.
func (m *DetailModel) ClearSearch() {
	m.messageMatches, m.diffMatches = nil, nil
	m.matchPos = -1
	m.reflow()
}

// SetSearchDisplay updates whether highlighting is currently dismissed and
// re-renders. term is accepted (unused) only to match ListModel's
// SetSearchDisplay signature, since app.go calls both uniformly — matches
// here were already computed by RunSearch, so DetailModel has no use for
// the term itself.
func (m *DetailModel) SetSearchDisplay(term string, dismissed bool) {
	m.searchDismissed = dismissed
	m.reflow()
}

// MatchPosition reports the current match position (1-based) and total
// count for status-bar display.
func (m DetailModel) MatchPosition() (pos, total int) {
	return matchPosition(m.matchPos, m.totalMatches())
}

func noDiffNotice(d *git.CommitDetail) string {
	if len(d.Parents) > 1 {
		return binaryNoticeStyle.Render("Merge commit: diff not shown (viewing merge diffs isn't supported yet).")
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
	sticky := renderFileHeader(m.detail.Files[m.currentFile], m.width)
	return lipgloss.JoinVertical(lipgloss.Left, sticky, m.leftVP.View())
}
