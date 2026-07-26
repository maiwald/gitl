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

// DetailModel renders full commit metadata plus a two-pane view: a large
// scrollable, highlighted diff on the left and the list of changed files on
// the right.
type DetailModel struct {
	detail *git.CommitDetail
	header string

	fileRanges []FileRange
	leftVP     viewport.Model
	files      FileListModel

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
}

// Update scrolls the diff panel in response to a navigation key press.
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
	}
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
