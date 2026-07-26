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

// paneFocus identifies which of the two detail-view panes receives
// navigation keys.
type paneFocus int

const (
	focusDiff paneFocus = iota
	focusFiles
)

// DetailModel renders full commit metadata plus a two-pane view: a large
// scrollable, highlighted diff on the left and the list of changed files on
// the right. The file list entry for whichever file is currently in view in
// the diff panel is highlighted, and the sync works both ways: navigating
// the file list and pressing enter jumps the diff to that file.
type DetailModel struct {
	detail *git.CommitDetail
	header string

	fileRanges  []FileRange
	currentFile int
	focus       paneFocus

	leftVP viewport.Model
	files  FileListModel

	width, height int
}

// NewDetailModel constructs a detail view for the given commit.
func NewDetailModel(detail *git.CommitDetail) DetailModel {
	return DetailModel{
		detail: detail,
		header: renderCommitHeader(detail),
		leftVP: viewport.New(0, 0),
		files:  NewFileListModel(detail.Files),
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

	headerHeight := lipgloss.Height(m.header)
	bodyHeight := m.height - headerHeight
	if bodyHeight < 0 {
		bodyHeight = 0
	}

	fileListWidth := int(float64(m.width) * fileListWidthFraction)
	if fileListWidth < minFileListWidth {
		fileListWidth = minFileListWidth
	}
	if fileListWidth > m.width {
		fileListWidth = m.width
	}
	diffWidth := m.width - fileListWidth

	var diffLines []string
	diffLines, m.fileRanges = RenderDiff(m.detail.Files, diffWidth)
	m.leftVP.Width = diffWidth
	m.leftVP.Height = bodyHeight
	m.leftVP.SetContent(strings.Join(diffLines, "\n"))

	m.files.SetSize(fileListWidth, bodyHeight)
	m.syncHighlightFromScroll()
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
// visible line belongs to, and highlights that file in the list.
func (m *DetailModel) syncHighlightFromScroll() {
	if len(m.fileRanges) == 0 {
		return
	}
	if i := FileAt(m.fileRanges, m.leftVP.YOffset); i >= 0 {
		m.currentFile = i
		m.files.SetHighlight(i)
	}
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
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.leftVP.View(), m.files.View())
	return lipgloss.JoinVertical(lipgloss.Left, m.header, body)
}
