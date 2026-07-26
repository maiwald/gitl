package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// ListModel renders a scrollable list of commits with a movable cursor.
type ListModel struct {
	commits []git.Commit
	cursor  int
	vp      viewport.Model
}

// NewListModel constructs a list model over the given commits.
func NewListModel(commits []git.Commit) ListModel {
	return ListModel{
		commits: commits,
		vp:      viewport.New(0, 0),
	}
}

// SetSize resizes the underlying viewport and re-renders content to match.
func (m *ListModel) SetSize(width, height int) {
	m.vp.Width = width
	m.vp.Height = height
	m.render()
}

// Selected returns the commit currently under the cursor, if any.
func (m ListModel) Selected() (git.Commit, bool) {
	if m.cursor < 0 || m.cursor >= len(m.commits) {
		return git.Commit{}, false
	}
	return m.commits[m.cursor], true
}

func (m *ListModel) Update(msg tea.KeyMsg) {
	switch msg.String() {
	case "up":
		m.moveCursor(-1)
	case "down":
		m.moveCursor(1)
	}
}

func (m *ListModel) moveCursor(delta int) {
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next > len(m.commits)-1 {
		next = len(m.commits) - 1
	}
	m.cursor = next
	m.ensureVisible()
	m.render()
}

// ensureVisible scrolls the viewport so the cursor row stays on screen.
func (m *ListModel) ensureVisible() {
	if m.vp.Height <= 0 {
		return
	}
	if m.cursor < m.vp.YOffset {
		m.vp.YOffset = m.cursor
	} else if m.cursor >= m.vp.YOffset+m.vp.Height {
		m.vp.YOffset = m.cursor - m.vp.Height + 1
	}
	if m.vp.YOffset < 0 {
		m.vp.YOffset = 0
	}
}

func (m *ListModel) render() {
	lines := make([]string, len(m.commits))
	for i, c := range m.commits {
		lines[i] = renderCommitRow(c, m.vp.Width, i == m.cursor)
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
}

func (m ListModel) View() string {
	return m.vp.View()
}
