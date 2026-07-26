package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// loadMoreLookahead is how close to the end of loaded commits the cursor
// must get before a lazy-load fetch is triggered.
const loadMoreLookahead = 20

// fetchRequest describes a batch of commits ListModel needs from the repo
// to satisfy a navigation that has run past the currently loaded data.
type fetchRequest struct {
	skip, limit int
}

// ListModel renders a scrollable list of commits with a movable cursor,
// lazily requesting more commits as the cursor approaches the loaded end.
type ListModel struct {
	commits []git.Commit
	cursor  int
	vp      viewport.Model

	totalCount       int
	loadingMore      bool
	jumpToEndPending bool
}

// NewListModel constructs a list model over the given commits.
func NewListModel(commits []git.Commit) ListModel {
	return ListModel{
		commits:    commits,
		vp:         viewport.New(0, 0),
		totalCount: len(commits),
	}
}

// SetSize resizes the underlying viewport and re-renders content to match.
func (m *ListModel) SetSize(width, height int) {
	m.vp.Width = width
	m.vp.Height = height
	m.render()
}

// SetTotalCount records the total number of commits reachable from HEAD, so
// the list knows when it has reached the true end of history.
func (m *ListModel) SetTotalCount(n int) {
	m.totalCount = n
}

// AppendCommits adds a freshly fetched batch to the loaded set, completing
// any pending jump-to-bottom once the tail is in view.
func (m *ListModel) AppendCommits(commits []git.Commit) {
	m.commits = append(m.commits, commits...)
	m.loadingMore = false
	if m.jumpToEndPending && len(m.commits) >= m.totalCount {
		m.jumpToEndPending = false
		m.cursor = len(m.commits) - 1
		m.ensureVisible()
	}
	m.render()
}

// Selected returns the commit currently under the cursor, if any.
func (m ListModel) Selected() (git.Commit, bool) {
	if m.cursor < 0 || m.cursor >= len(m.commits) {
		return git.Commit{}, false
	}
	return m.commits[m.cursor], true
}

// Update processes a navigation key press. It returns a fetchRequest and
// true when more commits must be loaded from the repo to satisfy the
// requested movement (e.g. scrolling near the loaded end, or jumping to the
// bottom of a not-fully-loaded history).
func (m *ListModel) Update(msg tea.KeyMsg) (fetchRequest, bool) {
	switch {
	case key.Matches(msg, DefaultKeyMap.Up):
		m.moveCursor(-1)
	case key.Matches(msg, DefaultKeyMap.Down):
		m.moveCursor(1)
	case key.Matches(msg, DefaultKeyMap.HalfPageUp):
		m.moveCursor(-m.halfPageSize())
	case key.Matches(msg, DefaultKeyMap.HalfPageDown):
		m.moveCursor(m.halfPageSize())
	case key.Matches(msg, DefaultKeyMap.Top):
		m.cursor = 0
		m.ensureVisible()
		m.render()
	case key.Matches(msg, DefaultKeyMap.Bottom):
		return m.jumpToBottom()
	}
	return m.maybeLoadMore()
}

func (m *ListModel) halfPageSize() int {
	h := m.vp.Height / 2
	if h < 1 {
		h = 1
	}
	return h
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

// jumpToBottom moves the cursor to the last loaded commit, or, if history
// isn't fully loaded yet, requests the remaining commits and defers the
// jump until they arrive.
func (m *ListModel) jumpToBottom() (fetchRequest, bool) {
	if len(m.commits) >= m.totalCount {
		m.cursor = len(m.commits) - 1
		m.ensureVisible()
		m.render()
		return fetchRequest{}, false
	}
	m.jumpToEndPending = true
	if m.loadingMore {
		return fetchRequest{}, false
	}
	m.loadingMore = true
	return fetchRequest{skip: len(m.commits), limit: m.totalCount - len(m.commits)}, true
}

// maybeLoadMore requests the next batch when the cursor has scrolled near
// the end of what's currently loaded and more commits remain.
func (m *ListModel) maybeLoadMore() (fetchRequest, bool) {
	if m.loadingMore || len(m.commits) >= m.totalCount {
		return fetchRequest{}, false
	}
	if m.cursor < len(m.commits)-loadMoreLookahead {
		return fetchRequest{}, false
	}
	m.loadingMore = true
	return fetchRequest{skip: len(m.commits), limit: batchSize}, true
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
