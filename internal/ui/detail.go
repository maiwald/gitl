package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

// fileListWidthFraction is the share of the detail view's width given to
// the right-hand file list; the remainder goes to the diff panel.
const fileListWidthFraction = 0.25

// minFileListWidth keeps the file list panel usable on narrow terminals.
const minFileListWidth = 20

// minDiffWidth keeps the diff panel usable on narrow terminals, taking
// priority over the file list's target share of the width when the two
// minimums can't both be satisfied.
const minDiffWidth = 10

// paneFocus identifies which of the two detail-view panes receives
// navigation keys.
type paneFocus int

const (
	focusDiff paneFocus = iota
	focusFiles
)

// DetailModel renders a commit's metadata, message and diff as a single
// scrollable left panel (so a long commit message doesn't permanently eat
// into the screen), with the list of changed files on the right. The file
// list entry for whichever file is currently in view in the diff panel is
// highlighted, and the sync works both ways: navigating the file list and
// pressing enter jumps the diff to that file. While the scroll position is
// still within the header/message (above the first file), the first file is
// treated as "current" since that's where scrolling down will land next.
type DetailModel struct {
	detail *git.CommitDetail

	fileRanges  []FileRange
	currentFile int
	focus       paneFocus

	leftVP viewport.Model
	files  FileListModel

	width, height int

	searchTerm string
	matches    []DiffMatch
	matchLines []int // parallel to matches, offset by header/message length
	matchPos   int
}

// NewDetailModel constructs a detail view for the given commit.
func NewDetailModel(detail *git.CommitDetail) DetailModel {
	return DetailModel{
		detail:   detail,
		leftVP:   viewport.New(0, 0),
		files:    NewFileListModel(detail.Files),
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

	fileListWidth := int(float64(m.width) * fileListWidthFraction)
	if fileListWidth < minFileListWidth {
		fileListWidth = minFileListWidth
	}
	maxFileListWidth := m.width - minDiffWidth
	if maxFileListWidth < 0 {
		maxFileListWidth = 0
	}
	if fileListWidth > maxFileListWidth {
		fileListWidth = maxFileListWidth
	}
	diffWidth := m.width - fileListWidth
	if diffWidth < 0 {
		diffWidth = 0
	}

	var diffLines []string
	var fileRanges []FileRange
	var matchLines []int
	if len(m.detail.Files) == 0 {
		diffLines = []string{noDiffNotice(m.detail)}
	} else {
		diffLines, fileRanges, matchLines = RenderDiff(m.detail.Files, diffWidth, m.matches, m.matchPos)
	}

	// The header/message is wrapped the same way diff lines are (each
	// visual row its own entry) so the line offset applied to fileRanges
	// below stays exact regardless of terminal width — see the
	// FileRange/FileAt convention in diffview.go.
	headerLines := wrapPlain(renderCommitHeader(m.detail), diffWidth)
	offset := len(headerLines) + 1

	content := make([]string, 0, len(headerLines)+1+len(diffLines))
	content = append(content, headerLines...)
	content = append(content, "")
	content = append(content, diffLines...)

	m.fileRanges = make([]FileRange, len(fileRanges))
	for i, r := range fileRanges {
		m.fileRanges[i] = FileRange{FileIndex: r.FileIndex, StartLine: r.StartLine + offset, EndLine: r.EndLine + offset}
	}

	m.matchLines = make([]int, len(matchLines))
	for i, ln := range matchLines {
		m.matchLines[i] = ln + offset
	}

	m.leftVP.Width = diffWidth
	m.leftVP.Height = m.height
	m.leftVP.SetContent(strings.Join(content, "\n"))

	m.files.SetSize(fileListWidth, m.height)
	m.syncHighlightFromScroll()
}

// wrapPlain word-wraps text to width, flattening every visual row into its
// own slice entry (see the comment in reflow for why that matters).
func wrapPlain(text string, width int) []string {
	if width <= 0 {
		return strings.Split(text, "\n")
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, strings.Split(lipgloss.NewStyle().Width(width).Render(line), "\n")...)
	}
	return out
}

// Update handles a navigation key press: Tab switches which pane receives
// further keys, and the rest are routed to whichever pane is focused.
func (m *DetailModel) Update(msg tea.KeyMsg) {
	if key.Matches(msg, DefaultKeyMap.Tab) {
		m.toggleFocus()
		return
	}

	if m.focus == focusFiles {
		m.updateFiles(msg)
	} else {
		m.updateDiff(msg)
	}
}

func (m *DetailModel) toggleFocus() {
	if len(m.fileRanges) == 0 {
		return
	}
	if m.focus == focusDiff {
		m.focus = focusFiles
	} else {
		m.focus = focusDiff
	}
}

func (m *DetailModel) updateDiff(msg tea.KeyMsg) {
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
	}
	m.syncHighlightFromScroll()
}

