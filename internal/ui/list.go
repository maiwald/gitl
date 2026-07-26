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

	searchTerm       string
	matches          []int // absolute commit positions, ascending
	matchPos         int   // index into matches; -1 when there are none
	matchJumpPending bool
}

// NewListModel constructs a list model over the given commits.
func NewListModel(commits []git.Commit) ListModel {
	return ListModel{
		commits:    commits,
		vp:         viewport.New(0, 0),
		totalCount: len(commits),
		matchPos:   -1,
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
// any pending jump-to-bottom or jump-to-search-match once the target
// position is in view.
func (m *ListModel) AppendCommits(commits []git.Commit) {
	m.commits = append(m.commits, commits...)
	m.loadingMore = false
	if m.jumpToEndPending && len(m.commits) >= m.totalCount {
		m.jumpToEndPending = false
		m.cursor = len(m.commits) - 1
		m.ensureVisible()
	}
	if m.matchJumpPending && m.matchPos >= 0 && m.matches[m.matchPos] < len(m.commits) {
		m.matchJumpPending = false
		m.cursor = m.matches[m.matchPos]
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

// LoadedCount reports how many commits are currently loaded.
func (m ListModel) LoadedCount() int { return len(m.commits) }

// TotalCount reports the total number of commits reachable from HEAD.
func (m ListModel) TotalCount() int { return m.totalCount }

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
	return m.beginLoadingUpTo(m.totalCount - 1)
}

// beginLoadingUpTo requests whatever commits are needed so that index
// target (plus a little lookahead) is loaded, unless a fetch is already in
// flight or it's already loaded. This is the same "fetch the rest in one
// shot" mechanism jumpToBottom uses (target = totalCount-1 there), just
// parameterized by an arbitrary position — used by search to load only as
// far as the relevant match rather than the entire remaining history.
func (m *ListModel) beginLoadingUpTo(target int) (fetchRequest, bool) {
	if m.loadingMore || len(m.commits) >= m.totalCount {
		return fetchRequest{}, false
	}
	need := target + 1 + loadMoreLookahead
	if need > m.totalCount {
		need = m.totalCount
	}
	if need <= len(m.commits) {
		return fetchRequest{}, false
	}
	m.loadingMore = true
	return fetchRequest{skip: len(m.commits), limit: need - len(m.commits)}, true
}

// RequestLoadUpTo dispatches (or reuses an already-in-flight) fetch to
// bring position target into the loaded range. Exported so app.go can
// trigger it directly for search-driven jumps.
func (m *ListModel) RequestLoadUpTo(target int) (fetchRequest, bool) {
	return m.beginLoadingUpTo(target)
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

// Search records matches (absolute commit positions, ascending, as computed
// by the caller from a git-provided hash set) and jumps to the first match
// at or after the cursor, wrapping to the first match if none qualify. It
// reports needsLoad=true when that position isn't loaded yet, in which
// case the caller should dispatch a fetch via RequestLoadUpTo(target); the
// jump completes automatically once AppendCommits delivers it.
func (m *ListModel) Search(term string, matches []int) (needsLoad bool, target int) {
	m.searchTerm = term
	m.matches = matches
	m.matchPos = -1
	m.matchJumpPending = false
	if len(matches) == 0 {
		m.render()
		return false, 0
	}
	m.matchPos = 0 // wrap to the first match if none qualify at/after the cursor
	for i, idx := range matches {
		if idx >= m.cursor {
			m.matchPos = i
			break
		}
	}
	return m.applyCurrentMatch()
}

// NextMatch and PrevMatch cycle to the following/preceding search match,
// wrapping at both ends. See Search for the needsLoad/target contract.
func (m *ListModel) NextMatch() (needsLoad bool, target int) { return m.stepMatch(1) }
func (m *ListModel) PrevMatch() (needsLoad bool, target int) { return m.stepMatch(-1) }

func (m *ListModel) stepMatch(delta int) (needsLoad bool, target int) {
	if len(m.matches) == 0 {
		return false, 0
	}
	m.matchPos = (m.matchPos + delta + len(m.matches)) % len(m.matches)
	return m.applyCurrentMatch()
}

// applyCurrentMatch moves the cursor to the current match if it's already
// loaded, or marks a jump as pending (resolved by AppendCommits) and
// reports needsLoad so the caller dispatches a targeted fetch.
func (m *ListModel) applyCurrentMatch() (needsLoad bool, target int) {
	target = m.matches[m.matchPos]
	if target >= len(m.commits) {
		m.matchJumpPending = true
		return true, target
	}
	m.cursor = target
	m.ensureVisible()
	m.render()
	return false, target
}

// ClearSearch discards the active search and its highlighting.
func (m *ListModel) ClearSearch() {
	m.searchTerm = ""
	m.matches = nil
	m.matchPos = -1
	m.matchJumpPending = false
	m.render()
}

// SearchStatus reports the active search term and match position (1-based)
// for status-bar display, and whether a search is active at all.
func (m ListModel) SearchStatus() (term string, pos, total int, active bool) {
	if m.searchTerm == "" {
		return "", 0, 0, false
	}
	return m.searchTerm, m.matchPos + 1, len(m.matches), true
}

func (m *ListModel) render() {
	activeMatch := -1
	if m.matchPos >= 0 && m.matchPos < len(m.matches) {
		activeMatch = m.matches[m.matchPos]
	}
	lines := make([]string, len(m.commits))
	for i, c := range m.commits {
		lines[i] = renderCommitRow(c, m.vp.Width, i == m.cursor, m.searchTerm, i == activeMatch)
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
}

func (m ListModel) View() string {
	return m.vp.View()
}