// syncHighlightFromScroll recomputes which file the diff panel's topmost
// visible line belongs to, and highlights that file in the list. Above the
// first file (still within the header/commit message) FileAt has nothing to
// report, so it clamps to the first file, since that's what scrolling down
// reaches next.
func (m *DetailModel) syncHighlightFromScroll() {
	if len(m.fileRanges) == 0 {
		return
	}
	i := FileAt(m.fileRanges, m.leftVP.YOffset)
	if i < 0 {
		i = 0
	}
	m.currentFile = i
	m.files.SetHighlight(i)
}

func (m *DetailModel) updateFiles(msg tea.KeyMsg) {
	switch {
	case key.Matches(msg, DefaultKeyMap.Up):
		m.moveFileHighlight(-1)
	case key.Matches(msg, DefaultKeyMap.Down):
		m.moveFileHighlight(1)
	case key.Matches(msg, DefaultKeyMap.Enter):
		m.jumpToCurrentFile()
	}
}

func (m *DetailModel) moveFileHighlight(delta int) {
	if len(m.fileRanges) == 0 {
		return
	}
	next := m.currentFile + delta
	if next < 0 {
		next = 0
	}
	if next > len(m.fileRanges)-1 {
		next = len(m.fileRanges) - 1
	}
	m.currentFile = next
	m.files.SetHighlight(next)
}

func (m *DetailModel) jumpToCurrentFile() {
	if m.currentFile < 0 || m.currentFile >= len(m.fileRanges) {
		return
	}
	m.leftVP.SetYOffset(m.fileRanges[m.currentFile].StartLine)
}

// Search finds every occurrence of term in the diff content and jumps to
// the first one, returning the total match count. Unlike list search, this
// is fully synchronous: the whole diff is already in memory once a commit
// is open, so there's no async loading to wait for.
func (m *DetailModel) Search(term string) int {
	m.searchTerm = term
	if term == "" {
		m.matches = nil
	} else {
		m.matches = FindDiffMatches(m.detail.Files, term)
	}
	m.matchPos = -1
	if len(m.matches) > 0 {
		m.matchPos = 0
	}
	m.jumpToMatch()
	return len(m.matches)
}

// NextMatch and PrevMatch cycle to the following/preceding match, wrapping
// at both ends, and report the new 1-based position and total count.
func (m *DetailModel) NextMatch() (pos, total int) { return m.stepMatch(1) }
func (m *DetailModel) PrevMatch() (pos, total int) { return m.stepMatch(-1) }

func (m *DetailModel) stepMatch(delta int) (pos, total int) {
	if len(m.matches) == 0 {
		return 0, 0
	}
	m.matchPos = (m.matchPos + delta + len(m.matches)) % len(m.matches)
	m.jumpToMatch()
	return m.matchPos + 1, len(m.matches)
}

// jumpToMatch re-renders (so the newly active match gets its distinct
// style) and scrolls the diff panel to bring it into view. Re-running
// reflow on every match change — rather than patching just the affected
// lines — is the same cost already paid on every resize, and it's pure
// in-memory string work with no I/O.
func (m *DetailModel) jumpToMatch() {
	m.reflow()
	if m.matchPos < 0 || m.matchPos >= len(m.matchLines) {
		return
	}
	m.leftVP.SetYOffset(m.matchLines[m.matchPos])
	m.syncHighlightFromScroll()
}

// ClearSearch discards the active search and its highlighting.
func (m *DetailModel) ClearSearch() {
	m.searchTerm = ""
	m.matches = nil
	m.matchPos = -1
	m.reflow()
}

// SearchStatus reports the active search term and match position (1-based)
// for status-bar display, and whether a search is active at all.
func (m DetailModel) SearchStatus() (term string, pos, total int, active bool) {
	if m.searchTerm == "" {
		return "", 0, 0, false
	}
	return m.searchTerm, m.matchPos + 1, len(m.matches), true
}

func noDiffNotice(d *git.CommitDetail) string {
	if len(d.Parents) > 1 {
		return binaryNoticeStyle.Render("Merge commit: diff not shown (viewing merge diffs isn't supported yet).")
	}
	return binaryNoticeStyle.Render("No changes in this commit.")
}

func renderCommitHeader(d *git.CommitDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "commit %s\n", d.Hash)
	if len(d.Parents) > 1 {
		fmt.Fprintf(&b, "Merge:  %s\n", strings.Join(d.Parents, " "))
	}
	fmt.Fprintf(&b, "Author: %s <%s>\n", d.AuthorName, d.AuthorEmail)
	fmt.Fprintf(&b, "Date:   %s\n\n", d.AuthorDate.Format("2006-01-02 15:04:05 -0700"))
	b.WriteString(strings.TrimRight(d.Body, "\n"))
	return b.String()
}

func (m DetailModel) View() string {
	if m.detail == nil {
		return ""
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, m.leftVP.View(), m.files.View())
}
